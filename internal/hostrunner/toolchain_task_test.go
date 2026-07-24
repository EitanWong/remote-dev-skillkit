package hostrunner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/toolchain"
)

func TestToolchainAdapterRequiresPackageInstallAuthorization(t *testing.T) {
	envelope := taskEnvelope{Adapter: "toolchain", Capabilities: []string{"shell.user"}}
	if got := missingAdapterCapability(envelope); got != "package.install.requiresAuthorization" {
		t.Fatalf("missingAdapterCapability(toolchain) = %q", got)
	}
	if !supportedAdapter("toolchain") {
		t.Fatal("toolchain adapter should be supported")
	}
}

func TestToolchainAdapterExecutesTypedPlanWithoutNPM(t *testing.T) {
	request := contracts.ToolchainRequest{
		SchemaVersion: contracts.ToolchainRequestSchemaVersion,
		Tool:          contracts.ToolchainCodex,
		Version:       "0.144.6",
		Policy: contracts.ToolchainPolicy{
			SchemaVersion: contracts.ToolchainPolicySchemaVersion,
			Region:        contracts.ToolchainRegionGlobal,
			ProxyMode:     contracts.ToolchainProxyInherit,
			Registries:    []contracts.ToolchainRegistry{{ID: "official", URL: "https://registry.example.test/npm", Regions: []string{contracts.ToolchainRegionGlobal}}},
		},
	}
	artifact, err := executeJobAdapterDirect(context.Background(), taskEnvelope{Adapter: "toolchain", Payload: request.TaskPayload()})
	if err != nil {
		t.Fatal(err)
	}
	var result toolchain.Result
	if err := json.Unmarshal([]byte(artifact), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != toolchain.ResultSchemaVersion || result.Status != toolchain.StatusPlanned || len(result.Attempts) != 0 || len(result.PlannedSources) != 1 || result.PlannedSources[0] != "official" {
		t.Fatalf("unexpected toolchain plan artifact: %s", artifact)
	}
}
