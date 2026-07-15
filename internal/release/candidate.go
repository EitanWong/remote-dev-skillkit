package release

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/skillkit"
)

const CandidateSchemaVersion = "rdev.release-candidate.v1"
const CandidateVerificationSchemaVersion = "rdev.release-candidate-verification.v1"
const ConnectionEntryReleasePackageSchemaVersion = "rdev.connection-entry-release-package.v1"
const connectionEntryReleaseArchivePath = "connection-entry-release.zip"
const layeredAssetManifestPath = "layered-assets.json"

const rdevHostWindowsAMD64AssetName = "rdev-host-windows-amd64.exe"

type CandidateOptions struct {
	SourceRoot        string
	OutDir            string
	Version           string
	GatewayURL        string
	ArtifactPaths     []string
	RequiredArtifacts []string
	Key               signing.Key
	Now               time.Time
}

type Candidate struct {
	SchemaVersion            string                      `json:"schema_version"`
	Version                  string                      `json:"version"`
	GeneratedAt              time.Time                   `json:"generated_at"`
	OutDir                   string                      `json:"out_dir"`
	RootPublicKey            string                      `json:"root_public_key"`
	ReleaseBundlePath        string                      `json:"release_bundle_path"`
	SkillkitPath             string                      `json:"skillkit_path"`
	SBOMPath                 string                      `json:"sbom_path"`
	ProvenancePath           string                      `json:"provenance_path"`
	ConnectionEntryPath      string                      `json:"connection_entry_path"`
	LayeredAssetManifestPath string                      `json:"layered_asset_manifest_path,omitempty"`
	ChecksumsPath            string                      `json:"checksums_path"`
	Artifacts                []CandidateArtifact         `json:"artifacts"`
	SkillkitVerification     skillkit.VerificationReport `json:"skillkit_verification"`
	BundleVerification       BundleVerification          `json:"bundle_verification"`
	Checks                   []CandidateCheck            `json:"checks"`
	Files                    []CandidateFile             `json:"files"`
	RecommendedActions       []string                    `json:"recommended_actions,omitempty"`
}

