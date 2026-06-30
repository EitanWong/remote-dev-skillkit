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
