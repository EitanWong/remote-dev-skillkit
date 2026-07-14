package gitworkflow

import (
	"fmt"
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
	Schema   string          `json:"schema"`
	OK       bool            `json:"ok"`
	RepoRoot string          `json:"repo_root"`
	Branch   string          `json:"branch"`
	Issue    int64           `json:"issue"`
	Base     string          `json:"base"`
	Worktree string          `json:"worktree"`
	Checks   []PolicyCheck   `json:"checks"`
	Commands []CommandRecord `json:"commands"`
}

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
