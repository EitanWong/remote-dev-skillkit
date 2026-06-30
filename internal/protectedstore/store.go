package protectedstore

import (
	"errors"
	"fmt"
	"strings"
)

const (
	KeychainPrefix  = "keychain:"
	DPAPIPrefix     = "dpapi:"
	LibsecretPrefix = "libsecret:"
)

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

type dpapiBackend interface {
	Load(service, account string) ([]byte, bool, error)
	Save(service, account string, content []byte) error
}

type libsecretBackend interface {
	Load(service, account string) ([]byte, bool, error)
	Save(service, account string, content []byte) error
}

var activeKeychainBackend keychainBackend = platformKeychainBackend()
var activeDPAPIBackend dpapiBackend = platformDPAPIBackend()
var activeLibsecretBackend libsecretBackend = platformLibsecretBackend()

func IsRef(value string) bool {
	return strings.HasPrefix(value, KeychainPrefix) ||
		strings.HasPrefix(value, DPAPIPrefix) ||
		strings.HasPrefix(value, LibsecretPrefix)
}

func ParseRef(value string) (Ref, error) {
	switch {
	case strings.HasPrefix(value, KeychainPrefix):
		service, account, err := parseServiceAccount(value, KeychainPrefix)
		if err != nil {
			return Ref{}, fmt.Errorf("keychain ref must be formatted keychain:<service>/<account>")
		}
		return Ref{
			Backend: "keychain",
			Service: service,
			Account: account,
		}, nil
	case strings.HasPrefix(value, DPAPIPrefix):
		service, account, err := parseServiceAccount(value, DPAPIPrefix)
		if err != nil {
			return Ref{}, fmt.Errorf("dpapi ref must be formatted dpapi:<service>/<account>")
		}
		return Ref{
			Backend: "dpapi",
			Service: service,
			Account: account,
		}, nil
	case strings.HasPrefix(value, LibsecretPrefix):
		service, account, err := parseServiceAccount(value, LibsecretPrefix)
		if err != nil {
			return Ref{}, fmt.Errorf("libsecret ref must be formatted libsecret:<service>/<account>")
		}
		return Ref{
			Backend: "libsecret",
			Service: service,
			Account: account,
		}, nil
	default:
		return Ref{}, fmt.Errorf("unsupported protected store ref %q", value)
	}
}

func parseServiceAccount(value, prefix string) (string, string, error) {
	raw := strings.TrimPrefix(value, prefix)
	raw = strings.TrimPrefix(raw, "//")
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("missing service or account")
	}
	return parts[0], parts[1], nil
}

func Open(value string) (Store, error) {
	ref, err := ParseRef(value)
	if err != nil {
		return nil, err
	}
	switch ref.Backend {
	case "keychain":
		return KeychainStore{Service: ref.Service, Account: ref.Account}, nil
	case "dpapi":
		return DPAPIStore{Service: ref.Service, Account: ref.Account}, nil
	case "libsecret":
		return LibsecretStore{Service: ref.Service, Account: ref.Account}, nil
	default:
		return nil, fmt.Errorf("unsupported protected store backend %q", ref.Backend)
	}
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

type DPAPIStore struct {
	Service string
	Account string
}

func (s DPAPIStore) Load() ([]byte, bool, error) {
	if s.Service == "" || s.Account == "" {
		return nil, false, fmt.Errorf("dpapi service and account are required")
	}
	return activeDPAPIBackend.Load(s.Service, s.Account)
}

func (s DPAPIStore) Save(content []byte) error {
	if s.Service == "" || s.Account == "" {
		return fmt.Errorf("dpapi service and account are required")
	}
	return activeDPAPIBackend.Save(s.Service, s.Account, content)
}

type LibsecretStore struct {
	Service string
	Account string
}

func (s LibsecretStore) Load() ([]byte, bool, error) {
	if s.Service == "" || s.Account == "" {
		return nil, false, fmt.Errorf("libsecret service and account are required")
	}
	return activeLibsecretBackend.Load(s.Service, s.Account)
}

func (s LibsecretStore) Save(content []byte) error {
	if s.Service == "" || s.Account == "" {
		return fmt.Errorf("libsecret service and account are required")
	}
	return activeLibsecretBackend.Save(s.Service, s.Account, content)
}

func SetKeychainBackendForTest(next keychainBackend) func() {
	previous := activeKeychainBackend
	activeKeychainBackend = next
	return func() {
		activeKeychainBackend = previous
	}
}

func SetDPAPIBackendForTest(next dpapiBackend) func() {
	previous := activeDPAPIBackend
	activeDPAPIBackend = next
	return func() {
		activeDPAPIBackend = previous
	}
}

func SetLibsecretBackendForTest(next libsecretBackend) func() {
	previous := activeLibsecretBackend
	activeLibsecretBackend = next
	return func() {
		activeLibsecretBackend = previous
	}
}
