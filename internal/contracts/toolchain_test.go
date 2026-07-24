package contracts

import (
	"strings"
	"testing"
)

func validToolchainRequestRaw() map[string]any {
	return map[string]any{
		"schema_version": ToolchainRequestSchemaVersion,
		"tool":           ToolchainCodex,
		"version":        "0.144.6",
		"execute":        true,
		"policy": map[string]any{
			"schema_version": ToolchainPolicySchemaVersion,
			"region":         ToolchainRegionCNMainland,
			"proxy_mode":     ToolchainProxyInherit,
			"registries": []any{
				map[string]any{"id": "cn-mirror", "url": "https://registry.example.cn/npm/", "regions": []any{ToolchainRegionCNMainland}},
				map[string]any{"id": "official", "url": "https://registry.npmjs.org/", "regions": []any{ToolchainRegionGlobal, ToolchainRegionCNMainland}},
			},
			"endpoint": map[string]any{
				"base_url":       "https://agents.example.test/v1",
				"model":          "test-model",
				"credential_env": "RDEV_CODEX_API_KEY",
				"auth_mode":      ToolchainAuthBearer,
			},
		},
	}
}

func TestDecodeToolchainRequestAcceptsTrustedMirrorFallbackPolicy(t *testing.T) {
	request, err := DecodeToolchainRequest(validToolchainRequestRaw())
	if err != nil {
		t.Fatal(err)
	}
	if request.Tool != ToolchainCodex || request.Version != "0.144.6" {
		t.Fatalf("unexpected request: %#v", request)
	}
	if got := request.Policy.EligibleRegistries(); len(got) != 2 || got[0].ID != "cn-mirror" || got[1].ID != "official" {
		t.Fatalf("eligible registries = %#v", got)
	}
	if request.Policy.Endpoint.CredentialEnv != "RDEV_CODEX_API_KEY" {
		t.Fatalf("credential reference was not preserved: %#v", request.Policy.Endpoint)
	}
}

func TestDecodeToolchainRequestRejectsFloatingVersionsAndUnsafeRegistries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "floating version",
			mutate: func(raw map[string]any) {
				raw["version"] = "latest"
			},
			want: "version",
		},
		{
			name: "plaintext registry",
			mutate: func(raw map[string]any) {
				policy := raw["policy"].(map[string]any)
				registries := policy["registries"].([]any)
				registries[0].(map[string]any)["url"] = "http://mirror.example.test/npm"
			},
			want: "https",
		},
		{
			name: "credential literal field",
			mutate: func(raw map[string]any) {
				policy := raw["policy"].(map[string]any)
				endpoint := policy["endpoint"].(map[string]any)
				endpoint["credential_env"] = "s" + "k-" + "test-fixture-literal"
			},
			want: "credential_env",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := validToolchainRequestRaw()
			tc.mutate(raw)
			_, err := DecodeToolchainRequest(raw)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("DecodeToolchainRequest() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestToolchainRequestSchemaExposesTypedTask(t *testing.T) {
	schema := ToolchainRequestSchema()
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("unexpected schema envelope: %#v", schema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || properties["policy"] == nil || properties["execute"] == nil {
		t.Fatalf("toolchain schema lacks required properties: %#v", schema)
	}
}

func TestDecodeToolchainRequestValidatesVerifiedPortableNodeBootstrap(t *testing.T) {
	raw := validToolchainRequestRaw()
	policy := raw["policy"].(map[string]any)
	policy["node_bootstrap"] = map[string]any{
		"version":             "22.14.0",
		"max_archive_bytes":   104857600,
		"max_extracted_bytes": 524288000,
		"sources": []any{map[string]any{
			"id":      "node-cn-mirror",
			"url":     "https://mirror.example.cn/node.zip",
			"sha256":  strings.Repeat("a", 64),
			"format":  "zip",
			"regions": []any{ToolchainRegionCNMainland},
		}},
	}
	request, err := DecodeToolchainRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if request.Policy.NodeBootstrap == nil || len(request.Policy.NodeBootstrap.EligibleSources(request.Policy.Region)) != 1 {
		t.Fatalf("node bootstrap was not preserved: %#v", request.Policy.NodeBootstrap)
	}

	policy["node_bootstrap"].(map[string]any)["sources"].([]any)[0].(map[string]any)["sha256"] = "not-a-hash"
	if _, err := DecodeToolchainRequest(raw); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("invalid bootstrap digest error = %v", err)
	}
}
