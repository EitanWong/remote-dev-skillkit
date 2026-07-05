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

func TestParseDPAPIRef(t *testing.T) {
	ref, err := ParseRef("dpapi:remote-dev-skillkit/managed-windows")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "dpapi" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-windows" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseDPAPIURLLikeRef(t *testing.T) {
	ref, err := ParseRef("dpapi://remote-dev-skillkit/managed-windows")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "dpapi" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-windows" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseDPAPIRefRejectsMissingAccount(t *testing.T) {
	if _, err := ParseRef("dpapi:remote-dev-skillkit"); err == nil {
		t.Fatal("expected missing account to fail")
	}
}

func TestParseLibsecretRef(t *testing.T) {
	ref, err := ParseRef("libsecret:remote-dev-skillkit/managed-linux")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "libsecret" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-linux" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseLibsecretURLLikeRef(t *testing.T) {
	ref, err := ParseRef("libsecret://remote-dev-skillkit/managed-linux")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "libsecret" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-linux" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseLibsecretRefRejectsMissingAccount(t *testing.T) {
	if _, err := ParseRef("libsecret:remote-dev-skillkit"); err == nil {
		t.Fatal("expected missing account to fail")
	}
}

func TestParseKeyctlRef(t *testing.T) {
	ref, err := ParseRef("keyctl:remote-dev-skillkit/managed-linux")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "keyctl" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-linux" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseKeyctlURLLikeRef(t *testing.T) {
	ref, err := ParseRef("keyctl://remote-dev-skillkit/managed-linux")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "keyctl" || ref.Service != "remote-dev-skillkit" || ref.Account != "managed-linux" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseKeyctlRefRejectsMissingAccount(t *testing.T) {
	if _, err := ParseRef("keyctl:remote-dev-skillkit"); err == nil {
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

func TestDPAPIStoreUsesBackend(t *testing.T) {
	backend := &memoryDPAPIBackend{items: map[string][]byte{}}
	restore := SetDPAPIBackendForTest(backend)
	defer restore()

	store, err := Open("dpapi:remote-dev-skillkit/managed-windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected empty store, ok=%v err=%v", ok, err)
	}
	content := []byte("protected windows content")
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

func TestLibsecretStoreUsesBackend(t *testing.T) {
	backend := &memoryLibsecretBackend{items: map[string][]byte{}}
	restore := SetLibsecretBackendForTest(backend)
	defer restore()

	store, err := Open("libsecret:remote-dev-skillkit/managed-linux")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected empty store, ok=%v err=%v", ok, err)
	}
	content := []byte("protected linux content")
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

func TestKeyctlStoreUsesBackend(t *testing.T) {
	backend := &memoryKeyctlBackend{items: map[string][]byte{}}
	restore := SetKeyctlBackendForTest(backend)
	defer restore()

	store, err := Open("keyctl:remote-dev-skillkit/managed-linux")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected empty store, ok=%v err=%v", ok, err)
	}
	content := []byte("protected headless linux content")
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

func TestTPMStoreUsesBackend(t *testing.T) {
	backend := &memoryTPMBackend{items: map[string][]byte{}}
	restore := SetTPMBackendForTest(backend)
	defer restore()

	store, err := Open("tpm:remote-dev-skillkit/fleet-host-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected empty tpm store, ok=%v err=%v", ok, err)
	}
	content := []byte("tpm-sealed key material")
	if err := store.Save(content); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(loaded, content) {
		t.Fatalf("unexpected tpm loaded content ok=%v content=%q", ok, loaded)
	}
}

func TestParseTPMRef(t *testing.T) {
	ref, err := ParseRef("tpm:remote-dev-skillkit/fleet-host-key")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "tpm" || ref.Service != "remote-dev-skillkit" || ref.Account != "fleet-host-key" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseTPMRefRejectsMissingAccount(t *testing.T) {
	if _, err := ParseRef("tpm:remote-dev-skillkit"); err == nil {
		t.Fatal("expected missing account to fail")
	}
}

func TestIsRefRecognisesTPMPrefix(t *testing.T) {
	if !IsRef("tpm:svc/acct") {
		t.Fatal("IsRef should recognise tpm: prefix")
	}
}

func TestMDMStoreUsesBackend(t *testing.T) {
	backend := &memoryMDMBackend{items: map[string][]byte{}}
	restore := SetMDMBackendForTest(backend)
	defer restore()

	store, err := Open("mdm:remote-dev-skillkit/fleet-mdm-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); err != nil || ok {
		t.Fatalf("expected empty mdm store, ok=%v err=%v", ok, err)
	}
	content := []byte("mdm-managed fleet identity")
	if err := store.Save(content); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(loaded, content) {
		t.Fatalf("unexpected mdm loaded content ok=%v content=%q", ok, loaded)
	}
}

func TestParseMDMRef(t *testing.T) {
	ref, err := ParseRef("mdm:remote-dev-skillkit/fleet-mdm-key")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Backend != "mdm" || ref.Service != "remote-dev-skillkit" || ref.Account != "fleet-mdm-key" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseMDMRefRejectsMissingAccount(t *testing.T) {
	if _, err := ParseRef("mdm:remote-dev-skillkit"); err == nil {
		t.Fatal("expected missing account to fail")
	}
}

func TestIsRefRecognisesMDMPrefix(t *testing.T) {
	if !IsRef("mdm:svc/acct") {
		t.Fatal("IsRef should recognise mdm: prefix")
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

type memoryDPAPIBackend struct {
	items map[string][]byte
}

func (b *memoryDPAPIBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *memoryDPAPIBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type memoryLibsecretBackend struct {
	items map[string][]byte
}

func (b *memoryLibsecretBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *memoryLibsecretBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type memoryKeyctlBackend struct {
	items map[string][]byte
}

func (b *memoryKeyctlBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *memoryKeyctlBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type memoryTPMBackend struct {
	items map[string][]byte
}

func (b *memoryTPMBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *memoryTPMBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

type memoryMDMBackend struct {
	items map[string][]byte
}

func (b *memoryMDMBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *memoryMDMBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}
