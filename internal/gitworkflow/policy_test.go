package gitworkflow

import "testing"

func TestParseBranchValidCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  BranchRef
	}{
		{name: "feat issue slug", input: "feat/123-worktree-governance", want: BranchRef{Name: "feat/123-worktree-governance", Type: "feat", Issue: 123, Slug: "worktree-governance"}},
		{name: "docs issue slug", input: "docs/7-git-guide", want: BranchRef{Name: "docs/7-git-guide", Type: "docs", Issue: 7, Slug: "git-guide"}},
		{name: "release issue slug", input: "release/42-v1-2-0", want: BranchRef{Name: "release/42-v1-2-0", Type: "release", Issue: 42, Slug: "v1-2-0"}},
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

func TestParseBranchInvalidCases(t *testing.T) {
	tests := []string{
		"codex/123-agent-work",
		"feat/worktree-governance",
		"feat/abc-worktree",
		"feat/123_Worktree",
		"main",
		"feature/123-worktree",
		"feat/123-",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
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
		{name: "missing type separator", subject: "add worktree doctor", valid: false},
		{name: "uppercase type", subject: "Feat: uppercase type", valid: false},
		{name: "empty description", subject: "feat:", valid: false},
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
		{name: "missing separator", title: "add worktree doctor", valid: false},
		{name: "uppercase type", title: "Feat: uppercase type", valid: false},
		{name: "empty description", title: "feat:", valid: false},
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
