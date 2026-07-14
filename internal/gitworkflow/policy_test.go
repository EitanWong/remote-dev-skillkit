package gitworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseBranchValidCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  BranchRef
	}{
		{name: "feat", input: "feat/123-worktree-governance", want: BranchRef{Name: "feat/123-worktree-governance", Type: "feat", Issue: 123, Slug: "worktree-governance"}},
		{name: "fix", input: "fix/124-stale-worktree-cleanup", want: BranchRef{Name: "fix/124-stale-worktree-cleanup", Type: "fix", Issue: 124, Slug: "stale-worktree-cleanup"}},
		{name: "refactor", input: "refactor/125-policy-api", want: BranchRef{Name: "refactor/125-policy-api", Type: "refactor", Issue: 125, Slug: "policy-api"}},
		{name: "docs", input: "docs/7-git-guide", want: BranchRef{Name: "docs/7-git-guide", Type: "docs", Issue: 7, Slug: "git-guide"}},
		{name: "test", input: "test/8-policy-coverage", want: BranchRef{Name: "test/8-policy-coverage", Type: "test", Issue: 8, Slug: "policy-coverage"}},
		{name: "chore", input: "chore/9-dependency-refresh", want: BranchRef{Name: "chore/9-dependency-refresh", Type: "chore", Issue: 9, Slug: "dependency-refresh"}},
		{name: "perf", input: "perf/10-faster-policy-checks", want: BranchRef{Name: "perf/10-faster-policy-checks", Type: "perf", Issue: 10, Slug: "faster-policy-checks"}},
		{name: "ci", input: "ci/11-workflow-hardening", want: BranchRef{Name: "ci/11-workflow-hardening", Type: "ci", Issue: 11, Slug: "workflow-hardening"}},
		{name: "hotfix", input: "hotfix/12-release-blocker", want: BranchRef{Name: "hotfix/12-release-blocker", Type: "hotfix", Issue: 12, Slug: "release-blocker"}},
		{name: "release", input: "release/42-v1-2-0", want: BranchRef{Name: "release/42-v1-2-0", Type: "release", Issue: 42, Slug: "v1-2-0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBranch(tt.input)
			if err != nil {
				t.Fatalf("ParseBranch() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseBranch() = %#v, want %#v", got, tt.want)
			}
			issue, err := IssueFromBranch(tt.input)
			if err != nil {
				t.Fatalf("IssueFromBranch() error = %v", err)
			}
			if issue != tt.want.Issue {
				t.Fatalf("IssueFromBranch() = %d, want %d", issue, tt.want.Issue)
			}
		})
	}
}

func TestParseBranchRejectsInvalidCases(t *testing.T) {
	tests := []string{
		"codex/123-agent-work",
		"agent/123-worktree",
		"feat/worktree-governance",
		"feat/abc-worktree",
		"feat/123_Worktree",
		"main",
		"feature/123-worktree",
		"feat/123-",
		" feat/123-worktree",
		"feat/123-worktree ",
	}

	for _, input := range tests {
		t.Run(strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := ParseBranch(input); err == nil {
				t.Fatalf("ParseBranch(%q) expected error", input)
			}
			if _, err := IssueFromBranch(input); err == nil {
				t.Fatalf("IssueFromBranch(%q) expected error", input)
			}
		})
	}
}

func TestValidateCommitSubject(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		valid   bool
	}{
		{name: "feat subject", subject: "feat: add worktree doctor", valid: true},
		{name: "fix with scope", subject: "fix(cli): reject main edits", valid: true},
		{name: "refactor subject", subject: "refactor(policy): split branch validation", valid: true},
		{name: "docs subject", subject: "docs: update git workflow guide", valid: true},
		{name: "test subject", subject: "test: cover git workflow policy", valid: true},
		{name: "chore subject", subject: "chore: refresh policy fixtures", valid: true},
		{name: "perf subject", subject: "perf: speed up policy checks", valid: true},
		{name: "ci subject", subject: "ci: harden branch checks", valid: true},
		{name: "hotfix subject", subject: "hotfix: unblock release branch", valid: true},
		{name: "release subject", subject: "release: publish v1.2.0", valid: true},
		{name: "invalid build type", subject: "build: add worktree doctor", valid: false},
		{name: "invalid revert type", subject: "revert: roll back worktree doctor", valid: false},
		{name: "missing type separator", subject: "add worktree doctor", valid: false},
		{name: "uppercase type", subject: "Feat: uppercase type", valid: false},
		{name: "empty description", subject: "feat:", valid: false},
		{name: "whitespace padded", subject: " feat: add worktree doctor", valid: false},
		{name: "imperative past tense", subject: "feat: added worktree doctor", valid: false},
		{name: "imperative third person", subject: "feat: rejects main edits", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCommitSubject(tt.subject)
			if tt.valid && err != nil {
				t.Fatalf("ValidateCommitSubject(%q) error = %v", tt.subject, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("ValidateCommitSubject(%q) expected error", tt.subject)
			}
		})
	}
}

func TestValidatePRTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		valid bool
	}{
		{name: "feat title", title: "feat: add worktree doctor", valid: true},
		{name: "fix title with scope", title: "fix(cli): reject main edits", valid: true},
		{name: "docs title", title: "docs: update git workflow guide", valid: true},
		{name: "hotfix title", title: "hotfix: unblock release branch", valid: true},
		{name: "invalid build type", title: "build: add worktree doctor", valid: false},
		{name: "invalid revert type", title: "revert: roll back worktree doctor", valid: false},
		{name: "missing separator", title: "add worktree doctor", valid: false},
		{name: "uppercase type", title: "Feat: uppercase type", valid: false},
		{name: "empty description", title: "feat:", valid: false},
		{name: "whitespace padded", title: "feat: add worktree doctor ", valid: false},
		{name: "imperative past tense", title: "feat: added worktree doctor", valid: false},
		{name: "imperative third person", title: "feat: rejects main edits", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePRTitle(tt.title)
			if tt.valid && err != nil {
				t.Fatalf("ValidatePRTitle(%q) error = %v", tt.title, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("ValidatePRTitle(%q) expected error", tt.title)
			}
		})
	}
}

func TestPolicyReportJSONSchema(t *testing.T) {
	report := PolicyReport{
		Schema:   SchemaVersion,
		OK:       true,
		RepoRoot: "/repo",
		Branch:   "feat/123-worktree-governance",
		Issue:    123,
		Base:     "origin/main",
		Worktree: "/worktrees/feat-123-worktree-governance",
		Checks: []PolicyCheck{{
			Name:   "branch_format",
			Passed: true,
			Detail: "branch matches schema",
		}},
		Commands: []CommandEvidence{{
			Argv:     []string{"git", "-C", "/worktrees/feat-123-worktree-governance", "status", "--short"},
			Dir:      "/worktrees/feat-123-worktree-governance",
			Stdout:   "",
			Stderr:   "",
			ExitCode: 0,
		}},
	}

	content, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	wantKeys := map[string]struct{}{
		"schema": {}, "ok": {}, "repo_root": {}, "branch": {}, "issue": {},
		"base": {}, "worktree": {}, "checks": {}, "commands": {},
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("JSON keys = %d, want exactly %d: %s", len(got), len(wantKeys), string(content))
	}
	for key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing JSON key %q in %s", key, string(content))
		}
	}
	if got["schema"] != SchemaVersion {
		t.Fatalf("schema = %v, want %q", got["schema"], SchemaVersion)
	}
	if got["branch"] != "feat/123-worktree-governance" {
		t.Fatalf("branch = %v, want branch string", got["branch"])
	}
	if got["issue"].(float64) != 123 {
		t.Fatalf("issue = %v, want 123", got["issue"])
	}
	if !got["ok"].(bool) {
		t.Fatalf("ok = %v, want true", got["ok"])
	}
}

func TestPolicyReportValidBranchProducesOK(t *testing.T) {
	repo := Repo{Root: "/repo/worktrees/feat-123-worktree-governance"}
	runner := &scriptedRunner{
		responses: map[string]scriptedResponse{
			keyFor(repo.Root, "rev-parse", "--path-format=absolute", "--git-common-dir"): {
				evidence: CommandEvidence{Stdout: "/repo/.git\n", ExitCode: 0},
			},
			keyFor(repo.Root, "branch", "--show-current"): {
				evidence: CommandEvidence{Stdout: "feat/123-worktree-governance\n", ExitCode: 0},
			},
		},
	}

	report, err := CheckPolicy(context.Background(), repo, runner, "origin/main")
	if err != nil {
		t.Fatalf("CheckPolicy() error = %v", err)
	}
	if !report.OK {
		t.Fatalf("report.OK = false, want true: %#v", report)
	}
	if report.Schema != SchemaVersion {
		t.Fatalf("schema = %q, want %q", report.Schema, SchemaVersion)
	}
	if report.RepoRoot != "/repo" {
		t.Fatalf("repo root = %q, want /repo", report.RepoRoot)
	}
	if report.Worktree != repo.Root {
		t.Fatalf("worktree = %q, want %q", report.Worktree, repo.Root)
	}
	if report.Branch != "feat/123-worktree-governance" {
		t.Fatalf("branch = %q", report.Branch)
	}
	if report.Issue != 123 {
		t.Fatalf("issue = %d, want 123", report.Issue)
	}
	if report.Base != "origin/main" {
		t.Fatalf("base = %q, want origin/main", report.Base)
	}
	if len(report.Commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(report.Commands))
	}
}

