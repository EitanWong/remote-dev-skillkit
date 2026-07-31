package hostcmd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
)

const (
	managedServiceConfigSchema    = "rdev.managed-service.v1"
	defaultManagedServiceName     = "RemoteDevSkillkitHost"
	managedServiceConfigFilename  = "service.json"
	managedServiceBinaryFilename  = "rdev-host.exe"
	managedServiceReleasesDir     = "releases"
	managedServiceIdentityFile    = "identity.json"
	managedServiceTrustFile       = "trust.json"
	managedServiceWorkspaceLocker = "workspace-locks"
	managedServiceRetryDelay      = 5 * time.Second
)

func runManagedServiceWithRetry(ctx context.Context, retryDelay time.Duration, run func(context.Context) error) error {
	for {
		err := run(ctx)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		var permanent permanentJoinFailure
		if errors.As(err, &permanent) {
			return err
		}

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

type managedServiceConfig struct {
	SchemaVersion string `json:"schema_version"`
	ServiceName   string `json:"service_name"`
	GatewayURL    string `json:"gateway_url"`
	JoinCode      string `json:"join_code"`
	StateRoot     string `json:"state_root"`
}

func (c managedServiceConfig) validate() error {
	if c.SchemaVersion != managedServiceConfigSchema {
		return fmt.Errorf("unsupported managed service config schema %q", c.SchemaVersion)
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return fmt.Errorf("managed service name is required")
	}
	if strings.TrimSpace(c.GatewayURL) == "" || !isSessionGatewayURL(c.GatewayURL) {
		return fmt.Errorf("managed service requires an HTTPS gateway")
	}
	if strings.TrimSpace(c.JoinCode) == "" {
		return fmt.Errorf("managed service join code is required")
	}
	if strings.TrimSpace(c.StateRoot) == "" {
		return fmt.Errorf("managed service state root is required")
	}
	return nil
}

func (c managedServiceConfig) serveOptions() serveOptions {
	return serveOptions{
		Mode:               "managed",
		GatewayURL:         c.GatewayURL,
		JoinCode:           c.JoinCode,
		Once:               false,
		Transport:          "long-poll",
		PollInterval:       time.Second,
		LongPollTimeout:    25 * time.Second,
		MaxTasks:           0,
		TrustStorePath:     filepath.Join(c.StateRoot, managedServiceTrustFile),
		IdentityStorePath:  filepath.Join(c.StateRoot, managedServiceIdentityFile),
		IdentityKeyID:      hostidentity.DefaultKeyID,
		WorkspaceLockStore: filepath.Join(c.StateRoot, managedServiceWorkspaceLocker),
		KeepAwake:          true,
	}
}

func readManagedServiceConfig(path string) (managedServiceConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return managedServiceConfig{}, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var config managedServiceConfig
	if err := decoder.Decode(&config); err != nil {
		return managedServiceConfig{}, fmt.Errorf("decode managed service config: %w", err)
	}
	if err := config.validate(); err != nil {
		return managedServiceConfig{}, err
	}
	return config, nil
}

func writeManagedServiceConfig(path string, config managedServiceConfig) error {
	config.SchemaVersion = managedServiceConfigSchema
	config.ServiceName = strings.TrimSpace(config.ServiceName)
	config.GatewayURL = strings.TrimSpace(config.GatewayURL)
	config.JoinCode = strings.TrimSpace(config.JoinCode)
	config.StateRoot = filepath.Clean(strings.TrimSpace(config.StateRoot))
	if err := config.validate(); err != nil {
		return err
	}

	content, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode managed service config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create managed service state root: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".service-*.json")
	if err != nil {
		return fmt.Errorf("create managed service config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect managed service config: %w", err)
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write managed service config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync managed service config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close managed service config: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace managed service config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("activate managed service config: %w", err)
	}
	return nil
}

func copyManagedServiceFileIfPresent(source, destination string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	if _, err := os.Stat(destination); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return copyManagedServiceFile(source, destination)
}

// prepareManagedServiceRelease copies a host executable into an immutable,
// content-addressed release path. A running service can keep its prior release
// open while a replacement is prepared at a different path.
func prepareManagedServiceRelease(root, source string) (string, error) {
	source = filepath.Clean(strings.TrimSpace(source))
	if source == "" || source == "." {
		return "", fmt.Errorf("managed service release source is required")
	}
	sum, err := managedServiceFileSHA256(source)
	if err != nil {
		return "", fmt.Errorf("hash managed service release source: %w", err)
	}
	destination := filepath.Join(root, managedServiceReleasesDir, sum, managedServiceBinaryFilename)
	if _, err := os.Stat(destination); err == nil {
		installedSum, err := managedServiceFileSHA256(destination)
		if err != nil {
			return "", fmt.Errorf("hash existing managed service release: %w", err)
		}
		if installedSum != sum {
			return "", fmt.Errorf("managed service release hash mismatch at %s", destination)
		}
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect managed service release: %w", err)
	}
	if err := copyManagedServiceFile(source, destination); err != nil {
		return "", err
	}
	installedSum, err := managedServiceFileSHA256(destination)
	if err != nil {
		return "", fmt.Errorf("hash staged managed service release: %w", err)
	}
	if installedSum != sum {
		return "", fmt.Errorf("managed service release hash mismatch after staging")
	}
	return destination, nil
}

func managedServiceFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func copyManagedServiceFile(source, destination string) error {
	source = filepath.Clean(strings.TrimSpace(source))
	destination = filepath.Clean(strings.TrimSpace(destination))
	if source == "" || destination == "" {
		return fmt.Errorf("managed service file paths are required")
	}
	if source == destination {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open managed service source: %w", err)
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create managed service destination: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".copy-*")
	if err != nil {
		return fmt.Errorf("create managed service copy: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return fmt.Errorf("copy managed service file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync managed service file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close managed service file: %w", err)
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace managed service file: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("activate managed service file: %w", err)
	}
	return nil
}
