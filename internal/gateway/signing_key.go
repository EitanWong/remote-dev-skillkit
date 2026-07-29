package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const SigningKeyFileSchemaVersion = "rdev.gateway-signing-key.v1"

// SigningKey is the local signing identity used to validate persistent gateway
// state across restarts. Its private key must remain in a protected runtime file.
type SigningKey struct {
	ID         string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

type signingKeyFile struct {
	SchemaVersion string `json:"schema_version"`
	SigningKeyID  string `json:"signing_key_id"`
	PrivateKey    string `json:"private_key"`
}

// LoadOrCreateFileSigningKey loads a stable gateway signing key from path, or
// creates it with owner-only permissions when the path does not yet exist.
func LoadOrCreateFileSigningKey(path, signingKeyID string) (SigningKey, bool, error) {
	path = strings.TrimSpace(path)
	signingKeyID = strings.TrimSpace(signingKeyID)
	if path == "" {
		return SigningKey{}, false, fmt.Errorf("gateway signing key file path is required")
	}
	if signingKeyID == "" {
		return SigningKey{}, false, fmt.Errorf("gateway signing key id is required")
	}

	key, err := loadFileSigningKey(path, signingKeyID)
	if err == nil {
		return key, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return SigningKey{}, false, err
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return SigningKey{}, false, fmt.Errorf("generate gateway signing key: %w", err)
	}
	created := SigningKey{
		ID:         signingKeyID,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}
	if err := writeNewFileSigningKey(path, created); err == nil {
		return created, true, nil
	} else if !errors.Is(err, os.ErrExist) {
		return SigningKey{}, false, err
	}

	key, err = loadFileSigningKey(path, signingKeyID)
	if err != nil {
		return SigningKey{}, false, err
	}
	return key, false, nil
}

func loadFileSigningKey(path, expectedID string) (SigningKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return SigningKey{}, err
	}
	if !info.Mode().IsRegular() {
		return SigningKey{}, fmt.Errorf("gateway signing key file must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return SigningKey{}, fmt.Errorf("gateway signing key file permissions must be 0600")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return SigningKey{}, fmt.Errorf("read gateway signing key file: %w", err)
	}
	var stored signingKeyFile
	if err := json.Unmarshal(content, &stored); err != nil {
		return SigningKey{}, fmt.Errorf("decode gateway signing key file: %w", err)
	}
	if stored.SchemaVersion != SigningKeyFileSchemaVersion {
		return SigningKey{}, fmt.Errorf("unsupported gateway signing key schema %q", stored.SchemaVersion)
	}
	if strings.TrimSpace(stored.SigningKeyID) == "" {
		return SigningKey{}, fmt.Errorf("gateway signing key file is missing signing_key_id")
	}
	if stored.SigningKeyID != expectedID {
		return SigningKey{}, fmt.Errorf("gateway signing key id %q does not match requested id %q", stored.SigningKeyID, expectedID)
	}
	privateKey, err := base64.StdEncoding.DecodeString(stored.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return SigningKey{}, fmt.Errorf("gateway signing key file has invalid private key")
	}
	key := SigningKey{ID: stored.SigningKeyID, PrivateKey: ed25519.PrivateKey(privateKey)}
	publicKey, ok := key.PrivateKey.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return SigningKey{}, fmt.Errorf("derive gateway signing public key")
	}
	key.PublicKey = publicKey
	return key, nil
}

func writeNewFileSigningKey(path string, key SigningKey) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve gateway signing key path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return fmt.Errorf("create gateway signing key directory: %w", err)
	}
	content, err := json.Marshal(signingKeyFile{
		SchemaVersion: SigningKeyFileSchemaVersion,
		SigningKeyID:  key.ID,
		PrivateKey:    base64.StdEncoding.EncodeToString(key.PrivateKey),
	})
	if err != nil {
		return fmt.Errorf("encode gateway signing key: %w", err)
	}
	content = append(content, '\n')

	file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set gateway signing key permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write gateway signing key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync gateway signing key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close gateway signing key: %w", err)
	}
	file = nil
	return nil
}
