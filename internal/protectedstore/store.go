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
	KeyctlPrefix    = "keyctl:"
	TPMPrefix       = "tpm:"
	MDMPrefix       = "mdm:"
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

type keyctlBackend interface {
	Load(service, account string) ([]byte, bool, error)
	Save(service, account string, content []byte) error
}

type tpmBackend interface {
	Load(service, account string) ([]byte, bool, error)
	Save(service, account string, content []byte) error
}

type mdmBackend interface {
	Load(service, account string) ([]byte, bool, error)
	Save(service, account string, content []byte) error
}

var activeKeychainBackend keychainBackend = platformKeychainBackend()
var activeDPAPIBackend dpapiBackend = platformDPAPIBackend()
var activeLibsecretBackend libsecretBackend = platformLibsecretBackend()
var activeKeyctlBackend keyctlBackend = platformKeyctlBackend()
var activeTPMBackend tpmBackend = platformTPMBackend()
var activeMDMBackend mdmBackend = platformMDMBackend()

func IsRef(value string) bool {
	return strings.HasPrefix(value, KeychainPrefix) ||
		strings.HasPrefix(value, DPAPIPrefix) ||
		strings.HasPrefix(value, LibsecretPrefix) ||
		strings.HasPrefix(value, KeyctlPrefix) ||
		strings.HasPrefix(value, TPMPrefix) ||
		strings.HasPrefix(value, MDMPrefix)
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
	case strings.HasPrefix(value, KeyctlPrefix):
		service, account, err := parseServiceAccount(value, KeyctlPrefix)
		if err != nil {
			return Ref{}, fmt.Errorf("keyctl ref must be formatted keyctl:<service>/<account>")
		}
		return Ref{
			Backend: "keyctl",
			Service: service,
			Account: account,
		}, nil
	case strings.HasPrefix(value, TPMPrefix):
		service, account, err := parseServiceAccount(value, TPMPrefix)
		if err != nil {
			return Ref{}, fmt.Errorf("tpm ref must be formatted tpm:<service>/<account>")
		}
		return Ref{
			Backend: "tpm",
			Service: service,
			Account: account,
		}, nil
	case strings.HasPrefix(value, MDMPrefix):
		service, account, err := parseServiceAccount(value, MDMPrefix)
		if err != nil {
			return Ref{}, fmt.Errorf("mdm ref must be formatted mdm:<service>/<account>")
		}
		return Ref{
			Backend: "mdm",
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
	case "keyctl":
		return KeyctlStore{Service: ref.Service, Account: ref.Account}, nil
	case "tpm":
		return TPMStore{Service: ref.Service, Account: ref.Account}, nil
	case "mdm":
		return MDMStore{Service: ref.Service, Account: ref.Account}, nil
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

type KeyctlStore struct {
	Service string
	Account string
}

func (s KeyctlStore) Load() ([]byte, bool, error) {
	if s.Service == "" || s.Account == "" {
		return nil, false, fmt.Errorf("keyctl service and account are required")
	}
	return activeKeyctlBackend.Load(s.Service, s.Account)
}

func (s KeyctlStore) Save(content []byte) error {
	if s.Service == "" || s.Account == "" {
		return fmt.Errorf("keyctl service and account are required")
	}
	return activeKeyctlBackend.Save(s.Service, s.Account, content)
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

func SetKeyctlBackendForTest(next keyctlBackend) func() {
	previous := activeKeyctlBackend
	activeKeyctlBackend = next
	return func() {
		activeKeyctlBackend = previous
	}
}

type TPMStore struct {
	Service string
	Account string
}

func (s TPMStore) Load() ([]byte, bool, error) {
	if s.Service == "" || s.Account == "" {
		return nil, false, fmt.Errorf("tpm service and account are required")
	}
	return activeTPMBackend.Load(s.Service, s.Account)
}

func (s TPMStore) Save(content []byte) error {
	if s.Service == "" || s.Account == "" {
		return fmt.Errorf("tpm service and account are required")
	}
	return activeTPMBackend.Save(s.Service, s.Account, content)
}

type MDMStore struct {
	Service string
	Account string
}

func (s MDMStore) Load() ([]byte, bool, error) {
	if s.Service == "" || s.Account == "" {
		return nil, false, fmt.Errorf("mdm service and account are required")
	}
	return activeMDMBackend.Load(s.Service, s.Account)
}

func (s MDMStore) Save(content []byte) error {
	if s.Service == "" || s.Account == "" {
		return fmt.Errorf("mdm service and account are required")
	}
	return activeMDMBackend.Save(s.Service, s.Account, content)
}

func SetTPMBackendForTest(next tpmBackend) func() {
	previous := activeTPMBackend
	activeTPMBackend = next
	return func() {
		activeTPMBackend = previous
	}
}

func SetMDMBackendForTest(next mdmBackend) func() {
	previous := activeMDMBackend
	activeMDMBackend = next
	return func() {
		activeMDMBackend = previous
	}
}