type CandidateArtifact struct {
	Name         string `json:"name"`
	SourcePath   string `json:"source_path,omitempty"`
	ArtifactPath string `json:"artifact_path"`
	ManifestPath string `json:"manifest_path"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
}

type CandidateCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type CandidateFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Kind      string `json:"kind"`
}

type ConnectionEntryReleasePackage struct {
	SchemaVersion            string                              `json:"schema_version"`
	Version                  string                              `json:"version"`
	GeneratedAt              time.Time                           `json:"generated_at"`
	RootPublicKey            string                              `json:"root_public_key"`
	ArchiveKind              string                              `json:"archive_kind"`
	ExecutionMode            string                              `json:"execution_mode"`
	NoPrivateParameters      bool                                `json:"no_private_parameters"`
	RequiredRuntimeData      []string                            `json:"required_runtime_data"`
	RequiredReleaseArtifacts []string                            `json:"required_release_artifacts"`
	Artifacts                []ConnectionEntryReleasePackageFile `json:"artifacts"`
	ReleaseMetadata          []ConnectionEntryReleasePackageFile `json:"release_metadata"`
	Launchers                []ConnectionEntryReleasePackageFile `json:"launchers"`
	ChecksumsPath            string                              `json:"checksums_path"`
	AgentRules               []string                            `json:"agent_rules"`
}

type ConnectionEntryReleasePackageFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Kind      string `json:"kind"`
}

type CandidateVerifyOptions struct {
	CandidatePath     string
	RequiredArtifacts []string
	GeneratedAt       time.Time
}

type CandidateVerification struct {
	SchemaVersion        string                      `json:"schema_version"`
	CandidatePath        string                      `json:"candidate_path"`
	CandidateDir         string                      `json:"candidate_dir"`
	Version              string                      `json:"version,omitempty"`
	GeneratedAt          time.Time                   `json:"generated_at"`
	RootPublicKey        string                      `json:"root_public_key,omitempty"`
	RequiredArtifacts    []string                    `json:"required_artifacts,omitempty"`
	Checks               []CandidateCheck            `json:"checks"`
	Files                []CandidateFileVerification `json:"files"`
	BundleVerification   BundleVerification          `json:"bundle_verification"`
	SkillkitVerification skillkit.VerificationReport `json:"skillkit_verification"`
	RecommendedActions   []string                    `json:"recommended_actions,omitempty"`
}

type CandidateFileVerification struct {
	Path           string           `json:"path"`
	Kind           string           `json:"kind,omitempty"`
	ExpectedSHA256 string           `json:"expected_sha256,omitempty"`
	ActualSHA256   string           `json:"actual_sha256,omitempty"`
	ExpectedSize   int64            `json:"expected_size,omitempty"`
	ActualSize     int64            `json:"actual_size,omitempty"`
	Checks         []CandidateCheck `json:"checks"`
}

func (v CandidateVerification) OK() bool {
	if len(v.Checks) == 0 || len(v.Files) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	for _, file := range v.Files {
		if len(file.Checks) == 0 {
			return false
		}
		for _, check := range file.Checks {
			if !check.Passed {
				return false
			}
		}
	}
	return v.BundleVerification.OK() && v.SkillkitVerification.OK()
}

func (c Candidate) OK() bool {
	if len(c.Checks) == 0 {
		return false
	}
	for _, check := range c.Checks {
		if !check.Passed {
			return false
		}
	}
	return c.SkillkitVerification.OK() && c.BundleVerification.OK()
}

func PrepareCandidate(opts CandidateOptions) (Candidate, error) {
	if strings.TrimSpace(opts.OutDir) == "" {
		return Candidate{}, fmt.Errorf("out is required")
	}
	if strings.TrimSpace(opts.Version) == "" {
		return Candidate{}, fmt.Errorf("version is required")
	}
	if len(opts.ArtifactPaths) == 0 {
		return Candidate{}, fmt.Errorf("artifacts are required")
	}
	if err := validateBundleSigningKey(opts.Key); err != nil {
		return Candidate{}, err
	}
	sourceRoot := opts.SourceRoot
	if sourceRoot == "" {
		sourceRoot = "."
	}
	sourceRootAbs, err := filepath.Abs(sourceRoot)
	if err != nil {
		return Candidate{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return Candidate{}, err
	}
	if err := prepareCandidateOut(outDir); err != nil {
		return Candidate{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	candidate := Candidate{
		SchemaVersion: CandidateSchemaVersion,
		Version:       opts.Version,
		GeneratedAt:   now.UTC(),
		OutDir:        ".",
		RootPublicKey: encodeReleaseRoot(opts.Key),
		SkillkitPath:  "skillkit",
	}
	add := func(name string, passed bool, detail string) {
		candidate.Checks = append(candidate.Checks, CandidateCheck{Name: name, Passed: passed, Detail: detail})
	}

	artifacts, artifactIDs, err := stageAndSignCandidateArtifacts(outDir, opts.ArtifactPaths, opts.Key, now)
	if err != nil {
		return Candidate{}, err
	}
	candidate.Artifacts = artifacts
	add("artifacts_staged", len(artifacts) == len(opts.ArtifactPaths), fmt.Sprintf("%d", len(artifacts)))
	add("artifact_manifests_written", artifactManifestsWritten(artifacts), "")
	if candidateHasWindowsCoreRuntime(candidate) {
		candidate.LayeredAssetManifestPath = layeredAssetManifestPath
		layeredAssetPath := filepath.Join(outDir, candidate.LayeredAssetManifestPath)
		layeredAssetEntry, err := WriteLayeredAssetManifest(layeredAssetPath, candidate, opts.Key, now)
		if err != nil {
			return Candidate{}, err
		}
		add("layered_asset_manifest_written", pathExists(layeredAssetPath), layeredAssetEntry.Path)
	}

	required := cleanStringList(opts.RequiredArtifacts)
	if len(required) == 0 {
		required = artifactIDs
	}
	bundle, err := CreateBundle(BundleOptions{
		Dir:               outDir,
		ArtifactPaths:     artifactIDs,
		RequiredArtifacts: required,
		Key:               opts.Key,
		Now:               now,
	})
	if err != nil {
		return Candidate{}, err
	}
	candidate.ReleaseBundlePath = "release-bundle.json"
	releaseBundlePath := filepath.Join(outDir, candidate.ReleaseBundlePath)
	if err := WriteBundle(releaseBundlePath, bundle); err != nil {
		return Candidate{}, err
	}
	add("release_bundle_written", pathExists(releaseBundlePath), candidate.ReleaseBundlePath)

	root := model.NewTrustBundle(opts.Key.ID, opts.Key.PublicKey)
	bundleVerification, err := VerifyBundle(releaseBundlePath, root, required)
	if err != nil {
		return Candidate{}, err
	}
	candidate.BundleVerification = bundleVerification
	candidate.BundleVerification = publicCandidateBundleVerification(candidate.BundleVerification)
	add("release_bundle_verified", bundleVerification.OK(), failedBundleCheckNames(bundleVerification))

	skillManifest, err := skillkit.Export(skillkit.ExportOptions{
		SourceRoot:  sourceRootAbs,
		OutDir:      filepath.Join(outDir, candidate.SkillkitPath),
		GatewayURL:  opts.GatewayURL,
		GeneratedAt: now,
	})
	if err != nil {
		return Candidate{}, err
	}
	add("skillkit_exported", skillManifest.SchemaVersion == skillkit.ManifestSchemaVersion, skillManifest.SchemaVersion)
	skillkitVerification, err := skillkit.Verify(skillkit.VerifyOptions{
		BundleDir:   filepath.Join(outDir, candidate.SkillkitPath),
		GeneratedAt: now,
	})
	if err != nil {
		return Candidate{}, err
	}
	candidate.SkillkitVerification = skillkitVerification
	candidate.SkillkitVerification = publicCandidateSkillkitVerification(candidate.SkillkitVerification)
	add("skillkit_verified", skillkitVerification.OK(), failedSkillkitCheckNames(skillkitVerification))

	candidate.SBOMPath = "sbom.spdx.json"
	sbomPath := filepath.Join(outDir, candidate.SBOMPath)
	sbomEntry, err := WriteCandidateSBOM(sbomPath, candidate.Version, candidate.Artifacts, now)
	if err != nil {
		return Candidate{}, err
	}
	add("sbom_written", pathExists(sbomPath), sbomEntry.Path)

	provenanceFiles, err := collectCandidateFiles(outDir, map[string]bool{
		"release-candidate.json": true,
		"checksums.txt":          true,
		"provenance.json":        true,
	})
	if err != nil {
		return Candidate{}, err
	}
	candidate.ProvenancePath = "provenance.json"
	provenancePath := filepath.Join(outDir, candidate.ProvenancePath)
	provenanceEntry, err := WriteCandidateProvenance(provenancePath, candidate, provenanceFiles, now)
	if err != nil {
		return Candidate{}, err
	}
	add("provenance_written", pathExists(provenancePath), provenanceEntry.Path)

	candidate.ConnectionEntryPath = connectionEntryReleaseArchivePath
	connectionEntryPath := filepath.Join(outDir, candidate.ConnectionEntryPath)
	connectionEntryEntry, err := WriteConnectionEntryReleaseArchive(connectionEntryPath, outDir, candidate, now)
	if err != nil {
		return Candidate{}, err
	}
	add("connection_entry_release_archive_written", pathExists(connectionEntryPath), connectionEntryEntry.Path)

	files, err := collectCandidateFiles(outDir, map[string]bool{
		"release-candidate.json": true,
		"checksums.txt":          true,
	})
	if err != nil {
		return Candidate{}, err
	}
	candidate.ChecksumsPath = "checksums.txt"
	checksumsPath := filepath.Join(outDir, candidate.ChecksumsPath)
	checksumEntry, err := writeCandidateChecksums(checksumsPath, files)
	if err != nil {
		return Candidate{}, err
	}
	files = append(files, checksumEntry)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	candidate.Files = files
	add("checksums_written", pathExists(checksumsPath), candidate.ChecksumsPath)

	if !candidate.OK() {
		candidate.RecommendedActions = []string{
			"Rebuild the release candidate from a clean output directory.",
			"Do not publish GitHub release assets until release bundle and Skillkit verification both pass.",
			"Check release-candidate.json, release-bundle.json, sbom.spdx.json, provenance.json, skillkit/manifest.json, and checksums.txt for the first failed check.",
		}
	}
	if err := writeCandidate(filepath.Join(outDir, "release-candidate.json"), candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

func VerifyCandidate(opts CandidateVerifyOptions) (CandidateVerification, error) {
	candidatePath, candidateDir, err := resolveCandidateInput(opts.CandidatePath)
	if err != nil {
		return CandidateVerification{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	verification := CandidateVerification{
		SchemaVersion:     CandidateVerificationSchemaVersion,
		CandidatePath:     "release-candidate.json",
		CandidateDir:      ".",
		GeneratedAt:       now.UTC(),
		RequiredArtifacts: cleanStringList(opts.RequiredArtifacts),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, CandidateCheck{Name: name, Passed: passed, Detail: detail})
	}

	content, err := os.ReadFile(candidatePath)
	add("candidate_file_exists", err == nil, "release-candidate.json")
	if err != nil {
		verification.RecommendedActions = failedCandidateVerificationActions()
		return verification, nil
	}
	var candidate Candidate
	err = json.Unmarshal(content, &candidate)
	add("candidate_json_valid", err == nil, errorDetail(err))
	if err != nil {
		verification.RecommendedActions = failedCandidateVerificationActions()
		return verification, nil
	}
	verification.Version = candidate.Version
	verification.RootPublicKey = candidate.RootPublicKey
	add("candidate_schema", candidate.SchemaVersion == CandidateSchemaVersion, candidate.SchemaVersion)
	add("candidate_version_present", strings.TrimSpace(candidate.Version) != "", candidate.Version)
	add("candidate_files_present", len(candidate.Files) > 0, fmt.Sprintf("%d", len(candidate.Files)))
	add("candidate_artifacts_present", len(candidate.Artifacts) > 0, fmt.Sprintf("%d", len(candidate.Artifacts)))
	add("candidate_sbom_listed", candidateHasFile(candidate.Files, "sbom.spdx.json"), "sbom.spdx.json")
	add("candidate_provenance_listed", candidateHasFile(candidate.Files, "provenance.json"), "provenance.json")
	add("candidate_connection_entry_release_archive_listed", candidateHasFile(candidate.Files, connectionEntryReleaseArchivePath), connectionEntryReleaseArchivePath)

	root, rootErr := parseCandidateRootPublicKey(candidate.RootPublicKey)
	add("candidate_root_public_key_valid", rootErr == nil, errorDetail(rootErr))
	if rootErr == nil {
		releaseBundlePath := filepath.Join(candidateDir, "release-bundle.json")
		add("release_bundle_exists", pathExists(releaseBundlePath), "release-bundle.json")
		bundleVerification, err := VerifyBundle(releaseBundlePath, root, opts.RequiredArtifacts)
		if err != nil {
			add("release_bundle_readable", false, publicCandidateDetail(err.Error(), candidateDir))
		} else {
			verification.BundleVerification = bundleVerification
			verification.BundleVerification = publicCandidateBundleVerification(verification.BundleVerification)
			add("release_bundle_verified", bundleVerification.OK(), failedBundleCheckNames(bundleVerification))
			if bundleVerification.OK() {
				signedWindowsCoreCount := verifiedBundleWindowsCoreRuntimeCount(bundleVerification)
				add("signed_bundle_windows_core_unique", signedWindowsCoreCount <= 1, fmt.Sprintf("%d", signedWindowsCoreCount))
				switch {
				case signedWindowsCoreCount == 1:
					verification.Checks = append(verification.Checks, verifyCandidateLayeredAssetManifest(candidateDir, candidate, root, now)...)
				case candidate.LayeredAssetManifestPath != "":
					add("layered_asset_manifest_has_signed_windows_core", false, candidate.LayeredAssetManifestPath)
				}
			}
		}
	} else if candidate.LayeredAssetManifestPath != "" {
		add("layered_asset_manifest_verified", false, "candidate root public key is invalid")
	}

	skillkitDir := filepath.Join(candidateDir, "skillkit")
	add("skillkit_dir_exists", dirExists(skillkitDir), "skillkit")
	skillkitVerification, err := skillkit.Verify(skillkit.VerifyOptions{
		BundleDir:   skillkitDir,
		GeneratedAt: now,
	})
	if err != nil {
		add("skillkit_readable", false, publicCandidateDetail(err.Error(), candidateDir))
	} else {
		verification.SkillkitVerification = skillkitVerification
		verification.SkillkitVerification = publicCandidateSkillkitVerification(verification.SkillkitVerification)
		add("skillkit_verified", skillkitVerification.OK(), failedSkillkitCheckNames(skillkitVerification))
	}

	fileVerification, fileChecks := verifyCandidateFiles(candidateDir, candidate.Files)
	verification.Files = fileVerification
	for _, check := range fileChecks {
		verification.Checks = append(verification.Checks, check)
	}

	checksumChecks := verifyCandidateChecksumFile(candidateDir, candidate.Files)
	for _, check := range checksumChecks {
		verification.Checks = append(verification.Checks, check)
	}

	sbomChecks := VerifyCandidateSBOM(candidateDir, candidate.Artifacts)
	for _, check := range sbomChecks {
		verification.Checks = append(verification.Checks, check)
	}

	provenanceChecks := VerifyCandidateProvenance(candidateDir, candidate)
	for _, check := range provenanceChecks {
		verification.Checks = append(verification.Checks, check)
	}

	connectionEntryChecks := VerifyConnectionEntryReleaseArchive(candidateDir, candidate)
	for _, check := range connectionEntryChecks {
		verification.Checks = append(verification.Checks, check)
	}

	unlisted, unlistedErr := findUnlistedCandidateFiles(candidateDir, candidate.Files)
	add("candidate_has_no_unlisted_files", unlistedErr == nil && len(unlisted) == 0, firstNonEmpty(errorDetail(unlistedErr), strings.Join(unlisted, ",")))

	if missing := missingCandidateRequiredArtifacts(candidate, opts.RequiredArtifacts); missing != "" {
		add("candidate_required_artifacts_present", false, missing)
	} else {
		add("candidate_required_artifacts_present", true, strings.Join(cleanStringList(opts.RequiredArtifacts), ","))
	}

	if !verification.OK() {
		verification.RecommendedActions = failedCandidateVerificationActions()
	}
	return verification, nil
}

func stageAndSignCandidateArtifacts(outDir string, sources []string, key signing.Key, now time.Time) ([]CandidateArtifact, []string, error) {
	seen := map[string]bool{}
	var artifacts []CandidateArtifact
	var artifactIDs []string
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			return nil, nil, fmt.Errorf("empty artifact path")
		}
		sourceAbs, err := filepath.Abs(source)
		if err != nil {
			return nil, nil, err
		}
		name := filepath.Base(sourceAbs)
		if name == "." || name == string(filepath.Separator) || seen[name] {
			return nil, nil, fmt.Errorf("duplicate artifact name %q", name)
		}
		seen[name] = true
		target := filepath.Join(outDir, name)
		if err := copyCandidateFile(sourceAbs, target); err != nil {
			return nil, nil, err
		}
		manifest, err := SignArtifact(target, key, now)
		if err != nil {
			return nil, nil, err
		}
		manifestPath := target + ".rdev-release.json"
		if err := WriteManifest(manifestPath, manifest); err != nil {
			return nil, nil, err
		}
		sha, size, err := fileDigest(target)
		if err != nil {
			return nil, nil, err
		}
		artifacts = append(artifacts, CandidateArtifact{
			Name:         name,
			ArtifactPath: name,
			ManifestPath: name + ".rdev-release.json",
			SHA256:       sha,
			SizeBytes:    size,
		})
		artifactIDs = append(artifactIDs, name)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	sort.Strings(artifactIDs)
	return artifacts, artifactIDs, nil
}

func prepareCandidateOut(dir string) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("output directory must be empty: %s", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func copyCandidateFile(source, target string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func artifactManifestsWritten(artifacts []CandidateArtifact) bool {
	if len(artifacts) == 0 {
		return false
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.ManifestPath) == "" {
			return false
		}
	}
	return true
}

func collectCandidateFiles(root string, skip map[string]bool) ([]CandidateFile, error) {
	var files []CandidateFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		bundlePath := filepath.ToSlash(rel)
		if skip[bundlePath] {
			return nil
		}
		sha, size, err := fileDigest(path)
		if err != nil {
			return err
		}
		files = append(files, CandidateFile{
			Path:      bundlePath,
			SHA256:    "sha256:" + sha,
			SizeBytes: size,
			Kind:      candidateFileKind(bundlePath),
		})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func candidateFileKind(path string) string {
	switch {
	case path == "release-bundle.json":
		return "release-bundle"
	case path == layeredAssetManifestPath:
		return "layered-asset-manifest"
	case path == "sbom.spdx.json":
		return "sbom"
	case path == "provenance.json":
		return "provenance"
	case path == connectionEntryReleaseArchivePath:
		return "connection-entry-release-archive"
	case strings.HasSuffix(path, ".rdev-release.json"):
		return "release-manifest"
	case strings.HasPrefix(path, "skillkit/"):
		return "skillkit"
	default:
		return "artifact"
	}
}

func WriteLayeredAssetManifest(path string, candidate Candidate, key signing.Key, now time.Time) (CandidateFile, error) {
	if candidate.RootPublicKey != encodeReleaseRoot(key) {
		return CandidateFile{}, fmt.Errorf("candidate root public key does not match layered asset signing key")
	}
	assets := make([]LayeredAsset, 0, 1)
	for _, artifact := range candidate.Artifacts {
		if artifact.Name != rdevHostWindowsAMD64AssetName || artifact.ArtifactPath != rdevHostWindowsAMD64AssetName {
			continue
		}
		sha, size, err := fileDigest(filepath.Join(filepath.Dir(path), artifact.ArtifactPath))
		if err != nil {
			return CandidateFile{}, err
		}
		if artifact.SHA256 != sha || artifact.SizeBytes != size {
			return CandidateFile{}, fmt.Errorf("staged artifact metadata mismatch for %s", rdevHostWindowsAMD64AssetName)
		}
		assets = append(assets, LayeredAsset{
			ID:           "rdev-host-windows-amd64",
			Platform:     "windows/amd64",
			Kind:         layeredAssetKindCoreRuntime,
			RelativePath: "assets/" + rdevHostWindowsAMD64AssetName,
			SHA256:       "sha256:" + sha,
			SizeBytes:    size,
		})
	}
	manifest, err := SignLayeredAssetManifest(LayeredAssetManifest{
		SchemaVersion: LayeredAssetManifestSchemaVersion,
		Version:       candidate.Version,
		GeneratedAt:   now.UTC(),
		Assets:        assets,
	}, key)
	if err != nil {
		return CandidateFile{}, err
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return CandidateFile{}, err
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return CandidateFile{}, err
	}
	sha, size, err := fileDigest(path)
	if err != nil {
		return CandidateFile{}, err
	}
	return CandidateFile{
		Path:      layeredAssetManifestPath,
		SHA256:    "sha256:" + sha,
		SizeBytes: size,
		Kind:      "layered-asset-manifest",
	}, nil
}

func candidateHasWindowsCoreRuntime(candidate Candidate) bool {
	for _, artifact := range candidate.Artifacts {
		if artifact.Name == rdevHostWindowsAMD64AssetName && artifact.ArtifactPath == rdevHostWindowsAMD64AssetName {
			return true
		}
	}
	return false
}

func verifyCandidateLayeredAssetManifest(candidateDir string, candidate Candidate, root model.TrustBundle, now time.Time) []CandidateCheck {
	var checks []CandidateCheck
	add := func(name string, passed bool, detail string) {
		checks = append(checks, CandidateCheck{Name: name, Passed: passed, Detail: detail})
	}
	pathValid := candidate.LayeredAssetManifestPath == layeredAssetManifestPath
	add("layered_asset_manifest_path", pathValid, candidate.LayeredAssetManifestPath)
	listed := 0
	listedWithKind := false
	for _, file := range candidate.Files {
		if file.Path == layeredAssetManifestPath {
			listed++
			listedWithKind = listedWithKind || file.Kind == "layered-asset-manifest"
		}
	}
	add("layered_asset_manifest_listed", listed == 1 && listedWithKind, fmt.Sprintf("%d", listed))
	if !pathValid {
		return checks
	}

	content, err := os.ReadFile(filepath.Join(candidateDir, layeredAssetManifestPath))
	add("layered_asset_manifest_exists", err == nil, layeredAssetManifestPath)
	if err != nil {
		return checks
	}
	var manifest LayeredAssetManifest
	err = json.Unmarshal(content, &manifest)
	add("layered_asset_manifest_json_valid", err == nil, errorDetail(err))
	if err != nil {
		return checks
	}
	err = VerifyLayeredAssetManifest(manifest, root, now)
	add("layered_asset_manifest_verified", err == nil, errorDetail(err))
	if err != nil {
		return checks
	}
	add("layered_asset_manifest_version_matches_candidate", manifest.Version == candidate.Version, manifest.Version)
	add("layered_asset_manifest_time_matches_candidate", manifest.GeneratedAt.Equal(candidate.GeneratedAt), manifest.GeneratedAt.UTC().Format(time.RFC3339Nano))
	checks = append(checks, verifyCandidateLayeredWindowsCore(candidateDir, candidate, manifest)...)
	return checks
}

func verifyCandidateLayeredWindowsCore(candidateDir string, candidate Candidate, manifest LayeredAssetManifest) []CandidateCheck {
	var checks []CandidateCheck
	add := func(name string, passed bool, detail string) {
		checks = append(checks, CandidateCheck{Name: name, Passed: passed, Detail: detail})
	}
	asset, err := SelectLayeredAsset(manifest, "windows/amd64", layeredAssetKindCoreRuntime, nil)
	contractValid := len(manifest.Assets) == 1 && err == nil && asset.ID == "rdev-host-windows-amd64" && asset.RelativePath == "assets/"+rdevHostWindowsAMD64AssetName
	add("layered_asset_manifest_windows_core_contract", contractValid, firstNonEmpty(errorDetail(err), fmt.Sprintf("assets=%d", len(manifest.Assets))))
	if !contractValid {
		return checks
	}
	artifact, artifactCount := candidateWindowsCoreRuntime(candidate)
	add("layered_asset_manifest_windows_core_artifact", artifactCount == 1, fmt.Sprintf("%d", artifactCount))
	if artifactCount != 1 {
		return checks
	}
	sha, size, err := fileDigest(filepath.Join(candidateDir, rdevHostWindowsAMD64AssetName))
	add("layered_asset_manifest_windows_core_readable", err == nil, publicCandidateDetail(errorDetail(err), candidateDir))
	if err != nil {
		return checks
	}
	matches := asset.SHA256 == "sha256:"+sha && asset.SizeBytes == size && artifact.SHA256 == sha && artifact.SizeBytes == size
	add("layered_asset_manifest_windows_core_matches_staged", matches, rdevHostWindowsAMD64AssetName)
	return checks
}

func candidateWindowsCoreRuntime(candidate Candidate) (CandidateArtifact, int) {
	var match CandidateArtifact
	count := 0
	for _, artifact := range candidate.Artifacts {
		if artifact.Name == rdevHostWindowsAMD64AssetName && artifact.ArtifactPath == rdevHostWindowsAMD64AssetName {
			match = artifact
			count++
		}
	}
	return match, count
}

func verifiedBundleWindowsCoreRuntimeCount(verification BundleVerification) int {
	count := 0
	for _, artifact := range verification.Artifacts {
		if artifact.Name == rdevHostWindowsAMD64AssetName && artifact.Artifact == rdevHostWindowsAMD64AssetName {
			count++
		}
	}
	return count
}

func candidateHasFile(files []CandidateFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func writeCandidateChecksums(path string, files []CandidateFile) (CandidateFile, error) {
	var builder strings.Builder
	for _, file := range files {
		builder.WriteString(strings.TrimPrefix(file.SHA256, "sha256:"))
		builder.WriteString("  ")
		builder.WriteString(file.Path)
		builder.WriteByte('\n')
	}
	content := []byte(builder.String())
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return CandidateFile{}, err
	}
	sum := sha256.Sum256(content)
	return CandidateFile{
		Path:      "checksums.txt",
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(content)),
		Kind:      "checksums",
	}, nil
}

func writeCandidate(path string, candidate Candidate) error {
	content, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

type connectionEntryArchiveEntry struct {
	Path    string
	Kind    string
	Content []byte
}

func WriteConnectionEntryReleaseArchive(path, candidateDir string, candidate Candidate, generatedAt time.Time) (CandidateFile, error) {
	entries, err := buildConnectionEntryReleaseEntries(candidateDir, candidate, generatedAt)
	if err != nil {
		return CandidateFile{}, err
	}
	if err := writeZipArchive(path, entries); err != nil {
		return CandidateFile{}, err
	}
	sha, size, err := fileDigest(path)
	if err != nil {
		return CandidateFile{}, err
	}
	return CandidateFile{
		Path:      connectionEntryReleaseArchivePath,
		SHA256:    "sha256:" + sha,
		SizeBytes: size,
		Kind:      "connection-entry-release-archive",
	}, nil
}

func buildConnectionEntryReleaseEntries(candidateDir string, candidate Candidate, generatedAt time.Time) ([]connectionEntryArchiveEntry, error) {
	var entries []connectionEntryArchiveEntry
	addContent := func(path, kind string, content []byte) {
		entries = append(entries, connectionEntryArchiveEntry{Path: path, Kind: kind, Content: append([]byte(nil), content...)})
	}
	addCandidateFile := func(sourcePath, archivePath, kind string) error {
		if !safeCandidatePath(sourcePath) || !safeCandidatePath(archivePath) {
			return fmt.Errorf("unsafe connection entry archive path %q -> %q", sourcePath, archivePath)
		}
		content, err := os.ReadFile(filepath.Join(candidateDir, filepath.FromSlash(sourcePath)))
		if err != nil {
			return err
		}
		addContent(archivePath, kind, content)
		return nil
	}

	for _, artifact := range candidate.Artifacts {
		if err := addCandidateFile(artifact.ArtifactPath, "bin/"+filepath.Base(artifact.ArtifactPath), "artifact"); err != nil {
			return nil, err
		}
		if err := addCandidateFile(artifact.ManifestPath, "bin/"+filepath.Base(artifact.ManifestPath), "release-manifest"); err != nil {
			return nil, err
		}
	}
	for _, item := range []struct {
		source string
		target string
		kind   string
	}{
		{candidate.ReleaseBundlePath, "release/release-bundle.json", "release-bundle"},
		{candidate.SBOMPath, "release/sbom.spdx.json", "sbom"},
		{candidate.ProvenancePath, "release/provenance.json", "provenance"},
	} {
		if err := addCandidateFile(item.source, item.target, item.kind); err != nil {
			return nil, err
		}
	}

	addContent("CONNECTION_ENTRY_RELEASE.md", "documentation", []byte(renderConnectionEntryReleaseReadme(candidate)))
	addContent("connection-entry-runner.template.json", "runner-template", []byte(renderConnectionEntryRunnerTemplate(candidate, generatedAt)))
	if connectionEntryReleaseUsesWindows(candidate) {
		addContent("launchers/Start-ConnectionEntry.ps1", "launcher", []byte(renderConnectionEntryPowerShellLauncher(candidate)))
	} else {
		addContent("launchers/start-connection-entry.sh", "launcher", []byte(renderConnectionEntryShellLauncher(candidate)))
	}

	files := connectionEntryPackageFiles(entries)
	manifest := buildConnectionEntryReleasePackage(candidate, files, generatedAt)
	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	manifestContent = append(manifestContent, '\n')
	addContent("connection-entry-release.json", "package-manifest", manifestContent)
	checksums := renderConnectionEntryArchiveChecksums(connectionEntryPackageFiles(entries))
	addContent("connection-entry-checksums.txt", "checksums", []byte(checksums))

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func buildConnectionEntryReleasePackage(candidate Candidate, files []ConnectionEntryReleasePackageFile, generatedAt time.Time) ConnectionEntryReleasePackage {
	var artifacts []ConnectionEntryReleasePackageFile
	var releaseMetadata []ConnectionEntryReleasePackageFile
	var launchers []ConnectionEntryReleasePackageFile
	for _, file := range files {
		switch file.Kind {
		case "artifact", "release-manifest":
			artifacts = append(artifacts, file)
		case "release-bundle", "sbom", "provenance", "runner-template", "documentation":
			releaseMetadata = append(releaseMetadata, file)
		case "launcher":
			launchers = append(launchers, file)
		}
	}
	return ConnectionEntryReleasePackage{
		SchemaVersion:       ConnectionEntryReleasePackageSchemaVersion,
		Version:             candidate.Version,
		GeneratedAt:         generatedAt.UTC(),
		RootPublicKey:       candidate.RootPublicKey,
		ArchiveKind:         "generic-connection-entry-release",
		ExecutionMode:       "runtime-invite-required",
		NoPrivateParameters: true,
		RequiredRuntimeData: []string{
			"join manifest URL or finalized connection-entry-runner.json",
			"manifest root public key",
			"ticket code and gateway candidates from the signed invite/join manifest",
		},
		RequiredReleaseArtifacts: connectionEntryRequiredReleaseArtifacts(candidate),
		Artifacts:                artifacts,
		ReleaseMetadata:          releaseMetadata,
		Launchers:                launchers,
		ChecksumsPath:            "connection-entry-checksums.txt",
		AgentRules: []string{
			"Run the visible launcher; it verifies release/release-bundle.json with the packaged rdev-verify binary before running any packaged rdev binary.",
			"Verify connection-entry-checksums.txt before redistributing this archive.",
			"Do not ask humans to assemble ticket, gateway, transport, or root-key values by hand.",
			"Use rdev support-session connect or rdev connection-entry plan to produce the runtime invite data before execution.",
			"Keep this archive generic; runtime private parameters belong in signed invite metadata, not release assets.",
		},
	}
}

func connectionEntryPackageFiles(entries []connectionEntryArchiveEntry) []ConnectionEntryReleasePackageFile {
	files := make([]ConnectionEntryReleasePackageFile, 0, len(entries))
	for _, entry := range entries {
		sum := sha256.Sum256(entry.Content)
		files = append(files, ConnectionEntryReleasePackageFile{
			Path:      entry.Path,
			SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
			SizeBytes: int64(len(entry.Content)),
			Kind:      entry.Kind,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func renderConnectionEntryArchiveChecksums(files []ConnectionEntryReleasePackageFile) string {
	var builder strings.Builder
	for _, file := range files {
		builder.WriteString(strings.TrimPrefix(file.SHA256, "sha256:"))
		builder.WriteString("  ")
		builder.WriteString(file.Path)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func connectionEntryReleaseUsesWindows(candidate Candidate) bool {
	for _, artifact := range candidate.Artifacts {
		if strings.EqualFold(filepath.Base(artifact.ArtifactPath), "rdev.exe") {
			return true
		}
	}
	return false
}

func connectionEntryRequiredReleaseArtifacts(candidate Candidate) []string {
	var required []string
	for _, artifact := range candidate.Artifacts {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = filepath.Base(artifact.ArtifactPath)
		}
		if name != "" {
			required = append(required, name)
		}
	}
	return cleanStringList(required)
}

func connectionEntryArtifactBase(candidate Candidate, preferred string) string {
	for _, artifact := range candidate.Artifacts {
		base := filepath.Base(artifact.ArtifactPath)
		if base == preferred {
			return base
		}
	}
	if strings.HasSuffix(preferred, ".exe") {
		return preferred
	}
	for _, artifact := range candidate.Artifacts {
		base := filepath.Base(artifact.ArtifactPath)
		if base == preferred+".exe" {
			return base
		}
	}
	return preferred
}

func connectionEntryRequiredArtifactsCSV(candidate Candidate) string {
	return strings.Join(connectionEntryRequiredReleaseArtifacts(candidate), ",")
}

func singleQuoteShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func writeZipArchive(path string, entries []connectionEntryArchiveEntry) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	zipTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, entry := range entries {
		if !safeCandidatePath(entry.Path) {
			_ = writer.Close()
			return fmt.Errorf("unsafe connection entry archive entry %q", entry.Path)
		}
		header := &zip.FileHeader{
			Name:     entry.Path,
			Method:   zip.Deflate,
			Modified: zipTime,
		}
		header.SetMode(0o644)
		if strings.HasSuffix(entry.Path, ".sh") {
			header.SetMode(0o755)
		}
		w, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return err
		}
		if _, err := w.Write(entry.Content); err != nil {
			_ = writer.Close()
			return err
		}
	}
	return writer.Close()
}

func renderConnectionEntryReleaseReadme(candidate Candidate) string {
	return fmt.Sprintf(`# Remote Dev Skillkit Connection Entry Release

Schema: %s
Version: %s

This archive is a generic release package for Connection Entry runners. It
contains signed release artifacts and visible launchers, but it intentionally
does not contain private ticket codes, gateway URLs, root pins, server
addresses, local paths, credentials, or dates that belong to a specific
operator session.

Agent flow:

1. Verify connection-entry-checksums.txt before redistributing this archive.
2. Create runtime invite data with rdev support-session connect or
   rdev connection-entry plan.
3. Materialize a real connection-entry-runner.json from the signed invite or
   join manifest.
4. Run the visible launcher from launchers/. The launcher first verifies
   release/release-bundle.json with the packaged rdev-verify binary and pinned
   release root before it runs packaged rdev:

   rdev-verify --bundle release/release-bundle.json --root-public-key <pinned-root> --require-artifacts <packaged-artifacts>

Humans should receive only the final link, visible command, or package selected
by the Agent. They should not hand-assemble ticket, gateway, transport, release,
checksum, or root-key parameters.
`, ConnectionEntryReleasePackageSchemaVersion, candidate.Version)
}

func renderConnectionEntryRunnerTemplate(candidate Candidate, generatedAt time.Time) string {
	payload := map[string]any{
		"schema_version":          "rdev.connection-entry-runner-template.v1",
		"runner_manifest_schema":  "rdev.connection-entry.runner.v1",
		"version":                 candidate.Version,
		"generated_at":            generatedAt.UTC(),
		"release_root_public_key": candidate.RootPublicKey,
		"execution_mode":          "runtime-invite-required",
		"missing_inputs": []string{
			"manifest_url",
			"manifest_root_public_key",
			"gateway_url",
			"ticket_code",
		},
		"standard_tools": []string{
			"rdev support-session connect",
			"rdev connection-entry plan",
			"rdev connection-entry run --runner-manifest connection-entry-runner.json",
		},
		"private_parameter_policy": "release archive must stay generic; runtime values come from signed invite or join manifest metadata",
	}
	content, _ := json.MarshalIndent(payload, "", "  ")
	return string(append(content, '\n'))
}

func renderConnectionEntryShellLauncher(candidate Candidate) string {
	rdevName := connectionEntryArtifactBase(candidate, "rdev")
	verifyName := connectionEntryArtifactBase(candidate, "rdev-verify")
	return fmt.Sprintf(`#!/bin/sh
set -eu
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PACKAGE_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
MANIFEST="${1:-$PACKAGE_DIR/connection-entry-runner.json}"
RDEV="$PACKAGE_DIR/bin/%s"
VERIFY="$PACKAGE_DIR/bin/%s"
BUNDLE="$PACKAGE_DIR/release/release-bundle.json"
ROOT_PUBLIC_KEY=%s
REQUIRED_ARTIFACTS=%s
if [ ! -f "$MANIFEST" ]; then
  echo "Missing runtime connection-entry-runner.json." >&2
  echo "Ask your Agent to run rdev support-session connect or rdev connection-entry plan first." >&2
  exit 2
fi
if [ ! -x "$RDEV" ]; then
  echo "Missing packaged rdev binary: $RDEV" >&2
  exit 2
fi
if [ ! -x "$VERIFY" ]; then
  echo "Missing packaged rdev-verify binary: $VERIFY" >&2
  exit 2
fi
if [ ! -f "$BUNDLE" ]; then
  echo "Missing release bundle: $BUNDLE" >&2
  exit 2
fi
"$VERIFY" --bundle "$BUNDLE" --root-public-key "$ROOT_PUBLIC_KEY" --require-artifacts "$REQUIRED_ARTIFACTS" >/dev/null
exec "$RDEV" connection-entry run --runner-manifest "$MANIFEST"
`, rdevName, verifyName, singleQuoteShell(candidate.RootPublicKey), singleQuoteShell(connectionEntryRequiredArtifactsCSV(candidate)))
}

func renderConnectionEntryPowerShellLauncher(candidate Candidate) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$PackageDir = Split-Path -Parent $PSScriptRoot
$ManifestPath = if ($args.Count -gt 0) { $args[0] } else { Join-Path $PackageDir 'connection-entry-runner.json' }
$Rdev = Join-Path $PackageDir 'bin\rdev.exe'
$Verifier = Join-Path $PackageDir 'bin\rdev-verify.exe'
$Bundle = Join-Path $PackageDir 'release\release-bundle.json'
$RootPublicKey = '%s'
$RequiredArtifacts = '%s'
if (-not (Test-Path -LiteralPath $ManifestPath)) {
  Write-Error 'Missing runtime connection-entry-runner.json. Ask your Agent to run rdev support-session connect or rdev connection-entry plan first.'
  exit 2
}
if (-not (Test-Path -LiteralPath $Rdev)) {
  Write-Error "Missing packaged rdev binary: $Rdev"
  exit 2
}
if (-not (Test-Path -LiteralPath $Verifier)) {
  Write-Error "Missing packaged rdev-verify binary: $Verifier"
  exit 2
}
if (-not (Test-Path -LiteralPath $Bundle)) {
  Write-Error "Missing release bundle: $Bundle"
  exit 2
}
& $Verifier --bundle $Bundle --root-public-key $RootPublicKey --require-artifacts $RequiredArtifacts | Out-Null
if ($LASTEXITCODE -ne 0) {
  Write-Error 'Release bundle verification failed; refusing to run packaged rdev.'
  exit $LASTEXITCODE
}
& $Rdev connection-entry run --runner-manifest $ManifestPath
exit $LASTEXITCODE
`, strings.ReplaceAll(candidate.RootPublicKey, "'", "''"), strings.ReplaceAll(connectionEntryRequiredArtifactsCSV(candidate), "'", "''"))
}

