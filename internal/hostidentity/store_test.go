package hostidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateCreates0600Identity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, created, err := LoadOrCreate(path, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected identity to be created")
	}
	if identity.KeyID != "host-test" {
		t.Fatalf("unexpected key id %q", identity.KeyID)
	}
	if identity.EncodedPublicKey() == "" || identity.Fingerprint() == "" {
		t.Fatal("expected public key and fingerprint")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %#o", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected 0700 directory permissions, got %#o", got)
	}
}

func TestLoadOrCreateReusesIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	first, created, err := LoadOrCreate(path, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected first call to create identity")
	}
	second, created, err := LoadOrCreate(path, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected second call to reuse identity")
	}
	if !second.PrivateKey.Equal(first.PrivateKey) {
		t.Fatal("expected private key to be reused")
	}
}

func TestLoadOrCreateRejectsKeyIDMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if _, _, err := LoadOrCreate(path, "first"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreate(path, "second"); err == nil {
		t.Fatal("expected key id mismatch")
	}
}
