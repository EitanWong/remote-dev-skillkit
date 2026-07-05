//go:build darwin

package protectedstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// mdmDarwinBackend is a stub MDM-backed store for macOS.
//
// On a macOS device enrolled in MDM (Apple Business Manager / JAMF / Mosyle /
// Kandji etc.), MDM profiles can deliver configuration profiles and managed
// preferences. A full implementation would read from the managed preferences
// domain using CFPreferencesCopyValue with kCFPreferencesAnyUser /
// kCFPreferencesCurrentHost to pick up fleet-distributed keys.
//
// This stub writes and reads a local file tagged with the service/account
// identity so that the rest of the system can exercise the MDM code path
// without requiring real MDM enrollment.
//
// Production deployments should replace the file operations below with:
//   - MDM profile delivery of the signing key material to a protected preference
//     domain (e.g. com.rdev.fleet-identity)
//   - CFPreferencesCopyValue reads restricted to the managed domain
//   - Mandatory code-signing entitlement checks before granting access
type mdmDarwinBackend struct{}

func platformMDMBackend() mdmBackend { return mdmDarwinBackend{} }

func (b mdmDarwinBackend) Load(service, account string) ([]byte, bool, error) {
	path, err := mdmPath(service, account)
	if err != nil {
		return nil, false, err
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("mdm load %s/%s: %w", service, account, err)
	}
	return content, true, nil
}

func (b mdmDarwinBackend) Save(service, account string, content []byte) error {
	path, err := mdmPath(service, account)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mdm save mkdir %s/%s: %w", service, account, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("mdm save %s/%s: %w", service, account, err)
	}
	return os.Chmod(path, 0o600)
}

func mdmPath(service, account string) (string, error) {
	if strings.ContainsAny(service+account, "/\\") {
		return "", fmt.Errorf("mdm service/account must not contain path separators")
	}
	base := os.Getenv("RDEV_MDM_STORE_DIR")
	if base == "" {
		base = "/Library/Managed Preferences/rdev"
	}
	return filepath.Join(base, service, account+".mdm-managed"), nil
}
