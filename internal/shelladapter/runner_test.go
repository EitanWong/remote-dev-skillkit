package shelladapter

import (
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
