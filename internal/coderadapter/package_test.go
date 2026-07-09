package coderadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testNow = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func TestBuildCoderPackageCreatesFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "coder")
	pkg, err := Build(Options{OutDir: out, GeneratedAt: testNow})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package OK, checks: %#v", pkg.Checks)
	}
	for _, name := range []string{"coder-adapter.json", "CODER_ADAPTER.md", "runner.env.example", "acceptance-evidence-plan.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("expected file %s: %v", name, err)
		}
	}
}

func TestBuildCoderPackageRequiresOutDir(t *testing.T) {
	_, err := Build(Options{})
	if err == nil {
		t.Fatal("expected error for missing out dir")
	}
}

func TestBuildCoderPackageRejectsNonEmpty(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(Options{OutDir: out})
	if err == nil {
		t.Fatal("expected error for non-empty output directory")
	}
}

func TestBuildCoderPackageForce(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(Options{OutDir: out, Force: true, GeneratedAt: testNow})
	if err != nil {
		t.Fatalf("Build with force: %v", err)
	}
}

func TestVerifyCoderPackageAcceptsBuilt(t *testing.T) {
	out := filepath.Join(t.TempDir(), "verify")
	if _, err := Build(Options{OutDir: out, GeneratedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	v, err := Verify(out)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !v.OK() {
		t.Fatalf("expected OK, checks: %#v", v.Checks)
	}
}

func TestVerifyCoderPackageFailsMissingManifest(t *testing.T) {
	out := t.TempDir()
	v, err := Verify(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.OK() {
		t.Fatal("expected failure when manifest is missing")
	}
}

func TestCoderAcceptancePlanHasRequiredFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "plan")
	if _, err := Build(Options{OutDir: out, GeneratedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(out, "acceptance-evidence-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan AcceptanceEvidencePlan
	if err := json.Unmarshal(content, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != AcceptanceEvidencePlanSchemaVersion {
		t.Fatalf("schema = %q", plan.SchemaVersion)
	}
	if !plan.ExternalMutation {
		t.Fatal("external_mutation must be true for Coder adapter")
	}
	paths := map[string]bool{}
	for _, f := range plan.EvidenceFiles {
		paths[f.Path] = true
	}
	for _, want := range []string{"coder-version.txt", "workspace-status.json", "host-registration.json", "workspace-stop.txt", "audit.jsonl"} {
		if !paths[want] {
			t.Fatalf("missing evidence file %q", want)
		}
	}
}

func TestCoderEnvTemplateHasRequiredVars(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env")
	if _, err := Build(Options{OutDir: out, GeneratedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(out, "runner.env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RDEV_CODER_URL", "RDEV_CODER_TOKEN", "RDEV_CODER_WORKSPACE"} {
		if !containsStr(string(content), want) {
			t.Fatalf("env template missing %q", want)
		}
	}
}

func TestCoderPackageHasAuthorizations(t *testing.T) {
	out := filepath.Join(t.TempDir(), "authorizations")
	pkg, err := Build(Options{OutDir: out, GeneratedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.AuthorizationRequired) < 3 {
		t.Fatalf("expected at least 3 authorization entries, got %d", len(pkg.AuthorizationRequired))
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}
