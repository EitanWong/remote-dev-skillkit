package gitworkflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const SchemaVersion = "rdev.git-workflow.v1"

type BranchRef struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Issue int64  `json:"issue"`
	Slug  string `json:"slug"`
}

type PolicyCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type PolicyReport struct {
	SchemaVersion string        `json:"schema_version"`
	Name          string        `json:"name"`
	Passed        bool          `json:"passed"`
	Detail        string        `json:"detail"`
	Branch        BranchRef     `json:"branch"`
	Checks        []PolicyCheck `json:"checks"`
}

var branchPattern = regexp.MustCompile(`^(feat|docs|release)/([0-9]+)-([a-z0-9]+(?:-[a-z0-9]+)*)$`)

var commitPattern = regexp.MustCompile(`^([a-z]+)(?:\(([A-Za-z0-9._/-]+)\))?: (.+)$`)

var allowedCommitTypes = map[string]struct{}{
	"build":    {},
	"ci":       {},
	"docs":     {},
	"feat":     {},
	"fix":      {},
	"perf":     {},
	"refactor": {},
	"release":  {},
	"revert":   {},
	"test":     {},
}

func ParseBranch(name string) (BranchRef, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return BranchRef{}, fmt.Errorf("branch name is required")
	}

	matches := branchPattern.FindStringSubmatch(trimmed)
	if matches == nil {
		return BranchRef{}, fmt.Errorf("branch %q must match <type>/<issue>-<slug>", trimmed)
	}

	issue, err := strconv.ParseInt(matches[2], 10, 64)
	if err != nil {
		return BranchRef{}, fmt.Errorf("branch %q has invalid issue number: %w", trimmed, err)
	}
	if issue <= 0 {
		return BranchRef{}, fmt.Errorf("branch %q must reference a positive issue number", trimmed)
	}

	ref := BranchRef{
		Name:  trimmed,
		Type:  matches[1],
		Issue: issue,
		Slug:  matches[3],
	}
	return ref, nil
}

func ValidateCommitSubject(subject string) error {
	return validateConventionalTitle(strings.TrimSpace(subject), "commit subject")
}

func ValidatePRTitle(title string) error {
	return validateConventionalTitle(strings.TrimSpace(title), "pull request title")
}

func IssueFromBranch(name string) (int64, error) {
	ref, err := ParseBranch(name)
	if err != nil {
		return 0, err
	}
	return ref.Issue, nil
}

func validateConventionalTitle(value, label string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}

	matches := commitPattern.FindStringSubmatch(value)
	if matches == nil {
		return fmt.Errorf("%s must use conventional format <type>[:scope]: <description>", label)
	}

	typeName := matches[1]
	if _, ok := allowedCommitTypes[typeName]; !ok {
		return fmt.Errorf("%s type %q is not allowed", label, typeName)
	}

	if strings.TrimSpace(matches[3]) == "" {
		return fmt.Errorf("%s description is required", label)
	}

	return nil
}
