package signing

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
	FileSchemaVersion = "rdev.signing-key.v1"
	DefaultKeyID      = "gateway-dev"
)

type Key struct {
	ID         string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

type fileKey struct {
	SchemaVersion string `json:"schema_version"`
	KeyID         string `json:"key_id"`
	SigningAlg    string `json:"signing_alg"`
	PublicKey     string `json:"public_key"`
	PrivateKey    string `json:"private_key"`
}

func Generate(keyID string) (Key, error) {
	if keyID == "" {
		keyID = DefaultKeyID
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Key{}, err
	}
	return Key{
		ID:         keyID,
		PublicKey:  append(ed25519.PublicKey(nil), publicKey...),
		PrivateKey: append(ed25519.PrivateKey(nil), privateKey...),
	}, nil
}

func LoadOrCreate(path, keyID string) (Key, bool, error) {
	if path == "" {
		key, err := Generate(keyID)
		return key, false, err
	}
	if keyID == "" {
		keyID = DefaultKeyID
	}
	content, err := os.ReadFile(path)
	if err == nil {
		key, err := decodeKey(content)
		if err != nil {
			return Key{}, false, err
		}
		if key.ID != keyID {
			return Key{}, false, fmt.Errorf("signing key id mismatch: file has %q, requested %q", key.ID, keyID)
		}
		return key, false, nil
	}
	if !os.IsNotExist(err) {
		return Key{}, false, err
	}
	key, err := Generate(keyID)
	if err != nil {
		return Key{}, false, err
	}
	if err := writeNewKeyFile(path, key); err != nil {
		return Key{}, false, err
	}
	return key, true, nil
}

func Fingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeNewKeyFile(path string, key Key) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content, err := encodeKey(key)
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
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	return nil
}

func encodeKey(key Key) ([]byte, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	encoded := fileKey{
		SchemaVersion: FileSchemaVersion,
		KeyID:         key.ID,
		SigningAlg:    model.JobEnvelopeSigningAlg,
		PublicKey:     base64.RawURLEncoding.EncodeToString(key.PublicKey),
		PrivateKey:    base64.RawURLEncoding.EncodeToString(key.PrivateKey),
	}
	content, err := json.MarshalIndent(encoded, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func decodeKey(content []byte) (Key, error) {
	var encoded fileKey
	if err := json.Unmarshal(content, &encoded); err != nil {
		return Key{}, err
	}
	if encoded.SchemaVersion != FileSchemaVersion {
		return Key{}, fmt.Errorf("unsupported signing key schema %q", encoded.SchemaVersion)
	}
	if encoded.SigningAlg != model.JobEnvelopeSigningAlg {
		return Key{}, fmt.Errorf("unsupported signing algorithm %q", encoded.SigningAlg)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(encoded.PublicKey)
	if err != nil {
		return Key{}, fmt.Errorf("decode public key: %w", err)
	}
	privateKey, err := base64.RawURLEncoding.DecodeString(encoded.PrivateKey)
	if err != nil {
		return Key{}, fmt.Errorf("decode private key: %w", err)
	}
	key := Key{
		ID:         encoded.KeyID,
		PublicKey:  ed25519.PublicKey(publicKey),
		PrivateKey: ed25519.PrivateKey(privateKey),
	}
	if err := validateKey(key); err != nil {
		return Key{}, err
	}
	return key, nil
}

func validateKey(key Key) error {
	if key.ID == "" {
		return fmt.Errorf("signing key id is required")
	}
	if len(key.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length %d", len(key.PublicKey))
	}
	if len(key.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key length %d", len(key.PrivateKey))
	}
	derived, ok := key.PrivateKey.Public().(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("derive public key")
	}
	if !derived.Equal(key.PublicKey) {
		return fmt.Errorf("public key does not match private key")
	}
	return nil
}
