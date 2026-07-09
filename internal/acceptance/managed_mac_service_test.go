package acceptance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/service"
)

func TestRunManagedMacServicePlanWritesVerifiedPlist(t *testing.T) {
	out := filepath.Join(t.TempDir(), "service-plan")
	repo := t.TempDir()
	binaryPath := filepath.Join(t.TempDir(), "rdev")
	plan, err := RunManagedMacServicePlan(context.Background(), ManagedMacServiceOptions{
		RepoRoot:                 repo,
		OutDir:                   out,
		BinaryPath:               binaryPath,
		GatewayURL:               "https://api.example.com/v1",
		TicketCode:               "ABCD-1234",
		Label:                    "com.example.rdev-acceptance",
		LogDir:                   filepath.Join(out, "logs"),
		WorkspaceLockStore:       filepath.Join(out, "locks"),
		ReleaseBundle:            "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey:     "release-root:abc123",
		ReleaseRequiredArtifacts: []string{"rdev", "rdev-host", "rdev-verify"},
		Now:                      time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != ManagedMacServicePlanSchemaVersion {
		t.Fatalf("unexpected schema %q", plan.SchemaVersion)
	}
	if !allChecksPassed(plan.Checks) {
		t.Fatalf("expected service plan checks to pass: %#v", plan.Checks)
	}
	if _, err := os.Stat(filepath.Join(out, "service-plan.json")); err != nil {
		t.Fatalf("expected service plan file: %v", err)
	}
	status, err := service.InspectMacOSLaunchAgent(plan.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists || status.Label != "com.example.rdev-acceptance" {
		t.Fatalf("unexpected status %#v", status)
	}
	commands := joinedServiceCommands(plan.Commands)
	for _, expected := range []string{
		"rdev host service-control --platform macos --action start",
		"rdev host service-control --platform macos --action inspect",
		"rdev acceptance managed-mac",
		"rdev acceptance verify",
		"rdev host service-control --platform macos --action stop",
		"rdev host uninstall-service",
	} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("expected command containing %q in %s", expected, commands)
		}
	}
	args := strings.Join(plan.LaunchAgent.ProgramArguments, "\x00")
	if !strings.Contains(args, "--release-bundle\x00/opt/rdev/release-bundle.json") {
		t.Fatalf("expected release bundle in LaunchAgent args: %#v", plan.LaunchAgent.ProgramArguments)
	}
}

func TestVerifyManagedMacServicePlanRejectsMissingReleaseGate(t *testing.T) {
	out := filepath.Join(t.TempDir(), "service-plan")
	plan, err := RunManagedMacServicePlan(context.Background(), ManagedMacServiceOptions{
		RepoRoot:   t.TempDir(),
		OutDir:     out,
		BinaryPath: filepath.Join(t.TempDir(), "rdev"),
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
		Label:      "com.example.rdev-acceptance",
		Now:        time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyManagedMacServicePlan(filepath.Join(plan.OutDir, "service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected missing release gate to fail verification")
	}
	if !strings.Contains(failedCheckNames(verification.Checks), "release_bundle_arg") {
		t.Fatalf("expected release bundle failure: %#v", verification.Checks)
	}
}

func TestPackageManagedMacServiceEvidencePackagesAndRedacts(t *testing.T) {
	requireGitForAcceptanceTest(t)
	fakeCodex := writeFakeCodexForAcceptanceTest(t)
	fixture := writeManagedMacServicePackageFixture(t, fakeCodex, `{"ok": true, "token": "ghp_abcdefghijklmnopqrstuvwx"}`)

	pkg, err := PackageManagedMacServiceEvidence(ManagedMacServicePackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		ReviewTranscriptPath:    fixture.reviewTranscriptPath,
		StartTranscriptPath:     fixture.startTranscriptPath,
		InspectTranscriptPath:   fixture.inspectTranscriptPath,
		LogsPath:                fixture.logsPath,
		ReleaseGatePath:         fixture.releaseGatePath,
		AuditPath:               fixture.auditPath,
		ReconnectPath:           fixture.reconnectPath,
		ManagedReportPath:       fixture.managedReportPath,
		StopTranscriptPath:      fixture.stopTranscriptPath,
		UninstallTranscriptPath: fixture.uninstallTranscriptPath,
		Now:                     time.Date(2026, 6, 30, 19, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package checks to pass: %#v", pkg.Checks)
	}
	if pkg.SchemaVersion != ManagedMacServicePackageSchemaVersion {
		t.Fatalf("unexpected schema %q", pkg.SchemaVersion)
	}
	for _, path := range []string{
		filepath.Join(pkg.OutDir, "package.json"),
		filepath.Join(pkg.OutDir, "checksums.txt"),
		filepath.Join(pkg.OutDir, "plan", "service-plan.json"),
		filepath.Join(pkg.OutDir, "plan", "com.example.rdev-acceptance.plist"),
		filepath.Join(pkg.OutDir, "plan", "plan-verification.json"),
		filepath.Join(pkg.OutDir, "evidence", "start-transcript.txt"),
		filepath.Join(pkg.OutDir, "evidence", "managed-mac", "report.json"),
		filepath.Join(pkg.OutDir, "evidence", "managed-mac", "evidence", "manifest.json"),
		filepath.Join(pkg.OutDir, "evidence", "managed-mac", "side-effect-probe-evidence", "manifest.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected packaged file %s: %v", path, err)
		}
	}
	releaseEvidence, err := os.ReadFile(filepath.Join(pkg.OutDir, "evidence", "release-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(releaseEvidence), "ghp_abcdefghijklmnopqrstuvwx") || !strings.Contains(string(releaseEvidence), "[REDACTED:") {
		t.Fatalf("expected release gate evidence to be redacted, got %s", string(releaseEvidence))
	}
	if pkg.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", pkg.RedactionRuleCounts)
	}
}

func TestPackageManagedMacServiceEvidenceRejectsFailedManagedReport(t *testing.T) {
	requireGitForAcceptanceTest(t)
	fakeCodex := writeFakeCodexForAcceptanceTest(t)
	fixture := writeManagedMacServicePackageFixture(t, fakeCodex, `{"ok": true}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(fixture.managedReportPath), "evidence", "coding-result.json"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := PackageManagedMacServiceEvidence(ManagedMacServicePackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		ReviewTranscriptPath:    fixture.reviewTranscriptPath,
		StartTranscriptPath:     fixture.startTranscriptPath,
		InspectTranscriptPath:   fixture.inspectTranscriptPath,
		LogsPath:                fixture.logsPath,
		ReleaseGatePath:         fixture.releaseGatePath,
		AuditPath:               fixture.auditPath,
		ReconnectPath:           fixture.reconnectPath,
		ManagedReportPath:       fixture.managedReportPath,
		StopTranscriptPath:      fixture.stopTranscriptPath,
		UninstallTranscriptPath: fixture.uninstallTranscriptPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatalf("expected tampered managed report to fail package checks")
	}
	if !strings.Contains(failedCheckNames(pkg.Checks), "managed_report_verified") {
		t.Fatalf("expected managed_report_verified failure: %#v", pkg.Checks)
	}
}

type managedMacServicePackageFixture struct {
	root                    string
	planPath                string
	reviewTranscriptPath    string
	startTranscriptPath     string
	inspectTranscriptPath   string
	logsPath                string
	releaseGatePath         string
	auditPath               string
	reconnectPath           string
	managedReportPath       string
	stopTranscriptPath      string
	uninstallTranscriptPath string
}

func writeManagedMacServicePackageFixture(t *testing.T, fakeCodex, releaseGate string) managedMacServicePackageFixture {
	t.Helper()
	root := t.TempDir()
	planOut := filepath.Join(root, "plan")
	if _, err := RunManagedMacServicePlan(context.Background(), ManagedMacServiceOptions{
		RepoRoot:                 t.TempDir(),
		OutDir:                   planOut,
		BinaryPath:               filepath.Join(root, "rdev"),
		GatewayURL:               "https://api.example.com/v1",
		TicketCode:               "ABCD-1234",
		Label:                    "com.example.rdev-acceptance",
		ReleaseBundle:            "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey:     "release-root:abc123",
		ReleaseRequiredArtifacts: []string{"rdev", "rdev-host", "rdev-verify"},
		WorkspaceLockStore:       filepath.Join(root, "locks"),
		LogDir:                   filepath.Join(root, "logs"),
	}); err != nil {
		t.Fatal(err)
	}
	managedOut := filepath.Join(root, "managed-mac-run")
	if _, err := RunManagedMac(context.Background(), ManagedMacOptions{
		OutDir:       managedOut,
		CodexCommand: fakeCodex,
	}); err != nil {
		t.Fatal(err)
	}
	reviewTranscriptPath := filepath.Join(root, "review.txt")
	startTranscriptPath := filepath.Join(root, "start.txt")
	inspectTranscriptPath := filepath.Join(root, "inspect.txt")
	logsPath := filepath.Join(root, "logs.txt")
	releaseGatePath := filepath.Join(root, "release-gate.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	reconnectPath := filepath.Join(root, "reconnect.txt")
	stopTranscriptPath := filepath.Join(root, "stop.txt")
	uninstallTranscriptPath := filepath.Join(root, "uninstall.txt")
	writeFileForManagedMacServiceTest(t, reviewTranscriptPath, "plutil -lint com.example.rdev-acceptance.plist\nOK\n")
	writeFileForManagedMacServiceTest(t, startTranscriptPath, "rdev host service-control --platform macos --action start --execute\n")
	writeFileForManagedMacServiceTest(t, inspectTranscriptPath, "rdev host service-control --platform macos --action inspect --execute\nstate = running\n")
	writeFileForManagedMacServiceTest(t, logsPath, "managed host log release gate passed\n")
	writeFileForManagedMacServiceTest(t, releaseGatePath, releaseGate+"\n")
	writeFileForManagedMacServiceTest(t, auditPath, `{"event":"host.registered"}`+"\n"+`{"event":"task.completed"}`+"\n")
	writeFileForManagedMacServiceTest(t, reconnectPath, "logout/login complete; host hst_123 reconnected\n")
	writeFileForManagedMacServiceTest(t, stopTranscriptPath, "rdev host service-control --platform macos --action stop --execute\n")
	writeFileForManagedMacServiceTest(t, uninstallTranscriptPath, "rdev host uninstall-service --platform macos --removed true\n")
	return managedMacServicePackageFixture{
		root:                    root,
		planPath:                filepath.Join(planOut, "service-plan.json"),
		reviewTranscriptPath:    reviewTranscriptPath,
		startTranscriptPath:     startTranscriptPath,
		inspectTranscriptPath:   inspectTranscriptPath,
		logsPath:                logsPath,
		releaseGatePath:         releaseGatePath,
		auditPath:               auditPath,
		reconnectPath:           reconnectPath,
		managedReportPath:       filepath.Join(managedOut, "report.json"),
		stopTranscriptPath:      stopTranscriptPath,
		uninstallTranscriptPath: uninstallTranscriptPath,
	}
}

func requireGitForAcceptanceTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for managed Mac acceptance fixtures")
	}
}

func writeFakeCodexForAcceptanceTest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-codex")
	content := "#!/bin/sh\ncat > README.md <<'EOF'\n# rdev acceptance fixture\n\nChanged by managed Mac service acceptance.\nEOF\necho fake codex acceptance run\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFileForManagedMacServiceTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func joinedServiceCommands(commands []ServiceCommand) string {
	var builder strings.Builder
	for _, command := range commands {
		builder.WriteString(command.Shell)
		builder.WriteByte('\n')
	}
	return builder.String()
}
