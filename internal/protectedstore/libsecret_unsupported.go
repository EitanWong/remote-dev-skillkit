//go:build !linux

package protectedstore

import "fmt"

func platformLibsecretBackend() libsecretBackend {
	return unsupportedLibsecretBackend{}
}

type unsupportedLibsecretBackend struct{}

func (unsupportedLibsecretBackend) Load(service, account string) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("Linux libsecret protected store is not available on this platform")
}

func (unsupportedLibsecretBackend) Save(service, account string, content []byte) error {
	return fmt.Errorf("Linux libsecret protected store is not available on this platform")
}
