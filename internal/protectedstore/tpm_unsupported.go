//go:build !linux

package protectedstore

import "fmt"

type tpmUnsupportedBackend struct{}

func platformTPMBackend() tpmBackend { return tpmUnsupportedBackend{} }

func (b tpmUnsupportedBackend) Load(service, account string) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("tpm store is not supported on this platform")
}

func (b tpmUnsupportedBackend) Save(service, account string, content []byte) error {
	return fmt.Errorf("tpm store is not supported on this platform")
}
