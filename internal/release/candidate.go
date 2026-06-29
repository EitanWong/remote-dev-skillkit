package release

import (
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
	SourcePath   string `json:"source_path"`
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
		OutDir:        outDir,
		RootPublicKey: encodeReleaseRoot(opts.Key),
		SkillkitPath:  filepath.Join(outDir, "skillkit"),
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
	candidate.ReleaseBundlePath = filepath.Join(outDir, "release-bundle.json")
	if err := WriteBundle(candidate.ReleaseBundlePath, bundle); err != nil {
		return Candidate{}, err
	}
	add("release_bundle_written", pathExists(candidate.ReleaseBundlePath), candidate.ReleaseBundlePath)

	root := model.NewTrustBundle(opts.Key.ID, opts.Key.PublicKey)
	bundleVerification, err := VerifyBundle(candidate.ReleaseBundlePath, root, required)
	if err != nil {
		return Candidate{}, err
	}
	candidate.BundleVerification = bundleVerification
	add("release_bundle_verified", bundleVerification.OK(), failedBundleCheckNames(bundleVerification))

	skillManifest, err := skillkit.Export(skillkit.ExportOptions{
		SourceRoot:  sourceRootAbs,
		OutDir:      candidate.SkillkitPath,
		GatewayURL:  opts.GatewayURL,
		GeneratedAt: now,
	})
	if err != nil {
		return Candidate{}, err
	}
	add("skillkit_exported", skillManifest.SchemaVersion == skillkit.ManifestSchemaVersion, skillManifest.SchemaVersion)
	skillkitVerification, err := skillkit.Verify(skillkit.VerifyOptions{
		BundleDir:   candidate.SkillkitPath,
		GeneratedAt: now,
	})
	if err != nil {
		return Candidate{}, err
	}
	candidate.SkillkitVerification = skillkitVerification
	add("skillkit_verified", skillkitVerification.OK(), failedSkillkitCheckNames(skillkitVerification))

	files, err := collectCandidateFiles(outDir, map[string]bool{
		"release-candidate.json": true,
		"checksums.txt":          true,
	})
	if err != nil {
		return Candidate{}, err
	}
	candidate.ChecksumsPath = filepath.Join(outDir, "checksums.txt")
	checksumEntry, err := writeCandidateChecksums(candidate.ChecksumsPath, files)
	if err != nil {
		return Candidate{}, err
	}
	files = append(files, checksumEntry)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	candidate.Files = files
	add("checksums_written", pathExists(candidate.ChecksumsPath), candidate.ChecksumsPath)

	if !candidate.OK() {
		candidate.RecommendedActions = []string{
			"Rebuild the release candidate from a clean output directory.",
			"Do not publish GitHub release assets until release bundle and Skillkit verification both pass.",
			"Check release-candidate.json, release-bundle.json, skillkit/manifest.json, and checksums.txt for the first failed check.",
		}
	}
	if err := writeCandidate(filepath.Join(outDir, "release-candidate.json"), candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
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
			SourcePath:   sourceAbs,
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
	case strings.HasSuffix(path, ".rdev-release.json"):
		return "release-manifest"
	case strings.HasPrefix(path, "skillkit/"):
		return "skillkit"
	default:
		return "artifact"
	}
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
