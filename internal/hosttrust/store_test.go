package hosttrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/protectedstore"
)

func TestFileStoreSavesAndLoadsTrustBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	path := filepath.Join(t.TempDir(), "trust", "bundle.json")
	store := FileStore{Path: path}
	bundle := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)

	if err := store.Save(bundle); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %#o", got)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected stored bundle")
	}
	if loaded.Sequence != bundle.Sequence || loaded.BundleID != bundle.BundleID {
		t.Fatalf("loaded wrong bundle: %#v", loaded)
	}
}

func TestFileStoreVerifyAndSaveUpdateRejectsRollback(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	store := FileStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	first := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     2,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	hash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rollback := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:           "managed-host",
		Sequence:           1,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)
	if err := store.VerifyAndSaveUpdate(rollback, model.NewTrustBundle("gateway", publicKey), now); err == nil {
		t.Fatal("expected rollback to fail")
	}
}

func TestFileStoreVerifyAndSaveUpdatePersistsValidUpdate(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	store := FileStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	first := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	hash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	next := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:           "managed-host",
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)
	if err := store.VerifyAndSaveUpdate(next, model.NewTrustBundle("gateway", publicKey), now); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Sequence != 2 {
		t.Fatalf("expected sequence 2, got ok=%v bundle=%#v", ok, loaded)
	}
}

func TestFileStoreVerifyAndSaveUpdateRejectsBadSignature(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, _ := testKeyPair(t)
	_, wrongPrivateKey := testKeyPair(t)
	store := FileStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	bundle := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, wrongPrivateKey, now)
	if err := store.VerifyAndSaveUpdate(bundle, model.NewTrustBundle("gateway", publicKey), now); !errors.Is(err, model.ErrTrustBundleSignature) {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestFileStoreVerifyAndSaveUpdateUsesStoredRoot(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	oldPublic, oldPrivate := testKeyPair(t)
	newPublic, newPrivate := testKeyPair(t)
	store := FileStore{Path: filepath.Join(t.TempDir(), "bundle.json")}
	first := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", oldPublic, model.TrustKeyStatusActive, now),
		},
	}, oldPrivate, now)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	hash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	forged := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:           "managed-host",
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", newPublic, model.TrustKeyStatusActive, now),
		},
	}, newPrivate, now)
	if err := store.VerifyAndSaveUpdate(forged, model.NewTrustBundle("gateway", newPublic), now); !errors.Is(err, model.ErrTrustBundleSignature) {
		t.Fatalf("expected signature error from stored root, got %v", err)
	}
}

func TestProtectedStoreSavesAndLoadsTrustBundle(t *testing.T) {
	backend := &trustMemoryKeychainBackend{items: map[string][]byte{}}
	restore := protectedstore.SetKeychainBackendForTest(backend)
	defer restore()

	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	store, err := OpenStore("keychain:remote-dev-skillkit/managed-mac-trust")
	if err != nil {
		t.Fatal(err)
	}
	bundle := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)

	if err := store.Save(bundle); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected protected trust bundle")
	}
	if loaded.Sequence != bundle.Sequence || loaded.BundleID != bundle.BundleID {
		t.Fatalf("loaded wrong protected bundle: %#v", loaded)
	}
}

func TestProtectedStoreVerifyAndSaveUpdateRejectsRollback(t *testing.T) {
	backend := &trustMemoryKeychainBackend{items: map[string][]byte{}}
	restore := protectedstore.SetKeychainBackendForTest(backend)
	defer restore()

	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	store, err := OpenStore("keychain:remote-dev-skillkit/managed-mac-trust")
	if err != nil {
		t.Fatal(err)
	}
	first := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     2,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	hash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rollback := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:           "managed-host",
		Sequence:           1,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: hash,
		SigningKeyID:       "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)
	if err := store.VerifyAndSaveUpdate(rollback, model.NewTrustBundle("gateway", publicKey), now); err == nil {
		t.Fatal("expected protected rollback to fail")
	}
}

func TestDPAPIProtectedStoreSavesAndLoadsTrustBundle(t *testing.T) {
	backend := &trustMemoryDPAPIBackend{items: map[string][]byte{}}
	restore := protectedstore.SetDPAPIBackendForTest(backend)
	defer restore()

	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	store, err := OpenStore("dpapi:remote-dev-skillkit/managed-windows-trust")
	if err != nil {
		t.Fatal(err)
	}
	bundle := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)

	if err := store.Save(bundle); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected DPAPI protected trust bundle")
	}
	if loaded.Sequence != bundle.Sequence || loaded.BundleID != bundle.BundleID {
		t.Fatalf("loaded wrong DPAPI protected bundle: %#v", loaded)
	}
}

func TestLibsecretProtectedStoreSavesAndLoadsTrustBundle(t *testing.T) {
	backend := &trustMemoryLibsecretBackend{items: map[string][]byte{}}
	restore := protectedstore.SetLibsecretBackendForTest(backend)
	defer restore()

	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
	store, err := OpenStore("libsecret:remote-dev-skillkit/managed-linux-trust")
	if err != nil {
		t.Fatal(err)
	}
	bundle := signedBundle(t, model.SignedTrustBundleSpec{
		BundleID:     "managed-host",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "gateway",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway", publicKey, model.TrustKeyStatusActive, now),
		},
	}, privateKey, now)

	if err := store.Save(bundle); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected libsecret protected trust bundle")
	}
	if loaded.Sequence != bundle.Sequence || loaded.BundleID != bundle.BundleID {
		t.Fatalf("loaded wrong libsecret protected bundle: %#v", loaded)
	}
}

type trustMemoryKeychainBackend struct {
	items map[string][]byte
}

func (b *trustMemoryKeychainBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *trustMemoryKeychainBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type trustMemoryDPAPIBackend struct {
	items map[string][]byte
}

func (b *trustMemoryDPAPIBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *trustMemoryDPAPIBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type trustMemoryLibsecretBackend struct {
	items map[string][]byte
}

func (b *trustMemoryLibsecretBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *trustMemoryLibsecretBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

func signedBundle(t *testing.T, spec model.SignedTrustBundleSpec, privateKey ed25519.PrivateKey, now time.Time) model.SignedTrustBundle {
	t.Helper()
	bundle, err := model.NewSignedTrustBundle(spec, now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = bundle.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}
