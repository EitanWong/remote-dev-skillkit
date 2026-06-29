package shelladapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecuteAllowedCommand(t *testing.T) {
	result, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, runtime.GOOS) {
		t.Fatalf("expected stdout to contain GOOS %q, got %q", runtime.GOOS, result.Stdout)
	}
	if result.ArtifactContent() == "" {
		t.Fatal("artifact content should be present")
	}
}

func TestExecuteRejectsCommandNotAllowlisted(t *testing.T) {
	_, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"git"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected command to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRejectsPathCommandWhenOnlyNameAllowlisted(t *testing.T) {
	_, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		Argv:               []string{"/tmp/go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected path command to be rejected")
	}
}

func TestExecuteRejectsMissingWorkspace(t *testing.T) {
	_, err := Execute(Spec{
		WorkspaceRoot:      "",
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected missing workspace to fail")
	}
}

func TestExecuteRejectsWriteScopeEscape(t *testing.T) {
	workspace := t.TempDir()
	_, err := Execute(Spec{
		WorkspaceRoot:      workspace,
		WriteScope:         []string{".."},
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected escaping write scope to fail")
	}
	if !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRejectsSymlinkWriteScopeEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	link := createSymlinkOrSkip(t, outside, filepath.Join(workspace, "outside-link"))
	_, err := Execute(Spec{
		WorkspaceRoot:      workspace,
		WriteScope:         []string{link},
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected symlink escaping write scope to fail")
	}
	if !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRejectsSymlinkWriteScopeEscapeForMissingChild(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	link := createSymlinkOrSkip(t, outside, filepath.Join(workspace, "outside-link"))
	_, err := Execute(Spec{
		WorkspaceRoot:      workspace,
		WriteScope:         []string{filepath.Join(link, "missing-child")},
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected symlink escaping missing child write scope to fail")
	}
	if !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteRejectsSymlinkParentTraversalEscape(t *testing.T) {
	workspace := t.TempDir()
	outsideParent := t.TempDir()
	outsideChild := filepath.Join(outsideParent, "child")
	if err := os.Mkdir(outsideChild, 0o755); err != nil {
		t.Fatal(err)
	}
	link := createSymlinkOrSkip(t, outsideChild, filepath.Join(workspace, "outside-link"))
	_, err := Execute(Spec{
		WorkspaceRoot:      workspace,
		WriteScope:         []string{link + string(filepath.Separator) + ".."},
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil {
		t.Fatal("expected symlink parent traversal write scope to fail")
	}
	if !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteAllowsMissingNestedWriteScopeInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	result, err := Execute(Spec{
		WorkspaceRoot:      workspace,
		WriteScope:         []string{filepath.Join("missing", "nested")},
		Argv:               []string{"go", "env", "GOOS"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}
}

func TestExecuteReturnsNonZeroEvidence(t *testing.T) {
	result, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		Argv:               []string{"go", "tool", "rdev-no-such-tool"},
		AllowCommands:      []string{"go"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     4096,
	})
	if err == nil {
		t.Fatal("expected nonzero command to fail")
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected nonzero exit code, got %d", result.ExitCode)
	}
	if result.ArtifactContent() == "" {
		t.Fatal("nonzero command should still produce artifact content")
	}
}

func TestArtifactContentIncludesSchemaAndPreservesNonSecretOutput(t *testing.T) {
	result := Result{
		Adapter:        "shell",
		Argv:           []string{"go", "env", "GOOS"},
		WorkspaceRoot:  t.TempDir(),
		ExitCode:       0,
		Stdout:         "darwin\n",
		Stderr:         "ok\n",
		StartedAt:      "2026-06-29T00:00:00Z",
		EndedAt:        "2026-06-29T00:00:01Z",
		DurationMillis: 1000,
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.SchemaVersion != ResultSchemaVersion {
		t.Fatalf("unexpected schema version %q", artifact.SchemaVersion)
	}
	if artifact.Stdout != result.Stdout || artifact.Stderr != result.Stderr {
		t.Fatalf("expected nonsecret output to be preserved, got stdout=%q stderr=%q", artifact.Stdout, artifact.Stderr)
	}
	if artifact.Redacted {
		t.Fatal("nonsecret artifact should not be marked redacted")
	}
	if len(artifact.RedactionCounts) != 0 {
		t.Fatalf("expected no redaction counts, got %#v", artifact.RedactionCounts)
	}
	if len(artifact.RedactionRules) == 0 {
		t.Fatal("redaction rules should be recorded")
	}
}

func TestArtifactContentRedactsSecretsAndReportsMetadata(t *testing.T) {
	openAIKey := "sk-" + "testsecret12345678901234567890"
	githubToken := "ghp_" + "abcdefghijklmnopqrstuvwxyz123456"
	bearer := "abc1234567890.token-value"
	awsKey := "AKIA" + "1234567890ABCDEF"
	result := Result{
		Adapter:       "shell",
		Argv:          []string{"tool", "password=hunter2", "token=cli-token-value"},
		WorkspaceRoot: t.TempDir(),
		ExitCode:      0,
		Stdout: strings.Join([]string{
			"OPENAI_API_KEY=" + openAIKey,
			"Authorization: Bearer " + bearer,
			`{"api_key":"json-secret-value"}`,
			"aws=" + awsKey,
		}, "\n"),
		Stderr:         "github token " + githubToken,
		StartedAt:      "2026-06-29T00:00:00Z",
		EndedAt:        "2026-06-29T00:00:01Z",
		DurationMillis: 1000,
	}
	content := result.ArtifactContent()
	for _, secret := range []string{openAIKey, githubToken, bearer, awsKey, "hunter2", "cli-token-value", "json-secret-value"} {
		if strings.Contains(content, secret) {
			t.Fatalf("artifact leaked secret %q in %s", secret, content)
		}
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(content), &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.Redacted {
		t.Fatal("secret artifact should be marked redacted")
	}
	for _, rule := range []string{"openai_api_key", "authorization_bearer", "github_token", "aws_access_key_id", "secret_assignment", "secret_json"} {
		if artifact.RedactionCounts[rule] == 0 {
			t.Fatalf("expected redaction count for %s, got %#v", rule, artifact.RedactionCounts)
		}
	}
	if !strings.Contains(content, "[REDACTED:openai_api_key]") {
		t.Fatalf("expected redaction marker in %s", content)
	}
}

func createSymlinkOrSkip(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation is not available: %v", err)
	}
	return link
}
