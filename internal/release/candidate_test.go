package release

import (
	"archive/zip"
	"encoding/json"
	"io"
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
	if candidate.LayeredAssetManifestPath != "" {
		t.Fatalf("nonmatching Windows artifact unexpectedly produced a layered manifest: %q", candidate.LayeredAssetManifestPath)
	}
	if _, err := os.Stat(filepath.Join(out, layeredAssetManifestPath)); !os.IsNotExist(err) {
		t.Fatalf("nonmatching Windows artifact should not write %s: %v", layeredAssetManifestPath, err)
	}
	for _, path := range []string{
		"release-candidate.json",
		"release-bundle.json",
		"sbom.spdx.json",
		"provenance.json",
		"connection-entry-release.zip",
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
	if !strings.Contains(string(checksums), "provenance.json") {
		t.Fatalf("expected provenance checksum, got %s", string(checksums))
	}
	if !strings.Contains(string(checksums), "connection-entry-release.zip") {
		t.Fatalf("expected Connection Entry release archive checksum, got %s", string(checksums))
	}
	archiveEntries := readConnectionEntryArchiveForTest(t, filepath.Join(out, "connection-entry-release.zip"))
	for _, path := range []string{
		"CONNECTION_ENTRY_RELEASE.md",
		"connection-entry-release.json",
		"connection-entry-runner.template.json",
		"connection-entry-checksums.txt",
		"release/release-bundle.json",
		"release/sbom.spdx.json",
		"release/provenance.json",
		"launchers/start-connection-entry.sh",
		"bin/rdev",
		"bin/rdev.rdev-release.json",
		"bin/rdev-host.exe",
		"bin/rdev-host.exe.rdev-release.json",
		"bin/rdev-verify.exe",
		"bin/rdev-verify.exe.rdev-release.json",
	} {
		if _, ok := archiveEntries[path]; !ok {
			t.Fatalf("expected Connection Entry archive entry %s", path)
		}
	}
	var entryPackage ConnectionEntryReleasePackage
	if err := json.Unmarshal(archiveEntries["connection-entry-release.json"], &entryPackage); err != nil {
		t.Fatal(err)
	}
	if entryPackage.SchemaVersion != ConnectionEntryReleasePackageSchemaVersion ||
		!entryPackage.NoPrivateParameters ||
		entryPackage.ExecutionMode != "runtime-invite-required" ||
		strings.Join(entryPackage.RequiredReleaseArtifacts, ",") != "rdev,rdev-host.exe,rdev-verify.exe" ||
		len(entryPackage.Launchers) < 1 ||
		len(entryPackage.Artifacts) < len(candidate.Artifacts) {
		t.Fatalf("unexpected Connection Entry release package: %#v", entryPackage)
	}
	launcher := string(archiveEntries["launchers/start-connection-entry.sh"])
	for _, want := range []string{
		"rdev-verify",
		"--bundle",
		"--root-public-key",
		"--require-artifacts",
		candidate.RootPublicKey,
		"rdev,rdev-host.exe,rdev-verify.exe",
	} {
		if !strings.Contains(launcher, want) {
			t.Fatalf("expected Connection Entry launcher to contain %q, got %s", want, launcher)
		}
	}
	if strings.Contains(string(archiveEntries["connection-entry-release.json"]), filepath.Dir(out)) ||
		strings.Contains(string(archiveEntries["CONNECTION_ENTRY_RELEASE.md"]), "192.168.") {
		t.Fatalf("Connection Entry archive leaked private/local metadata")
	}
	sbom := readReleaseCandidateTestFile(t, filepath.Join(out, "sbom.spdx.json"))
	for _, want := range []string{`"spdxVersion": "SPDX-2.3"`, `"fileName": "./rdev-host.exe"`, `"algorithm": "SHA256"`} {
		if !strings.Contains(sbom, want) {
			t.Fatalf("expected SBOM to contain %q, got %s", want, sbom)
		}
	}
	provenance := readReleaseCandidateTestFile(t, filepath.Join(out, "provenance.json"))
	for _, want := range []string{`"schema_version": "rdev.release-provenance.v1"`, `"external_mutation": false`, `"path": "rdev-host.exe"`, `"path": "sbom.spdx.json"`} {
		if !strings.Contains(provenance, want) {
			t.Fatalf("expected provenance to contain %q, got %s", want, provenance)
		}
	}
	candidateJSON := readReleaseCandidateTestFile(t, filepath.Join(out, "release-candidate.json"))
	if strings.Contains(candidateJSON, filepath.Dir(rdev)) {
		t.Fatalf("release candidate should not leak source artifact directory %q: %s", filepath.Dir(rdev), candidateJSON)
	}
	if strings.Contains(candidateJSON, out) || strings.Contains(candidateJSON, filepath.Dir(out)) {
		t.Fatalf("release candidate should not leak local output directory %q: %s", out, candidateJSON)
	}
	for _, want := range []string{`"out_dir": "."`, `"release_bundle_path": "release-bundle.json"`, `"skillkit_path": "skillkit"`, `"sbom_path": "sbom.spdx.json"`, `"provenance_path": "provenance.json"`, `"checksums_path": "checksums.txt"`} {
		if !strings.Contains(candidateJSON, want) {
			t.Fatalf("expected candidate summary to contain %q, got %s", want, candidateJSON)
		}
	}
	if !strings.Contains(candidateJSON, `"connection_entry_path": "connection-entry-release.zip"`) {
		t.Fatalf("expected candidate summary to include Connection Entry archive path, got %s", candidateJSON)
	}
}

func TestPrepareCandidateWritesSignedWindowsLayeredAssets(t *testing.T) {
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	core := writeCandidateArtifactForTest(t, input, "rdev-host-windows-amd64.exe", "windows-core-runtime")
	helper := writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary")
	nearMatch := writeCandidateArtifactForTest(t, input, "rdev-host-windows-amd64.exe.bak", "not-a-core-runtime")
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	candidate, err := PrepareCandidate(CandidateOptions{
		SourceRoot:    filepath.Join("..", ".."),
		OutDir:        out,
		Version:       "v0.2.0",
		ArtifactPaths: []string{core, helper, nearMatch},
		Key:           key,
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.LayeredAssetManifestPath != layeredAssetManifestPath {
		t.Fatalf("unexpected layered asset manifest path %q", candidate.LayeredAssetManifestPath)
	}

	manifestContent, err := os.ReadFile(filepath.Join(out, layeredAssetManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestContent), input) || strings.Contains(string(manifestContent), out) {
		t.Fatalf("layered asset manifest leaked a private input or output path: %s", manifestContent)
	}
	var manifest LayeredAssetManifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		t.Fatal(err)
	}
	root, err := parseCandidateRootPublicKey(candidate.RootPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyLayeredAssetManifest(manifest, root, now); err != nil {
		t.Fatalf("layered asset manifest did not verify with candidate root: %v", err)
	}
	if len(manifest.Assets) != 1 {
		t.Fatalf("expected only the exact Windows host artifact in layered manifest, got %#v", manifest.Assets)
	}
	selected, err := SelectLayeredAsset(manifest, "windows/amd64", layeredAssetKindCoreRuntime, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "rdev-host-windows-amd64" ||
		selected.Platform != "windows/amd64" ||
		selected.RelativePath != "assets/rdev-host-windows-amd64.exe" ||
		selected.Kind != layeredAssetKindCoreRuntime {
		t.Fatalf("unexpected Windows core runtime selection: %#v", selected)
	}
	var coreSHA256 string
	for _, artifact := range candidate.Artifacts {
		if artifact.Name == "rdev-host-windows-amd64.exe" {
			coreSHA256 = artifact.SHA256
			break
		}
	}
	if coreSHA256 == "" || selected.SHA256 != "sha256:"+coreSHA256 {
		t.Fatalf("selected runtime digest does not match staged core runtime: %q", selected.SHA256)
	}

	var manifestFile CandidateFile
	manifestFileCount := 0
	for _, file := range candidate.Files {
		if file.Path == layeredAssetManifestPath {
			manifestFile = file
			manifestFileCount++
		}
	}
	if manifestFileCount != 1 || manifestFile.Kind != "layered-asset-manifest" || manifestFile.SHA256 == "" || manifestFile.SizeBytes <= 0 {
		t.Fatalf("candidate files missing layered manifest metadata: %#v", manifestFile)
	}
	checksums, err := os.ReadFile(filepath.Join(out, candidate.ChecksumsPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(checksums), "  "+layeredAssetManifestPath+"\n") != 1 {
		t.Fatalf("candidate checksums omitted layered manifest: %s", checksums)
	}

	archiveEntries := readConnectionEntryArchiveForTest(t, filepath.Join(out, candidate.ConnectionEntryPath))
	bundleContent := readReleaseCandidateTestFile(t, filepath.Join(out, candidate.ReleaseBundlePath))
	sbomContent := readReleaseCandidateTestFile(t, filepath.Join(out, candidate.SBOMPath))
	for _, artifact := range []string{
		"rdev-host-windows-amd64.exe",
		"rdev-host-windows-amd64.exe.bak",
		"rdev-verify.exe",
	} {
		if !strings.Contains(bundleContent, `"name": "`+artifact+`"`) || !strings.Contains(sbomContent, `"fileName": "./`+artifact+`"`) {
			t.Fatalf("bundle or SBOM omitted staged artifact %s", artifact)
		}
		for _, archivePath := range []string{"bin/" + artifact, "bin/" + artifact + ".rdev-release.json"} {
			if _, ok := archiveEntries[archivePath]; !ok {
				t.Fatalf("existing archive omitted staged artifact %s", archivePath)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(out, rdevHostWindowsAMD64AssetName)); err != nil {
		t.Fatalf("core runtime moved from existing root staging layout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "assets", rdevHostWindowsAMD64AssetName)); !os.IsNotExist(err) {
		t.Fatalf("public asset URL path should not duplicate the staged runtime: %v", err)
	}
	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out, GeneratedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("candidate with layered Windows runtime did not verify: %#v", verification.Checks)
	}

	manifest.Assets[0].RelativePath = "assets/rdev-host-windows-amd64.exe.bak"
	tamperedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(out, layeredAssetManifestPath)
	if err := os.WriteFile(manifestPath, append(tamperedManifest, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestSHA, manifestSize, err := fileDigest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	filesForChecksums := make([]CandidateFile, 0, len(candidate.Files)-1)
	for index := range candidate.Files {
		switch candidate.Files[index].Path {
		case layeredAssetManifestPath:
			candidate.Files[index].SHA256 = "sha256:" + manifestSHA
			candidate.Files[index].SizeBytes = manifestSize
		case candidate.ChecksumsPath:
			continue
		}
		filesForChecksums = append(filesForChecksums, candidate.Files[index])
	}
	checksumEntry, err := writeCandidateChecksums(filepath.Join(out, candidate.ChecksumsPath), filesForChecksums)
	if err != nil {
		t.Fatal(err)
	}
	for index := range candidate.Files {
		if candidate.Files[index].Path == candidate.ChecksumsPath {
			candidate.Files[index] = checksumEntry
			break
		}
	}
	if err := writeCandidate(filepath.Join(out, "release-candidate.json"), candidate); err != nil {
		t.Fatal(err)
	}
	tamperedVerification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out, GeneratedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if tamperedVerification.OK() {
		t.Fatal("expected candidate verification to reject a tampered layered manifest even when unsigned checksums were updated")
	}
	if !strings.Contains(failedCandidateVerificationNames(tamperedVerification), "layered_asset_manifest_verified") {
		t.Fatalf("expected layered signature failure, got %s", failedCandidateVerificationNames(tamperedVerification))
	}

	wrongRoot := candidate
	wrongRoot.RootPublicKey = "other-root:" + strings.SplitN(candidate.RootPublicKey, ":", 2)[1]
	if _, err := WriteLayeredAssetManifest(filepath.Join(out, "wrong-root-layered-assets.json"), wrongRoot, key, now); err == nil {
		t.Fatal("expected layered manifest writer to reject a candidate root that does not match the signing key")
	}
	if err := os.WriteFile(filepath.Join(out, rdevHostWindowsAMD64AssetName), []byte("tampered-runtime"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLayeredAssetManifest(filepath.Join(out, "tampered-layered-assets.json"), candidate, key, now); err == nil {
		t.Fatal("expected layered manifest writer to reject staged runtime metadata mismatch")
	}
}

func TestVerifyCandidateRequiresLayeredManifestForSignedWindowsCore(t *testing.T) {
	out, candidate, _, now := prepareWindowsLayeredCandidateForTest(t)
	root, err := parseCandidateRootPublicKey(candidate.RootPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	bundleVerification, err := VerifyBundle(filepath.Join(out, candidate.ReleaseBundlePath), root, nil)
	if err != nil || !bundleVerification.OK() {
		t.Fatalf("signed release bundle should verify: %#v, %v", bundleVerification.Checks, err)
	}
	signedCorePresent := false
	for _, artifact := range bundleVerification.Artifacts {
		if artifact.Name == rdevHostWindowsAMD64AssetName && artifact.Artifact == rdevHostWindowsAMD64AssetName {
			signedCorePresent = true
			break
		}
	}
	if !signedCorePresent {
		t.Fatal("test fixture signed bundle does not contain the Windows core runtime")
	}

	downgraded := candidate
	downgraded.LayeredAssetManifestPath = ""
	downgraded.Files = make([]CandidateFile, 0, len(candidate.Files)-1)
	for _, file := range candidate.Files {
		if file.Path != layeredAssetManifestPath {
			downgraded.Files = append(downgraded.Files, file)
		}
	}
	if err := os.Remove(filepath.Join(out, layeredAssetManifestPath)); err != nil {
		t.Fatal(err)
	}
	rewriteCandidateChecksumsForTest(t, out, &downgraded)

	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out, GeneratedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected signed Windows core bundle to require its layered manifest despite unsigned metadata removal")
	}
	if !strings.Contains(failedCandidateVerificationNames(verification), "layered_asset_manifest_path") {
		t.Fatalf("expected missing layered manifest path failure, got %s", failedCandidateVerificationNames(verification))
	}
}

func TestVerifyCandidateRejectsLayeredDeclarationWithoutSignedWindowsCore(t *testing.T) {
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	candidate, err := PrepareCandidate(CandidateOptions{
		SourceRoot:    filepath.Join("..", ".."),
		OutDir:        out,
		Version:       "v0.2.0",
		ArtifactPaths: []string{writeCandidateArtifactForTest(t, input, "rdev-host.exe", "legacy-host-runtime")},
		Key:           key,
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	declared := candidate
	declared.LayeredAssetManifestPath = layeredAssetManifestPath
	if err := writeCandidate(filepath.Join(out, "release-candidate.json"), declared); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out, GeneratedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected layered declaration without a signed Windows core artifact to fail verification")
	}
	if !strings.Contains(failedCandidateVerificationNames(verification), "layered_asset_manifest_has_signed_windows_core") {
		t.Fatalf("expected signed Windows core requirement failure, got %s", failedCandidateVerificationNames(verification))
	}
}

func TestVerifyCandidateRejectsSignedLayeredManifestWithExtraAsset(t *testing.T) {
	out, candidate, key, now := prepareWindowsLayeredCandidateForTest(t)
	manifest := readLayeredAssetManifestForTest(t, filepath.Join(out, layeredAssetManifestPath))
	var helper CandidateArtifact
	for _, artifact := range candidate.Artifacts {
		if artifact.Name == "rdev-verify.exe" {
			helper = artifact
			break
		}
	}
	manifest.Assets = append(manifest.Assets, LayeredAsset{
		ID:           "rdev-verify-windows-amd64",
		Platform:     "windows/amd64",
		Kind:         layeredAssetKindOptionalHelper,
		RelativePath: "assets/rdev-verify.exe",
		SHA256:       "sha256:" + helper.SHA256,
		SizeBytes:    helper.SizeBytes,
	})
	signed, err := SignLayeredAssetManifest(manifest, key)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := candidate
	rewritten.Files = append([]CandidateFile(nil), candidate.Files...)
	writeLayeredAssetManifestForTest(t, filepath.Join(out, layeredAssetManifestPath), signed)
	refreshCandidateFileForTest(t, out, &rewritten, layeredAssetManifestPath)
	rewriteCandidateChecksumsForTest(t, out, &rewritten)

	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out, GeneratedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected candidate verification to reject a signed layered manifest with an extra asset")
	}
	if !strings.Contains(failedCandidateVerificationNames(verification), "layered_asset_manifest_windows_core_contract") {
		t.Fatalf("expected exact layered manifest contract failure, got %s", failedCandidateVerificationNames(verification))
	}
}

func TestVerifyCandidateRejectsDuplicateLayeredManifestChecksum(t *testing.T) {
	out, candidate, _, now := prepareWindowsLayeredCandidateForTest(t)
	checksumsPath := filepath.Join(out, candidate.ChecksumsPath)
	content, err := os.ReadFile(checksumsPath)
	if err != nil {
		t.Fatal(err)
	}
	var layeredLine string
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if strings.HasSuffix(line, "  "+layeredAssetManifestPath) {
			layeredLine = line
			break
		}
	}
	if layeredLine == "" {
		t.Fatal("layered manifest checksum line missing from fixture")
	}
	if err := os.WriteFile(checksumsPath, append(content, []byte(layeredLine+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	rewritten := candidate
	rewritten.Files = append([]CandidateFile(nil), candidate.Files...)
	refreshCandidateFileForTest(t, out, &rewritten, candidate.ChecksumsPath)
	if err := writeCandidate(filepath.Join(out, "release-candidate.json"), rewritten); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out, GeneratedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected duplicate layered manifest checksum line to fail verification")
	}
	if !strings.Contains(failedCandidateVerificationNames(verification), "checksums_paths_unique") {
		t.Fatalf("expected duplicate checksum path failure, got %s", failedCandidateVerificationNames(verification))
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

func TestVerifyCandidateDetectsTamperedProvenance(t *testing.T) {
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
	provenancePath := filepath.Join(out, "provenance.json")
	provenance := strings.Replace(readReleaseCandidateTestFile(t, provenancePath), `"sha256": "`, `"sha256": "0000000000000000000000000000000000000000000000000000000000000000", "_old": "`, 1)
	if err := os.WriteFile(provenancePath, []byte(provenance), 0o644); err != nil {
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
		t.Fatal("expected tampered provenance to fail verification")
	}
	failures := failedCandidateVerificationNames(verification)
	if !strings.Contains(failures, "provenance.json:file_sha256_matches") ||
		!strings.Contains(failures, "provenance_hashes_match_subjects") {
		t.Fatalf("expected provenance checksum and content failures, got %s", failures)
	}
}

func TestVerifyCandidateDetectsTamperedConnectionEntryReleaseArchive(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(out, "connection-entry-release.zip"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyCandidate(CandidateVerifyOptions{CandidatePath: out})
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected tampered Connection Entry release archive to fail verification")
	}
	failures := failedCandidateVerificationNames(verification)
	if !strings.Contains(failures, "connection-entry-release.zip:file_sha256_matches") ||
		!strings.Contains(failures, "connection_entry_release_archive_exists") {
		t.Fatalf("expected archive checksum and zip failures, got %s", failures)
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

func prepareWindowsLayeredCandidateForTest(t *testing.T) (string, Candidate, signing.Key, time.Time) {
	t.Helper()
	input := t.TempDir()
	out := filepath.Join(t.TempDir(), "candidate")
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	candidate, err := PrepareCandidate(CandidateOptions{
		SourceRoot: filepath.Join("..", ".."),
		OutDir:     out,
		Version:    "v0.2.0",
		ArtifactPaths: []string{
			writeCandidateArtifactForTest(t, input, rdevHostWindowsAMD64AssetName, "windows-core-runtime"),
			writeCandidateArtifactForTest(t, input, "rdev-verify.exe", "verify-binary"),
		},
		Key: key,
		Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out, candidate, key, now
}

func readLayeredAssetManifestForTest(t *testing.T, path string) LayeredAssetManifest {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest LayeredAssetManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeLayeredAssetManifestForTest(t *testing.T, path string, manifest LayeredAssetManifest) {
	t.Helper()
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func refreshCandidateFileForTest(t *testing.T, out string, candidate *Candidate, path string) {
	t.Helper()
	sha, size, err := fileDigest(filepath.Join(out, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	for index := range candidate.Files {
		if candidate.Files[index].Path == path {
			candidate.Files[index].SHA256 = "sha256:" + sha
			candidate.Files[index].SizeBytes = size
			return
		}
	}
	t.Fatalf("candidate file %s not found", path)
}

func rewriteCandidateChecksumsForTest(t *testing.T, out string, candidate *Candidate) {
	t.Helper()
	files := make([]CandidateFile, 0, len(candidate.Files)-1)
	for _, file := range candidate.Files {
		if file.Path != candidate.ChecksumsPath {
			files = append(files, file)
		}
	}
	checksumEntry, err := writeCandidateChecksums(filepath.Join(out, candidate.ChecksumsPath), files)
	if err != nil {
		t.Fatal(err)
	}
	for index := range candidate.Files {
		if candidate.Files[index].Path == candidate.ChecksumsPath {
			candidate.Files[index] = checksumEntry
			if err := writeCandidate(filepath.Join(out, "release-candidate.json"), *candidate); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("candidate checksums file metadata not found")
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

func readConnectionEntryArchiveForTest(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := map[string][]byte{}
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(handle)
		_ = handle.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = data
	}
	return entries
}
