package gitworkflow

import (
	"context"
	"errors"
	"os"
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

func TestPlanPRRejectsInconsistentBranchMetadata(t *testing.T) {
	_, err := PlanPR(
		Repo{Root: "/repo"},
		BranchRef{Name: "feat/123-worktree-governance", Type: "fix", Issue: 456, Slug: "wrong"},
		"feat: add git policy reports",
		"## Summary\n- add policy reporting",
	)
	if err == nil {
		t.Fatal("PlanPR() expected inconsistent metadata error")
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

func TestPlanPRRejectsMalformedClosureReferences(t *testing.T) {
	for _, body := range []string{
		"## Summary\n- add policy reporting\n\nCloses #123abc",
		"## Summary\n- add policy reporting\n\nnotCloses #123",
	} {
		t.Run(strings.ReplaceAll(body, " ", "_"), func(t *testing.T) {
			_, err := PlanPR(
				Repo{Root: "/repo"},
				BranchRef{Name: "feat/123-worktree-governance", Type: "feat", Issue: 123, Slug: "worktree-governance"},
				"feat: add git policy reports",
				body,
			)
			if err == nil {
				t.Fatalf("PlanPR() expected malformed closure error for %q", body)
			}
		})
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
			Stdout:   "Authorization: Bearer abc123",
			Stderr:   "HTTP 401: Bad credentials\nghp_super_secret_token",
			ExitCode: 4,
		},
		err: errors.New("HTTP 401: Bad credentials\nghp_super_secret_token"),
	}

	evidence, err := ExecutePR(context.Background(), runner, "/repo", plan)
	if err == nil {
		t.Fatal("ExecutePR() expected error")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("error = %q, want safe unauthenticated message", err.Error())
	}
	if strings.Contains(err.Error(), "ghp_super_secret_token") {
		t.Fatalf("error leaked token: %q", err.Error())
	}
	if strings.Contains(evidence.Stderr, "ghp_super_secret_token") {
		t.Fatalf("stderr leaked token: %q", evidence.Stderr)
	}
	if strings.Contains(evidence.Stdout, "abc123") {
		t.Fatalf("stdout leaked bearer token: %q", evidence.Stdout)
	}
	if got, want := strings.Join(runner.args, " "), strings.Join(plan.Args, " "); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestExecutePRNormalizesMissingClosureInExecutedBody(t *testing.T) {
	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   "main",
		Head:   "feat/123-worktree-governance",
		Title:  "feat: add git policy reports",
		Body:   "## Summary\n- add policy reporting",
	}
	runner := &rawCommandRunner{}

	if _, err := ExecutePR(context.Background(), runner, "/repo", plan); err != nil {
		t.Fatalf("ExecutePR() error = %v", err)
	}
	if len(runner.args) == 0 {
		t.Fatal("runner did not receive argv")
	}
	bodyIndex := indexOf(runner.args, "--body")
	if bodyIndex < 0 || bodyIndex+1 >= len(runner.args) {
		t.Fatalf("argv missing body: %#v", runner.args)
	}
	if !strings.Contains(runner.args[bodyIndex+1], "Closes #123") {
		t.Fatalf("executed body = %q, want closure", runner.args[bodyIndex+1])
	}
}

func TestExecutePRRedactsSensitiveTitleAndBodyInEvidence(t *testing.T) {
	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   "main",
		Head:   "feat/123-worktree-governance",
		Title:  "feat: add policy token=ghp_title_secret",
		Body:   "## Summary\nAuthorization: Bearer body_secret\n\nCloses #123",
	}
	runner := &rawCommandRunner{}

	evidence, err := ExecutePR(context.Background(), runner, "/repo", plan)
	if err != nil {
		t.Fatalf("ExecutePR() error = %v", err)
	}
	serialized := strings.Join(evidence.Argv, "\x00") + evidence.Stdout + evidence.Stderr
	for _, secret := range []string{"ghp_title_secret", "body_secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("evidence leaked %q: %#v", secret, evidence)
		}
	}
	if !strings.Contains(strings.Join(evidence.Argv, " "), "--title") ||
		!strings.Contains(strings.Join(evidence.Argv, " "), "--body") {
		t.Fatalf("evidence lost safe argv structure: %#v", evidence.Argv)
	}
}

func TestGitHubRunnerRunsGHDirectly(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/gh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	plan := PRPlan{
		Schema: SchemaVersion,
		Base:   "main",
		Head:   "feat/123-worktree-governance",
		Title:  "feat: add policy",
		Body:   "Closes #123",
	}
	evidence, err := (GitHubRunner{}).executeGitHubPR(context.Background(), dir, plan)
	if err != nil {
		t.Fatalf("GitHubRunner.executeGitHubPR() error = %v", err)
	}
	if got, want := strings.Join(evidence.Argv, " "), "gh pr create --base main --head feat/123-worktree-governance --title feat: add policy --body Closes #123"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(evidence.Argv, " "), "git -C") {
		t.Fatalf("argv incorrectly used git -C: %#v", evidence.Argv)
	}
	if got, want := strings.TrimSpace(evidence.Stdout), "pr\ncreate\n--base\nmain\n--head\nfeat/123-worktree-governance\n--title\nfeat: add policy\n--body\nCloses #123"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestExecRunnerCannotSatisfyGitHubExecutor(t *testing.T) {
	var candidate any = ExecRunner{}
	if _, ok := candidate.(GitHubExecutor); ok {
		t.Fatal("ExecRunner must not satisfy GitHubExecutor")
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

func (r *rawCommandRunner) executeGitHubPR(_ context.Context, dir string, plan PRPlan) (CommandEvidence, error) {
	args := buildPRArgs(plan)
	r.args = append([]string(nil), args...)
	if len(r.evidence.Argv) == 0 {
		r.evidence.Argv = append([]string(nil), args...)
	}
	if r.evidence.Dir == "" {
		r.evidence.Dir = dir
	}
	return r.evidence, r.err
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}
