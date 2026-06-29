package service

import (
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewMacOSLaunchAgentBuildsManagedHostArguments(t *testing.T) {
	agent, err := NewMacOSLaunchAgent(LaunchAgentOptions{
		Label:             "com.example.rdev-host",
		BinaryPath:        "/opt/rdev/bin/rdev",
		GatewayURL:        "https://api.example.com/v1",
		TicketCode:        "ABCD-1234",
		IdentityStorePath: "/Users/eitan/.rdev/host/identity.json",
		TrustStorePath:    "/Users/eitan/.rdev/host/trust.json",
		NonceStorePath:    "/Users/eitan/.rdev/host/nonces.json",
		ApprovalStorePath: "/Users/eitan/.rdev/host/approvals.json",
		LogDir:            "/Users/eitan/Library/Logs/rdev",
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
		"/Users/eitan/.rdev/host/identity.json",
		"--trust-store",
		"/Users/eitan/.rdev/host/trust.json",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected argument %q in %#v", expected, agent.ProgramArguments)
		}
	}
	if agent.StdoutPath != "/Users/eitan/Library/Logs/rdev/com.example.rdev-host.out.log" {
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
}
