//go:build linux

package protectedstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tpmLinuxBackend is a stub TPM-backed store for Linux.
//
// A full implementation would use the tpm2-tss or go-tpm library to seal data
// to the TPM's PCR values, ensuring the sealed blob can only be unsealed on the
// same hardware in the same measured-boot state. This stub writes and reads a
// local file tagged with the service/account identity so that the rest of the
// system can exercise the TPM code path without requiring real TPM hardware.
//
// Production deployments should replace the file operations below with:
//   - tpm2_createprimary + tpm2_create for sealing
//   - tpm2_unseal for loading
//   - PCR policy binding to enforce boot-state integrity
type tpmLinuxBackend struct{}

func platformTPMBackend() tpmBackend { return tpmLinuxBackend{} }

func (b tpmLinuxBackend) Load(service, account string) ([]byte, bool, error) {
	path, err := tpmPath(service, account)
	if err != nil {
		return nil, false, err
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("tpm load %s/%s: %w", service, account, err)
	}
	return content, true, nil
}

func (b tpmLinuxBackend) Save(service, account string, content []byte) error {
	path, err := tpmPath(service, account)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("tpm save mkdir %s/%s: %w", service, account, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("tpm save %s/%s: %w", service, account, err)
	}
	return os.Chmod(path, 0o600)
}

func tpmPath(service, account string) (string, error) {
	if strings.ContainsAny(service+account, "/\\") {
		return "", fmt.Errorf("tpm service/account must not contain path separators")
	}
	base := os.Getenv("RDEV_TPM_STORE_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("tpm store: locate home dir: %w", err)
		}
		base = filepath.Join(home, ".rdev", "tpm-store")
	}
	return filepath.Join(base, service, account+".tpm-sealed"), nil
}
