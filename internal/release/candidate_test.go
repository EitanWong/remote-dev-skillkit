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
		"sbom.spdx.json",
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
	if !strings.Contains(string(checksums), "sbom.spdx.json") {
		t.Fatalf("expected SBOM checksum, got %s", string(checksums))
	}
	sbom := readReleaseCandidateTestFile(t, filepath.Join(out, "sbom.spdx.json"))
	for _, want := range []string{`"spdxVersion": "SPDX-2.3"`, `"fileName": "./rdev-host.exe"`, `"algorithm": "SHA256"`} {
		if !strings.Contains(sbom, want) {
			t.Fatalf("expected SBOM to contain %q, got %s", want, sbom)
		}
	}
}

func TestVerifyCandidatePassesAfterDirectoryMove(t *testing.T) {
	input := t.TempDir()
	root := t.TempDir()
	out := filepath.Join(root, "candidate")
	moved := filepath.Join(root, "downloaded-candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	rdev := writeCandidateArtifactForTest(t, input, "rdev", "cli-binary")
	host := writeCandidateArtifactForTest(t, input, "rdev-host.exe", "host-binary")
	verify := writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary")

	_, err = PrepareCandidate(CandidateOptions{
		SourceRoot:        filepath.Join("..", ".."),
		OutDir:            out,
		Version:           "v0.1.0",
		GatewayURL:        "https://api.example.com/v1",
		ArtifactPaths:     []string{rdev, host, verify},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(out, moved); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{
		CandidatePath:     moved,
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		GeneratedAt:       time.Date(2026, 6, 30, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected moved release candidate to verify: %#v", verification.Checks)
	}
	if verification.SchemaVersion != CandidateVerificationSchemaVersion {
		t.Fatalf("unexpected verification schema %q", verification.SchemaVersion)
	}
}

func TestVerifyCandidateDetectsTamperedArtifact(t *testing.T) {
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	rdev := writeCandidateArtifactForTest(t, input, "rdev", "cli-binary")
	host := writeCandidateArtifactForTest(t, input, "rdev-host.exe", "host-binary")
	verify := writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary")

	_, err = PrepareCandidate(CandidateOptions{
		SourceRoot:        filepath.Join("..", ".."),
		OutDir:            out,
		Version:           "v0.1.0",
		GatewayURL:        "https://api.example.com/v1",
		ArtifactPaths:     []string{rdev, host, verify},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "rdev-host.exe"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{
		CandidatePath:     filepath.Join(out, "release-candidate.json"),
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected tampered release candidate verification to fail")
	}
	if !strings.Contains(failedCandidateVerificationNames(verification), "rdev-host.exe:file_sha256_matches") ||
		!strings.Contains(failedCandidateVerificationNames(verification), "rdev-host.exe:signed_manifest_verifies_artifact") {
		t.Fatalf("expected artifact and bundle failures, got %s", failedCandidateVerificationNames(verification))
	}
}

func TestVerifyCandidateRejectsUnlistedFiles(t *testing.T) {
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	rdev := writeCandidateArtifactForTest(t, input, "rdev", "cli-binary")
	host := writeCandidateArtifactForTest(t, input, "rdev-host.exe", "host-binary")
	verify := writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary")

	_, err = PrepareCandidate(CandidateOptions{
		SourceRoot:        filepath.Join("..", ".."),
		OutDir:            out,
		Version:           "v0.1.0",
		GatewayURL:        "https://api.example.com/v1",
		ArtifactPaths:     []string{rdev, host, verify},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "unexpected.txt"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected unlisted release candidate file to fail verification")
	}
	if !strings.Contains(failedCandidateVerificationNames(verification), "candidate_has_no_unlisted_files") {
		t.Fatalf("expected unlisted file failure, got %s", failedCandidateVerificationNames(verification))
	}
}

func TestVerifyCandidateDetectsTamperedSBOM(t *testing.T) {
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	rdev := writeCandidateArtifactForTest(t, input, "rdev", "cli-binary")
	host := writeCandidateArtifactForTest(t, input, "rdev-host.exe", "host-binary")
	verify := writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary")

	_, err = PrepareCandidate(CandidateOptions{
		SourceRoot:        filepath.Join("..", ".."),
		OutDir:            out,
		Version:           "v0.1.0",
		GatewayURL:        "https://api.example.com/v1",
		ArtifactPaths:     []string{rdev, host, verify},
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
		Key:               key,
		Now:               time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	sbomPath := filepath.Join(out, "sbom.spdx.json")
	sbom := strings.Replace(readReleaseCandidateTestFile(t, sbomPath), `"checksumValue": "`, `"checksumValue": "0000000000000000000000000000000000000000000000000000000000000000", "_old": "`, 1)
	if err := os.WriteFile(sbomPath, []byte(sbom), 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{
		CandidatePath:     out,
		RequiredArtifacts: []string{"rdev-host.exe", "rdev-verify.exe"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected tampered SBOM to fail verification")
	}
	failures := failedCandidateVerificationNames(verification)
	if !strings.Contains(failures, "sbom.spdx.json:file_sha256_matches") ||
		!strings.Contains(failures, "sbom_hashes_match_artifacts") {
		t.Fatalf("expected SBOM checksum and content failures, got %s", failures)
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

func failedCandidateVerificationNames(verification CandidateVerification) string {
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
	for _, artifact := range verification.BundleVerification.Artifacts {
		for _, check := range artifact.Checks {
			if !check.Passed {
				failed = append(failed, artifact.Name+":"+check.Name)
			}
		}
	}
	return strings.Join(failed, ",")
}

func writeCandidateArtifactForTest(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readReleaseCandidateTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