func publicCandidateBundleVerification(verification BundleVerification) BundleVerification {
	verification.BundlePath = "release-bundle.json"
	return verification
}

func publicCandidateSkillkitVerification(report skillkit.VerificationReport) skillkit.VerificationReport {
	report.BundleDir = "skillkit"
	report.ManifestPath = "skillkit/manifest.json"
	for i, check := range report.Checks {
		if check.Name == "manifest_exists" {
			report.Checks[i].Detail = "skillkit/manifest.json"
		}
	}
	return report
}

func publicCandidateDetail(detail, candidateDir string) string {
	detail = strings.ReplaceAll(detail, candidateDir, ".")
	detail = strings.ReplaceAll(detail, filepath.ToSlash(candidateDir), ".")
	return detail
}

func failedBundleCheckNames(verification BundleVerification) string {
	var failed []string
	for _, check := range verification.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	for _, artifact := range verification.Artifacts {
		for _, check := range artifact.Checks {
			if !check.Passed {
				failed = append(failed, artifact.Name+":"+check.Name)
			}
		}
	}
	sort.Strings(failed)
	return strings.Join(failed, ",")
}

func failedSkillkitCheckNames(verification skillkit.VerificationReport) string {
	var failed []string
	for _, check := range verification.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	sort.Strings(failed)
	return strings.Join(failed, ",")
}

