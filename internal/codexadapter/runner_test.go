package codexadapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteCapturesDiffAndVerificationEvidence(t *testing.T) {
	repo := initGitRepo(t)
	fakeCodex := writeFakeCodexProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nupdated by fake codex\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex changed README")
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:             repo,
		Prompt:                    "update the README",
		CodexCommand:              "go",
		CodexArgs:                 []string{"run", fakeCodex},
		VerificationCommands:      [][]string{{"git", "status", "--short"}},
		AllowVerificationCommands: []string{"git"},
		MaxDurationSeconds:        30,
		MaxOutputBytes:            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CodexCommand.ExitCode != 0 {
		t.Fatalf("expected fake codex success, got %#v", result.CodexCommand)
	}
	content := result.ArtifactContent()
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(content), &artifact); err != nil {
		t.Fatalf("decode artifact: %v\n%s", err, content)
	}
	if artifact.SchemaVersion != ResultSchemaVersion {
		t.Fatalf("unexpected schema %q", artifact.SchemaVersion)
	}
	if !strings.Contains(artifact.GitStatus.Stdout, "M README.md") {
		t.Fatalf("expected git status evidence, got %q", artifact.GitStatus.Stdout)
	}
	if !strings.Contains(artifact.GitDiff.Stdout, "updated by fake codex") {
		t.Fatalf("expected git diff evidence, got %q", artifact.GitDiff.Stdout)
	}
	if len(artifact.VerificationResults) != 1 || artifact.VerificationResults[0].ExitCode != 0 {
		t.Fatalf("expected verification evidence, got %#v", artifact.VerificationResults)
	}
}

func TestExecuteRejectsVerificationCommandWithoutAllowlist(t *testing.T) {
	repo := initGitRepo(t)
	fakeCodex := writeFakeCodexProgram(t, `package main

func main() {}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:        repo,
		Prompt:               "no-op",
		CodexCommand:         "go",
		CodexArgs:            []string{"run", fakeCodex},
		VerificationCommands: [][]string{{"git", "status", "--short"}},
		MaxDurationSeconds:   30,
		MaxOutputBytes:       64 * 1024,
	})
	if err == nil {
		t.Fatal("expected missing allowlist to fail")
	}
	if !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.ArtifactContent(), `"schema_version": "rdev.codex-result.v1"`) {
		t.Fatalf("expected codex artifact after verification denial, got %s", result.ArtifactContent())
	}
}

func TestArtifactContentRedactsCodexOutput(t *testing.T) {
	secret := "sk-" + "testsecret12345678901234567890"
	result := Result{
		Adapter:       "codex",
		WorkspaceRoot: t.TempDir(),
		Prompt:        "use token=" + secret,
		CodexCommand: CommandResult{
			Argv:     []string{"codex", "exec", "token=" + secret},
			ExitCode: 0,
			Stdout:   "token=" + secret,
		},
	}
	content := result.ArtifactContent()
	if strings.Contains(content, secret) {
		t.Fatalf("artifact leaked secret: %s", content)
	}
	if !strings.Contains(content, "[REDACTED:openai_api_key]") {
		t.Fatalf("expected redaction marker, got %s", content)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for codex adapter tests")
	}
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func writeFakeCodexProgram(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakecodex.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
