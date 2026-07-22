package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const LayeredAssetManifestSchemaVersion = "rdev.layered-assets.v1"

const (
	layeredAssetKindCoreRuntime    = "core-runtime"
	layeredAssetKindOptionalHelper = "optional-helper"
)

type LayeredAssetManifest struct {
	SchemaVersion string         `json:"schema_version"`
	Version       string         `json:"version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	ExpiresAt     time.Time      `json:"expires_at"`
	SigningKeyID  string         `json:"signing_key_id"`
	Assets        []LayeredAsset `json:"assets"`
	Signature     string         `json:"signature"`
}

type LayeredAsset struct {
	ID           string   `json:"id"`
	Platform     string   `json:"platform"`
	Kind         string   `json:"kind"`
	RelativePath string   `json:"relative_path"`
	SHA256       string   `json:"sha256"`
	SizeBytes    int64    `json:"size_bytes"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type LayeredTrustRoot struct {
	SigningKeyID string
	PublicKey    ed25519.PublicKey
}

func ParseLayeredTrustRoot(value string) (LayeredTrustRoot, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return LayeredTrustRoot{}, fmt.Errorf("trust root public key must be formatted key_id:base64url_public_key")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return LayeredTrustRoot{}, fmt.Errorf("invalid trust root public key")
	}
	return LayeredTrustRoot{SigningKeyID: parts[0], PublicKey: append(ed25519.PublicKey(nil), publicKey...)}, nil
}

func VerifyLayeredAssetManifestRoot(manifest LayeredAssetManifest, root LayeredTrustRoot, now time.Time) error {
	if err := validateLayeredAssetManifest(manifest); err != nil {
		return err
	}
	if len(root.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: layered asset manifest trust root is invalid", ErrManifestInvalid)
	}
	return verifyLayeredAssetManifest(manifest, root, now)
}

func verifyLayeredAssetManifest(manifest LayeredAssetManifest, root LayeredTrustRoot, now time.Time) error {
	if manifest.Signature == "" {
		return fmt.Errorf("%w: layered asset manifest signature is required", ErrManifestSignature)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: layered asset manifest verification time is required", ErrManifestInvalid)
	}
	verificationTime := now.UTC()
	if manifest.GeneratedAt.After(verificationTime) {
		return fmt.Errorf("%w: layered asset manifest generated_at is in the future", ErrManifestInvalid)
	}
	if !verificationTime.Before(manifest.ExpiresAt) {
		return fmt.Errorf("%w: layered asset manifest expired", ErrManifestInvalid)
	}
	if root.SigningKeyID != manifest.SigningKeyID {
		return fmt.Errorf("%w: layered asset manifest trust root key id mismatch", ErrManifestInvalid)
	}
	signature, err := base64.RawURLEncoding.DecodeString(manifest.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: malformed layered asset manifest signature", ErrManifestSignature)
	}
	message, err := canonicalUnsignedLayeredAssetManifest(manifest)
	if err != nil {
		return err
	}
	if !ed25519.Verify(root.PublicKey, message, signature) {
		return fmt.Errorf("%w: layered asset manifest signature mismatch", ErrManifestSignature)
	}
	return nil
}

func SelectLayeredAsset(manifest LayeredAssetManifest, platform, kind string, capabilities []string) (LayeredAsset, error) {
	if err := validateLayeredAssetManifest(manifest); err != nil {
		return LayeredAsset{}, err
	}
	if strings.TrimSpace(platform) == "" || !validLayeredAssetKind(kind) {
		return LayeredAsset{}, fmt.Errorf("%w: invalid layered asset selection", ErrManifestInvalid)
	}

	matches := make([]LayeredAsset, 0, 1)
	for _, asset := range manifest.Assets {
		if asset.Platform == platform && asset.Kind == kind && hasLayeredAssetCapabilities(asset, capabilities) {
			matches = append(matches, cloneLayeredAsset(asset))
		}
	}
	if len(matches) != 1 {
		return LayeredAsset{}, fmt.Errorf("%w: expected one matching layered asset, found %d", ErrManifestInvalid, len(matches))
	}
	return matches[0], nil
}

