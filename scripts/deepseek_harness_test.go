package scripts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeepSeekHarnessBundleContract(t *testing.T) {
	root := filepath.Dir(mustGetwd(t))
	dir := filepath.Join(root, "integrations", "deepseek-harness")

	var manifest struct {
		License string            `json:"license"`
		Private bool              `json:"private"`
		Files   []string          `json:"files"`
		Engines map[string]string `json:"engines"`
		DSH     struct {
			Bundle struct {
				Patch string `json:"patch"`
			} `json:"bundle"`
		} `json:"dsh"`
	}
	content, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.Private || manifest.License != "MIT" || manifest.Engines["node"] != "^22.19.0 || >=24.0.0" || manifest.DSH.Bundle.Patch != "./cordis.patch.yml" {
		t.Fatalf("invalid private bundle manifest: %#v", manifest)
	}
	for _, name := range []string{"cordis.patch.yml", "README.md", "README.zh-CN.md"} {
		if !containsString(manifest.Files, name) {
			t.Fatalf("bundle manifest does not package %s", name)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("packaged file %s: %v", name, err)
		}
	}

	patchBytes, err := os.ReadFile(filepath.Join(dir, "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	patch := string(patchBytes)
	if strings.Count(patch, "- id: rdev") != 1 {
		t.Fatalf("expected exactly one rdev row:\n%s", patch)
	}
	for _, required := range []string{
		"@deepseek-ai/dsh-mcp-client",
		"transport: stdio",
		"process.env.RDEV_BIN",
		"'.local', 'bin'",
		"RDEV_GATEWAY_URL",
		"--operator-token-file",
		"RDEV_GATEWAY_OPERATOR_TOKEN_FILE",
		"failOnStartupError: true",
		"toolCallTimeoutMs: 60000",
		"maxAttempts: 8",
	} {
		if !strings.Contains(patch, required) {
			t.Fatalf("bundle patch does not contain %q", required)
		}
	}
	if strings.Contains(patch, "\n        env:") || strings.Contains(patch, "Bearer ") {
		t.Fatal("bundle patch embeds a child env block or bearer material")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
