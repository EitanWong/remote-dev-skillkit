package hostidentity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/protectedstore"
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

func TestLoadOrCreateUsesProtectedStoreRef(t *testing.T) {
	backend := &identityMemoryKeychainBackend{items: map[string][]byte{}}
	restore := protectedstore.SetKeychainBackendForTest(backend)
	defer restore()

	ref := "keychain:remote-dev-skillkit/managed-mac"
	first, created, err := LoadOrCreate(ref, "host-keychain")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected protected identity to be created")
	}
	second, created, err := LoadOrCreate(ref, "host-keychain")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected protected identity to be reused")
	}
	if !second.PrivateKey.Equal(first.PrivateKey) {
		t.Fatal("expected protected private key to be reused")
	}
	if _, _, err := LoadOrCreate(ref, "other-key"); err == nil {
		t.Fatal("expected protected key id mismatch")
	}
}

func TestLoadOrCreateUsesDPAPIProtectedStoreRef(t *testing.T) {
	backend := &identityMemoryDPAPIBackend{items: map[string][]byte{}}
	restore := protectedstore.SetDPAPIBackendForTest(backend)
	defer restore()

	ref := "dpapi:remote-dev-skillkit/managed-windows"
	first, created, err := LoadOrCreate(ref, "host-dpapi")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected DPAPI identity to be created")
	}
	second, created, err := LoadOrCreate(ref, "host-dpapi")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected DPAPI identity to be reused")
	}
	if !second.PrivateKey.Equal(first.PrivateKey) {
		t.Fatal("expected DPAPI private key to be reused")
	}
	if _, _, err := LoadOrCreate(ref, "other-key"); err == nil {
		t.Fatal("expected DPAPI key id mismatch")
	}
}

type identityMemoryKeychainBackend struct {
	items map[string][]byte
}

func (b *identityMemoryKeychainBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *identityMemoryKeychainBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type identityMemoryDPAPIBackend struct {
	items map[string][]byte
}

func (b *identityMemoryDPAPIBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *identityMemoryDPAPIBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}
