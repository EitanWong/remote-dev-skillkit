//go:build windows

package protectedstore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	dpapiFileSchemaVersion  = "rdev.protected-store.dpapi-file.v1"
	dpapiScopeCurrentUser   = "current-user"
	cryptProtectUIForbidden = 0x1
)

var (
	crypt32DLL         = syscall.NewLazyDLL("crypt32.dll")
	kernel32DLL        = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtect   = crypt32DLL.NewProc("CryptProtectData")
	procCryptUnprotect = crypt32DLL.NewProc("CryptUnprotectData")
	procLocalFree      = kernel32DLL.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

type dpapiFileEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Backend       string `json:"backend"`
	Scope         string `json:"scope"`
	Service       string `json:"service"`
	Account       string `json:"account"`
	ProtectedData string `json:"protected_data"`
}

func platformDPAPIBackend() dpapiBackend {
	return windowsDPAPIBackend{}
}

type windowsDPAPIBackend struct{}

func (windowsDPAPIBackend) Load(service, account string) ([]byte, bool, error) {
	path, err := dpapiStorePath(service, account)
	if err != nil {
		return nil, false, err
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var envelope dpapiFileEnvelope
	if err := json.Unmarshal(content, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.SchemaVersion != dpapiFileSchemaVersion {
		return nil, false, fmt.Errorf("unsupported DPAPI protected store schema %q", envelope.SchemaVersion)
	}
	if envelope.Backend != "dpapi" || envelope.Scope != dpapiScopeCurrentUser {
		return nil, false, fmt.Errorf("unsupported DPAPI protected store backend %q scope %q", envelope.Backend, envelope.Scope)
	}
	if envelope.Service != service || envelope.Account != account {
		return nil, false, fmt.Errorf("DPAPI protected store ref mismatch")
	}
	protectedData, err := base64.StdEncoding.DecodeString(envelope.ProtectedData)
	if err != nil {
		return nil, false, fmt.Errorf("decode DPAPI protected data: %w", err)
	}
	plaintext, err := dpapiUnprotect(protectedData, dpapiEntropy(service, account))
	if err != nil {
		return nil, false, err
	}
	return plaintext, true, nil
}

func (windowsDPAPIBackend) Save(service, account string, content []byte) error {
	path, err := dpapiStorePath(service, account)
	if err != nil {
		return err
	}
	protectedData, err := dpapiProtect(content, dpapiDescription(service, account), dpapiEntropy(service, account))
	if err != nil {
		return err
	}
	envelope := dpapiFileEnvelope{
		SchemaVersion: dpapiFileSchemaVersion,
		Backend:       "dpapi",
		Scope:         dpapiScopeCurrentUser,
		Service:       service,
		Account:       account,
		ProtectedData: base64.StdEncoding.EncodeToString(protectedData),
	}
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".dpapi-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	cleanup = false
	return os.Chmod(path, 0o600)
}

func dpapiProtect(content []byte, description string, entropy []byte) ([]byte, error) {
	in := bytesToBlob(content)
	entropyBlob := bytesToBlob(entropy)
	descriptionPtr, err := syscall.UTF16PtrFromString(description)
	if err != nil {
		return nil, err
	}
	var out dataBlob
	ret, _, callErr := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		uintptr(unsafe.Pointer(descriptionPtr)),
		uintptr(unsafe.Pointer(&entropyBlob)),
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CryptProtectData failed: %w", callErr)
	}
	defer localFree(out.pbData)
	return blobBytes(out), nil
}

func dpapiUnprotect(protectedData []byte, entropy []byte) ([]byte, error) {
	in := bytesToBlob(protectedData)
	entropyBlob := bytesToBlob(entropy)
	var out dataBlob
	ret, _, callErr := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		uintptr(unsafe.Pointer(&entropyBlob)),
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("CryptUnprotectData failed: %w", callErr)
	}
	defer localFree(out.pbData)
	return blobBytes(out), nil
}

func bytesToBlob(content []byte) dataBlob {
	if len(content) == 0 {
		return dataBlob{}
	}
	return dataBlob{
		cbData: uint32(len(content)),
		pbData: &content[0],
	}
}

func blobBytes(blob dataBlob) []byte {
	if blob.cbData == 0 || blob.pbData == nil {
		return nil
	}
	content := unsafe.Slice(blob.pbData, int(blob.cbData))
	return append([]byte(nil), content...)
}

func localFree(ptr *byte) {
	if ptr != nil {
		_, _, _ = procLocalFree.Call(uintptr(unsafe.Pointer(ptr)))
	}
}

func dpapiDescription(service, account string) string {
	return "remote-dev-skillkit:" + service + "/" + account
}

func dpapiEntropy(service, account string) []byte {
	return []byte("rdev.protectedstore.dpapi.v1\x00" + service + "\x00" + account)
}

func dpapiStorePath(service, account string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(service + "\x00" + account))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(configDir, "RemoteDevSkillkit", "ProtectedStore", "dpapi", name), nil
}
