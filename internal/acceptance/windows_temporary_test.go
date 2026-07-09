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
	if len(plan.DenialProbes) < 4 {
		t.Fatalf("expected denial probes: %#v", plan.DenialProbes)
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

func TestVerifyWindowsTemporaryPlanWithReleaseBundle(t *testing.T) {
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
		ReleaseBundleURL:       "https://agent.example.com/release-bundle.json",
		ReleaseRootPublicKey:   "release-root:abc",
		VerifierDownloadURL:    "https://agent.example.com/rdev-verify.exe",
		VerifierExpectedSHA256: strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !allChecksPassed(plan.Checks) {
		t.Fatalf("expected all checks to pass: %#v", plan.Checks)
	}
	if plan.ReleaseManifestURL != "" {
		t.Fatalf("expected bundle-only plan to omit release manifest, got %q", plan.ReleaseManifestURL)
	}
	if plan.ReleaseBundleRequiredArtifacts != "rdev-host.exe,rdev-verify.exe" {
		t.Fatalf("unexpected required artifacts %q", plan.ReleaseBundleRequiredArtifacts)
	}
	launcherBytes, err := os.ReadFile(plan.LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	launcher := string(launcherBytes)
	for _, expected := range []string{
		"-ReleaseBundleUrl 'https://agent.example.com/release-bundle.json'",
		"-ReleaseBundleRequiredArtifacts 'rdev-host.exe,rdev-verify.exe'",
	} {
		if !strings.Contains(launcher, expected) {
			t.Fatalf("expected launcher to contain %q:\n%s", expected, launcher)
		}
	}
	if strings.Contains(launcher, "-ReleaseManifestUrl") {
		t.Fatalf("bundle-only launcher should not include release manifest:\n%s", launcher)
	}
	verification, err := VerifyWindowsTemporaryPlan(filepath.Join(out, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
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

func TestPackageWindowsTemporaryEvidencePackagesAndRedacts(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true, "token": "sk-abcdefghijklmnop"}`)

	pkg, err := PackageWindowsTemporaryEvidence(WindowsTemporaryPackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		TranscriptPath:          fixture.transcriptPath,
		ReleaseVerificationPath: fixture.releaseVerificationPath,
		AuditPath:               fixture.auditPath,
		NoPersistenceDir:        fixture.noPersistenceDir,
		DenialProbesDir:         fixture.denialProbesDir,
		Now:                     time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package checks to pass: %#v", pkg.Checks)
	}
	if pkg.SchemaVersion != WindowsTemporaryPackageSchemaVersion {
		t.Fatalf("unexpected schema %q", pkg.SchemaVersion)
	}
	for _, path := range []string{
		filepath.Join(pkg.OutDir, "package.json"),
		filepath.Join(pkg.OutDir, "checksums.txt"),
		filepath.Join(pkg.OutDir, "plan", "windows-temporary-plan.json"),
		filepath.Join(pkg.OutDir, "plan", "run-windows-temporary.ps1"),
		filepath.Join(pkg.OutDir, "evidence", "no-persistence", "scheduled-tasks.txt"),
		filepath.Join(pkg.OutDir, "evidence", "denial-probes", "package-install.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected packaged file %s: %v", path, err)
		}
	}
	releaseEvidence, err := os.ReadFile(filepath.Join(pkg.OutDir, "evidence", "release-verification.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(releaseEvidence), "sk-abcdefghijklmnop") || !strings.Contains(string(releaseEvidence), "[REDACTED:") {
		t.Fatalf("expected release verification evidence to be redacted, got %s", string(releaseEvidence))
	}
	if len(pkg.RedactionRuleCounts) == 0 {
		t.Fatalf("expected redaction counts, got %#v", pkg.RedactionRuleCounts)
	}
}

func TestPackageWindowsTemporaryEvidenceRejectsFailedReleaseVerification(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": false}`)

	pkg, err := PackageWindowsTemporaryEvidence(WindowsTemporaryPackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		TranscriptPath:          fixture.transcriptPath,
		ReleaseVerificationPath: fixture.releaseVerificationPath,
		AuditPath:               fixture.auditPath,
		NoPersistenceDir:        fixture.noPersistenceDir,
		DenialProbesDir:         fixture.denialProbesDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatalf("expected failed release verification to fail package checks")
	}
	if !strings.Contains(failedCheckNames(pkg.Checks), "release_verification_ok") {
		t.Fatalf("expected release_verification_ok failure: %#v", pkg.Checks)
	}
	if len(pkg.RecommendedActions) == 0 {
		t.Fatalf("expected recommended actions for failed package")
	}
}

type windowsTemporaryPackageFixture struct {
	root                    string
	planPath                string
	transcriptPath          string
	releaseVerificationPath string
	auditPath               string
	noPersistenceDir        string
	denialProbesDir         string
}

func writeWindowsTemporaryPackageFixture(t *testing.T, releaseVerification string) windowsTemporaryPackageFixture {
	t.Helper()
	root := t.TempDir()
	script := filepath.Join(root, "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	planOut := filepath.Join(root, "plan")
	if _, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:                 planOut,
		GatewayURL:             "https://api.example.com/v1",
		TicketCode:             "ABCD-1234",
		DownloadURL:            "https://agent.example.com/rdev-host.exe",
		ExpectedSHA256:         strings.Repeat("a", 64),
		BootstrapScriptPath:    script,
		ReleaseBundleURL:       "https://agent.example.com/release-bundle.json",
		ReleaseRootPublicKey:   "release-root:abc",
		VerifierDownloadURL:    "https://agent.example.com/rdev-verify.exe",
		VerifierExpectedSHA256: strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(root, "transcript.txt")
	releaseVerificationPath := filepath.Join(root, "rdev-verify.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	noPersistenceDir := filepath.Join(root, "no-persistence")
	denialProbesDir := filepath.Join(root, "denial-probes")
	if err := os.WriteFile(transcriptPath, []byte("temporary host transcript\nAuthorization: Bearer abcdefghijklmnop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseVerificationPath, []byte(releaseVerification+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, []byte(`{"event":"session.joined"}`+"\n"+`{"event":"task.completed"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEvidenceFiles(t, noPersistenceDir, []string{
		"services.txt",
		"scheduled_tasks.txt",
		"hkcu_run_key.txt",
		"hklm_run_key.txt",
		"startup_folders.txt",
		"firewall_rules.txt",
	})
	writeEvidenceFiles(t, denialProbesDir, []string{
		"package.install.txt",
		"elevation.request.txt",
		"service.manage.txt",
		"gui.control.txt",
		"credential.change.txt",
	})
	return windowsTemporaryPackageFixture{
		root:                    root,
		planPath:                filepath.Join(planOut, "windows-temporary-plan.json"),
		transcriptPath:          transcriptPath,
		releaseVerificationPath: releaseVerificationPath,
		auditPath:               auditPath,
		noPersistenceDir:        noPersistenceDir,
		denialProbesDir:         denialProbesDir,
	}
}

func writeEvidenceFiles(t *testing.T, dir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+" ok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
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
