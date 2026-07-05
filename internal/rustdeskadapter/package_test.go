package rustdeskadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testNow = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func TestBuildRustDeskPackageCreatesFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "rustdesk")
	pkg, err := Build(Options{
		OutDir:      out,
		Name:        "test-rustdesk-adapter",
		Variant:     "rustdesk",
		GeneratedAt: testNow,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package OK, checks: %#v", pkg.Checks)
	}
	for _, name := range []string{"rustdesk-adapter.json", "RUSTDESK_ADAPTER.md", "acceptance-evidence-plan.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("expected file %s: %v", name, err)
		}
	}
}

func TestBuildMeshCentralPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mesh")
	pkg, err := Build(Options{
		OutDir:      out,
		Variant:     "meshcentral",
		GeneratedAt: testNow,
	})
	if err != nil {
		t.Fatalf("Build meshcentral: %v", err)
	}
	if pkg.Variant != "meshcentral" {
		t.Fatalf("variant = %q", pkg.Variant)
	}
	if pkg.Helper.Tool != "meshagent" {
		t.Fatalf("helper tool = %q", pkg.Helper.Tool)
	}
}

func TestBuildPackageDefaultsToRustDesk(t *testing.T) {
	out := filepath.Join(t.TempDir(), "default")
	pkg, err := Build(Options{OutDir: out, GeneratedAt: testNow})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if pkg.Variant != "rustdesk" {
		t.Fatalf("default variant = %q", pkg.Variant)
	}
}

func TestBuildPackageRequiresOutDir(t *testing.T) {
	_, err := Build(Options{})
	if err == nil {
		t.Fatal("expected error for missing out dir")
	}
}

func TestBuildPackageRejectsNonEmptyOutDir(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(Options{OutDir: out})
	if err == nil {
		t.Fatal("expected error for non-empty output directory")
	}
}

func TestBuildPackageForceOverwrite(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(Options{OutDir: out, Force: true, GeneratedAt: testNow})
	if err != nil {
		t.Fatalf("Build with force: %v", err)
	}
}

func TestVerifyAcceptsBuiltPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "verify")
	if _, err := Build(Options{OutDir: out, GeneratedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	verification, err := Verify(out)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification OK, checks: %#v", verification.Checks)
	}
}

func TestVerifyFailsWhenManifestMissing(t *testing.T) {
	out := t.TempDir()
	verification, err := Verify(out)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if verification.OK() {
		t.Fatal("expected verification failure when manifest missing")
	}
}

func TestAcceptanceEvidencePlanHasRequiredFiles(t *testing.T) {
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
		t.Fatalf("plan schema = %q", plan.SchemaVersion)
	}
	if !plan.ExternalMutation {
		t.Fatal("external_mutation must be true for remote desktop adapter")
	}
	paths := map[string]bool{}
	for _, f := range plan.EvidenceFiles {
		paths[f.Path] = true
	}
	for _, want := range []string{"helper-status.json", "session-start-transcript.txt", "host-registration.json", "session-teardown-transcript.txt", "audit.jsonl"} {
		if !paths[want] {
			t.Fatalf("missing evidence file %q", want)
		}
	}
}

func TestPackageHasApprovalBoundaries(t *testing.T) {
	out := filepath.Join(t.TempDir(), "approvals")
	pkg, err := Build(Options{OutDir: out, GeneratedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.ApprovalRequired) < 4 {
		t.Fatalf("expected at least 4 approval entries, got %d", len(pkg.ApprovalRequired))
	}
}

func TestPackageNoPrivateDataLeaked(t *testing.T) {
	out := filepath.Join(t.TempDir(), "private")
	if _, err := Build(Options{OutDir: out, GeneratedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(out, "rustdesk-adapter.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"BEGIN PRIVATE KEY", "192.168.", "10.0.", "password="} {
		if containsIgnoreCase(string(content), forbidden) {
			t.Fatalf("package leaked private material %q", forbidden)
		}
	}
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		func() bool {
			sl, subl := []rune(s), []rune(substr)
			for i := 0; i <= len(sl)-len(subl); i++ {
				match := true
				for j := range subl {
					if toLowerRune(sl[i+j]) != toLowerRune(subl[j]) {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
			return false
		}())
}

func toLowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}
