package mcpstdio

import (
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func TestSessionsTaskAcceptsTypedToolchainRequest(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := NewServer(gw)
	created := callSessionTool(t, server, "rdev.sessions.create", map[string]any{"reason": "toolchain bootstrap"})
	session := mapValue(t, created, "session")
	sessionID := stringValue(t, session, "id")
	joinCode := stringValue(t, session, "join_code")
	if _, _, _, _, err := gw.JoinSessionByCode(joinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Platform:            "linux/amd64",
		IdentityFingerprint: "fp-toolchain",
		Capabilities:        []string{"package.install.requiresAuthorization"},
		Transport:           controlplane.TransportLongPoll,
	}); err != nil {
		t.Fatal(err)
	}
	result := callSessionTool(t, server, "rdev.sessions.task", map[string]any{
		"session_id":      sessionID,
		"adapter":         "toolchain",
		"capabilities":    []any{"package.install.requiresAuthorization"},
		"idempotency_key": "toolchain-1",
		"toolchain_request": map[string]any{
			"schema_version": contracts.ToolchainRequestSchemaVersion,
			"tool":           contracts.ToolchainCodex,
			"version":        "0.144.6",
			"execute":        false,
			"policy": map[string]any{
				"schema_version": contracts.ToolchainPolicySchemaVersion,
				"region":         contracts.ToolchainRegionGlobal,
				"proxy_mode":     contracts.ToolchainProxyInherit,
				"registries": []any{map[string]any{
					"id":      "official",
					"url":     "https://registry.npmjs.org/",
					"regions": []any{contracts.ToolchainRegionGlobal},
				}},
			},
		},
	})
	task := mapValue(t, result, "task")
	if stringValue(t, task, "adapter") != "toolchain" {
		t.Fatalf("unexpected task: %#v", task)
	}
	payload := mapValue(t, task, "payload")
	if _, ok := payload["toolchain_request"].(map[string]any); !ok {
		t.Fatalf("toolchain request was not normalized into payload: %#v", payload)
	}
}
