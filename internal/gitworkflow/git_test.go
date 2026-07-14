package gitworkflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverRepoResolvesCanonicalRoot(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	canonicalRepo := canonicalPathForTest(t, repo)
	linkRoot := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, linkRoot); err != nil {
		t.Skipf("symlink creation failed: %v", err)
	}

	got, err := DiscoverRepo(context.Background(), ExecRunner{}, linkRoot)
	if err != nil {
		t.Fatalf("DiscoverRepo() error = %v", err)
	}
	if got.Root != canonicalRepo {
		t.Fatalf("DiscoverRepo() root = %q, want %q", got.Root, canonicalRepo)
	}
}

func TestExecRunnerRecordsCommandEvidence(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	canonicalRepo := canonicalPathForTest(t, repo)

	evidence, err := ExecRunner{}.Run(context.Background(), repo, "status", "--short")
	if err != nil {
		t.Fatalf("ExecRunner.Run() error = %v", err)
	}
	if got, want := evidence.Dir, canonicalRepo; got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
	if evidence.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", evidence.ExitCode)
	}
	if len(evidence.Argv) < 4 {
		t.Fatalf("Argv = %#v, want git -C <dir> ...", evidence.Argv)
	}
	if evidence.Argv[0] != "git" || evidence.Argv[1] != "-C" || evidence.Argv[2] != canonicalRepo {
		t.Fatalf("Argv = %#v, want git -C %q ...", evidence.Argv, canonicalRepo)
	}
	if evidence.Argv[3] != "status" {
		t.Fatalf("Argv = %#v, want status command", evidence.Argv)
	}
	if strings.Contains(evidence.Stdout, "RDEV_GIT_LEAK_TEST") || strings.Contains(evidence.Stderr, "RDEV_GIT_LEAK_TEST") {
		t.Fatalf("evidence leaked environment marker: %#v", evidence)
	}
}

func TestExecRunnerRecordsFailureEvidenceAndTrimsStderr(t *testing.T) {
	requireGit(t)

	repo := initGitRepo(t)
	t.Setenv("RDEV_GIT_LEAK_TEST", "leak-secret-12345")

	evidence, err := ExecRunner{}.Run(context.Background(), repo, "rev-parse", "--verify", "missing-branch")
	if err == nil {
		t.Fatal("ExecRunner.Run() expected error")
	}
	if evidence.ExitCode == 0 {
		t.Fatalf("ExitCode = %d, want non-zero", evidence.ExitCode)
	}
	trimmedErr := strings.TrimSpace(err.Error())
	if trimmedErr == "" {
		t.Fatal("error message is empty")
	}
	if strings.Contains(trimmedErr, "leak-secret-12345") || strings.Contains(evidence.Stdout, "leak-secret-12345") || strings.Contains(evidence.Stderr, "leak-secret-12345") {
		t.Fatalf("command output leaked environment token: err=%q evidence=%#v", trimmedErr, evidence)
	}
	if trimmedErr != strings.TrimSpace(evidence.Stderr) {
		t.Fatalf("error = %q, want trimmed stderr %q", trimmedErr, strings.TrimSpace(evidence.Stderr))
	}
	if !strings.Contains(trimmedErr, "fatal:") {
		t.Fatalf("error = %q, want trimmed git stderr", trimmedErr)
	}
}

func TestDiscoverRepoRejectsNonRepository(t *testing.T) {
	requireGit(t)

	emptyDir := t.TempDir()
	_, err := DiscoverRepo(context.Background(), ExecRunner{}, emptyDir)
	if err == nil {
		t.Fatal("DiscoverRepo() expected error")
	}
	if strings.TrimSpace(err.Error()) == "" {
		t.Fatal("DiscoverRepo() returned empty error")
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitForTest(t, repo, "init")
	runGitForTest(t, repo, "config", "user.email", "rdev-test@example.com")
	runGitForTest(t, repo, "config", "user.name", "Rdev Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, repo, "add", "README.md")
	runGitForTest(t, repo, "commit", "-m", "initial")
	return repo
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func canonicalPathForTest(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