func encodeReleaseRoot(key signing.Key) string {
	return key.ID + ":" + base64.RawURLEncoding.EncodeToString(key.PublicKey)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func resolveCandidateInput(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("candidate is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(abs, "release-candidate.json"), abs, nil
	}
	return abs, filepath.Dir(abs), nil
}

func parseCandidateRootPublicKey(value string) (model.TrustBundle, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return model.TrustBundle{}, fmt.Errorf("root public key must be key_id:base64url_public_key")
	}
	key, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return model.TrustBundle{}, err
	}
	if len(key) != ed25519.PublicKeySize {
		return model.TrustBundle{}, fmt.Errorf("root public key must be %d bytes", ed25519.PublicKeySize)
	}
	return model.NewTrustBundle(parts[0], ed25519.PublicKey(key)), nil
}

func verifyCandidateFiles(candidateDir string, files []CandidateFile) ([]CandidateFileVerification, []CandidateCheck) {
	seen := map[string]bool{}
	var duplicate []string
	var unsafe []string
	var verified []CandidateFileVerification
	for _, file := range files {
		result := CandidateFileVerification{
			Path:           file.Path,
			Kind:           file.Kind,
			ExpectedSHA256: file.SHA256,
			ExpectedSize:   file.SizeBytes,
		}
		add := func(name string, passed bool, detail string) {
			result.Checks = append(result.Checks, CandidateCheck{Name: name, Passed: passed, Detail: detail})
		}
		if seen[file.Path] {
			duplicate = append(duplicate, file.Path)
		}
		seen[file.Path] = true
		safe := safeCandidatePath(file.Path)
		if !safe {
			unsafe = append(unsafe, file.Path)
		}
		add("file_path_safe", safe, file.Path)
		add("expected_sha256_format", strings.HasPrefix(file.SHA256, "sha256:") && isHexSHA256String(strings.TrimPrefix(file.SHA256, "sha256:")), file.SHA256)
		add("expected_size_valid", file.SizeBytes >= 0, fmt.Sprintf("%d", file.SizeBytes))
		if safe {
			path := filepath.Join(candidateDir, filepath.FromSlash(file.Path))
			sha, size, err := fileDigest(path)
			if err == nil {
				result.ActualSHA256 = "sha256:" + sha
				result.ActualSize = size
			}
			add("file_exists", err == nil, errorDetail(err))
			add("file_sha256_matches", err == nil && result.ActualSHA256 == file.SHA256, file.SHA256)
			add("file_size_matches", err == nil && size == file.SizeBytes, fmt.Sprintf("%d", file.SizeBytes))
		}
		verified = append(verified, result)
	}
	sort.Strings(duplicate)
	sort.Strings(unsafe)
	checks := []CandidateCheck{
		{Name: "candidate_file_paths_unique", Passed: len(duplicate) == 0, Detail: strings.Join(duplicate, ",")},
		{Name: "candidate_file_paths_safe", Passed: len(unsafe) == 0, Detail: strings.Join(unsafe, ",")},
	}
	return verified, checks
}

