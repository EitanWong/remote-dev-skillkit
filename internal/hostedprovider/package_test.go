package hostedprovider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildAndVerifyHostedProviderPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	pkg, err := Build(Options{
		OutDir:          out,
		Name:            "self-hosted-rdev",
		StorageProvider: "file",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package ok: %#v", pkg.Checks)
	}
	for _, path := range []string{"hosted-provider.json", "HOSTED_PROVIDER.md", "gateway.env.example"} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Fatalf("expected hosted provider file %s: %v", path, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(out, "hosted-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), filepath.Dir(out)) || strings.Contains(string(content), "BEGIN PRIVATE KEY") {
		t.Fatalf("hosted provider package leaked private material: %s", string(content))
	}

	verification, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
	if verification.SchemaVersion != VerificationSchemaVersion ||
		verification.StorageProvider != "file" ||
		verification.AuthProvider != "hosted-ed25519-jwt" {
		t.Fatalf("unexpected verification: %#v", verification)
	}
}

func TestVerifyHostedProviderPackageDetectsTamperedFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	_, err := Build(Options{
		OutDir:          out,
		StorageProvider: "file",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "gateway.env.example"), []byte("RDEV_SECRET=sk-private\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	verification, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected tampered hosted provider package to fail")
	}
	failures := failedNames(verification)
	if !strings.Contains(failures, "gateway.env.example:file_sha256_matches") ||
		!strings.Contains(failures, "gateway.env.example:file_has_no_private_surface") {
		t.Fatalf("expected checksum and private-surface failures, got %s", failures)
	}
}

func failedNames(verification Verification) string {
	var failed []string
	for _, check := range verification.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	for _, file := range verification.Files {
		for _, check := range file.Checks {
			if !check.Passed {
				failed = append(failed, file.Path+":"+check.Name)
			}
		}
	}
	return strings.Join(failed, ",")
}