func validateLayeredAssetManifest(manifest LayeredAssetManifest) error {
	if manifest.SchemaVersion != LayeredAssetManifestSchemaVersion {
		return fmt.Errorf("%w: unsupported layered asset manifest schema version", ErrManifestInvalid)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("%w: layered asset manifest version is required", ErrManifestInvalid)
	}
	if manifest.GeneratedAt.IsZero() {
		return fmt.Errorf("%w: layered asset manifest generated_at is required", ErrManifestInvalid)
	}
	if manifest.ExpiresAt.IsZero() || !manifest.GeneratedAt.Before(manifest.ExpiresAt) {
		return fmt.Errorf("%w: layered asset manifest validity window is invalid", ErrManifestInvalid)
	}
	if strings.TrimSpace(manifest.SigningKeyID) == "" {
		return fmt.Errorf("%w: layered asset manifest signing key id is required", ErrManifestInvalid)
	}
	if len(manifest.Assets) == 0 {
		return fmt.Errorf("%w: layered asset manifest has no assets", ErrManifestInvalid)
	}

	seenIDs := make(map[string]struct{}, len(manifest.Assets))
	corePlatforms := make(map[string]struct{})
	for _, asset := range manifest.Assets {
		if err := validateLayeredAsset(asset); err != nil {
			return err
		}
		if _, exists := seenIDs[asset.ID]; exists {
			return fmt.Errorf("%w: duplicate layered asset id %q", ErrManifestInvalid, asset.ID)
		}
		seenIDs[asset.ID] = struct{}{}
		if asset.Kind == layeredAssetKindCoreRuntime {
			if _, exists := corePlatforms[asset.Platform]; exists {
				return fmt.Errorf("%w: duplicate core runtime for platform %q", ErrManifestInvalid, asset.Platform)
			}
			corePlatforms[asset.Platform] = struct{}{}
		}
	}
	if len(corePlatforms) == 0 {
		return fmt.Errorf("%w: layered asset manifest requires a core runtime", ErrManifestInvalid)
	}
	return nil
}

func validateLayeredAsset(asset LayeredAsset) error {
	if strings.TrimSpace(asset.ID) == "" || strings.TrimSpace(asset.Platform) == "" {
		return fmt.Errorf("%w: layered asset identity is required", ErrManifestInvalid)
	}
	if !validLayeredAssetKind(asset.Kind) {
		return fmt.Errorf("%w: unsupported layered asset kind %q", ErrManifestInvalid, asset.Kind)
	}
	if !validRelativeAssetPath(asset.RelativePath) {
		return fmt.Errorf("%w: invalid layered asset relative path", ErrManifestInvalid)
	}
	if !validLayeredAssetSHA256(asset.SHA256) {
		return fmt.Errorf("%w: invalid layered asset sha256", ErrManifestInvalid)
	}
	if asset.SizeBytes <= 0 {
		return fmt.Errorf("%w: layered asset size must be positive", ErrManifestInvalid)
	}
	return nil
}

func cloneLayeredAssetManifest(manifest LayeredAssetManifest) LayeredAssetManifest {
	cloned := manifest
	cloned.Assets = make([]LayeredAsset, len(manifest.Assets))
	for index, asset := range manifest.Assets {
		cloned.Assets[index] = cloneLayeredAsset(asset)
	}
	return cloned
}

func cloneLayeredAsset(asset LayeredAsset) LayeredAsset {
	cloned := asset
	cloned.Capabilities = append([]string(nil), asset.Capabilities...)
	return cloned
}

func validLayeredAssetKind(kind string) bool {
	return kind == layeredAssetKindCoreRuntime || kind == layeredAssetKindOptionalHelper
}

func validLayeredAssetSHA256(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}

func hasLayeredAssetCapabilities(asset LayeredAsset, required []string) bool {
	available := make(map[string]struct{}, len(asset.Capabilities))
	for _, capability := range asset.Capabilities {
		available[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := available[capability]; !ok {
			return false
		}
	}
	return true
}
