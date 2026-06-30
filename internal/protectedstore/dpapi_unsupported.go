//go:build !windows

package protectedstore

import "fmt"

func platformDPAPIBackend() dpapiBackend {
	return unsupportedDPAPIBackend{}
}

type unsupportedDPAPIBackend struct{}

func (unsupportedDPAPIBackend) Load(service, account string) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("Windows DPAPI protected store is not available on this platform")
}

func (unsupportedDPAPIBackend) Save(service, account string, content []byte) error {
	return fmt.Errorf("Windows DPAPI protected store is not available on this platform")
}
