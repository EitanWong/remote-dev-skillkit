package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWindowsTemporaryPlanWritesLauncherAndChecks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:                 out,
		GatewayURL:             "https://api.example.com/v1",
		TicketCode:             "ABCD-1234",
		DownloadURL:            "https://agent.example.com/rdev-host.exe",
		ExpectedSHA256:         strings.Repeat("a", 64),
		BootstrapScriptPath:    script,
		ManifestURL:            "https://agent.example.com/j/ABCD-1234/manifest",
		ManifestRootPublicKey:  "manifest-root:abc",
		ReleaseManifestURL:     "https://agent.example.com/rdev-host.exe.rdev-release.json",
		ReleaseRootPublicKey:   "release-root:abc",
		VerifierDownloadURL:    "https://agent.example.com/rdev-verify.exe",
		VerifierExpectedSHA256: strings.Repeat("b", 64),
		TrustPin:               "sha256:" + strings.Repeat("c", 64),
		HostName:               "acceptance-windows",
		Now:                    time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != WindowsTemporaryPlanSchemaVersion {
		t.Fatalf("unexpected schema %q", plan.SchemaVersion)
	}
	if !allChecksPassed(plan.Checks) {
		t.Fatalf("expected all checks to pass: %#v", plan.Checks)
	}
	if _, err := os.Stat(filepath.Join(out, "windows-temporary-plan.json")); err != nil {
		t.Fatalf("expected plan file: %v", err)
	}
	launcher, err := os.ReadFile(plan.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	launcherText := string(launcher)
	for _, expected := range []string{
		"-GatewayUrl 'https://api.example.com/v1'",
		"-TicketCode 'ABCD-1234'",
		"-ReleaseManifestUrl 'https://agent.example.com/rdev-host.exe.rdev-release.json'",
		"-VerifierDownloadUrl 'https://agent.example.com/rdev-verify.exe'",
	} {
		if !strings.Contains(launcherText, expected) {
			t.Fatalf("expected launcher to contain %q:\n%s", expected, launcherText)
		}
	}
	commands := joinedWindowsCommands(plan.Commands) + joinedWindowsCommands(plan.NoPersistenceChecks)
	for _, expected := range []string{
		"powershell.exe -NoProfile -File",
		"Get-Service",
		"Get-ScheduledTask",
		"CurrentVersion\\Run",
		"Get-NetFirewallRule",
	} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("expected command containing %q in %s", expected, commands)
		}
	}
	if len(plan.ApprovalProbes) < 4 {
		t.Fatalf("expected approval probes: %#v", plan.ApprovalProbes)
	}
}

func TestVerifyWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:                 out,
		GatewayURL:             "https://api.example.com/v1",
		TicketCode:             "ABCD-1234",
		DownloadURL:            "https://agent.example.com/rdev-host.exe",
		ExpectedSHA256:         strings.Repeat("a", 64),
		BootstrapScriptPath:    script,
		ReleaseManifestURL:     "https://agent.example.com/rdev-host.exe.rdev-release.json",
		ReleaseRootPublicKey:   "release-root:abc",
		VerifierDownloadURL:    "https://agent.example.com/rdev-verify.exe",
		VerifierExpectedSHA256: strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyWindowsTemporaryPlan(filepath.Join(out, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}

	if err := os.WriteFile(plan.LauncherPath, []byte("New-Service rdev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyWindowsTemporaryPlan(filepath.Join(out, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected tampered launcher verification to fail")
	}
	if !strings.Contains(failedCheckNames(verification.Checks), "launcher_has_no_forbidden_side_effects") {
		t.Fatalf("expected forbidden side-effect failure: %#v", verification.Checks)
	}
}

func TestVerifyWindowsTemporaryPlanWithRemoteBootstrapPin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	missingScript := filepath.Join(t.TempDir(), "missing-windows-temporary.ps1")
	if _, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:                        out,
		GatewayURL:                    "https://api.example.com/v1",
		TicketCode:                    "ABCD-1234",
		DownloadURL:                   "https://agent.example.com/rdev-host.exe",
		ExpectedSHA256:                strings.Repeat("a", 64),
		BootstrapScriptPath:           missingScript,
		BootstrapScriptURL:            "https://agent.example.com/windows-temporary.ps1",
		BootstrapScriptExpectedSHA256: strings.Repeat("c", 64),
		ReleaseManifestURL:            "https://agent.example.com/rdev-host.exe.rdev-release.json",
		ReleaseRootPublicKey:          "release-root:abc",
		VerifierDownloadURL:           "https://agent.example.com/rdev-verify.exe",
		VerifierExpectedSHA256:        strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyWindowsTemporaryPlan(filepath.Join(out, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok with remote bootstrap pin: %#v", verification.Checks)
	}
}

func joinedWindowsCommands(commands []WindowsAcceptanceCommand) string {
	var builder strings.Builder
	for _, command := range commands {
		builder.WriteString(command.Shell)
		builder.WriteByte('\n')
	}
	return builder.String()
}
