package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestPrepareCandidateStagesSignedReleaseAndSkillkit(t *testing.T) {
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	rdev := writeCandidateArtifactForTest(t, input, "rdev", "cli-binary")
	host := writeCandidateArtifactForTest(t, input, "rdev-host.exe", "host-binary")
	verify := writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary")

	candidate, err := PrepareCandidate(CandidateOptions{
		SourceRoot:        filepath.Join("..", ".."),
		OutDir:            out,
		Version:           "v0.1.0",
		GatewayURL:        "https://api.example.com/v1",
		ArtifactPaths:     []string{rdev, host, verify},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.OK() {
		t.Fatalf("expected release candidate ok: %#v", candidate.Checks)
	}
	if candidate.SchemaVersion != CandidateSchemaVersion {
		t.Fatalf("unexpected schema %q", candidate.SchemaVersion)
	}
	for _, path := range []string{
		"release-candidate.json",
		"release-bundle.json",
		"checksums.txt",
		"rdev",
		"rdev.rdev-release.json",
		"rdev-host.exe",
		"rdev-host.exe.rdev-release.json",
		"rdev-verify.exe",
		"rdev-verify.exe.rdev-release.json",
		"skillkit/manifest.json",
		"skillkit/INSTALL.md",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected release candidate file %s: %v", path, err)
		}
	}
	checksums, err := os.ReadFile(filepath.Join(out, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checksums), "release-bundle.json") || !strings.Contains(string(checksums), "skillkit/manifest.json") {
		t.Fatalf("expected release and skillkit checksums, got %s", string(checksums))
	}
}

func TestPrepareCandidateRejectsDuplicateArtifactNames(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	left := writeCandidateArtifactForTest(t, first, "rdev", "left")
	right := writeCandidateArtifactForTest(t, second, "rdev", "right")

	_, err = PrepareCandidate(CandidateOptions{
		SourceRoot:    filepath.Join("..", ".."),
		OutDir:        filepath.Join(t.TempDir(), "candidate"),
		Version:       "v0.1.0",
		ArtifactPaths: []string{left, right},
		Key:           key,
	})
	if err == nil {
		t.Fatal("expected duplicate artifact name to fail")
	}
	if !strings.Contains(err.Error(), "duplicate artifact name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeCandidateArtifactForTest(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
