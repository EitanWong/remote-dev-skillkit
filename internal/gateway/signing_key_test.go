package gateway

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateFileSigningKeyPersistsStableIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway", "signing-key.json")

	first, created, err := LoadOrCreateFileSigningKey(path, "managed-gateway")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first signing key load must create the explicit key file")
	}
	if first.ID != "managed-gateway" || len(first.PublicKey) != ed25519.PublicKeySize || len(first.PrivateKey) != ed25519.PrivateKeySize {
		t.Fatalf("unexpected first signing key: %#v", first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("signing key permissions = %#o, want 0600", got)
	}

	second, created, err := LoadOrCreateFileSigningKey(path, "managed-gateway")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second signing key load must reuse the existing key")
	}
	if second.ID != first.ID || !second.PublicKey.Equal(first.PublicKey) || !second.PrivateKey.Equal(first.PrivateKey) {
		t.Fatalf("signing key changed across reload: first=%#v second=%#v", first, second)
	}
}

func TestLoadOrCreateFileSigningKeyRejectsOverPermissiveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signing-key.json")
	if _, _, err := LoadOrCreateFileSigningKey(path, "managed-gateway"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateFileSigningKey(path, "managed-gateway"); err == nil {
		t.Fatal("expected over-permissive signing key file to be rejected")
	}
}
