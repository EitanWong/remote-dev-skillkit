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
	if branch.Name == "" || branch.Issue <= 0 {
		return PRPlan{}, fmt.Errorf("branch reference must include a positive issue number")
	}
	if err := ValidatePRTitle(title); err != nil {
		return PRPlan{}, err
	}

	normalizedBody, err := normalizePRBody(branch.Issue, body)
	if err != nil {
		return PRPlan{}, err
	}

	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   defaultBaseBranch,
		Head:   branch.Name,
		Title:  title,
		Body:   normalizedBody,
	}
	plan.Args = []string{
		"gh", "pr", "create",
		"--base", plan.Base,
		"--head", plan.Head,
		"--title", plan.Title,
		"--body", plan.Body,
	}
	return plan, nil
}

func ExecutePR(ctx context.Context, r Runner, repoRoot string, plan PRPlan) (CommandEvidence, error) {
	if r == nil {
		return CommandEvidence{}, fmt.Errorf("github runner is required")
	}
	if strings.TrimSpace(repoRoot) == "" {
		return CommandEvidence{}, fmt.Errorf("repository root is required")
	}
	if err := validatePRPlan(plan); err != nil {
		return CommandEvidence{}, err
	}

	evidence, err := r.Run(ctx, repoRoot, plan.Args...)
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

func validatePRPlan(plan PRPlan) error {
	if plan.Schema != SchemaVersion {
		return fmt.Errorf("unsupported PR plan schema %q", plan.Schema)
	}
	if err := validateBaseRef(plan.Base); err != nil {
		return err
	}
	if _, err := ParseBranch(plan.Head); err != nil {
		return err
	}
	if err := ValidatePRTitle(plan.Title); err != nil {
		return err
	}
	issue, err := IssueFromBranch(plan.Head)
	if err != nil {
		return err
	}
	if _, err := normalizePRBody(issue, plan.Body); err != nil {
		return err
	}
	if len(plan.Args) == 0 {
		return fmt.Errorf("PR plan arguments are required")
	}
	expected := []string{
		"gh", "pr", "create",
		"--base", plan.Base,
		"--head", plan.Head,
		"--title", plan.Title,
		"--body", plan.Body,
	}
	if len(plan.Args) != len(expected) {
		return fmt.Errorf("PR plan arguments do not match the approved gh invocation")
	}
	for index := range expected {
		if plan.Args[index] != expected[index] {
			return fmt.Errorf("PR plan arguments do not match the approved gh invocation")
		}
	}
	return nil
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

var tokenPattern = regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9_]+|(?:token|password|secret|authorization)[=: ]+[^\s]+)`)

func redactEvidence(evidence CommandEvidence) CommandEvidence {
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
		strings.Contains(normalized, "could not resolve to a repository")
}
