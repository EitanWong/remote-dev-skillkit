package hosttrust

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestOpenStoreNoopForEmptyRef(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(NoopStore); !ok {
		t.Fatalf("expected NoopStore, got %T", store)
	}
}

func TestOpenStoreRejectsMalformedProtectedRef(t *testing.T) {
	// "keychain:" has the protected prefix but no service/account, so Open fails.
	if _, err := OpenStore("keychain:"); err == nil {
		t.Fatal("expected malformed protected ref to fail")
	}
}

func TestOpenStoreTreatsUnknownPrefixAsFilePath(t *testing.T) {
	// Any string that is not a protected ref is by design a file path.
	store, err := OpenStore("unknown:service/account")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(FileStore); !ok {
		t.Fatalf("expected FileStore, got %T", store)
	}
}

func TestOpenStoreAcceptsFileRef(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(FileStore); !ok {
		t.Fatalf("expected FileStore, got %T", store)
	}
}

func TestNoopStoreNeverPersists(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey := testKeyPair(t)
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

	var store NoopStore
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("NoopStore.Load should return not-found, ok=%v err=%v", ok, err)
	}
	if err := store.Save(bundle); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyAndSaveUpdate(bundle, model.NewTrustBundle("gateway", publicKey), now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("NoopStore must stay empty after save, ok=%v err=%v", ok, err)
	}
}

func TestFileStoreLoadMissingFile(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "does-not-exist.json")}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected not-found, ok=%v err=%v", ok, err)
	}
}

func TestFileStoreLoadCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := FileStore{Path: path}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("expected corrupt JSON to fail")
	}
}

func TestFileStoreLoadRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"rdev.unknown.v9","trust_bundle":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := FileStore{Path: path}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("expected unsupported schema to fail")
	}
}

func TestFileStoreEmptyPathIsNoop(t *testing.T) {
	store := FileStore{}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("empty path load should be noop, ok=%v err=%v", ok, err)
	}
	if err := store.Save(model.SignedTrustBundle{}); err != nil {
		t.Fatalf("empty path save should be noop: %v", err)
	}
}
