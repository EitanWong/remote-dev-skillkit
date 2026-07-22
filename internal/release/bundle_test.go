//go:build !rdev_bootstrap_focused

package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestCreateAndVerifyBundle(t *testing.T) {
	dir := t.TempDir()
	key := signReleaseArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	signReleaseArtifactWithKeyForTest(t, dir, "rdev-verify.exe", "verify-binary", key)

	bundle, err := CreateBundle(BundleOptions{
		Dir:               dir,
		ArtifactPaths:     []string{"rdev-host.exe", "rdev-verify.exe"},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Signature == "" {
		t.Fatal("bundle signature should be present")
	}
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyBundle(bundlePath, model.NewTrustBundle(key.ID, key.PublicKey), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected bundle verification ok: %#v", verification)
	}
}

func TestVerifyBundleRejectsTamperedArtifact(t *testing.T) {
	dir := t.TempDir()
	key := signReleaseArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	bundle := createBundleForTest(t, dir, key, []string{"rdev-host.exe"})
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rdev-host.exe"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyBundle(bundlePath, model.NewTrustBundle(key.ID, key.PublicKey), nil)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected tampered bundle verification to fail")
	}
	if !bundleArtifactCheckFailed(verification, "artifact_sha256_matches_index") ||
		!bundleArtifactCheckFailed(verification, "signed_manifest_verifies_artifact") {
		t.Fatalf("expected artifact tamper checks to fail: %#v", verification.Artifacts)
	}
}

func TestVerifyBundleRejectsTamperedIndex(t *testing.T) {
	dir := t.TempDir()
	key := signReleaseArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	bundle := createBundleForTest(t, dir, key, []string{"rdev-host.exe"})
	bundle.Artifacts[0].ManifestPath = "evil.json"
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyBundle(bundlePath, model.NewTrustBundle(key.ID, key.PublicKey), nil)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected tampered bundle index verification to fail")
	}
	if !bundleCheckFailed(verification, "bundle_signature_valid") {
		content, _ := json.MarshalIndent(verification, "", "  ")
		t.Fatalf("expected bundle signature failure: %s", content)
	}
}

func TestVerifyBundleRejectsMissingRequiredArtifact(t *testing.T) {
	dir := t.TempDir()
	key := signReleaseArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	bundle := createBundleForTest(t, dir, key, []string{"rdev-host.exe"})
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyBundle(bundlePath, model.NewTrustBundle(key.ID, key.PublicKey), []string{"rdev-verify.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatalf("expected missing required artifact verification to fail")
	}
	if !bundleCheckFailed(verification, "required_artifacts_present") {
		t.Fatalf("expected required artifact check to fail: %#v", verification.Checks)
	}
}

func TestCreateBundleBindsVersionAndTargetPlatform(t *testing.T) {
	dir := t.TempDir()
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"rdev-host.exe", "rdev-verify.exe"} {
		artifactPath := filepath.Join(dir, name)
		if err := os.WriteFile(artifactPath, []byte(name+"-binary"), 0o644); err != nil {
			t.Fatal(err)
		}
		manifest, err := SignArtifactForRelease(artifactPath, key, now, "v0.2.0", "windows/amd64")
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteManifest(artifactPath+".rdev-release.json", manifest); err != nil {
			t.Fatal(err)
		}
	}

	bundle, err := CreateBundle(BundleOptions{
		Dir:               dir,
		Version:           "v0.2.0",
		TargetPlatform:    "windows/amd64",
		ArtifactPaths:     []string{"rdev-host.exe", "rdev-verify.exe"},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Version != "v0.2.0" || bundle.TargetPlatform != "windows/amd64" {
		t.Fatalf("bundle omitted signed release metadata: %#v", bundle)
	}
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle(key.ID, key.PublicKey)
	verification, err := VerifyBundle(bundlePath, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() || verification.Version != bundle.Version || verification.TargetPlatform != bundle.TargetPlatform {
		t.Fatalf("verified bundle did not expose signed release metadata: %#v", verification)
	}

	tampered := bundle
	tampered.TargetPlatform = "linux/amd64"
	if err := WriteBundle(bundlePath, tampered); err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyBundle(bundlePath, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() || !bundleCheckFailed(verification, "bundle_signature_valid") {
		t.Fatalf("tampered signed target platform should fail bundle verification: %#v", verification)
	}
}

func signReleaseArtifactForTest(t *testing.T, dir, name, content string) signing.Key {
	t.Helper()
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	signReleaseArtifactWithKeyForTest(t, dir, name, content, key)
	return key
}

func signReleaseArtifactWithKeyForTest(t *testing.T, dir, name, content string, key signing.Key) {
	t.Helper()
	artifactPath := filepath.Join(dir, name)
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := SignArtifact(artifactPath, key, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(artifactPath+".rdev-release.json", manifest); err != nil {
		t.Fatal(err)
	}
}

func createBundleForTest(t *testing.T, dir string, key signing.Key, artifacts []string) Bundle {
	t.Helper()
	bundle, err := CreateBundle(BundleOptions{
		Dir:               dir,
		ArtifactPaths:     artifacts,
		RequiredArtifacts: artifacts,
		Key:               key,
		Now:               time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func bundleCheckFailed(verification BundleVerification, name string) bool {
	for _, check := range verification.Checks {
		if check.Name == name && !check.Passed {
			return true
		}
	}
	return false
}

func bundleArtifactCheckFailed(verification BundleVerification, name string) bool {
	for _, artifact := range verification.Artifacts {
		for _, check := range artifact.Checks {
			if check.Name == name && !check.Passed {
				return true
			}
		}
	}
	return false
}