func verifyCandidateChecksumFile(candidateDir string, files []CandidateFile) []CandidateCheck {
	checksumsPath := filepath.Join(candidateDir, "checksums.txt")
	content, err := os.ReadFile(checksumsPath)
	checks := []CandidateCheck{{Name: "checksums_file_exists", Passed: err == nil, Detail: "checksums.txt"}}
	if err != nil {
		return checks
	}
	expected := map[string]string{}
	for _, file := range files {
		if file.Path == "checksums.txt" {
			continue
		}
		expected[file.Path] = strings.TrimPrefix(file.SHA256, "sha256:")
	}
	listed := map[string]string{}
	var malformed []string
	var duplicate []string
	seen := map[string]bool{}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || !isHexSHA256String(parts[0]) || !safeCandidatePath(parts[1]) {
			malformed = append(malformed, line)
			continue
		}
		if seen[parts[1]] {
			duplicate = append(duplicate, parts[1])
			continue
		}
		seen[parts[1]] = true
		listed[parts[1]] = parts[0]
	}
	var missing []string
	var mismatch []string
	for path, sha := range expected {
		got, ok := listed[path]
		if !ok {
			missing = append(missing, path)
			continue
		}
		if got != sha {
			mismatch = append(mismatch, path)
		}
	}
	var unexpected []string
	for path := range listed {
		if _, ok := expected[path]; !ok {
			unexpected = append(unexpected, path)
		}
	}
	sort.Strings(malformed)
	sort.Strings(duplicate)
	sort.Strings(missing)
	sort.Strings(mismatch)
	sort.Strings(unexpected)
	checks = append(checks,
		CandidateCheck{Name: "checksums_lines_valid", Passed: len(malformed) == 0, Detail: strings.Join(malformed, ",")},
		CandidateCheck{Name: "checksums_paths_unique", Passed: len(duplicate) == 0, Detail: strings.Join(duplicate, ",")},
		CandidateCheck{Name: "checksums_cover_candidate_files", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")},
		CandidateCheck{Name: "checksums_match_candidate_files", Passed: len(mismatch) == 0, Detail: strings.Join(mismatch, ",")},
		CandidateCheck{Name: "checksums_have_no_unexpected_files", Passed: len(unexpected) == 0, Detail: strings.Join(unexpected, ",")},
	)
	return checks
}

