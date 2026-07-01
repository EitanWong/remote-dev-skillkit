package service

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewMacOSLaunchAgentBuildsManagedHostArguments(t *testing.T) {
	agent, err := NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:                    "com.example.rdev-host",
		BinaryPath:               "/opt/rdev/bin/rdev",
		GatewayURL:               "https://api.example.com/v1",
		TicketCode:               "ABCD-1234",
		IdentityStorePath:        "/Users/example/.rdev/host/identity.json",
		TrustStorePath:           "/Users/example/.rdev/host/trust.json",
		NonceStorePath:           "/Users/example/.rdev/host/nonces.json",
		ApprovalStorePath:        "/Users/example/.rdev/host/approvals.json",
		WorkspaceLockStorePath:   "/Users/example/.rdev/host/workspace-locks",
		ReleaseBundlePath:        "/opt/rdev/release-bundle.json",
		ReleaseRootPublicKey:     "release-root:abc123",
		ReleaseRequiredArtifacts: []string{"rdev-host", "rdev-verify"},
		LogDir:                   "/Users/example/Library/Logs/rdev",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(agent.ProgramArguments, "\x00")
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
		"/Users/example/.rdev/host/identity.json",
		"--trust-store",
		"/Users/example/.rdev/host/trust.json",
		"--workspace-lock-store",
		"/Users/example/.rdev/host/workspace-locks",
		"--release-bundle",
		"/opt/rdev/release-bundle.json",
		"--release-root-public-key",
		"release-root:abc123",
		"--release-require-artifacts",
		"rdev-host,rdev-verify",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected argument %q in %#v", expected, agent.ProgramArguments)
		}
	}
	if agent.StdoutPath != "/Users/example/Library/Logs/rdev/com.example.rdev-host.out.log" {
		t.Fatalf("unexpected stdout path %q", agent.StdoutPath)
	}
	if !agent.KeepAlive || !agent.RunAtLoad {
		t.Fatal("managed LaunchAgent should keep alive and run at load")
	}
}

func TestRenderMacOSLaunchAgentEscapesXML(t *testing.T) {
	agent, err := NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:      "com.example.rdev-host",
		BinaryPath: "/opt/rdev/bin/rdev",
		GatewayURL: "https://api.example.com/v1?x=1&y=2",
		TicketCode: "ABCD-1234",
		LogDir:     filepath.Join(t.TempDir(), "logs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := RenderMacOSLaunchAgent(agent)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("https://api.example.com/v1?x=1&amp;y=2")) {
		t.Fatalf("expected escaped gateway URL, got %s", string(content))
	}
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		if _, err := decoder.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("rendered plist should be XML: %v\n%s", err, string(content))
		}
	}
	if decoder.InputOffset() == 0 {
		t.Fatalf("rendered plist should be XML: %s", string(content))
	}
}

func TestInspectMacOSLaunchAgentReadsRenderedPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	agent, err := NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:      "com.example.rdev-host",
		BinaryPath: "/opt/rdev/bin/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
		LogDir:     filepath.Join(dir, "logs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := RenderMacOSLaunchAgent(agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := InspectMacOSLaunchAgent(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists {
		t.Fatal("expected plist to exist")
	}
	if status.Label != "com.example.rdev-host" {
		t.Fatalf("unexpected label %q", status.Label)
	}
	if !status.KeepAlive || !status.RunAtLoad {
		t.Fatalf("expected keepalive and runatload, got %#v", status)
	}
	if len(status.ProgramArguments) == 0 || status.ProgramArguments[0] != "/opt/rdev/bin/rdev" {
		t.Fatalf("unexpected program arguments %#v", status.ProgramArguments)
	}
	if status.Mode != "0600" {
		t.Fatalf("expected 0600 mode, got %q", status.Mode)
	}
}

func TestNewMacOSLaunchAgentControlPlan(t *testing.T) {
	start, err := NewMacOSLaunchAgentControlPlan(LaunchAgentControlOptions{
		Action:    "start",
		Label:     "com.example.rdev-host",
		PlistPath: "/Users/example/Library/LaunchAgents/com.example.rdev-host.plist",
		Domain:    "gui/501",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(start.Argv, " ") != "launchctl bootstrap gui/501 /Users/example/Library/LaunchAgents/com.example.rdev-host.plist" {
		t.Fatalf("unexpected start argv %#v", start.Argv)
	}
	if !strings.Contains(start.Shell, "launchctl bootstrap gui/501") {
		t.Fatalf("unexpected shell command %q", start.Shell)
	}
	inspect, err := NewMacOSLaunchAgentControlPlan(LaunchAgentControlOptions{
		Action: "inspect",
		Label:  "com.example.rdev-host",
		Domain: "gui/501",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(inspect.Argv, " ") != "launchctl print gui/501/com.example.rdev-host" {
		t.Fatalf("unexpected inspect argv %#v", inspect.Argv)
	}
	if _, err := NewMacOSLaunchAgentControlPlan(LaunchAgentControlOptions{Action: "restart"}); err == nil {
		t.Fatal("expected unsupported action to fail")
	}
}

func TestNewMacOSLaunchAgentRejectsUnsafeOptions(t *testing.T) {
	_, err := NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:      "bad label",
		BinaryPath: "/opt/rdev/bin/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid launchd label") {
		t.Fatalf("expected invalid label error, got %v", err)
	}
	_, err = NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:      "com.example.rdev-host",
		BinaryPath: "relative/rdev",
		GatewayURL: "https://api.example.com/v1",
		TicketCode: "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "binary path must be absolute") {
		t.Fatalf("expected binary path error, got %v", err)
	}
	_, err = NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:      "com.example.rdev-host",
		BinaryPath: "/opt/rdev/bin/rdev",
	})
	if err == nil || !strings.Contains(err.Error(), "ticket code or manifest URL is required") {
		t.Fatalf("expected enrollment error, got %v", err)
	}
	_, err = NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:             "com.example.rdev-host",
		BinaryPath:        "/opt/rdev/bin/rdev",
		GatewayURL:        "https://api.example.com/v1",
		TicketCode:        "ABCD-1234",
		ReleaseBundlePath: "/opt/rdev/release-bundle.json",
	})
	if err == nil || !strings.Contains(err.Error(), "release root public key is required") {
		t.Fatalf("expected release root error, got %v", err)
	}
}
