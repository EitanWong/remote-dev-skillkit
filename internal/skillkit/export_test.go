package skillkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportWritesInstallableSkillkitBundle(t *testing.T) {
	sourceRoot := filepath.Join("..", "..")
	out := filepath.Join(t.TempDir(), "bundle")
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)

	manifest, err := Export(ExportOptions{
		SourceRoot:  sourceRoot,
		OutDir:      out,
		GatewayURL:  "https://api.example.com/v1",
		GeneratedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if manifest.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("unexpected schema %q", manifest.SchemaVersion)
	}
	if manifest.GatewayURL != "https://api.example.com/v1" {
		t.Fatalf("unexpected gateway URL %q", manifest.GatewayURL)
	}
	for _, skill := range []string{"safe-remote-support", "host-triage", "remote-job-review", "remote-vibe-coding"} {
		if !hasSkill(manifest, skill) {
			t.Fatalf("expected skill %q in manifest: %#v", skill, manifest.Skills)
		}
		if _, err := os.Stat(filepath.Join(out, "skills", skill, "SKILL.md")); err != nil {
			t.Fatalf("expected exported skill %s: %v", skill, err)
		}
	}
	for _, path := range []string{
		"manifest.json",
		"INSTALL.md",
		"mcp/tools.json",
		"frameworks/codex.md",
		"frameworks/claude-code.md",
		"frameworks/hermes.md",
		"frameworks/openclaw-opencode.md",
		"frameworks/generic-mcp-agent.md",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected bundle file %s: %v", path, err)
		}
	}
	install := readFile(t, filepath.Join(out, "INSTALL.md"))
	if !strings.Contains(install, "https://api.example.com/v1") {
		t.Fatalf("expected gateway URL in install doc, got %s", install)
	}
	content := readFile(t, filepath.Join(out, "manifest.json"))
	var onDisk Manifest
	if err := json.Unmarshal([]byte(content), &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk.Files) == 0 {
		t.Fatal("manifest should include file checksums")
	}
	if !hasFile(onDisk, "mcp/tools.json") {
		t.Fatalf("manifest should include mcp/tools.json: %#v", onDisk.Files)
	}
}

func TestExportRejectsNonEmptyOutputDirectory(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Export(ExportOptions{SourceRoot: filepath.Join("..", ".."), OutDir: out})
	if err == nil {
		t.Fatal("expected non-empty output directory to fail")
	}
	if !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func hasSkill(manifest Manifest, name string) bool {
	for _, skill := range manifest.Skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func hasFile(manifest Manifest, path string) bool {
	for _, file := range manifest.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
