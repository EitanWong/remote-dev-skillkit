//go:build !darwin

package protectedstore

import "fmt"

type mdmUnsupportedBackend struct{}

func platformMDMBackend() mdmBackend { return mdmUnsupportedBackend{} }

func (b mdmUnsupportedBackend) Load(service, account string) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("mdm store is not supported on this platform")
}

func (b mdmUnsupportedBackend) Save(service, account string, content []byte) error {
	return fmt.Errorf("mdm store is not supported on this platform")
}
