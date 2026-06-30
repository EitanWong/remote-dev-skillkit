package acpxadapter

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
	fakeAcpx := writeFakeAcpxProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nupdated by fake acpx\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake acpx changed README")
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:             repo,
		Prompt:                    "update the README",
		AcpxCommand:               "go",
		AcpxArgs:                  []string{"run", fakeAcpx},
		VerificationCommands:      [][]string{{"git", "status", "--short"}},
		AllowVerificationCommands: []string{"git"},
		MaxDurationSeconds:        30,
		MaxOutputBytes:            64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AcpxCommand.ExitCode != 0 {
		t.Fatalf("expected fake acpx success, got %#v", result.AcpxCommand)
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
	if !strings.Contains(artifact.GitDiff.Stdout, "updated by fake acpx") {
		t.Fatalf("expected git diff evidence, got %q", artifact.GitDiff.Stdout)
	}
	if len(artifact.VerificationResults) != 1 || artifact.VerificationResults[0].ExitCode != 0 {
		t.Fatalf("expected verification evidence, got %#v", artifact.VerificationResults)
	}
}

func TestDefaultArgvUsesAcpxOneShotExecShape(t *testing.T) {
	repo := t.TempDir()
	argv := acpxArgv("summarize this repo", repo, Spec{})
	want := []string{"acpx", "--cwd", repo, "codex", "exec", "summarize this repo"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unexpected default argv:\nwant %#v\ngot  %#v", want, argv)
	}

	argv = acpxArgv("fix tests", repo, Spec{AcpxAgent: "claude"})
	want = []string{"acpx", "--cwd", repo, "claude", "exec", "fix tests"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unexpected agent override argv:\nwant %#v\ngot  %#v", want, argv)
	}
}

func TestExecuteParsesGoTestJSONVerificationReport(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/rdevtest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "math.go"), []byte(`package rdevtest

func Add(a, b int) int {
	return a + b
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "math_test.go"), []byte(`package rdevtest

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("bad sum")
	}
}

func TestSkip(t *testing.T) {
	t.Skip("exercise skip reporting")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "go.mod", "math.go", "math_test.go")
	runGit(t, repo, "commit", "-m", "add go tests")
	fakeAcpx := writeFakeAcpxProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("NOTE.md", []byte("acpx note\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake acpx wrote NOTE")
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:             repo,
		Prompt:                    "write a note and run tests",
		AcpxCommand:               "go",
		AcpxArgs:                  []string{"run", fakeAcpx},
		VerificationCommands:      [][]string{{"go", "test", "-json", "./..."}},
		AllowVerificationCommands: []string{"go"},
		MaxDurationSeconds:        30,
		MaxOutputBytes:            256 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact.VerificationResults) != 1 {
		t.Fatalf("expected one verification result, got %#v", artifact.VerificationResults)
	}
	report := artifact.VerificationResults[0].TestReport
	if report == nil {
		t.Fatalf("expected go test report, got %#v", artifact.VerificationResults[0])
	}
	if report.SchemaVersion != TestReportSchemaVersion || report.Tool != "go test" {
		t.Fatalf("unexpected report metadata: %#v", report)
	}
	if report.Passed != 1 || report.Skipped != 1 || report.Failed != 0 || report.Total != 2 {
		t.Fatalf("unexpected test counts: %#v", report)
	}
	if len(report.Tests) != 2 {
		t.Fatalf("expected two test cases, got %#v", report.Tests)
	}
}

func TestExecuteRejectsVerificationCommandWithoutAllowlist(t *testing.T) {
	repo := initGitRepo(t)
	fakeAcpx := writeFakeAcpxProgram(t, `package main

func main() {}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:        repo,
		Prompt:               "no-op",
		AcpxCommand:          "go",
		AcpxArgs:             []string{"run", fakeAcpx},
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
	if !strings.Contains(result.ArtifactContent(), `"schema_version": "rdev.acpx-result.v1"`) {
		t.Fatalf("expected acpx artifact after verification denial, got %s", result.ArtifactContent())
	}
}

func TestArtifactContentRedactsAcpxOutput(t *testing.T) {
	secret := "sk-" + "testsecret12345678901234567890"
	result := Result{
		Adapter:       "acpx",
		WorkspaceRoot: t.TempDir(),
		Prompt:        "use token=" + secret,
		AcpxCommand: CommandResult{
			Argv:     []string{"acpx", "exec", "token=" + secret},
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
		t.Skip("git is required for acpx adapter tests")
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

func writeFakeAcpxProgram(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakeacpx.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
