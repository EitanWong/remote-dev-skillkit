package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	SchemaVersion        string                      `json:"schema_version"`
	Version              string                      `json:"version"`
	GeneratedAt          time.Time                   `json:"generated_at"`
	OutDir               string                      `json:"out_dir"`
	RootPublicKey        string                      `json:"root_public_key"`
	ReleaseBundlePath    string                      `json:"release_bundle_path"`
	SkillkitPath         string                      `json:"skillkit_path"`
	SBOMPath             string                      `json:"sbom_path"`
	ProvenancePath       string                      `json:"provenance_path"`
	ChecksumsPath        string                      `json:"checksums_path"`
	Artifacts            []CandidateArtifact         `json:"artifacts"`
	SkillkitVerification skillkit.VerificationReport `json:"skillkit_verification"`
	BundleVerification   BundleVerification          `json:"bundle_verification"`
	Checks               []CandidateCheck            `json:"checks"`
	Files                []CandidateFile             `json:"files"`
	RecommendedActions   []string                    `json:"recommended_actions,omitempty"`
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
		}
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
	case path == "sbom.spdx.json":
		return "sbom"
	case path == "provenance.json":
		return "provenance"
	case strings.HasSuffix(path, ".rdev-release.json"):
		return "release-manifest"
	case strings.HasPrefix(path, "skillkit/"):
		return "skillkit"
	default:
		return "artifact"
	}
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
	sort.Strings(missing)
	sort.Strings(mismatch)
	sort.Strings(unexpected)
	checks = append(checks,
		CandidateCheck{Name: "checksums_lines_valid", Passed: len(malformed) == 0, Detail: strings.Join(malformed, ",")},
		CandidateCheck{Name: "checksums_cover_candidate_files", Passed: len(missing) == 0, Detail: strings.Join(missing, ",")},
		CandidateCheck{Name: "checksums_match_candidate_files", Passed: len(mismatch) == 0, Detail: strings.Join(mismatch, ",")},
		CandidateCheck{Name: "checksums_have_no_unexpected_files", Passed: len(unexpected) == 0, Detail: strings.Join(unexpected, ",")},
	)
	return checks
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
