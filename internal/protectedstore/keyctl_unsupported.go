//go:build !linux

package protectedstore

import "fmt"

func platformKeyctlBackend() keyctlBackend {
	return unsupportedKeyctlBackend{}
}

type unsupportedKeyctlBackend struct{}

func (unsupportedKeyctlBackend) Load(service, account string) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("Linux keyctl protected store is not available on this platform")
}

func (unsupportedKeyctlBackend) Save(service, account string, content []byte) error {
	return fmt.Errorf("Linux keyctl protected store is not available on this platform")
}
