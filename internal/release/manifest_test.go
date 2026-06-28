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
