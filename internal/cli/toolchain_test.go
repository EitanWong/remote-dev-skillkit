package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/toolchain"
)

func TestCLIToolchainPlanEmitsTypedZeroSideEffectPlan(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "policy.json")
	policy := contracts.ToolchainPolicy{
		SchemaVersion: contracts.ToolchainPolicySchemaVersion,
		Region:        contracts.ToolchainRegionCNMainland,
		ProxyMode:     contracts.ToolchainProxyInherit,
		Registries: []contracts.ToolchainRegistry{
			{ID: "trusted-mirror", URL: "https://mirror.example.test/npm", Regions: []string{contracts.ToolchainRegionCNMainland}},
			{ID: "official", URL: "https://registry.example.test/npm", Regions: []string{contracts.ToolchainRegionGlobal, contracts.ToolchainRegionCNMainland}},
		},
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{"toolchain", "plan", "--tool", contracts.ToolchainCodex, "--version", "0.144.6", "--policy-file", policyPath, "--root", filepath.Join(root, "managed")}); err != nil {
		t.Fatal(err)
	}
	var result toolchain.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, stdout.String())
	}
	if result.Status != toolchain.StatusPlanned || len(result.PlannedSources) != 2 || result.PlannedSources[0] != "trusted-mirror" {
		t.Fatalf("unexpected plan: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "managed", "profiles")); !os.IsNotExist(err) {
		t.Fatalf("plan created toolchain profile directory: %v", err)
	}
}

func TestCLIToolchainEnsureRequiresExplicitExecute(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "policy.json")
	if err := os.WriteFile(policyPath, []byte(`{"schema_version":"rdev.toolchain-policy.v1","region":"global","proxy_mode":"inherit","registries":[{"id":"official","url":"https://registry.example.test/npm","regions":["global"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"toolchain", "ensure", "--tool", contracts.ToolchainCodex, "--version", "0.144.6", "--policy-file", policyPath})
	if err == nil || !strings.Contains(err.Error(), "--execute") {
		t.Fatalf("ensure without explicit execute error = %v", err)
	}
}
