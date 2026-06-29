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

func TestVerifyAcceptsExportedBundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
		GatewayURL: "https://api.example.com/v1",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(VerifyOptions{BundleDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("expected exported bundle to verify: %#v", report.Checks)
	}
	if report.SchemaVersion != VerificationSchemaVersion {
		t.Fatalf("unexpected schema %q", report.SchemaVersion)
	}
	if report.FilesVerified == 0 {
		t.Fatalf("expected verified files")
	}
}

func TestVerifyRejectsTamperedBundleFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
	}); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(out, "skills", "remote-vibe-coding", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(VerifyOptions{BundleDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected tampered bundle verification to fail")
	}
	if !checkDetailContains(report.Checks, "listed_files_sha256_match", "skills/remote-vibe-coding/SKILL.md") {
		t.Fatalf("expected sha256 failure for tampered skill: %#v", report.Checks)
	}
}

func TestVerifyRejectsUnlistedBundleFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := Export(ExportOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "surprise.txt"), []byte("not in manifest\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(VerifyOptions{BundleDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected unlisted bundle file verification to fail")
	}
	if !checkDetailContains(report.Checks, "bundle_has_no_unlisted_files", "surprise.txt") {
		t.Fatalf("expected unlisted file failure: %#v", report.Checks)
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

func checkDetailContains(checks []VerificationCheck, name, value string) bool {
	for _, check := range checks {
		if check.Name == name && strings.Contains(check.Detail, value) {
			return true
		}
	}
	return false
}
