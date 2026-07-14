package gitworkflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const SchemaVersion = "rdev.git-workflow.v1"

var allowedWorkflowTypes = map[string]struct{}{
	"feat":     {},
	"fix":      {},
	"refactor": {},
	"docs":     {},
	"test":     {},
	"chore":    {},
	"perf":     {},
	"ci":       {},
	"hotfix":   {},
	"release":  {},
}

var branchPattern = regexp.MustCompile(`^(feat|fix|refactor|docs|test|chore|perf|ci|hotfix|release)/([0-9]+)-([a-z0-9]+(?:-[a-z0-9]+)*)$`)

var commitPattern = regexp.MustCompile(`^([a-z]+)(?:\(([A-Za-z0-9._/-]+)\))?: (.+)$`)

var imperativeSummaryPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var imperativeSummaryVerbs = map[string]struct{}{
	"add":      {},
	"apply":    {},
	"audit":    {},
	"build":    {},
	"check":    {},
	"clean":    {},
	"close":    {},
	"confirm":  {},
	"cover":    {},
	"create":   {},
	"delete":   {},
	"deploy":   {},
	"detect":   {},
	"document": {},
	"enforce":  {},
	"fix":      {},
	"harden":   {},
	"improve":  {},
	"install":  {},
	"keep":     {},
	"lock":     {},
	"merge":    {},
	"open":     {},
	"prune":    {},
	"publish":  {},
	"reject":   {},
	"refresh":  {},
	"remove":   {},
	"replace":  {},
	"report":   {},
	"resolve":  {},
	"run":      {},
	"split":    {},
	"speed":    {},
	"test":     {},
	"unblock":  {},
	"update":   {},
	"validate": {},
	"verify":   {},
}

// BranchRef is the parsed representation of a policy branch name.
type BranchRef struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Issue int64  `json:"issue"`
	Slug  string `json:"slug"`
}

// PolicyCheck records one deterministic policy assertion.
type PolicyCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// CommandRecord captures command evidence for the git-workflow envelope.
type CommandRecord struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd,omitempty"`
}

// PolicyReport matches the approved rdev.git-workflow.v1 JSON envelope.
type PolicyReport struct {
	Schema   string            `json:"schema"`
	OK       bool              `json:"ok"`
	RepoRoot string            `json:"repo_root"`
	Branch   string            `json:"branch"`
	Issue    int64             `json:"issue"`
	Base     string            `json:"base"`
	Worktree string            `json:"worktree"`
	Checks   []PolicyCheck     `json:"checks"`
	Commands []CommandEvidence `json:"commands"`
}

var baseRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func ParseBranch(name string) (BranchRef, error) {
	if name == "" {
		return BranchRef{}, fmt.Errorf("branch name is required")
	}
	if name != strings.TrimSpace(name) {
		return BranchRef{}, fmt.Errorf("branch %q must not include surrounding whitespace", name)
	}

	matches := branchPattern.FindStringSubmatch(name)
	if matches == nil {
		return BranchRef{}, fmt.Errorf("branch %q must match <type>/<issue>-<slug> with an allowed type", name)
	}

	if _, ok := allowedWorkflowTypes[matches[1]]; !ok {
		return BranchRef{}, fmt.Errorf("branch %q uses an unknown workflow type", name)
	}

	issue, err := strconv.ParseInt(matches[2], 10, 64)
	if err != nil {
		return BranchRef{}, fmt.Errorf("branch %q has invalid issue number: %w", name, err)
	}
	if issue <= 0 {
		return BranchRef{}, fmt.Errorf("branch %q must reference a positive issue number", name)
	}

	return BranchRef{
		Name:  name,
		Type:  matches[1],
		Issue: issue,
		Slug:  matches[3],
	}, nil
}

func ValidateCommitSubject(subject string) error {
	return validateTitle(subject, "commit subject")
}

func ValidatePRTitle(title string) error {
	return validateTitle(title, "pull request title")
}

func IssueFromBranch(name string) (int64, error) {
	ref, err := ParseBranch(name)
	if err != nil {
		return 0, err
	}
	return ref.Issue, nil
}

func validateTitle(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not include surrounding whitespace", label)
	}

	matches := commitPattern.FindStringSubmatch(value)
	if matches == nil {
		return fmt.Errorf("%s must use conventional format <type>(<scope>): <imperative summary>", label)
	}

	typeName := matches[1]
	if _, ok := allowedWorkflowTypes[typeName]; !ok {
		return fmt.Errorf("%s type %q is not allowed", label, typeName)
	}

	if err := validateImperativeSummary(matches[3]); err != nil {
		return fmt.Errorf("%s summary must be imperative: %w", label, err)
	}

	return nil
}

// validateImperativeSummary uses a deterministic project rule: the summary must
// begin with a lowercase ASCII verb-like token and must not look past tense or
// third-person singular. This intentionally rejects summaries such as
// "added worktree doctor" and "rejects main edits" while allowing summaries
// such as "add worktree doctor" and "reject main edits".
func validateImperativeSummary(summary string) error {
	if summary == "" {
		return fmt.Errorf("summary is required")
	}
	if summary != strings.TrimSpace(summary) {
		return fmt.Errorf("summary must not include surrounding whitespace")
	}

	parts := strings.Fields(summary)
	if len(parts) == 0 {
		return fmt.Errorf("summary is required")
	}

	first := parts[0]
	if !imperativeSummaryPattern.MatchString(first) {
		return fmt.Errorf("summary must start with a lowercase ASCII word")
	}
	if _, ok := imperativeSummaryVerbs[first]; !ok {
		return fmt.Errorf("summary must begin with a project-approved imperative verb")
	}
	return nil
}

