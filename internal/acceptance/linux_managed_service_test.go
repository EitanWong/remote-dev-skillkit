package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunLinuxManagedServicePlanWritesUnitAndChecks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	plan, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:                   out,
		BinaryPath:               "/opt/rdev/rdev",
		GatewayURL:               "https://api.example.com/v1",
		TicketCode:               "ABCD-1234",
		UnitName:                 "rdev-host.service",
		WorkspaceLockStore:       "/var/lib/rdev/workspace-locks",
		ReleaseBundle:            "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey:     "release-root:abc123",
		ReleaseRequiredArtifacts: []string{"rdev", "rdev-host", "rdev-verify"},
		Now:                      time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != LinuxManagedServicePlanSchemaVersion {
		t.Fatalf("unexpected schema %q", plan.SchemaVersion)
	}
	if !allChecksPassed(plan.Checks) {
		t.Fatalf("expected all checks to pass: %#v", plan.Checks)
	}
	for _, path := range []string{filepath.Join(out, "linux-managed-service-plan.json"), filepath.Join(out, "rdev-host.service")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	if plan.Unit.Restart != "on-failure" || !plan.Unit.NoNewPrivileges || !plan.Unit.PrivateTmp {
		t.Fatalf("expected hardened restart policy, got %#v", plan.Unit)
	}
	commands := joinedServiceCommands(plan.Commands)
	for _, expected := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now rdev-host.service",
		"systemctl --user status rdev-host.service",
		"journalctl --user -u 'rdev-host.service'",
		"rdev host service-control --platform linux --action stop",
		"rdev host uninstall-service --platform linux",
		"rdev acceptance verify-linux-managed-service",
	} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("expected command containing %q in %s", expected, commands)
		}
	}
	args := strings.Join(plan.Unit.ExecStart, "\x00")
	for _, expected := range []string{
		"--mode\x00managed",
		"--once=false",
		"--release-bundle\x00/opt/rdev/release-bundle.json",
		"--release-require-artifacts\x00rdev,rdev-host,rdev-verify",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("expected arg %q in %#v", expected, plan.Unit.ExecStart)
		}
	}
}

func TestVerifyLinuxManagedServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	plan, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:               out,
		BinaryPath:           "/opt/rdev/rdev",
		GatewayURL:           "https://api.example.com/v1",
		TicketCode:           "ABCD-1234",
		ReleaseBundle:        "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey: "release-root:abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyLinuxManagedServicePlan(filepath.Join(out, "linux-managed-service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}

	plan.Start.Commands = [][]string{{"sudo", "systemctl", "enable", "--now", "remote-dev-skillkit-host.service"}}
	content := mustMarshalJSONForTest(t, plan)
	if err := os.WriteFile(filepath.Join(out, "linux-managed-service-plan.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyLinuxManagedServicePlan(filepath.Join(out, "linux-managed-service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected tampered plan verification to fail")
	}
	failed := failedCheckNames(verification.Checks)
	if !strings.Contains(failed, "systemctl_daemon_reload_present") || !strings.Contains(failed, "no_policy_weakening_commands") {
		t.Fatalf("expected systemctl and forbidden command failures, got %#v", verification.Checks)
	}
}

func TestRunLinuxManagedServicePlanRequiresReleaseGateForPassingChecks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	plan, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:     out,
		BinaryPath: "/opt/rdev/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if allChecksPassed(plan.Checks) {
		t.Fatalf("expected missing release gate to fail acceptance checks")
	}
	if !strings.Contains(failedCheckNames(plan.Checks), "release_bundle_arg") {
		t.Fatalf("expected release bundle failure: %#v", plan.Checks)
	}
}

func TestVerifyLinuxManagedServicePlanRejectsMissingUnitFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	_, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:               out,
		BinaryPath:           "/opt/rdev/rdev",
		GatewayURL:           "https://api.example.com/v1",
		TicketCode:           "ABCD-1234",
		ReleaseBundle:        "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey: "release-root:abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(out, "remote-dev-skillkit-host.service")); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyLinuxManagedServicePlan(filepath.Join(out, "linux-managed-service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected missing unit file verification to fail")
	}
	if !strings.Contains(failedCheckNames(verification.Checks), "unit_file_exists") {
		t.Fatalf("expected unit_file_exists failure: %#v", verification.Checks)
	}
}

