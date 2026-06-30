package claudecodeadapter

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/pkg/adapterkit"
)

func TestConformanceCanonicalizesWorkspaceRoot(t *testing.T) {
	repo := initGitRepo(t)
	fakeClaudeCode := writeFakeClaudeCodeProgram(t, `package main

func main() {}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      filepath.Join(repo, "."),
		Prompt:             "inspect workspace",
		ClaudeCodeCommand:  "go",
		ClaudeCodeArgs:     []string{"run", fakeClaudeCode},
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspaceRoot != canonical {
		t.Fatalf("expected canonical workspace %q, got %q", canonical, result.WorkspaceRoot)
	}
}

func TestConformanceRejectsWriteScopeEscapeBeforeClaudeCodeRuns(t *testing.T) {
	repo := initGitRepo(t)
	outside := t.TempDir()
	link := filepath.Join(repo, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is not available: %v", err)
	}
	fakeClaudeCode := writeFakeClaudeCodeProgram(t, `package main

import "os"

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nclaude-code should not run\n"), 0o644); err != nil {
		panic(err)
	}
}
`)
	_, err := Execute(Spec{
		WorkspaceRoot:      repo,
		WriteScope:         []string{filepath.Join(link, "missing-child")},
		Prompt:             "attempt escaped write scope",
		ClaudeCodeCommand:  "go",
		ClaudeCodeArgs:     []string{"run", fakeClaudeCode},
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64 * 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected workspace escape error, got %v", err)
	}
	content, readErr := os.ReadFile(filepath.Join(repo, "README.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content), "claude-code should not run") {
		t.Fatalf("claude code command ran after write-scope escape: %s", string(content))
	}
}

func TestConformanceNonzeroClaudeCodeExitStillReturnsEvidence(t *testing.T) {
	repo := initGitRepo(t)
	fakeClaudeCode := writeFakeClaudeCodeProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# demo\n\nchanged before failure\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("stdout before failure")
	fmt.Fprintln(os.Stderr, "stderr before failure")
	os.Exit(7)
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      repo,
		Prompt:             "change README but fail",
		ClaudeCodeCommand:  "go",
		ClaudeCodeArgs:     []string{"run", fakeClaudeCode},
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64 * 1024,
	})
	if err == nil {
		t.Fatal("expected nonzero claude code command to fail")
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatalf("decode artifact: %v\n%s", err, result.ArtifactContent())
	}
	if artifact.SchemaVersion != ResultSchemaVersion {
		t.Fatalf("expected result schema, got %#v", artifact)
	}
	if artifact.ClaudeCodeCommand.ExitCode == 0 {
		t.Fatalf("expected nonzero exit evidence, got %#v", artifact.ClaudeCodeCommand)
	}
	if !strings.Contains(artifact.ClaudeCodeCommand.Stdout, "stdout before failure") {
		t.Fatalf("expected stdout evidence, got %#v", artifact.ClaudeCodeCommand)
	}
	if !strings.Contains(artifact.ClaudeCodeCommand.Stderr, "stderr before failure") {
		t.Fatalf("expected stderr evidence, got %#v", artifact.ClaudeCodeCommand)
	}
	if !strings.Contains(artifact.GitDiff.Stdout, "changed before failure") {
		t.Fatalf("expected diff evidence after failure, got %q", artifact.GitDiff.Stdout)
	}
	assertClaudeCodeAdapterKitConformance(t, result.ArtifactContent())
}

func TestConformanceRedactsPromptArgvOutputAndDiff(t *testing.T) {
	repo := initGitRepo(t)
	secret := "sk-" + "conformance12345678901234567890"
	fakeClaudeCode := writeFakeClaudeCodeProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	secret := os.Args[len(os.Args)-1]
	if err := os.WriteFile("README.md", []byte("# demo\n\ntoken="+secret+"\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("stdout token=" + secret)
	fmt.Fprintln(os.Stderr, "stderr token="+secret)
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      repo,
		Prompt:             "use token=" + secret,
		ClaudeCodeCommand:  "go",
		ClaudeCodeArgs:     []string{"run", fakeClaudeCode, secret},
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := result.ArtifactContent()
	if strings.Contains(content, secret) {
		t.Fatalf("artifact leaked secret: %s", content)
	}
	if !strings.Contains(content, "[REDACTED:openai_api_key]") {
		t.Fatalf("expected redaction marker, got %s", content)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(content), &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.Redacted {
		t.Fatalf("expected artifact redacted flag, got %#v", artifact)
	}
	if artifact.RedactionCounts["openai_api_key"] == 0 {
		t.Fatalf("expected openai redaction count, got %#v", artifact.RedactionCounts)
	}
	assertClaudeCodeAdapterKitConformance(t, content)
}

func TestConformanceOutputTruncationIsReported(t *testing.T) {
	repo := initGitRepo(t)
	fakeClaudeCode := buildFakeClaudeCodeBinary(t, `package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Print(strings.Repeat("x", 4096))
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      repo,
		Prompt:             "produce large output",
		ClaudeCodeCommand:  fakeClaudeCode,
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64,
	})
	if err != nil {
		t.Fatal(err)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.ClaudeCodeCommand.OutputTruncated {
		t.Fatalf("expected output_truncated=true, got %#v", artifact.ClaudeCodeCommand)
	}
	if len(artifact.ClaudeCodeCommand.Stdout) > 64 {
		t.Fatalf("expected truncated stdout length <= 64, got %d", len(artifact.ClaudeCodeCommand.Stdout))
	}
	assertClaudeCodeAdapterKitConformance(t, result.ArtifactContent())
}

