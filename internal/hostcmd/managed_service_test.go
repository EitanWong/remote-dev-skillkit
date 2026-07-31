package hostcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagedServiceConfigRoundTripProducesManagedServeOptions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, managedServiceConfigFilename)
	input := managedServiceConfig{
		ServiceName: defaultManagedServiceName,
		GatewayURL:  "https://gateway.example.test",
		JoinCode:    "JOIN-TEST",
		StateRoot:   root,
	}
	if err := writeManagedServiceConfig(path, input); err != nil {
		t.Fatal(err)
	}
	config, err := readManagedServiceConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	options := config.serveOptions()
	if options.Mode != "managed" || options.Once || options.Transport != "long-poll" || options.MaxTasks != 0 {
		t.Fatalf("unexpected managed service options: %#v", options)
	}
	if options.IdentityStorePath != filepath.Join(root, managedServiceIdentityFile) || options.TrustStorePath != filepath.Join(root, managedServiceTrustFile) || options.WorkspaceLockStore != filepath.Join(root, managedServiceWorkspaceLocker) {
		t.Fatalf("managed service state paths were not derived from the protected root: %#v", options)
	}
}

func TestPrepareManagedServiceReleaseUsesImmutableContentAddressedPaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "rdev-host.exe")
	if err := os.WriteFile(source, []byte("first host release"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := prepareManagedServiceRelease(root, source)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(source, []byte("second host release"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := prepareManagedServiceRelease(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("release paths must differ when host content changes: %q", first)
	}
	firstContent, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstContent) != "first host release" {
		t.Fatalf("first release was overwritten: %q", firstContent)
	}
	if repeated, err := prepareManagedServiceRelease(root, source); err != nil || repeated != second {
		t.Fatalf("same content release = %q, %v; want existing %q", repeated, err, second)
	}
}

func TestCopyManagedServiceFileIfPresentKeepsExistingState(t *testing.T) {
	source := filepath.Join(t.TempDir(), "staged-identity.json")
	destination := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(source, []byte("stale staging identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("persisted service identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyManagedServiceFileIfPresent(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "persisted service identity" {
		t.Fatalf("existing managed state was overwritten: %q", content)
	}
}

func TestRunManagedServiceWithRetryRetriesTransientStartupFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	attempts := 0
	err := runManagedServiceWithRetry(ctx, time.Millisecond, func(context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("gateway unavailable")
		}
		cancel()
		return context.Canceled
	})
	if err != nil {
		t.Fatalf("runManagedServiceWithRetry() error = %v, want nil", err)
	}
	if attempts != 2 {
		t.Fatalf("run attempts = %d, want 2", attempts)
	}
}
