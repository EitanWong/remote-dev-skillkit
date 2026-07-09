package service

import (
	"strings"
	"testing"
)

func TestNewWindowsServiceBuildsManagedHostCommands(t *testing.T) {
	service, err := NewWindowsService(WindowsServiceOptions{
		ServiceName:              "RemoteDevSkillkitHost",
		DisplayName:              "Remote Dev Skillkit Host",
		BinaryPath:               `C:\Program Files\rdev\rdev.exe`,
		GatewayURL:               "https://api.example.com/v1",
		TicketCode:               "ABCD-1234",
		IdentityStorePath:        `C:\ProgramData\rdev\identity.json`,
		TrustStorePath:           `C:\ProgramData\rdev\trust.json`,
		WorkspaceLockStorePath:   `C:\ProgramData\rdev\workspace-locks`,
		ReleaseBundlePath:        `C:\Program Files\rdev\release-bundle.json`,
		ReleaseRootPublicKey:     "release-root:abc123",
		ReleaseRequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joinedArgs := strings.Join(service.Args, "\x00")
	for _, expected := range []string{
		`C:\Program Files\rdev\rdev.exe`,
		"host",
		"serve",
		"--mode",
		"managed",
		"--gateway",
		"https://api.example.com/v1",
		"--ticket-code",
		"ABCD-1234",
		"--once=false",
		"--transport",
		"long-poll",
		"--identity-store",
		`C:\ProgramData\rdev\identity.json`,
		"--trust-store",
		`C:\ProgramData\rdev\trust.json`,
		"--workspace-lock-store",
		`C:\ProgramData\rdev\workspace-locks`,
		"--release-bundle",
		`C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key",
		"release-root:abc123",
		"--release-require-artifacts",
		"rdev-host.exe,rdev-verify.exe",
	} {
		if !strings.Contains(joinedArgs, expected) {
			t.Fatalf("expected argument %q in %#v", expected, service.Args)
		}
	}
	if service.ServiceName != "RemoteDevSkillkitHost" || service.StartType != "demand" {
		t.Fatalf("unexpected service identity: %#v", service)
	}
	if len(service.Commands) != 2 || service.Commands[0][0] != "sc.exe" || service.Commands[0][1] != "create" {
		t.Fatalf("expected sc.exe create command, got %#v", service.Commands)
	}
	if !strings.Contains(service.BinPath, `"C:\Program Files\rdev\rdev.exe"`) {
		t.Fatalf("expected quoted binary path, got %q", service.BinPath)
	}
	if !strings.Contains(strings.Join(service.Shell, "\n"), "sc.exe create RemoteDevSkillkitHost") {
		t.Fatalf("expected shell command preview, got %#v", service.Shell)
	}
}

func TestNewWindowsServiceRejectsUnsafeOptions(t *testing.T) {
	_, err := NewWindowsService(WindowsServiceOptions{
		ServiceName: "../bad",
		BinaryPath:  `C:\rdev\rdev.exe`,
		GatewayURL:  "https://api.example.com/v1",
		TicketCode:  "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid Windows service name") {
		t.Fatalf("expected service name error, got %v", err)
	}
	_, err = NewWindowsService(WindowsServiceOptions{
		ServiceName: "RemoteDevSkillkitHost",
		BinaryPath:  `C:\rdev\rdev.exe`,
	})
	if err == nil || !strings.Contains(err.Error(), "ticket code or manifest URL is required") {
		t.Fatalf("expected enrollment error, got %v", err)
	}
	_, err = NewWindowsService(WindowsServiceOptions{
		ServiceName:       "RemoteDevSkillkitHost",
		BinaryPath:        `C:\rdev\rdev.exe`,
		GatewayURL:        "https://api.example.com/v1",
		TicketCode:        "ABCD-1234",
		ReleaseBundlePath: `C:\rdev\release-bundle.json`,
	})
	if err == nil || !strings.Contains(err.Error(), "release root public key is required") {
		t.Fatalf("expected release root error, got %v", err)
	}
}

func TestWindowsServiceControlAndUninstallPlans(t *testing.T) {
	start, err := NewWindowsServiceControlPlan(WindowsServiceControlOptions{
		Action:      "start",
		ServiceName: "RemoteDevSkillkitHost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(start.Commands[0], " ") != "sc.exe start RemoteDevSkillkitHost" {
		t.Fatalf("unexpected start command %#v", start.Commands)
	}
	inspect, err := NewWindowsServiceControlPlan(WindowsServiceControlOptions{
		Action:      "inspect",
		ServiceName: "RemoteDevSkillkitHost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inspect.Commands) != 2 || strings.Join(inspect.Commands[1], " ") != "sc.exe qc RemoteDevSkillkitHost" {
		t.Fatalf("unexpected inspect command %#v", inspect.Commands)
	}
	uninstall, err := NewWindowsServiceUninstallPlan("RemoteDevSkillkitHost")
	if err != nil {
		t.Fatal(err)
	}
	if len(uninstall.Commands) != 2 || strings.Join(uninstall.Commands[1], " ") != "sc.exe delete RemoteDevSkillkitHost" {
		t.Fatalf("unexpected uninstall command %#v", uninstall.Commands)
	}
	if _, err := NewWindowsServiceControlPlan(WindowsServiceControlOptions{Action: "restart", ServiceName: "RemoteDevSkillkitHost"}); err == nil {
		t.Fatal("expected unsupported action to fail")
	}
}
