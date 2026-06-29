package powershelladapter

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
)

func TestExecuteRunsAllowlistedPowerShellCommand(t *testing.T) {
	fakePowerShell := buildPowerShellAdapterHelper(t, `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println(strings.Join(os.Args[1:], "|"))
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		PowerShellCommand:  fakePowerShell,
		Command:            `Write-Output "hello"`,
		AllowCommands:      []string{fakePowerShell},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "-NoProfile|-NonInteractive|-Command|Write-Output") {
		t.Fatalf("expected PowerShell argv evidence, got %q", result.Stdout)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.SchemaVersion != ResultSchemaVersion {
		t.Fatalf("unexpected schema version %q", artifact.SchemaVersion)
	}
	if artifact.Adapter != "powershell" {
		t.Fatalf("unexpected adapter %q", artifact.Adapter)
	}
}

func TestExecuteRejectsMissingCommand(t *testing.T) {
	_, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		PowerShellCommand:  "pwsh",
		AllowCommands:      []string{"pwsh"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("expected missing command error, got %v", err)
	}
}

func TestExecuteRejectsNonAllowlistedPowerShellExecutable(t *testing.T) {
	fakePowerShell := buildPowerShellAdapterHelper(t, `package main

func main() {}
`)
	_, err := Execute(Spec{
		WorkspaceRoot:      t.TempDir(),
		PowerShellCommand:  fakePowerShell,
		Command:            "Get-ChildItem",
		AllowCommands:      []string{"pwsh"},
		MaxDurationSeconds: 10,
		MaxOutputBytes:     1024,
	})
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("expected allowlist error, got %v", err)
	}
}

func TestExecuteContextCancelsRunningPowerShellCommand(t *testing.T) {
	fakePowerShell := buildPowerShellAdapterHelper(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := ExecuteContext(ctx, Spec{
		WorkspaceRoot:      t.TempDir(),
		PowerShellCommand:  fakePowerShell,
		Command:            "Start-Sleep -Seconds 5",
		AllowCommands:      []string{fakePowerShell},
		MaxDurationSeconds: 30,
		MaxOutputBytes:     1024,
	})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected canceled command error, got %v", err)
	}
	if !result.Canceled {
		t.Fatalf("expected canceled result, got %#v", result)
	}
	if result.TimedOut {
		t.Fatalf("canceled command should not be marked timed out: %#v", result)
	}
	var artifact ResultArtifact
	if err := json.Unmarshal([]byte(result.ArtifactContent()), &artifact); err != nil {
		t.Fatal(err)
	}
	if !artifact.Canceled {
		t.Fatalf("expected canceled artifact, got %#v", artifact)
	}
}

func TestArtifactRedactsPowerShellSecrets(t *testing.T) {
	secret := "sk-" + "testsecret12345678901234567890"
	result := Result{
		Adapter:           "powershell",
		PowerShellCommand: "pwsh",
		Command:           "Write-Output " + secret,
		Argv:              []string{"pwsh", "-Command", "Write-Output " + secret},
		WorkspaceRoot:     t.TempDir(),
		ExitCode:          0,
		Stdout:            "token=" + secret,
		StartedAt:         "2026-06-30T00:00:00Z",
		EndedAt:           "2026-06-30T00:00:01Z",
		DurationMillis:    1000,
	}
	content := result.ArtifactContent()
	if strings.Contains(content, secret) {
		t.Fatalf("artifact leaked secret: %s", content)
	}
	if !strings.Contains(content, "[REDACTED:openai_api_key]") {
		t.Fatalf("expected redaction marker, got %s", content)
	}
}

func buildPowerShellAdapterHelper(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(dir, "fake-powershell")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake PowerShell binary: %v\n%s", err, string(output))
	}
	return binaryPath
}
