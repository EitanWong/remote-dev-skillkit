package signing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateCreates0600Key(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-key.json")
	key, created, err := LoadOrCreate(path, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected key to be created")
	}
	if key.ID != "test-key" {
		t.Fatalf("unexpected key id %q", key.ID)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %#o", got)
	}
	if Fingerprint(key.PublicKey) == "" {
		t.Fatal("fingerprint should be present")
	}
}

func TestLoadOrCreateReusesExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-key.json")
	first, created, err := LoadOrCreate(path, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected first load to create key")
	}
	second, created, err := LoadOrCreate(path, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected second load to reuse key")
	}
	if !second.PrivateKey.Equal(first.PrivateKey) {
		t.Fatal("expected reused private key")
	}
}

func TestLoadOrCreateRejectsKeyIDMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-key.json")
	if _, _, err := LoadOrCreate(path, "first-key"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreate(path, "second-key"); err == nil {
		t.Fatal("expected key id mismatch")
	}
}