func TestVerifyLinuxManagedServicePlanRejectsTamperedUnitFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	_, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:               out,
		BinaryPath:           "/opt/rdev/rdev",
		GatewayURL:           "https://api.example.com/v1",
		TicketCode:           "ABCD-1234",
		ReleaseBundle:        "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey: "release-root:abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(out, "remote-dev-skillkit-host.service")
	tampered := []byte("[Unit]\nDescription=tampered\n\n[Service]\nType=simple\nExecStart=/opt/rdev/rdev host serve --mode temporary\nRestart=on-failure\nRestartSec=5s\nNoNewPrivileges=true\nPrivateTmp=true\n\n[Install]\nWantedBy=default.target\n")
	if err := os.WriteFile(unitPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyLinuxManagedServicePlan(filepath.Join(out, "linux-managed-service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected tampered unit verification to fail")
	}
	failed := failedCheckNames(verification.Checks)
	if !strings.Contains(failed, "unit_exec_managed") || !strings.Contains(failed, "unit_exec_release_gate") {
		t.Fatalf("expected unit exec failures, got %#v", verification.Checks)
	}
}

func TestRunLinuxManagedServicePlanRejectsRelativeBinary(t *testing.T) {
	_, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:     filepath.Join(t.TempDir(), "linux-managed-service"),
		BinaryPath: "rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute binary error, got %v", err)
	}
}

