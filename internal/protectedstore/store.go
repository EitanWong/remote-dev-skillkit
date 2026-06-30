package protectedstore

import (
	"errors"
	"fmt"
	"strings"
)

const KeychainPrefix = "keychain:"

var ErrNotFound = errors.New("protected store item not found")

type Ref struct {
	Backend string
	Service string
	Account string
}

type Store interface {
	Load() ([]byte, bool, error)
	Save(content []byte) error
}

type keychainBackend interface {
	Load(service, account string) ([]byte, bool, error)
	Save(service, account string, content []byte) error
}

var activeKeychainBackend keychainBackend = platformKeychainBackend()

func IsRef(value string) bool {
	return strings.HasPrefix(value, KeychainPrefix)
}

func ParseRef(value string) (Ref, error) {
	if !IsRef(value) {
		return Ref{}, fmt.Errorf("unsupported protected store ref %q", value)
	}
	raw := strings.TrimPrefix(value, KeychainPrefix)
	raw = strings.TrimPrefix(raw, "//")
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Ref{}, fmt.Errorf("keychain ref must be formatted keychain:<service>/<account>")
	}
	return Ref{
		Backend: "keychain",
		Service: parts[0],
		Account: parts[1],
	}, nil
}

func Open(value string) (Store, error) {
	ref, err := ParseRef(value)
	if err != nil {
		return nil, err
	}
	return KeychainStore{Service: ref.Service, Account: ref.Account}, nil
}

type KeychainStore struct {
	Service string
	Account string
}

func (s KeychainStore) Load() ([]byte, bool, error) {
	if s.Service == "" || s.Account == "" {
		return nil, false, fmt.Errorf("keychain service and account are required")
	}
	return activeKeychainBackend.Load(s.Service, s.Account)
}

func (s KeychainStore) Save(content []byte) error {
	if s.Service == "" || s.Account == "" {
		return fmt.Errorf("keychain service and account are required")
	}
	return activeKeychainBackend.Save(s.Service, s.Account, content)
}

func SetKeychainBackendForTest(next keychainBackend) func() {
	previous := activeKeychainBackend
	activeKeychainBackend = next
	return func() {
		activeKeychainBackend = previous
	}
}
