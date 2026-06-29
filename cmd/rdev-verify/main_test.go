package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

func TestRunVerifiesArtifact(t *testing.T) {
	dir := t.TempDir()
	key := signVerifierArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	root := trustref.Encode(key.ID, key.PublicKey)

	var stdout bytes.Buffer
	if err := run([]string{
		"--artifact", filepath.Join(dir, "rdev-host.exe"),
		"--manifest", filepath.Join(dir, "rdev-host.exe.rdev-release.json"),
		"--root-public-key", root,
	}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok artifact verification, got %s", stdout.String())
	}
}

func TestRunVerifiesReleaseBundle(t *testing.T) {
	dir := t.TempDir()
	key := signVerifierArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	signVerifierArtifactWithKeyForTest(t, dir, "rdev-verify.exe", "verify-binary", key)
	bundle := createVerifierBundleForTest(t, dir, key, []string{"rdev-host.exe", "rdev-verify.exe"})
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := release.WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	root := trustref.Encode(key.ID, key.PublicKey)

	var stdout bytes.Buffer
	if err := run([]string{
		"--bundle", bundlePath,
		"--root-public-key", root,
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
	}, &stdout); err != nil {
		t.Fatalf("expected bundle verification to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "rdev.release-bundle-verification.v1") {
		t.Fatalf("expected structured bundle verification, got %s", stdout.String())
	}
}

func TestRunRejectsTamperedReleaseBundle(t *testing.T) {
	dir := t.TempDir()
	key := signVerifierArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	bundle := createVerifierBundleForTest(t, dir, key, []string{"rdev-host.exe"})
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if err := release.WriteBundle(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rdev-host.exe"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := run([]string{
		"--bundle", bundlePath,
		"--root-public-key", trustref.Encode(key.ID, key.PublicKey),
	}, &stdout)
	if err == nil {
		t.Fatalf("expected tampered bundle verification to fail: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ok": false`) ||
		!strings.Contains(stdout.String(), "artifact_sha256_matches_index") ||
		!strings.Contains(stdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured tampered failure, got %s", stdout.String())
	}
}

func TestRunRejectsMixedBundleAndArtifactMode(t *testing.T) {
	dir := t.TempDir()
	key := signVerifierArtifactForTest(t, dir, "rdev-host.exe", "host-binary")
	root := trustref.Encode(key.ID, key.PublicKey)

	err := run([]string{
		"--bundle", filepath.Join(dir, "release-bundle.json"),
		"--artifact", filepath.Join(dir, "rdev-host.exe"),
		"--root-public-key", root,
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected mixed bundle and artifact mode to fail")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func signVerifierArtifactForTest(t *testing.T, dir, name, content string) signing.Key {
	t.Helper()
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	signVerifierArtifactWithKeyForTest(t, dir, name, content, key)
	return key
}

func signVerifierArtifactWithKeyForTest(t *testing.T, dir, name, content string, key signing.Key) {
	t.Helper()
	artifactPath := filepath.Join(dir, name)
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := release.SignArtifact(artifactPath, key, time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := release.WriteManifest(artifactPath+".rdev-release.json", manifest); err != nil {
		t.Fatal(err)
	}
}

func createVerifierBundleForTest(t *testing.T, dir string, key signing.Key, artifacts []string) release.Bundle {
	t.Helper()
	bundle, err := release.CreateBundle(release.BundleOptions{
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