func TestPackageLinuxManagedServiceEvidencePackagesAndRedacts(t *testing.T) {
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	fixture := writeLinuxManagedServicePackageFixture(t, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	pkg, err := PackageLinuxManagedServiceEvidence(LinuxManagedServicePackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		StartTranscriptPath:     fixture.startTranscriptPath,
		StatusTranscriptPath:    fixture.statusTranscriptPath,
		LogsPath:                fixture.logsPath,
		ReleaseGatePath:         fixture.releaseGatePath,
		AuditPath:               fixture.auditPath,
		ReconnectPath:           fixture.reconnectPath,
		SessionEvidenceDir:      fixture.sessionEvidenceDir,
		StopTranscriptPath:      fixture.stopTranscriptPath,
		UninstallTranscriptPath: fixture.uninstallTranscriptPath,
		Now:                     time.Date(2026, 6, 30, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package checks to pass: %#v", pkg.Checks)
	}
	if pkg.SchemaVersion != LinuxManagedServicePackageSchemaVersion {
		t.Fatalf("unexpected schema %q", pkg.SchemaVersion)
	}
	for _, path := range []string{
		filepath.Join(pkg.OutDir, "package.json"),
		filepath.Join(pkg.OutDir, "checksums.txt"),
		filepath.Join(pkg.OutDir, "plan", "linux-managed-service-plan.json"),
		filepath.Join(pkg.OutDir, "plan", "remote-dev-skillkit-host.service"),
		filepath.Join(pkg.OutDir, "plan", "plan-verification.json"),
		filepath.Join(pkg.OutDir, "evidence", "start-transcript.txt"),
		filepath.Join(pkg.OutDir, "evidence", "session-evidence", "manifest.json"),
		filepath.Join(pkg.OutDir, "evidence", "session-evidence", "artifacts", "host-denial.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected packaged file %s: %v", path, err)
		}
	}
	releaseEvidence, err := os.ReadFile(filepath.Join(pkg.OutDir, "evidence", "release-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(releaseEvidence), fakeGitHubToken) || !strings.Contains(string(releaseEvidence), "[REDACTED:") {
		t.Fatalf("expected release gate evidence to be redacted, got %s", string(releaseEvidence))
	}
	if pkg.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", pkg.RedactionRuleCounts)
	}
}

func TestPackageLinuxManagedServiceEvidenceRejectsFailedReleaseGate(t *testing.T) {
	fixture := writeLinuxManagedServicePackageFixture(t, `{"ok": false}`)

	pkg, err := PackageLinuxManagedServiceEvidence(LinuxManagedServicePackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		StartTranscriptPath:     fixture.startTranscriptPath,
		StatusTranscriptPath:    fixture.statusTranscriptPath,
		LogsPath:                fixture.logsPath,
		ReleaseGatePath:         fixture.releaseGatePath,
		AuditPath:               fixture.auditPath,
		ReconnectPath:           fixture.reconnectPath,
		SessionEvidenceDir:      fixture.sessionEvidenceDir,
		StopTranscriptPath:      fixture.stopTranscriptPath,
		UninstallTranscriptPath: fixture.uninstallTranscriptPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatalf("expected failed release gate to fail package checks")
	}
	if !strings.Contains(failedCheckNames(pkg.Checks), "release_gate_ok") {
		t.Fatalf("expected release_gate_ok failure: %#v", pkg.Checks)
	}
}

func TestPackageLinuxManagedServiceEvidenceRequiresHostDenialProof(t *testing.T) {
	fixture := writeLinuxManagedServicePackageFixture(t, `{"ok": true}`)
	denialArtifact := filepath.Join(fixture.sessionEvidenceDir, "artifacts", "host-denial.json")
	if err := os.WriteFile(denialArtifact, []byte(`{"schema_version":"rdev.shell-result.v1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	pkg, err := PackageLinuxManagedServiceEvidence(LinuxManagedServicePackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  filepath.Join(fixture.root, "package"),
		StartTranscriptPath:     fixture.startTranscriptPath,
		StatusTranscriptPath:    fixture.statusTranscriptPath,
		LogsPath:                fixture.logsPath,
		ReleaseGatePath:         fixture.releaseGatePath,
		AuditPath:               fixture.auditPath,
		ReconnectPath:           fixture.reconnectPath,
		SessionEvidenceDir:      fixture.sessionEvidenceDir,
		StopTranscriptPath:      fixture.stopTranscriptPath,
		UninstallTranscriptPath: fixture.uninstallTranscriptPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatalf("expected missing host-denial evidence to fail package checks")
	}
	if !strings.Contains(failedCheckNames(pkg.Checks), "host_denial_probe_evidence_present") {
		t.Fatalf("expected host-denial evidence failure: %#v", pkg.Checks)
	}
}

type linuxManagedServicePackageFixture struct {
	root                    string
	planPath                string
	startTranscriptPath     string
	statusTranscriptPath    string
	logsPath                string
	releaseGatePath         string
	auditPath               string
	reconnectPath           string
	sessionEvidenceDir      string
	stopTranscriptPath      string
	uninstallTranscriptPath string
}

func writeLinuxManagedServicePackageFixture(t *testing.T, releaseGate string) linuxManagedServicePackageFixture {
	t.Helper()
	root := t.TempDir()
	planOut := filepath.Join(root, "plan")
	if _, err := RunLinuxManagedServicePlan(LinuxManagedServiceOptions{
		OutDir:               planOut,
		BinaryPath:           "/opt/rdev/rdev",
		GatewayURL:           "https://api.example.com/v1",
		TicketCode:           "ABCD-1234",
		ReleaseBundle:        "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey: "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	startTranscriptPath := filepath.Join(root, "start.txt")
	statusTranscriptPath := filepath.Join(root, "status.txt")
	logsPath := filepath.Join(root, "logs.txt")
	releaseGatePath := filepath.Join(root, "release-gate.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	reconnectPath := filepath.Join(root, "reconnect.txt")
	stopTranscriptPath := filepath.Join(root, "stop.txt")
	uninstallTranscriptPath := filepath.Join(root, "uninstall.txt")
	sessionEvidenceDir := filepath.Join(root, "session-evidence")
	writeFileForLinuxPackageTest(t, startTranscriptPath, "systemctl --user daemon-reload\nsystemctl --user enable --now remote-dev-skillkit-host.service\n")
	writeFileForLinuxPackageTest(t, statusTranscriptPath, "systemctl --user status remote-dev-skillkit-host.service\nactive (running)\n")
	writeFileForLinuxPackageTest(t, logsPath, "journalctl --user -u remote-dev-skillkit-host.service\nrelease gate passed\n")
	writeFileForLinuxPackageTest(t, releaseGatePath, releaseGate+"\n")
	writeFileForLinuxPackageTest(t, auditPath, `{"event":"session.joined"}`+"\n"+`{"event":"task.completed"}`+"\n")
	writeFileForLinuxPackageTest(t, reconnectPath, "rebooted host reconnected as hst_123\n")
	writeFileForLinuxPackageTest(t, stopTranscriptPath, "systemctl --user disable --now remote-dev-skillkit-host.service\n")
	writeFileForLinuxPackageTest(t, uninstallTranscriptPath, "rdev host uninstall-service --platform linux --removed true\n")
	writeFileForLinuxPackageTest(t, filepath.Join(sessionEvidenceDir, "manifest.json"), `{"schema_version":"rdev.session-evidence.v1"}`+"\n")
	writeFileForLinuxPackageTest(t, filepath.Join(sessionEvidenceDir, "artifacts", "host-denial.json"), `{"schema_version":"rdev.host-denial.v1"}`+"\n")
	return linuxManagedServicePackageFixture{
		root:                    root,
		planPath:                filepath.Join(planOut, "linux-managed-service-plan.json"),
		startTranscriptPath:     startTranscriptPath,
		statusTranscriptPath:    statusTranscriptPath,
		logsPath:                logsPath,
		releaseGatePath:         releaseGatePath,
		auditPath:               auditPath,
		reconnectPath:           reconnectPath,
		sessionEvidenceDir:      sessionEvidenceDir,
		stopTranscriptPath:      stopTranscriptPath,
		uninstallTranscriptPath: uninstallTranscriptPath,
	}
}

func writeFileForLinuxPackageTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
