package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

const (
	BundleSchemaVersion             = "rdev.release-bundle.v1"
	BundleVerificationSchemaVersion = "rdev.release-bundle-verification.v1"
)

type Bundle struct {
	SchemaVersion     string           `json:"schema_version"`
	Version           string           `json:"version,omitempty"`
	TargetPlatform    string           `json:"target_platform,omitempty"`
	GeneratedAt       time.Time        `json:"generated_at"`
	SigningAlg        string           `json:"signing_alg"`
	SigningKeyID      string           `json:"signing_key_id"`
	RequiredArtifacts []string         `json:"required_artifacts,omitempty"`
	Artifacts         []BundleArtifact `json:"artifacts"`
	Signature         string           `json:"signature,omitempty"`
}

type BundleArtifact struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	ManifestPath   string `json:"manifest_path"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	ArtifactSize   int64  `json:"artifact_size"`
	ManifestSHA256 string `json:"manifest_sha256"`
	ManifestSize   int64  `json:"manifest_size"`
}

type BundleOptions struct {
	Dir               string
	Version           string
	TargetPlatform    string
	ArtifactPaths     []string
	RequiredArtifacts []string
	Key               signing.Key
	Now               time.Time
}

type BundleVerification struct {
	SchemaVersion      string                       `json:"schema_version"`
	Version            string                       `json:"version,omitempty"`
	TargetPlatform     string                       `json:"target_platform,omitempty"`
	BundlePath         string                       `json:"bundle_path"`
	RootKeyID          string                       `json:"root_key_id"`
	GeneratedAt        time.Time                    `json:"generated_at"`
	Checks             []BundleCheck                `json:"checks"`
	Artifacts          []BundleArtifactVerification `json:"artifacts"`
	RecommendedActions []string                     `json:"recommended_actions,omitempty"`
}

type BundleArtifactVerification struct {
	Name        string        `json:"name"`
	Artifact    string        `json:"artifact"`
	Manifest    string        `json:"manifest"`
	Checks      []BundleCheck `json:"checks"`
	ManifestSHA string        `json:"manifest_sha256,omitempty"`
	ArtifactSHA string        `json:"artifact_sha256,omitempty"`
}

type BundleCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func (v BundleVerification) OK() bool {
	if len(v.Checks) == 0 || len(v.Artifacts) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	for _, artifact := range v.Artifacts {
		if len(artifact.Checks) == 0 {
			return false
		}
		for _, check := range artifact.Checks {
			if !check.Passed {
				return false
			}
		}
	}
	return true
}

func CreateBundle(opts BundleOptions) (Bundle, error) {
	if strings.TrimSpace(opts.Dir) == "" {
		return Bundle{}, fmt.Errorf("bundle directory is required")
	}
	if len(opts.ArtifactPaths) == 0 {
		return Bundle{}, fmt.Errorf("at least one artifact is required")
	}
	if err := validateBundleSigningKey(opts.Key); err != nil {
		return Bundle{}, err
	}
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return Bundle{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	root := model.NewTrustBundle(opts.Key.ID, opts.Key.PublicKey)
	artifacts := make([]BundleArtifact, 0, len(opts.ArtifactPaths))
	seen := map[string]bool{}
	for _, artifactInput := range opts.ArtifactPaths {
		rel, err := relativeBundlePath(dir, artifactInput)
		if err != nil {
			return Bundle{}, err
		}
		if seen[rel] {
			return Bundle{}, fmt.Errorf("duplicate artifact %q", rel)
		}
		seen[rel] = true
		artifactPath := filepath.Join(dir, filepath.FromSlash(rel))
		manifestRel := rel + ".rdev-release.json"
		manifestPath := filepath.Join(dir, filepath.FromSlash(manifestRel))
		manifest, err := ReadManifest(manifestPath)
		if err != nil {
			return Bundle{}, err
		}
		if err := manifest.VerifyArtifact(artifactPath, root); err != nil {
			return Bundle{}, err
		}
		if manifest.ReleaseVersion != strings.TrimSpace(opts.Version) {
			return Bundle{}, fmt.Errorf("release manifest version %q does not match bundle version %q", manifest.ReleaseVersion, strings.TrimSpace(opts.Version))
		}
		if manifest.TargetPlatform != strings.TrimSpace(opts.TargetPlatform) {
			return Bundle{}, fmt.Errorf("release manifest target platform %q does not match bundle target platform %q", manifest.TargetPlatform, strings.TrimSpace(opts.TargetPlatform))
		}
		if manifest.ArtifactName != filepath.Base(rel) {
			return Bundle{}, fmt.Errorf("release manifest artifact name %q does not match %q", manifest.ArtifactName, filepath.Base(rel))
		}
		artifactSHA, artifactSize, err := fileDigest(artifactPath)
		if err != nil {
			return Bundle{}, err
		}
		manifestSHA, manifestSize, err := fileDigest(manifestPath)
		if err != nil {
			return Bundle{}, err
		}
		artifacts = append(artifacts, BundleArtifact{
			Name:           filepath.Base(rel),
			Path:           filepath.ToSlash(rel),
			ManifestPath:   filepath.ToSlash(manifestRel),
			ArtifactSHA256: artifactSHA,
			ArtifactSize:   artifactSize,
			ManifestSHA256: manifestSHA,
			ManifestSize:   manifestSize,
		})
	}
	bundle := Bundle{
		SchemaVersion:     BundleSchemaVersion,
		Version:           strings.TrimSpace(opts.Version),
		TargetPlatform:    strings.TrimSpace(opts.TargetPlatform),
		GeneratedAt:       now.UTC(),
		SigningAlg:        model.SigningAlgEd25519,
		SigningKeyID:      opts.Key.ID,
		RequiredArtifacts: cleanStringList(opts.RequiredArtifacts),
		Artifacts:         artifacts,
	}
	return bundle.Sign(opts.Key.PrivateKey)
}

func (b Bundle) Sign(privateKey ed25519.PrivateKey) (Bundle, error) {
	if err := b.validateForSigning(); err != nil {
		return Bundle{}, err
	}
	message, err := b.signingBytes()
	if err != nil {
		return Bundle{}, err
	}
	b.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return b, nil
}

func (b Bundle) Verify(root model.TrustBundle) error {
	if err := b.validateForSigning(); err != nil {
		return err
	}
	if b.Signature == "" {
		return fmt.Errorf("%w: missing bundle signature", ErrManifestSignature)
	}
	if root.SigningKeyID != b.SigningKeyID {
		return fmt.Errorf("%w: release bundle key id mismatch", ErrManifestInvalid)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(b.Signature)
	if err != nil {
		return fmt.Errorf("%w: malformed bundle signature", ErrManifestSignature)
	}
	message, err := b.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrManifestSignature
	}
	return nil
}

func VerifyBundle(bundlePath string, root model.TrustBundle, requiredArtifacts []string) (BundleVerification, error) {
	if strings.TrimSpace(bundlePath) == "" {
		return BundleVerification{}, fmt.Errorf("bundle is required")
	}
	abs, err := filepath.Abs(bundlePath)
	if err != nil {
		return BundleVerification{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return BundleVerification{}, err
	}
	var bundle Bundle
	if err := json.Unmarshal(content, &bundle); err != nil {
		return BundleVerification{}, err
	}
	baseDir := filepath.Dir(abs)
	verification := BundleVerification{
		SchemaVersion:  BundleVerificationSchemaVersion,
		Version:        bundle.Version,
		TargetPlatform: bundle.TargetPlatform,
		BundlePath:     abs,
		RootKeyID:      root.SigningKeyID,
		GeneratedAt:    time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, BundleCheck{Name: name, Passed: passed, Detail: detail})
	}
	add("bundle_schema", bundle.SchemaVersion == BundleSchemaVersion, bundle.SchemaVersion)
	if err := bundle.Verify(root); err != nil {
		add("bundle_signature_valid", false, err.Error())
	} else {
		add("bundle_signature_valid", true, bundle.SigningKeyID)
	}
	add("bundle_artifacts_present", len(bundle.Artifacts) > 0, fmt.Sprintf("%d", len(bundle.Artifacts)))

	seen := map[string]bool{}
	duplicate := ""
	for _, artifact := range bundle.Artifacts {
		for _, id := range bundleArtifactIDs(artifact) {
			if seen[id] && duplicate == "" {
				duplicate = id
			}
			seen[id] = true
		}
		verification.Artifacts = append(verification.Artifacts, verifyBundleArtifact(baseDir, artifact, root, bundle.Version, bundle.TargetPlatform))
	}
	add("bundle_artifact_ids_unique", duplicate == "", duplicate)
	missing := missingBundleRequiredArtifacts(seen, append(cleanStringList(bundle.RequiredArtifacts), cleanStringList(requiredArtifacts)...))
	add("required_artifacts_present", missing == "", missing)

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Recreate the release bundle index from a clean release directory.",
			"Verify every artifact with rdev release verify before publishing.",
			"Do not use the release bundle for bootstrap or host execution until verification passes.",
		}
	}
	return verification, nil
}

func ReadBundle(path string) (Bundle, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	var bundle Bundle
	if err := json.Unmarshal(content, &bundle); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func WriteBundle(path string, bundle Bundle) error {
	content, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

func verifyBundleArtifact(baseDir string, artifact BundleArtifact, root model.TrustBundle, version, targetPlatform string) BundleArtifactVerification {
	result := BundleArtifactVerification{
		Name:     firstNonEmpty(artifact.Name, filepath.Base(artifact.Path)),
		Artifact: artifact.Path,
		Manifest: artifact.ManifestPath,
	}
	add := func(name string, passed bool, detail string) {
		result.Checks = append(result.Checks, BundleCheck{Name: name, Passed: passed, Detail: detail})
	}
	artifactPath, artifactPathOK := resolveBundlePath(baseDir, artifact.Path)
	manifestPath, manifestPathOK := resolveBundlePath(baseDir, artifact.ManifestPath)
	add("artifact_path_relative", artifactPathOK, artifact.Path)
	add("manifest_path_relative", manifestPathOK, artifact.ManifestPath)
	artifactSHA, artifactSize, artifactErr := fileDigest(artifactPath)
	manifestSHA, manifestSize, manifestDigestErr := fileDigest(manifestPath)
	if artifactErr == nil {
		result.ArtifactSHA = artifactSHA
	}
	if manifestDigestErr == nil {
		result.ManifestSHA = manifestSHA
	}
	add("artifact_file_exists", artifactPathOK && artifactErr == nil, artifact.Path)
	add("manifest_file_exists", manifestPathOK && manifestDigestErr == nil, artifact.ManifestPath)
	add("artifact_sha256_matches_index", artifactErr == nil && artifactSHA == artifact.ArtifactSHA256, artifact.ArtifactSHA256)
	add("artifact_size_matches_index", artifactErr == nil && artifactSize == artifact.ArtifactSize, fmt.Sprintf("%d", artifact.ArtifactSize))
	add("manifest_sha256_matches_index", manifestDigestErr == nil && manifestSHA == artifact.ManifestSHA256, artifact.ManifestSHA256)
	add("manifest_size_matches_index", manifestDigestErr == nil && manifestSize == artifact.ManifestSize, fmt.Sprintf("%d", artifact.ManifestSize))
	manifest, manifestErr := ReadManifest(manifestPath)
	add("manifest_readable", manifestErr == nil, errorDetail(manifestErr))
	add("manifest_artifact_name_matches", manifestErr == nil && manifest.ArtifactName == filepath.Base(artifact.Path), manifest.ArtifactName)
	add("manifest_release_version_matches_bundle", manifestErr == nil && manifest.ReleaseVersion == version, manifest.ReleaseVersion)
	add("manifest_target_platform_matches_bundle", manifestErr == nil && manifest.TargetPlatform == targetPlatform, manifest.TargetPlatform)
	verifyErr := manifestErr
	if verifyErr == nil {
		verifyErr = manifest.VerifyArtifact(artifactPath, root)
	}
	add("signed_manifest_verifies_artifact", verifyErr == nil, errorDetail(verifyErr))
	return result
}

func (b Bundle) validateForSigning() error {
	if b.SchemaVersion != BundleSchemaVersion {
		return fmt.Errorf("%w: unsupported release bundle schema version", ErrManifestInvalid)
	}
	if b.GeneratedAt.IsZero() {
		return fmt.Errorf("%w: generated_at is required", ErrManifestInvalid)
	}
	if b.SigningAlg != model.SigningAlgEd25519 || b.SigningKeyID == "" {
		return fmt.Errorf("%w: unsupported release bundle signing metadata", ErrManifestInvalid)
	}
	if len(b.Artifacts) == 0 {
		return fmt.Errorf("%w: release bundle has no artifacts", ErrManifestInvalid)
	}
	for _, artifact := range b.Artifacts {
		if strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.ManifestPath) == "" {
			return fmt.Errorf("%w: release bundle artifact path is required", ErrManifestInvalid)
		}
		if !isHexSHA256String(artifact.ArtifactSHA256) || !isHexSHA256String(artifact.ManifestSHA256) {
			return fmt.Errorf("%w: release bundle artifact hash is invalid", ErrManifestInvalid)
		}
		if artifact.ArtifactSize < 0 || artifact.ManifestSize < 0 {
			return fmt.Errorf("%w: release bundle artifact size is invalid", ErrManifestInvalid)
		}
	}
	return nil
}

func (b Bundle) signingBytes() ([]byte, error) {
	unsigned := b
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}

func validateBundleSigningKey(key signing.Key) error {
	if strings.TrimSpace(key.ID) == "" || len(key.PublicKey) != ed25519.PublicKeySize || len(key.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("valid release signing key is required")
	}
	return nil
}

func relativeBundlePath(baseDir, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("artifact path is required")
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return "", err
		}
		path = rel
	}
	clean := filepath.Clean(path)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("artifact path %q escapes bundle directory", path)
	}
	return filepath.ToSlash(clean), nil
}

func resolveBundlePath(baseDir, path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(baseDir, clean), true
}

func bundleArtifactIDs(artifact BundleArtifact) []string {
	values := []string{
		strings.TrimSpace(artifact.Name),
		filepath.ToSlash(strings.TrimSpace(artifact.Path)),
		filepath.Base(strings.TrimSpace(artifact.Path)),
	}
	seen := map[string]bool{}
	var ids []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		ids = append(ids, value)
	}
	return ids
}

func missingBundleRequiredArtifacts(seen map[string]bool, required []string) string {
	var missing []string
	for _, value := range required {
		if value == "" {
			continue
		}
		if !seen[value] {
			missing = append(missing, value)
		}
	}
	return strings.Join(missing, ",")
}

func cleanStringList(values []string) []string {
	seen := map[string]bool{}
	var cleaned []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func isHexSHA256String(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F') {
			continue
		}
		return false
	}
	return true
}
