package mcpstdio

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
)

func TestHostsToolsRegisteredInToolList(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	tools := result["tools"].([]any)
	seen := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		seen[tool["name"].(string)] = true
	}
	for _, name := range []string{"rdev.hosts.list", "rdev.hosts.rename"} {
		if !seen[name] {
			t.Fatalf("missing host tool %s from tools/list: %#v", name, seen)
		}
	}
}

func TestHostsListAndRenameLocalGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "mcp hosts fixture", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "dev-win",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-win",
		Transport:           controlplane.TransportLongPoll,
	}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(gw)

	listResult := callSessionTool(t, server, "rdev.hosts.list", map[string]any{})
	hosts := listResult["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("hosts list = %#v", hosts)
	}
	host := hosts[0].(map[string]any)
	if host["identity_fingerprint"] != "fp-win" {
		t.Fatalf("host record = %#v", host)
	}

	renameResult := callSessionTool(t, server, "rdev.hosts.rename", map[string]any{
		"host_id":      host["host_id"],
		"display_name": "Unity Dev Machine",
	})
	renamed := mapValue(t, renameResult, "host")
	if renamed["display_name"] != "Unity Dev Machine" {
		t.Fatalf("renamed host = %#v", renamed)
	}
}

func TestHostsToolsProxyToRemoteGateway(t *testing.T) {
	backend := gateway.NewMemoryGateway()
	session, err := backend.CreateSession(controlplane.SessionSpec{Reason: "proxy hosts fixture", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := backend.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "dev-win",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-win",
		Transport:           controlplane.TransportLongPoll,
	}); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(httpapi.NewServer(backend).Handler())
	defer remote.Close()

	proxy := NewServerWithRemoteGateway(gateway.NewMemoryGateway(), remote.URL)

	listResult := callSessionTool(t, proxy, "rdev.hosts.list", map[string]any{})
	hosts := listResult["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("proxied hosts = %#v", hosts)
	}
	host := hosts[0].(map[string]any)
	if host["identity_fingerprint"] != "fp-win" {
		t.Fatalf("proxied host = %#v", host)
	}

	renameResult := callSessionTool(t, proxy, "rdev.hosts.rename", map[string]any{
		"host_id":      host["host_id"],
		"display_name": "Unity Dev Machine",
	})
	renamed := mapValue(t, renameResult, "host")
	if renamed["display_name"] != "Unity Dev Machine" {
		t.Fatalf("proxied renamed host = %#v", renamed)
	}
}

func TestHostsRenameRejectsMissingArguments(t *testing.T) {
	server := NewServer(gateway.NewMemoryGateway())
	message := callSessionToolError(t, server, "rdev.hosts.rename", map[string]any{})
	if !strings.Contains(message, "host_id") {
		t.Fatalf("rename without host_id should fail with missing-argument error, got %q", message)
	}
}
