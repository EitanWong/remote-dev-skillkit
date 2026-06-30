//go:build linux

package protectedstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	libsecretSchemaVersion = "rdev.protected-store.libsecret.v1"
	libsecretCommand       = "secret-tool"
	libsecretTimeout       = 30 * time.Second
)

var errLibsecretNotFound = errors.New("libsecret item not found")

type libsecretEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Backend       string `json:"backend"`
	Service       string `json:"service"`
	Account       string `json:"account"`
	Content       string `json:"content"`
}

func platformLibsecretBackend() libsecretBackend {
	return secretToolBackend{}
}

type secretToolBackend struct{}

func (secretToolBackend) Load(service, account string) ([]byte, bool, error) {
	secret, err := runSecretTool(nil, "lookup", "rdev_service", service, "rdev_account", account)
	if errors.Is(err, errLibsecretNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var envelope libsecretEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(secret), &envelope); err != nil {
		return nil, false, err
	}
	if envelope.SchemaVersion != libsecretSchemaVersion {
		return nil, false, fmt.Errorf("unsupported libsecret protected store schema %q", envelope.SchemaVersion)
	}
	if envelope.Backend != "libsecret" {
		return nil, false, fmt.Errorf("unsupported libsecret protected store backend %q", envelope.Backend)
	}
	if envelope.Service != service || envelope.Account != account {
		return nil, false, fmt.Errorf("libsecret protected store ref mismatch")
	}
	content, err := base64.StdEncoding.DecodeString(envelope.Content)
	if err != nil {
		return nil, false, fmt.Errorf("decode libsecret protected content: %w", err)
	}
	return content, true, nil
}

func (secretToolBackend) Save(service, account string, content []byte) error {
	envelope := libsecretEnvelope{
		SchemaVersion: libsecretSchemaVersion,
		Backend:       "libsecret",
		Service:       service,
		Account:       account,
		Content:       base64.StdEncoding.EncodeToString(content),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = runSecretTool(encoded, "store", "--label", libsecretLabel(service, account), "rdev_service", service, "rdev_account", account)
	return err
}

func runSecretTool(stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), libsecretTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, libsecretCommand, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s %s timed out", libsecretCommand, strings.Join(args, " "))
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok &&
			exitErr.ExitCode() == 1 &&
			stdout.Len() == 0 &&
			strings.TrimSpace(stderr.String()) == "" {
			return nil, errLibsecretNotFound
		}
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s %s failed: %s", libsecretCommand, strings.Join(args, " "), strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("%s %s failed: %w", libsecretCommand, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func libsecretLabel(service, account string) string {
	return "Remote Dev Skillkit " + service + "/" + account
}
