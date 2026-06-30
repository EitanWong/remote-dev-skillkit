//go:build linux

package protectedstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	keyctlSchemaVersion = "rdev.protected-store.keyctl.v1"
	keyctlCommand       = "keyctl"
	keyctlTimeout       = 30 * time.Second
	keyctlKeyring       = "@u"
	keyctlKeyType       = "user"
	keyctlPermissions   = "0x3f3f0000"
)

var errKeyctlNotFound = errors.New("keyctl item not found")

type keyctlEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Backend       string `json:"backend"`
	Keyring       string `json:"keyring"`
	Service       string `json:"service"`
	Account       string `json:"account"`
	Content       string `json:"content"`
}

func platformKeyctlBackend() keyctlBackend {
	return linuxKeyctlBackend{}
}

type linuxKeyctlBackend struct{}

func (linuxKeyctlBackend) Load(service, account string) ([]byte, bool, error) {
	keyID, ok, err := findKeyctlItem(service, account)
	if err != nil || !ok {
		return nil, ok, err
	}
	raw, err := runKeyctl(nil, "pipe", keyID)
	if err != nil {
		return nil, false, err
	}
	var envelope keyctlEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.SchemaVersion != keyctlSchemaVersion {
		return nil, false, fmt.Errorf("unsupported keyctl protected store schema %q", envelope.SchemaVersion)
	}
	if envelope.Backend != "keyctl" || envelope.Keyring != keyctlKeyring {
		return nil, false, fmt.Errorf("unsupported keyctl protected store backend %q keyring %q", envelope.Backend, envelope.Keyring)
	}
	if envelope.Service != service || envelope.Account != account {
		return nil, false, fmt.Errorf("keyctl protected store ref mismatch")
	}
	content, err := base64.StdEncoding.DecodeString(envelope.Content)
	if err != nil {
		return nil, false, fmt.Errorf("decode keyctl protected content: %w", err)
	}
	return content, true, nil
}

func (linuxKeyctlBackend) Save(service, account string, content []byte) error {
	envelope := keyctlEnvelope{
		SchemaVersion: keyctlSchemaVersion,
		Backend:       "keyctl",
		Keyring:       keyctlKeyring,
		Service:       service,
		Account:       account,
		Content:       base64.StdEncoding.EncodeToString(content),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	keyID, ok, err := findKeyctlItem(service, account)
	if err != nil {
		return err
	}
	if ok {
		if _, err := runKeyctl(encoded, "pupdate", keyID); err != nil {
			return err
		}
		return hardenKeyctlItem(keyID)
	}
	out, err := runKeyctl(encoded, "padd", keyctlKeyType, keyctlDescription(service, account), keyctlKeyring)
	if err != nil {
		return err
	}
	keyID = strings.TrimSpace(string(out))
	if keyID == "" {
		return fmt.Errorf("keyctl padd returned an empty key id")
	}
	return hardenKeyctlItem(keyID)
}

func findKeyctlItem(service, account string) (string, bool, error) {
	out, err := runKeyctl(nil, "search", keyctlKeyring, keyctlKeyType, keyctlDescription(service, account))
	if errors.Is(err, errKeyctlNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	keyID := strings.TrimSpace(string(out))
	if keyID == "" {
		return "", false, fmt.Errorf("keyctl search returned an empty key id")
	}
	return keyID, true, nil
}

func hardenKeyctlItem(keyID string) error {
	if _, err := runKeyctl(nil, "setperm", keyID, keyctlPermissions); err != nil {
		return err
	}
	_, err := runKeyctl(nil, "timeout", keyID, "0")
	return err
}

func runKeyctl(stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, keyctlCommand, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s %s timed out", keyctlCommand, strings.Join(args, " "))
	}
	if err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if strings.Contains(stderrText, "Requested key not available") ||
			strings.Contains(stderrText, "Required key not available") ||
			strings.Contains(stderrText, "No such file or directory") {
			return nil, errKeyctlNotFound
		}
		if stderrText != "" {
			return nil, fmt.Errorf("%s %s failed: %s", keyctlCommand, strings.Join(args, " "), stderrText)
		}
		return nil, fmt.Errorf("%s %s failed: %w", keyctlCommand, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func keyctlDescription(service, account string) string {
	sum := sha256.Sum256([]byte(service + "\x00" + account))
	return "rdev.protected-store.keyctl.v1:" + hex.EncodeToString(sum[:])
}
