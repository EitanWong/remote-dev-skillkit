package gitworkflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type PRPlan struct {
	Schema string   `json:"schema"`
	Base   string   `json:"base"`
	Head   string   `json:"head"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Args   []string `json:"args"`
}

// GitHubExecutor runs an argv-form GitHub CLI command in a repository.
// Implementations must execute argv[0] directly; the git-only ExecRunner is
// not compatible because it prefixes commands with "git -C".
type GitHubExecutor interface {
	Run(ctx context.Context, dir string, args ...string) (CommandEvidence, error)
}

// GitHubRunner executes gh directly and never prefixes it with git -C.
type GitHubRunner struct{}

func (GitHubRunner) Run(ctx context.Context, dir string, args ...string) (CommandEvidence, error) {
	if len(args) == 0 {
		return CommandEvidence{}, fmt.Errorf("command argv is required")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	evidence := CommandEvidence{
		Argv:     append([]string(nil), args...),
		Dir:      dir,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: commandExitCode(runErr),
	}
	if runErr != nil {
		return evidence, runErr
	}
	return evidence, nil
}

func PlanPR(repo Repo, branch BranchRef, title, body string) (PRPlan, error) {
	if strings.TrimSpace(repo.Root) == "" {
		return PRPlan{}, fmt.Errorf("repository root is required")
	}
	parsedBranch, err := ParseBranch(branch.Name)
	if err != nil {
		return PRPlan{}, err
	}
	if branch.Type != parsedBranch.Type || branch.Issue != parsedBranch.Issue || branch.Slug != parsedBranch.Slug {
		return PRPlan{}, fmt.Errorf("branch metadata does not match parsed branch %q", branch.Name)
	}
	if err := ValidatePRTitle(title); err != nil {
		return PRPlan{}, err
	}

	normalizedBody, err := normalizePRBody(parsedBranch.Issue, body)
	if err != nil {
		return PRPlan{}, err
	}

	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   defaultBaseBranch,
		Head:   parsedBranch.Name,
		Title:  title,
		Body:   normalizedBody,
	}
	plan.Args = buildPRArgs(plan)
	return plan, nil
}

func ExecutePR(ctx context.Context, r GitHubExecutor, repoRoot string, plan PRPlan) (CommandEvidence, error) {
	if r == nil {
		return CommandEvidence{}, fmt.Errorf("github runner is required")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return CommandEvidence{}, fmt.Errorf("repository root is required")
	}
	normalizedPlan, err := normalizePRPlan(plan)
	if err != nil {
		return CommandEvidence{}, err
	}

	evidence, err := r.Run(ctx, repoRoot, normalizedPlan.Args...)
	evidence = redactEvidence(evidence)
	if err == nil {
		return evidence, nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return evidence, fmt.Errorf("gh is not installed or not available on PATH")
	}

	message := redactText(err.Error())
	if isGitHubAuthFailure(message) || isGitHubAuthFailure(evidence.Stderr) || isGitHubAuthFailure(evidence.Stdout) {
		return evidence, fmt.Errorf("gh is not authenticated; run `gh auth login` and retry")
	}
	if strings.TrimSpace(message) == "" {
		message = "gh pr create failed"
	}
	return evidence, fmt.Errorf("gh pr create failed: %s", message)
}

func normalizePRPlan(plan PRPlan) (PRPlan, error) {
	if plan.Schema != SchemaVersion {
		return PRPlan{}, fmt.Errorf("unsupported PR plan schema %q", plan.Schema)
	}
	if err := validateBaseRef(plan.Base); err != nil {
		return PRPlan{}, err
	}
	parsedBranch, err := ParseBranch(plan.Head)
	if err != nil {
		return PRPlan{}, err
	}
	if err := ValidatePRTitle(plan.Title); err != nil {
		return PRPlan{}, err
	}
	normalizedBody, err := normalizePRBody(parsedBranch.Issue, plan.Body)
	if err != nil {
		return PRPlan{}, err
	}
	plan.Head = parsedBranch.Name
	plan.Body = normalizedBody
	plan.Args = buildPRArgs(plan)
	return plan, nil
}

func buildPRArgs(plan PRPlan) []string {
	return []string{
		"gh", "pr", "create",
		"--base", plan.Base,
		"--head", plan.Head,
		"--title", plan.Title,
		"--body", plan.Body,
	}
}

func normalizePRBody(issue int64, body string) (string, error) {
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("pull request body is required")
	}
	if body != strings.TrimSpace(body) {
		return "", fmt.Errorf("pull request body must not include surrounding whitespace")
	}

	required := "Closes #" + strconv.FormatInt(issue, 10)
	matches := issueReferencePattern.FindAllStringSubmatch(body, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		if !strings.EqualFold(match[1], "closes") {
			continue
		}
		if match[2] != strconv.FormatInt(issue, 10) {
			return "", fmt.Errorf("pull request body must include %s", required)
		}
	}
	if strings.Contains(body, required) {
		return body, nil
	}
	return body + "\n\n" + required, nil
}

var issueReferencePattern = regexp.MustCompile(`(?im)\b(close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved)\s+#([0-9]+)\b`)

var tokenPattern = regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9_]+|bearer\s+[A-Za-z0-9._~+/=-]+|(?:token|password|secret|authorization)(?:\s*[:=]\s*|\s+)(?:bearer\s+)?[^\s,;]+)`)

func redactEvidence(evidence CommandEvidence) CommandEvidence {
	evidence.Argv = append([]string(nil), evidence.Argv...)
	for index, arg := range evidence.Argv {
		evidence.Argv[index] = redactText(arg)
	}
	evidence.Stdout = redactText(evidence.Stdout)
	evidence.Stderr = redactText(evidence.Stderr)
	return evidence
}

func redactText(value string) string {
	return tokenPattern.ReplaceAllStringFunc(value, func(match string) string {
		lower := strings.ToLower(match)
		switch {
		case strings.HasPrefix(lower, "github_pat_"), strings.HasPrefix(lower, "ghp_"), strings.HasPrefix(lower, "gho_"), strings.HasPrefix(lower, "ghu_"), strings.HasPrefix(lower, "ghs_"), strings.HasPrefix(lower, "ghr_"):
			return "[REDACTED]"
		default:
			parts := strings.SplitN(match, " ", 2)
			if len(parts) == 2 && strings.ContainsAny(parts[0], "=:") {
				return parts[0][:len(parts[0])-1] + "=[REDACTED]"
			}
			if idx := strings.IndexAny(match, "=:"); idx >= 0 {
				return match[:idx+1] + "[REDACTED]"
			}
			return "[REDACTED]"
		}
	})
}

func isGitHubAuthFailure(message string) bool {
	normalized := strings.ToLower(redactText(message))
	return strings.Contains(normalized, "auth login") ||
		strings.Contains(normalized, "not logged into") ||
		strings.Contains(normalized, "authentication failed") ||
		strings.Contains(normalized, "authentication required") ||
		strings.Contains(normalized, "bad credentials") ||
		strings.Contains(normalized, "unauthorized") ||
		strings.Contains(normalized, "http 401") ||
		strings.Contains(normalized, "401 unauthorized") ||
		strings.Contains(normalized, "could not resolve to a repository")
}
