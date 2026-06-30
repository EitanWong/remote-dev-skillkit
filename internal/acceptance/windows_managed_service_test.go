package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWindowsManagedServicePlanWritesPlanAndChecks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	plan, err := RunWindowsManagedServicePlan(WindowsManagedServiceOptions{
		OutDir:                   out,
		BinaryPath:               `C:\Program Files\rdev\rdev.exe`,
		GatewayURL:               "https://api.example.com/v1",
		TicketCode:               "ABCD-1234",
		ServiceName:              "RemoteDevSkillkitHost",
		WorkspaceLockStore:       `C:\ProgramData\rdev\workspace-locks`,
		ReleaseBundle:            `C:\Program Files\rdev\release-bundle.json`,
		ReleaseRootPublicKey:     "release-root:abc123",
		ReleaseRequiredArtifacts: []string{"rdev.exe", "rdev-host.exe", "rdev-verify.exe"},
		Now:                      time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != WindowsManagedServicePlanSchemaVersion {
		t.Fatalf("unexpected schema %q", plan.SchemaVersion)
	}
	if !allChecksPassed(plan.Checks) {
		t.Fatalf("expected all checks to pass: %#v", plan.Checks)
	}
	if _, err := os.Stat(filepath.Join(out, "windows-managed-service-plan.json")); err != nil {
		t.Fatalf("expected plan file: %v", err)
	}
	if plan.Service.StartType != "demand" {
		t.Fatalf("expected demand start, got %#v", plan.Service)
	}
	commands := joinedServiceCommands(plan.Commands)
	for _, expected := range []string{
		"sc.exe create RemoteDevSkillkitHost",
		"sc.exe query RemoteDevSkillkitHost",
		"sc.exe qc RemoteDevSkillkitHost",
		"rdev host service-control --platform windows --action start",
		"rdev host service-control --platform windows --action stop",
		"sc.exe delete RemoteDevSkillkitHost",
		"rdev acceptance verify-windows-managed-service",
	} {
		if !strings.Contains(commands, expected) {
			t.Fatalf("expected command containing %q in %s", expected, commands)
		}
	}
	args := strings.Join(plan.Service.Args, "\x00")
	for _, expected := range []string{
		"--mode\x00managed",
		"--once=false",
		"--release-bundle\x00C:\\Program Files\\rdev\\release-bundle.json",
		"--release-require-artifacts\x00rdev.exe,rdev-host.exe,rdev-verify.exe",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("expected arg %q in %#v", expected, plan.Service.Args)
		}
	}
}

func TestVerifyWindowsManagedServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	plan, err := RunWindowsManagedServicePlan(WindowsManagedServiceOptions{
		OutDir:               out,
		BinaryPath:           `C:\Program Files\rdev\rdev.exe`,
		GatewayURL:           "https://api.example.com/v1",
		TicketCode:           "ABCD-1234",
		ReleaseBundle:        `C:\Program Files\rdev\release-bundle.json`,
		ReleaseRootPublicKey: "release-root:abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyWindowsManagedServicePlan(filepath.Join(out, "windows-managed-service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}

	plan.Service.Commands = [][]string{{"New-Service", "RemoteDevSkillkitHost"}}
	content := mustMarshalJSONForTest(t, plan)
	if err := os.WriteFile(filepath.Join(out, "windows-managed-service-plan.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyWindowsManagedServicePlan(filepath.Join(out, "windows-managed-service-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected tampered plan verification to fail")
	}
	failed := failedCheckNames(verification.Checks)
	if !strings.Contains(failed, "sc_create_present") || !strings.Contains(failed, "no_policy_weakening_commands") {
		t.Fatalf("expected create and forbidden command failures, got %#v", verification.Checks)
	}
}

func TestRunWindowsManagedServicePlanRequiresReleaseGateForPassingChecks(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	plan, err := RunWindowsManagedServicePlan(WindowsManagedServiceOptions{
		OutDir:     out,
		BinaryPath: `C:\Program Files\rdev\rdev.exe`,
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

func mustMarshalJSONForTest(t *testing.T, value any) []byte {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(content, '\n')
}
