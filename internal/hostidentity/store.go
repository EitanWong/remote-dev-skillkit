package hostidentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	FileSchemaVersion = "rdev.host-identity.v1"
	DefaultKeyID      = "host"
)

type Identity struct {
	KeyID      string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

type fileIdentity struct {
	SchemaVersion string `json:"schema_version"`
	KeyID         string `json:"key_id"`
	SigningAlg    string `json:"signing_alg"`
	PublicKey     string `json:"public_key"`
	PrivateKey    string `json:"private_key"`
}

func LoadOrCreate(path, keyID string) (Identity, bool, error) {
	if keyID == "" {
		keyID = DefaultKeyID
	}
	if path == "" {
		identity, err := Generate(keyID)
		return identity, false, err
	}
	content, err := os.ReadFile(path)
	if err == nil {
		identity, err := Decode(content)
		if err != nil {
			return Identity{}, false, err
		}
		if identity.KeyID != keyID {
			return Identity{}, false, fmt.Errorf("host identity key id mismatch: file has %q, requested %q", identity.KeyID, keyID)
		}
		return identity, false, nil
	}
	if !os.IsNotExist(err) {
		return Identity{}, false, err
	}
	identity, err := Generate(keyID)
	if err != nil {
		return Identity{}, false, err
	}
	if err := writeNew(path, identity); err != nil {
		return Identity{}, false, err
	}
	return identity, true, nil
}

func Generate(keyID string) (Identity, error) {
	if keyID == "" {
		keyID = DefaultKeyID
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		KeyID:      keyID,
		PublicKey:  append(ed25519.PublicKey(nil), publicKey...),
		PrivateKey: append(ed25519.PrivateKey(nil), privateKey...),
	}, nil
}

func Decode(content []byte) (Identity, error) {
	var encoded fileIdentity
	if err := json.Unmarshal(content, &encoded); err != nil {
		return Identity{}, err
	}
	if encoded.SchemaVersion != FileSchemaVersion {
		return Identity{}, fmt.Errorf("unsupported host identity schema %q", encoded.SchemaVersion)
	}
	if encoded.SigningAlg != model.JobEnvelopeSigningAlg {
		return Identity{}, fmt.Errorf("unsupported signing algorithm %q", encoded.SigningAlg)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(encoded.PublicKey)
	if err != nil {
		return Identity{}, fmt.Errorf("decode public key: %w", err)
	}
	privateKey, err := base64.RawURLEncoding.DecodeString(encoded.PrivateKey)
	if err != nil {
		return Identity{}, fmt.Errorf("decode private key: %w", err)
	}
	identity := Identity{
		KeyID:      encoded.KeyID,
		PublicKey:  ed25519.PublicKey(publicKey),
		PrivateKey: ed25519.PrivateKey(privateKey),
	}
	if err := validate(identity); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (i Identity) EncodedPublicKey() string {
	return base64.RawURLEncoding.EncodeToString(i.PublicKey)
}

func (i Identity) Fingerprint() string {
	sum := sha256.Sum256(i.PublicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeNew(path string, identity Identity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := encode(identity)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		return err
	}
	return file.Chmod(0o600)
}

func encode(identity Identity) ([]byte, error) {
	if err := validate(identity); err != nil {
		return nil, err
	}
	encoded := fileIdentity{
		SchemaVersion: FileSchemaVersion,
		KeyID:         identity.KeyID,
		SigningAlg:    model.JobEnvelopeSigningAlg,
		PublicKey:     identity.EncodedPublicKey(),
		PrivateKey:    base64.RawURLEncoding.EncodeToString(identity.PrivateKey),
	}
	content, err := json.MarshalIndent(encoded, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func validate(identity Identity) error {
	if identity.KeyID == "" {
		return fmt.Errorf("host identity key id is required")
	}
	if len(identity.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length %d", len(identity.PublicKey))
	}
	if len(identity.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key length %d", len(identity.PrivateKey))
	}
	derived, ok := identity.PrivateKey.Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(identity.PublicKey) {
		return fmt.Errorf("public key does not match private key")
	}
	return nil
}
