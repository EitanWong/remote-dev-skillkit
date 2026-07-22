//go:build !rdev_bootstrap_focused

package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

const ManifestSchemaVersion = "rdev.release-artifact.v1"

var (
	ErrManifestInvalid   = errors.New("release manifest invalid")
	ErrManifestSignature = errors.New("release manifest signature invalid")
)

type Manifest struct {
	SchemaVersion  string    `json:"schema_version"`
	ArtifactName   string    `json:"artifact_name"`
	ArtifactSHA256 string    `json:"artifact_sha256"`
	ArtifactSize   int64     `json:"artifact_size"`
	ReleaseVersion string    `json:"release_version,omitempty"`
	TargetPlatform string    `json:"target_platform,omitempty"`
	IssuedAt       time.Time `json:"issued_at"`
	SigningAlg     string    `json:"signing_alg"`
	SigningKeyID   string    `json:"signing_key_id"`
	Signature      string    `json:"signature,omitempty"`
}

func SignArtifact(path string, key signing.Key, now time.Time) (Manifest, error) {
	return SignArtifactForRelease(path, key, now, "", "")
}

func SignArtifactForRelease(path string, key signing.Key, now time.Time, releaseVersion, targetPlatform string) (Manifest, error) {
	digest, size, err := fileDigest(path)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		SchemaVersion:  ManifestSchemaVersion,
		ArtifactName:   filepath.Base(path),
		ArtifactSHA256: digest,
		ArtifactSize:   size,
		ReleaseVersion: releaseVersion,
		TargetPlatform: targetPlatform,
		IssuedAt:       now.UTC(),
		SigningAlg:     model.SigningAlgEd25519,
		SigningKeyID:   key.ID,
	}
	return manifest.Sign(key.PrivateKey)
}

func (m Manifest) Sign(privateKey ed25519.PrivateKey) (Manifest, error) {
	if err := m.validateForSigning(); err != nil {
		return Manifest{}, err
	}
	message, err := m.signingBytes()
	if err != nil {
		return Manifest{}, err
	}
	m.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, message))
	return m, nil
}

func (m Manifest) VerifyArtifact(path string, root model.TrustBundle) error {
	if err := m.Verify(root); err != nil {
		return err
	}
	digest, size, err := fileDigest(path)
	if err != nil {
		return err
	}
	if digest != m.ArtifactSHA256 {
		return fmt.Errorf("%w: sha256 mismatch", ErrManifestInvalid)
	}
	if size != m.ArtifactSize {
		return fmt.Errorf("%w: size mismatch", ErrManifestInvalid)
	}
	return nil
}

func (m Manifest) Verify(root model.TrustBundle) error {
	if err := m.validateForSigning(); err != nil {
		return err
	}
	if m.Signature == "" {
		return fmt.Errorf("%w: missing signature", ErrManifestSignature)
	}
	if root.SigningKeyID != m.SigningKeyID {
		return fmt.Errorf("%w: trust root key id mismatch", ErrManifestInvalid)
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("%w: malformed signature", ErrManifestSignature)
	}
	message, err := m.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, message, signature) {
		return ErrManifestSignature
	}
	return nil
}

func ReadManifest(path string) (Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func WriteManifest(path string, manifest Manifest) error {
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

func (m Manifest) validateForSigning() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrManifestInvalid)
	}
	if m.ArtifactName == "" || m.ArtifactSHA256 == "" || m.ArtifactSize < 0 {
		return fmt.Errorf("%w: missing artifact metadata", ErrManifestInvalid)
	}
	if m.IssuedAt.IsZero() {
		return fmt.Errorf("%w: issued_at is required", ErrManifestInvalid)
	}
	if m.SigningAlg != model.SigningAlgEd25519 || m.SigningKeyID == "" {
		return fmt.Errorf("%w: unsupported signing metadata", ErrManifestInvalid)
	}
	return nil
}

func (m Manifest) signingBytes() ([]byte, error) {
	unsigned := m
	unsigned.Signature = ""
	return json.Marshal(unsigned)
}

func fileDigest(path string) (string, int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), int64(len(content)), nil
}