func CheckPolicy(ctx context.Context, repo Repo, r Runner, base string) (PolicyReport, error) {
	report := PolicyReport{
		Schema:   SchemaVersion,
		RepoRoot: repo.Root,
		Base:     base,
		Worktree: repo.Root,
		Checks:   []PolicyCheck{},
		Commands: []CommandEvidence{},
	}

	if r == nil {
		return failPolicy(report, PolicyCheck{Name: "runner_available", Passed: false, Detail: "git runner is required"})
	}
	if strings.TrimSpace(repo.Root) == "" {
		return failPolicy(report, PolicyCheck{Name: "repo_root", Passed: false, Detail: "repository root is required"})
	}
	if err := validateBaseRef(base); err != nil {
		return failPolicy(report, PolicyCheck{Name: "base_reference", Passed: false, Detail: err.Error()})
	}
	report.Base = base

	repoRoot, evidence, err := resolveCommonRepoRoot(ctx, repo.Root, r)
	report.Commands = append(report.Commands, evidence)
	if err != nil {
		return report, err
	}
	report.RepoRoot = repoRoot

	branchEvidence, err := r.Run(ctx, repo.Root, "branch", "--show-current")
	report.Commands = append(report.Commands, branchEvidence)
	if err != nil {
		return report, err
	}
	report.Branch = strings.TrimSpace(branchEvidence.Stdout)
	checks, parsed, policyErr := evaluatePolicy(report.Branch, base)
	report.Checks = append(report.Checks, checks...)
	if parsed.Issue > 0 {
		report.Issue = parsed.Issue
	}
	if policyErr != nil {
		report.OK = false
		return report, policyErr
	}
	report.OK = true
	return report, nil
}

func evaluatePolicy(branch, base string) ([]PolicyCheck, BranchRef, error) {
	checks := []PolicyCheck{
		{Name: "base_reference", Passed: true, Detail: base},
	}
	if strings.HasPrefix(branch, "codex/") {
		checks = append(checks,
			PolicyCheck{Name: "legacy_codex_branch_forbidden", Passed: false, Detail: branch},
			PolicyCheck{Name: "branch_naming", Passed: false, Detail: branch},
			PolicyCheck{Name: "issue_reference", Passed: false, Detail: "branch must encode a positive issue number"},
		)
		return checks, BranchRef{}, joinPolicyErrors(checks)
	}

	ref, err := ParseBranch(branch)
	if err != nil {
		checks = append(checks,
			PolicyCheck{Name: "legacy_codex_branch_forbidden", Passed: true, Detail: branch},
			PolicyCheck{Name: "branch_naming", Passed: false, Detail: err.Error()},
			PolicyCheck{Name: "issue_reference", Passed: false, Detail: "branch must encode a positive issue number"},
		)
		return checks, BranchRef{}, joinPolicyErrors(checks)
	}

	checks = append(checks,
		PolicyCheck{Name: "legacy_codex_branch_forbidden", Passed: true, Detail: ref.Name},
		PolicyCheck{Name: "branch_naming", Passed: true, Detail: ref.Name},
		PolicyCheck{Name: "issue_reference", Passed: ref.Issue > 0, Detail: strconv.FormatInt(ref.Issue, 10)},
	)
	if ref.Issue <= 0 {
		return checks, BranchRef{}, joinPolicyErrors(checks)
	}
	return checks, ref, nil
}

func resolveCommonRepoRoot(ctx context.Context, dir string, r Runner) (string, CommandEvidence, error) {
	evidence, err := r.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return dir, evidence, err
	}
	commonDir := strings.TrimSpace(evidence.Stdout)
	if commonDir == "" {
		return dir, evidence, fmt.Errorf("git rev-parse --git-common-dir returned empty output")
	}
	commonDir = filepath.Clean(commonDir)
	if filepath.Base(commonDir) != ".git" {
		return dir, evidence, fmt.Errorf("git common directory %q must end in .git", commonDir)
	}
	return filepath.Dir(commonDir), evidence, nil
}

func validateBaseRef(base string) error {
	if base == "" {
		return fmt.Errorf("base reference is required")
	}
	if base != strings.TrimSpace(base) {
		return fmt.Errorf("base reference must not include surrounding whitespace")
	}
	if !baseRefPattern.MatchString(base) {
		return fmt.Errorf("base reference %q is invalid", base)
	}
	if strings.Contains(base, "..") || strings.HasPrefix(base, "/") || strings.HasSuffix(base, "/") {
		return fmt.Errorf("base reference %q is invalid", base)
	}
	return nil
}

func failPolicy(report PolicyReport, check PolicyCheck) (PolicyReport, error) {
	report.Checks = append(report.Checks, check)
	report.OK = false
	return report, errors.New(check.Detail)
}

func joinPolicyErrors(checks []PolicyCheck) error {
	failures := make([]error, 0, len(checks))
	for _, check := range checks {
		if check.Passed {
			continue
		}
		failures = append(failures, fmt.Errorf("%s: %s", check.Name, check.Detail))
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.Join(failures...)
}
