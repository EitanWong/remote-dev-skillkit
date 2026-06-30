//go:build !darwin || !cgo

package protectedstore

import "fmt"

func platformKeychainBackend() keychainBackend {
	return unsupportedKeychainBackend{}
}

type unsupportedKeychainBackend struct{}

func (unsupportedKeychainBackend) Load(service, account string) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("macOS Keychain protected store is not available on this platform")
}

func (unsupportedKeychainBackend) Save(service, account string, content []byte) error {
	return fmt.Errorf("macOS Keychain protected store is not available on this platform")
}