func TestPolicyReportRejectsLegacyCodexBranch(t *testing.T) {
	repo := Repo{Root: "/repo/worktrees/codex-legacy"}
	runner := &scriptedRunner{
		responses: map[string]scriptedResponse{
			keyFor(repo.Root, "rev-parse", "--path-format=absolute", "--git-common-dir"): {
				evidence: CommandEvidence{Stdout: "/repo/.git\n", ExitCode: 0},
			},
			keyFor(repo.Root, "branch", "--show-current"): {
				evidence: CommandEvidence{Stdout: "codex/phase1-regional-tunnel-availability\n", ExitCode: 0},
			},
		},
	}

	report, err := CheckPolicy(context.Background(), repo, runner, "origin/main")
	if err == nil {
		t.Fatal("CheckPolicy() expected error")
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false: %#v", report)
	}
	check, ok := findPolicyCheck(report.Checks, "legacy_codex_branch_forbidden")
	if !ok {
		t.Fatalf("missing legacy_codex_branch_forbidden check: %#v", report.Checks)
	}
	if check.Passed {
		t.Fatalf("legacy_codex_branch_forbidden passed unexpectedly: %#v", check)
	}
}

func TestPolicyReportRejectsInvalidBase(t *testing.T) {
	repo := Repo{Root: "/repo/worktrees/feat-123-worktree-governance"}

	report, err := CheckPolicy(context.Background(), repo, &scriptedRunner{responses: map[string]scriptedResponse{}}, " origin/main")
	if err == nil {
		t.Fatal("CheckPolicy() expected error")
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false: %#v", report)
	}
	check, ok := findPolicyCheck(report.Checks, "base_reference")
	if !ok {
		t.Fatalf("missing base_reference check: %#v", report.Checks)
	}
	if check.Passed {
		t.Fatalf("base_reference passed unexpectedly: %#v", check)
	}
}

func findPolicyCheck(checks []PolicyCheck, name string) (PolicyCheck, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return PolicyCheck{}, false
}

func TestPolicyReportAggregatesPolicyFailures(t *testing.T) {
	repo := Repo{Root: "/repo/worktrees/bad"}
	runner := &scriptedRunner{
		responses: map[string]scriptedResponse{
			keyFor(repo.Root, "rev-parse", "--path-format=absolute", "--git-common-dir"): {
				evidence: CommandEvidence{Stdout: "/repo/.git\n", ExitCode: 0},
			},
			keyFor(repo.Root, "branch", "--show-current"): {
				evidence: CommandEvidence{Stdout: "feat/nope\n", ExitCode: 0},
			},
		},
	}

	report, err := CheckPolicy(context.Background(), repo, runner, "origin/main")
	if err == nil {
		t.Fatal("CheckPolicy() expected error")
	}
	var policyErr interface{ Unwrap() []error }
	if !errors.As(err, &policyErr) {
		t.Fatalf("error = %T, want joined error", err)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false")
	}
}

func TestValidateBaseRefRejectsGitInvalidRefs(t *testing.T) {
	tests := []string{
		"main.",
		"main.lock",
		"origin/main..feature",
		"origin//main",
		"origin/.hidden",
		".hidden/main",
		"origin/main@{1}",
		"origin/main with-space",
		"origin/main\\feature",
		"origin/main~1",
		"origin/main^",
		"origin/main:feature",
		"origin/main?feature",
		"origin/main*",
		"origin/main[0]",
		"/origin/main",
		"origin/main/",
		"-main",
		"origin/-main",
		"@",
		".",
		"..",
		"origin/\x7fmain",
	}
	for _, ref := range tests {
		t.Run(strings.ReplaceAll(ref, "/", "_"), func(t *testing.T) {
			if err := validateBaseRef(ref); err == nil {
				t.Fatalf("validateBaseRef(%q) expected error", ref)
			}
		})
	}
}
