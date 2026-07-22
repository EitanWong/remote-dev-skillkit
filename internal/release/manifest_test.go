//go:build !rdev_bootstrap_focused

package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestSignAndVerifyArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	manifest, err := SignArtifact(artifactPath, key, now)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ArtifactName != "rdev-host.exe" {
		t.Fatalf("unexpected artifact name %q", manifest.ArtifactName)
	}
	if manifest.Signature == "" {
		t.Fatal("signature should be present")
	}
	root := model.NewTrustBundle("release-root", key.PublicKey)
	if err := manifest.VerifyArtifact(artifactPath, root); err != nil {
		t.Fatalf("expected artifact to verify: %v", err)
	}
}

func TestVerifyArtifactRejectsTamperedContent(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := SignArtifact(artifactPath, key, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = manifest.VerifyArtifact(artifactPath, model.NewTrustBundle("release-root", key.PublicKey))
	if !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected invalid artifact, got %v", err)
	}
}

func TestVerifyRejectsWrongRoot(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := SignArtifact(artifactPath, key, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	err = manifest.Verify(model.NewTrustBundle("release-root", wrongKey.PublicKey))
	if !errors.Is(err, ErrManifestSignature) {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestSignArtifactForReleaseBindsVersionAndTargetPlatform(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	manifest, err := SignArtifactForRelease(artifactPath, key, now, "v0.2.0", "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ReleaseVersion != "v0.2.0" || manifest.TargetPlatform != "windows/amd64" {
		t.Fatalf("release metadata was not bound into artifact manifest: %#v", manifest)
	}
	root := model.NewTrustBundle(key.ID, key.PublicKey)
	if err := manifest.VerifyArtifact(artifactPath, root); err != nil {
		t.Fatalf("release-bound artifact should verify: %v", err)
	}

	for _, mutate := range []func(*Manifest){
		func(value *Manifest) { value.ReleaseVersion = "v0.1.0" },
		func(value *Manifest) { value.TargetPlatform = "linux/amd64" },
	} {
		tampered := manifest
		mutate(&tampered)
		if err := tampered.VerifyArtifact(artifactPath, root); !errors.Is(err, ErrManifestSignature) {
			t.Fatalf("tampered release metadata should invalidate signature, got %v", err)
		}
	}
}