func TestConformanceTimeoutCancelsClaudeCodeCommandWithEvidence(t *testing.T) {
	repo := initGitRepo(t)
	fakeClaudeCode := buildFakeClaudeCodeBinary(t, `package main

import "time"

func main() {
	time.Sleep(3 * time.Second)
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      repo,
		Prompt:             "sleep too long",
		ClaudeCodeCommand:  fakeClaudeCode,
		MaxDurationSeconds: 1,
		MaxOutputBytes:     64 * 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.ClaudeCodeCommand.TimedOut {
		t.Fatalf("expected timeout evidence, got %#v", artifact.ClaudeCodeCommand)
	}
	assertClaudeCodeAdapterKitConformance(t, result.ArtifactContent())
}

func TestConformanceExternalContextCancelsClaudeCodeCommandWithEvidence(t *testing.T) {
	repo := initGitRepo(t)
	fakeClaudeCode := buildFakeClaudeCodeBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := ExecuteContext(ctx, Spec{
		WorkspaceRoot:      repo,
		Prompt:             "cancel from caller",
		ClaudeCodeCommand:  fakeClaudeCode,
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64 * 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.ClaudeCodeCommand.Canceled || artifact.ClaudeCodeCommand.TimedOut {
		t.Fatalf("expected canceled non-timeout evidence, got %#v", artifact.ClaudeCodeCommand)
	}
	if artifact.GitStatus.Argv == nil || artifact.GitDiff.Argv == nil {
		t.Fatalf("expected cancellation evidence to include git status and diff, got %#v", artifact)
	}
	assertClaudeCodeAdapterKitConformance(t, result.ArtifactContent())
	assertClaudeCodeAdapterKitCancellationConformance(t, result.ArtifactContent())
}

func assertClaudeCodeAdapterKitConformance(t *testing.T, content string) {
	t.Helper()
	report := adapterkit.VerifyResultArtifactJSON([]byte(content), adapterkit.ResultArtifactContract{
		Adapter:                 "claude-code",
		SchemaVersion:           ResultSchemaVersion,
		CommandFields:           []string{"claude_code_command", "git_status", "git_diff_stat", "git_diff"},
		RequiredStringFields:    []string{"workspace_root"},
		RequireTiming:           true,
		RequireRedaction:        true,
		RejectUnredactedSecrets: true,
	})
	if !report.OK {
		t.Fatalf("ClaudeCode artifact failed adapterkit conformance: %#v\n%s", report, content)
	}
}

func assertClaudeCodeAdapterKitCancellationConformance(t *testing.T, content string) {
	t.Helper()
	report := adapterkit.VerifyCancellationArtifactJSON([]byte(content), adapterkit.CancellationContract{
		Adapter:                 "claude-code",
		SchemaVersion:           ResultSchemaVersion,
		CommandFields:           []string{"claude_code_command"},
		RequiredStringFields:    []string{"workspace_root"},
		RequireTiming:           true,
		RequireRedaction:        true,
		RejectUnredactedSecrets: true,
	})
	if !report.OK {
		t.Fatalf("ClaudeCode cancellation artifact failed adapterkit conformance: %#v\n%s", report, content)
	}
}

func buildFakeClaudeCodeBinary(t *testing.T, source string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is required for claude-code conformance tests")
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "fakeclaude-code.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryName := "fakeclaude-code"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(dir, binaryName)
	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake claude code binary: %v\n%s", err, string(output))
	}
	return binaryPath
}
