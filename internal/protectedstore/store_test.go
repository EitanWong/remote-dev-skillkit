package protectedstore

import (
	"bytes"
	"testing"
)

func TestParseKeychainRef(t *testing.T) {
	ref, err := ParseRef("keychain:remote-dev-skillkit/managed-mac")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "keychain" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-mac" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseKeychainURLLikeRef(t *testing.T) {
	ref, err := ParseRef("keychain://remote-dev-skillkit/managed-mac")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Service != "remote-dev-skillkit" || ref.Account != "managed-mac" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseKeychainRefRejectsMissingAccount(t *testing.T) {
	if _, err := ParseRef("keychain:remote-dev-skillkit"); err == nil {
		t.Fatal("expected missing account to fail")
	}
}

func TestKeychainStoreUsesBackend(t *testing.T) {
	backend := &memoryKeychainBackend{items: map[string][]byte{}}
	restore := SetKeychainBackendForTest(backend)
	defer restore()

	store, err := Open("keychain:remote-dev-skillkit/managed-mac")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected empty store, ok=%v err=%v", ok, err)
	}
	content := []byte("protected content")
	if err := store.Save(content); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(loaded, content) {
		t.Fatalf("unexpected loaded content ok=%v content=%q", ok, loaded)
	}
}

type memoryKeychainBackend struct {
	items map[string][]byte
}

func (b *memoryKeychainBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *memoryKeychainBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}