func VerifyConnectionEntryReleaseArchive(candidateDir string, candidate Candidate) []CandidateCheck {
	archivePath := filepath.Join(candidateDir, connectionEntryReleaseArchivePath)
	reader, err := zip.OpenReader(archivePath)
	checks := []CandidateCheck{{Name: "connection_entry_release_archive_exists", Passed: err == nil, Detail: connectionEntryReleaseArchivePath}}
	if err != nil {
		return checks
	}
	defer reader.Close()

	entries := map[string][]byte{}
	var unsafe []string
	var duplicate []string
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if !safeCandidatePath(name) {
			unsafe = append(unsafe, name)
			continue
		}
		if _, exists := entries[name]; exists {
			duplicate = append(duplicate, name)
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			checks = append(checks, CandidateCheck{Name: "connection_entry_release_archive_readable", Passed: false, Detail: name + ":" + err.Error()})
			continue
		}
		entries[name] = content
	}
	sort.Strings(unsafe)
	sort.Strings(duplicate)
	checks = append(checks,
		CandidateCheck{Name: "connection_entry_archive_paths_safe", Passed: len(unsafe) == 0, Detail: strings.Join(unsafe, ",")},
		CandidateCheck{Name: "connection_entry_archive_paths_unique", Passed: len(duplicate) == 0, Detail: strings.Join(duplicate, ",")},
	)

	required := []string{
		"CONNECTION_ENTRY_RELEASE.md",
		"connection-entry-release.json",
		"connection-entry-runner.template.json",
		"connection-entry-checksums.txt",
		"release/release-bundle.json",
		"release/sbom.spdx.json",
		"release/provenance.json",
	}
	if connectionEntryReleaseUsesWindows(candidate) {
		required = append(required, "launchers/Start-ConnectionEntry.ps1")
	} else {
		required = append(required, "launchers/start-connection-entry.sh")
	}
	var missing []string
	for _, path := range required {
		if _, ok := entries[path]; !ok {
			missing = append(missing, path)
		}
	}
	for _, artifact := range candidate.Artifacts {
		for _, path := range []string{
			"bin/" + filepath.Base(artifact.ArtifactPath),
			"bin/" + filepath.Base(artifact.ManifestPath),
		} {
			if _, ok := entries[path]; !ok {
				missing = append(missing, path)
			}
		}
	}
	sort.Strings(missing)
	checks = append(checks, CandidateCheck{Name: "connection_entry_archive_required_files_present", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")})

	manifestContent, ok := entries["connection-entry-release.json"]
	if !ok {
		return checks
	}
	var manifest ConnectionEntryReleasePackage
	err = json.Unmarshal(manifestContent, &manifest)
	checks = append(checks, CandidateCheck{Name: "connection_entry_release_manifest_json_valid", Passed: err == nil, Detail: errorDetail(err)})
	if err != nil {
		return checks
	}
	checks = append(checks,
		CandidateCheck{Name: "connection_entry_release_manifest_schema", Passed: manifest.SchemaVersion == ConnectionEntryReleasePackageSchemaVersion, Detail: manifest.SchemaVersion},
		CandidateCheck{Name: "connection_entry_release_version_matches_candidate", Passed: manifest.Version == candidate.Version, Detail: manifest.Version},
		CandidateCheck{Name: "connection_entry_release_root_matches_candidate", Passed: manifest.RootPublicKey == candidate.RootPublicKey, Detail: manifest.RootPublicKey},
		CandidateCheck{Name: "connection_entry_release_no_private_parameters", Passed: manifest.NoPrivateParameters, Detail: fmt.Sprintf("%t", manifest.NoPrivateParameters)},
		CandidateCheck{Name: "connection_entry_release_requires_runtime_invite", Passed: manifest.ExecutionMode == "runtime-invite-required" && len(manifest.RequiredRuntimeData) > 0, Detail: manifest.ExecutionMode},
		CandidateCheck{Name: "connection_entry_release_required_artifacts_present", Passed: len(manifest.RequiredReleaseArtifacts) > 0, Detail: strings.Join(manifest.RequiredReleaseArtifacts, ",")},
		CandidateCheck{Name: "connection_entry_release_launchers_present", Passed: len(manifest.Launchers) >= 1, Detail: fmt.Sprintf("%d", len(manifest.Launchers))},
		CandidateCheck{Name: "connection_entry_release_artifacts_present", Passed: len(manifest.Artifacts) >= len(candidate.Artifacts), Detail: fmt.Sprintf("%d", len(manifest.Artifacts))},
	)

	checks = append(checks, verifyConnectionEntryArchiveChecksums(entries)...)
	checks = append(checks, verifyConnectionEntryManifestFiles(manifest, entries)...)
	checks = append(checks, verifyConnectionEntryLaunchersVerifyBundle(candidate, entries)...)
	checks = append(checks, CandidateCheck{Name: "connection_entry_archive_no_private_surface", Passed: connectionEntryArchiveHasNoPrivateSurface(entries), Detail: "no ticket/root/gateway placeholders with private values"})
	return checks
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func verifyConnectionEntryArchiveChecksums(entries map[string][]byte) []CandidateCheck {
	content, ok := entries["connection-entry-checksums.txt"]
	checks := []CandidateCheck{{Name: "connection_entry_archive_checksums_present", Passed: ok, Detail: "connection-entry-checksums.txt"}}
	if !ok {
		return checks
	}
	listed := map[string]string{}
	var malformed []string
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || !isHexSHA256String(parts[0]) || !safeCandidatePath(parts[1]) {
			malformed = append(malformed, line)
			continue
		}
		listed[parts[1]] = parts[0]
	}
	var missing []string
	var mismatch []string
	var unexpected []string
	for path, data := range entries {
		if path == "connection-entry-checksums.txt" {
			continue
		}
		want := sha256.Sum256(data)
		got, ok := listed[path]
		if !ok {
			missing = append(missing, path)
			continue
		}
		if got != hex.EncodeToString(want[:]) {
			mismatch = append(mismatch, path)
		}
	}
	for path := range listed {
		if path == "connection-entry-checksums.txt" {
			unexpected = append(unexpected, path)
			continue
		}
		if _, ok := entries[path]; !ok {
			unexpected = append(unexpected, path)
		}
	}
	sort.Strings(malformed)
	sort.Strings(missing)
	sort.Strings(mismatch)
	sort.Strings(unexpected)
	checks = append(checks,
		CandidateCheck{Name: "connection_entry_archive_checksum_lines_valid", Passed: len(malformed) == 0, Detail: strings.Join(malformed, ",")},
		CandidateCheck{Name: "connection_entry_archive_checksums_cover_entries", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")},
		CandidateCheck{Name: "connection_entry_archive_checksums_match_entries", Passed: len(mismatch) == 0, Detail: strings.Join(mismatch, ",")},
		CandidateCheck{Name: "connection_entry_archive_checksums_have_no_unexpected_entries", Passed: len(unexpected) == 0, Detail: strings.Join(unexpected, ",")},
	)
	return checks
}

func verifyConnectionEntryManifestFiles(manifest ConnectionEntryReleasePackage, entries map[string][]byte) []CandidateCheck {
	var missing []string
	var mismatch []string
	for _, file := range append(append([]ConnectionEntryReleasePackageFile{}, manifest.Artifacts...), append(manifest.ReleaseMetadata, manifest.Launchers...)...) {
		content, ok := entries[file.Path]
		if !ok {
			missing = append(missing, file.Path)
			continue
		}
		sum := sha256.Sum256(content)
		if file.SHA256 != "sha256:"+hex.EncodeToString(sum[:]) || file.SizeBytes != int64(len(content)) {
			mismatch = append(mismatch, file.Path)
		}
	}
	sort.Strings(missing)
	sort.Strings(mismatch)
	return []CandidateCheck{
		{Name: "connection_entry_release_manifest_files_exist", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")},
		{Name: "connection_entry_release_manifest_files_match", Passed: len(mismatch) == 0, Detail: strings.Join(mismatch, ",")},
	}
}

func verifyConnectionEntryLaunchersVerifyBundle(candidate Candidate, entries map[string][]byte) []CandidateCheck {
	var launcherContent []string
	for path, content := range entries {
		if strings.HasPrefix(path, "launchers/") {
			launcherContent = append(launcherContent, string(content))
		}
	}
	joined := strings.Join(launcherContent, "\n")
	required := connectionEntryRequiredArtifactsCSV(candidate)
	return []CandidateCheck{
		{Name: "connection_entry_launchers_use_packaged_verifier", Passed: strings.Contains(joined, "rdev-verify"), Detail: "rdev-verify"},
		{Name: "connection_entry_launchers_verify_release_bundle", Passed: strings.Contains(joined, "--bundle") && (strings.Contains(joined, "release/release-bundle.json") || strings.Contains(joined, "release\\release-bundle.json")), Detail: "release-bundle.json"},
		{Name: "connection_entry_launchers_pin_release_root", Passed: strings.Contains(joined, "--root-public-key") && strings.Contains(joined, candidate.RootPublicKey), Detail: candidate.RootPublicKey},
		{Name: "connection_entry_launchers_require_release_artifacts", Passed: strings.Contains(joined, "--require-artifacts") && required != "" && strings.Contains(joined, required), Detail: required},
	}
}

func connectionEntryArchiveHasNoPrivateSurface(entries map[string][]byte) bool {
	for path, content := range entries {
		if strings.HasPrefix(path, "bin/") {
			continue
		}
		text := string(content)
		if strings.Contains(text, "/Users/") ||
			strings.Contains(text, "192.168.") ||
			strings.Contains(text, "10.0.") ||
			strings.Contains(text, "ticket_code\":\"") ||
			strings.Contains(text, "gateway_url\":\"http") ||
			strings.Contains(text, "manifest_url\":\"http") {
			return false
		}
		if filepath.IsAbs(path) || strings.Contains(path, `\`) {
			return false
		}
	}
	return true
}

func findUnlistedCandidateFiles(candidateDir string, files []CandidateFile) ([]string, error) {
	listed := map[string]bool{"release-candidate.json": true}
	for _, file := range files {
		listed[file.Path] = true
	}
	var unlisted []string
	err := filepath.WalkDir(candidateDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(candidateDir, path)
		if err != nil {
			return err
		}
		candidatePath := filepath.ToSlash(rel)
		if !listed[candidatePath] {
			unlisted = append(unlisted, candidatePath)
		}
		return nil
	})
	sort.Strings(unlisted)
	return unlisted, err
}

func safeCandidatePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func missingCandidateRequiredArtifacts(candidate Candidate, required []string) string {
	required = cleanStringList(required)
	if len(required) == 0 {
		return ""
	}
	seen := map[string]bool{}
	for _, artifact := range candidate.Artifacts {
		for _, id := range []string{artifact.Name, artifact.ArtifactPath, filepath.Base(artifact.ArtifactPath)} {
			if strings.TrimSpace(id) != "" {
				seen[id] = true
			}
		}
	}
	var missing []string
	for _, value := range required {
		if !seen[value] {
			missing = append(missing, value)
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, ",")
}

func failedCandidateVerificationActions() []string {
	return []string{
		"Discard the release candidate and fetch it again from a trusted release source.",
		"Re-run rdev release prepare-candidate from a clean output directory if you produced this candidate locally.",
		"Do not publish, install, or use bootstrap artifacts from this candidate until verification passes.",
	}
}
