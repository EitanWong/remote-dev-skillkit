package gitworkflow

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestPlanPRIncludesClosesIssueWithoutExecutingGH(t *testing.T) {
	plan, err := PlanPR(
		Repo{Root: "/repo"},
		BranchRef{Name: "feat/123-worktree-governance", Type: "feat", Issue: 123, Slug: "worktree-governance"},
		"feat: add git policy reports",
		"## Summary\n- add policy reporting",
	)
	if err != nil {
		t.Fatalf("PlanPR() error = %v", err)
	}
	if plan.Schema != SchemaVersion {
		t.Fatalf("schema = %q, want %q", plan.Schema, SchemaVersion)
	}
	if plan.Base != defaultBaseBranch {
		t.Fatalf("base = %q, want %q", plan.Base, defaultBaseBranch)
	}
	if !strings.Contains(plan.Body, "Closes #123") {
		t.Fatalf("body = %q, want Closes #123", plan.Body)
	}
	if len(plan.Args) == 0 {
		t.Fatal("plan args are empty")
	}
	if got, want := strings.Join(plan.Args[:3], " "), "gh pr create"; got != want {
		t.Fatalf("argv prefix = %q, want %q", got, want)
	}
}

func TestPlanPRRequiresMatchingIssueText(t *testing.T) {
	_, err := PlanPR(
		Repo{Root: "/repo"},
		BranchRef{Name: "feat/123-worktree-governance", Type: "feat", Issue: 123, Slug: "worktree-governance"},
		"feat: add git policy reports",
		"## Summary\n- add policy reporting\n\nCloses #456",
	)
	if err == nil {
		t.Fatal("PlanPR() expected error")
	}
}

func TestExecutePRUsesArgvAndSanitizesAuthenticationFailure(t *testing.T) {
	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   "main",
		Head:   "feat/123-worktree-governance",
		Title:  "feat: add git policy reports",
		Body:   "## Summary\n- add policy reporting\n\nCloses #123",
		Args: []string{
			"gh", "pr", "create",
			"--base", "main",
			"--head", "feat/123-worktree-governance",
			"--title", "feat: add git policy reports",
			"--body", "## Summary\n- add policy reporting\n\nCloses #123",
		},
	}
	runner := &rawCommandRunner{
		evidence: CommandEvidence{
			Argv:     append([]string(nil), plan.Args...),
			Dir:      "/repo",
			Stderr:   "gh auth login required\nghp_super_secret_token",
			ExitCode: 4,
		},
		err: errors.New("gh auth login required\nghp_super_secret_token"),
	}

	evidence, err := ExecutePR(context.Background(), runner, "/repo", plan)
	if err == nil {
		t.Fatal("ExecutePR() expected error")
	}
	if strings.Contains(err.Error(), "ghp_super_secret_token") {
		t.Fatalf("error leaked token: %q", err.Error())
	}
	if strings.Contains(evidence.Stderr, "ghp_super_secret_token") {
		t.Fatalf("stderr leaked token: %q", evidence.Stderr)
	}
	if got, want := strings.Join(runner.args, " "), strings.Join(plan.Args, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestExecutePRReportsMissingGHSafely(t *testing.T) {
	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   "main",
		Head:   "feat/123-worktree-governance",
		Title:  "feat: add git policy reports",
		Body:   "## Summary\n- add policy reporting\n\nCloses #123",
		Args:   []string{"gh", "pr", "create", "--base", "main", "--head", "feat/123-worktree-governance", "--title", "feat: add git policy reports", "--body", "## Summary\n- add policy reporting\n\nCloses #123"},
	}
	runner := &rawCommandRunner{
		evidence: CommandEvidence{Argv: append([]string(nil), plan.Args...), Dir: "/repo", ExitCode: -1},
		err:      exec.ErrNotFound,
	}

	_, err := ExecutePR(context.Background(), runner, "/repo", plan)
	if err == nil {
		t.Fatal("ExecutePR() expected error")
	}
	if !strings.Contains(err.Error(), "gh is not installed") {
		t.Fatalf("error = %q, want gh missing message", err.Error())
	}
}

type rawCommandRunner struct {
	args     []string
	evidence CommandEvidence
	err      error
}

func (r *rawCommandRunner) Run(_ context.Context, dir string, args ...string) (CommandEvidence, error) {
	r.args = append([]string(nil), args...)
	if len(r.evidence.Argv) == 0 {
		r.evidence.Argv = append([]string(nil), args...)
	}
	if r.evidence.Dir == "" {
		r.evidence.Dir = dir
	}
	return r.evidence, r.err
}
