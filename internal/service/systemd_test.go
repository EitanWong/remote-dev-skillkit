package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLinuxSystemdUserServiceBuildsManagedHostArguments(t *testing.T) {
	unit, err := NewLinuxSystemdUserService(SystemdUserServiceOptions{
		UnitName:               "rdev-host.service",
		BinaryPath:             "/opt/rdev/bin/rdev",
		GatewayURL:             "https://api.example.com/v1",
		TicketCode:             "ABCD-1234",
		IdentityStorePath:      "/home/eitan/.rdev/host/identity.json",
		TrustStorePath:         "/home/eitan/.rdev/host/trust.json",
		NonceStorePath:         "/home/eitan/.rdev/host/nonces.json",
		ApprovalStorePath:      "/home/eitan/.rdev/host/approvals.json",
		WorkspaceLockStorePath: "/home/eitan/.rdev/host/workspace-locks",
		LogDir:                 "/home/eitan/.local/state/rdev/logs",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(unit.ExecStart, "\x00")
	for _, expected := range []string{
		"/opt/rdev/bin/rdev",
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
		"/home/eitan/.rdev/host/identity.json",
		"--trust-store",
		"/home/eitan/.rdev/host/trust.json",
		"--workspace-lock-store",
		"/home/eitan/.rdev/host/workspace-locks",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected argument %q in %#v", expected, unit.ExecStart)
		}
	}
	if unit.StandardOutput != "append:/home/eitan/.local/state/rdev/logs/rdev-host.out.log" {
		t.Fatalf("unexpected stdout target %q", unit.StandardOutput)
	}
	if unit.Restart != "on-failure" || unit.RestartSec != "5s" {
		t.Fatalf("unexpected restart policy %#v", unit)
	}
	if !unit.NoNewPrivileges || !unit.PrivateTmp {
		t.Fatal("managed systemd service should use basic hardening")
	}
}

func TestRenderLinuxSystemdUserServiceQuotesExecStart(t *testing.T) {
	unit, err := NewLinuxSystemdUserService(SystemdUserServiceOptions{
		UnitName:   "rdev-host.service",
		BinaryPath: "/opt/Remote Dev/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := RenderLinuxSystemdUserService(unit)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(content)
	for _, expected := range []string{
		"[Unit]",
		"Description=Remote Dev Skillkit managed host",
		"[Service]",
		`ExecStart="/opt/Remote Dev/rdev" host serve --mode managed`,
		"Restart=on-failure",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered unit to contain %q, got:\n%s", expected, rendered)
		}
	}
}

func TestInspectLinuxSystemdUserServiceReadsRenderedUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	unit, err := NewLinuxSystemdUserService(SystemdUserServiceOptions{
		UnitName:   "rdev-host.service",
		BinaryPath: "/opt/rdev/bin/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
		LogDir:     filepath.Join(dir, "logs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := RenderLinuxSystemdUserService(unit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := InspectLinuxSystemdUserService(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("expected unit to exist")
	}
	if status.UnitName != "rdev-host.service" {
		t.Fatalf("unexpected unit name %q", status.UnitName)
	}
	if !strings.Contains(status.ExecStart, "/opt/rdev/bin/rdev host serve") {
		t.Fatalf("unexpected exec start %q", status.ExecStart)
	}
	if status.StandardOutput == "" || status.StandardError == "" {
		t.Fatalf("expected log paths in status %#v", status)
	}
	if !status.NoNewPrivileges || !status.PrivateTmp {
		t.Fatalf("expected hardening flags in status %#v", status)
	}
	if status.Mode != "0600" {
		t.Fatalf("expected 0600 mode, got %q", status.Mode)
	}
}

func TestNewLinuxSystemdControlPlan(t *testing.T) {
	start, err := NewLinuxSystemdControlPlan(SystemdControlOptions{
		Action:   "start",
		UnitName: "rdev-host.service",
		User:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(start.Commands) != 2 {
		t.Fatalf("expected daemon-reload and enable commands, got %#v", start.Commands)
	}
	if strings.Join(start.Commands[0], " ") != "systemctl --user daemon-reload" {
		t.Fatalf("unexpected reload command %#v", start.Commands[0])
	}
	if strings.Join(start.Commands[1], " ") != "systemctl --user enable --now rdev-host.service" {
		t.Fatalf("unexpected start command %#v", start.Commands[1])
	}
	inspect, err := NewLinuxSystemdControlPlan(SystemdControlOptions{
		Action:   "inspect",
		UnitName: "rdev-host.service",
		User:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(inspect.Commands[0], " ") != "systemctl --user status rdev-host.service" {
		t.Fatalf("unexpected inspect command %#v", inspect.Commands[0])
	}
	if _, err := NewLinuxSystemdControlPlan(SystemdControlOptions{Action: "restart", UnitName: "rdev-host.service"}); err == nil {
		t.Fatal("expected unsupported action to fail")
	}
}

func TestNewLinuxSystemdUserServiceRejectsUnsafeOptions(t *testing.T) {
	_, err := NewLinuxSystemdUserService(SystemdUserServiceOptions{
		UnitName:   "../rdev-host.service",
		BinaryPath: "/opt/rdev/bin/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid systemd unit name") {
		t.Fatalf("expected unit name error, got %v", err)
	}
	_, err = NewLinuxSystemdUserService(SystemdUserServiceOptions{
		UnitName:   "rdev-host.service",
		BinaryPath: "relative/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "binary path must be absolute") {
		t.Fatalf("expected binary path error, got %v", err)
	}
	_, err = NewLinuxSystemdUserService(SystemdUserServiceOptions{
		UnitName:   "rdev-host.service",
		BinaryPath: "/opt/rdev/bin/rdev",
	})
	if err == nil || !strings.Contains(err.Error(), "ticket code or manifest URL is required") {
		t.Fatalf("expected enrollment error, got %v", err)
	}
}
