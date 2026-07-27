package hostrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/toolchain"
)

func TestResolveAgentRuntimeProfileMapsClaudeSecretAtLaunchOnly(t *testing.T) {
	root := t.TempDir()
	profile := toolchain.RuntimeProfile{
		SchemaVersion: toolchain.RuntimeProfileSchemaVersion,
		ID:            "claude-code-2.1.215",
		Tool:          contracts.ToolchainClaudeCode,
		Version:       "2.1.215",
		Command:       filepath.Join(root, "agents", "claude"),
		Endpoint: contracts.ToolchainEndpoint{
			BaseURL:       "https://agents.example.test/anthropic",
			Model:         "claude-test",
			CredentialEnv: "RDEV_TEST_CLAUDE_TOKEN",
			AuthMode:      contracts.ToolchainAuthBearer,
		},
	}
	writeRuntimeProfileFixture(t, root, profile)
	t.Setenv("RDEV_TEST_CLAUDE_TOKEN", "launch-only-secret")

	runtime, err := resolveAgentRuntimeProfile(taskEnvelope{Payload: map[string]any{"toolchain_profile_id": profile.ID}}, contracts.ToolchainClaudeCode, Options{ToolchainRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Command != profile.Command || runtime.Environment["ANTHROPIC_AUTH_TOKEN"] != "launch-only-secret" || runtime.Environment["ANTHROPIC_BASE_URL"] != profile.Endpoint.BaseURL {
		t.Fatalf("unexpected resolved agent runtime: %#v", runtime)
	}
}

func TestResolveAgentRuntimeProfileRejectsWrongToolAndUnsafeID(t *testing.T) {
	root := t.TempDir()
	profile := toolchain.RuntimeProfile{
		SchemaVersion: toolchain.RuntimeProfileSchemaVersion,
		ID:            "claude-code-2.1.215",
		Tool:          contracts.ToolchainClaudeCode,
		Version:       "2.1.215",
		Command:       filepath.Join(root, "agents", "claude"),
	}
	writeRuntimeProfileFixture(t, root, profile)

	_, err := resolveAgentRuntimeProfile(taskEnvelope{Payload: map[string]any{"toolchain_profile_id": profile.ID}}, contracts.ToolchainCodex, Options{ToolchainRoot: root})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong tool profile error = %v", err)
	}
	_, err = resolveAgentRuntimeProfile(taskEnvelope{Payload: map[string]any{"toolchain_profile_id": "../outside"}}, contracts.ToolchainCodex, Options{ToolchainRoot: root})
	if err == nil || !strings.Contains(err.Error(), "profile id") {
		t.Fatalf("unsafe profile id error = %v", err)
	}
}

func writeRuntimeProfileFixture(t *testing.T, root string, profile toolchain.RuntimeProfile) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles", profile.ID+".json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
