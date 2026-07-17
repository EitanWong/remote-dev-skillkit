//go:build !windows && !rdev_bootstrap_focused

package windowsentry

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

func TestWindowsEntryRejectsCacheFileReplacedDuringDownload(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	transport := &recordingTransport{responses: map[string]transportFixture{
		fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
	}}
	commandCalled := false
	app := App{
		Transport: transport,
		Now:       fixture.now,
		download: func(_ context.Context, opts assetdownload.Options) (assetdownload.Result, error) {
			if err := os.WriteFile(opts.OutputPath, fixture.core, 0o600); err != nil {
				return assetdownload.Result{}, err
			}
			target := opts.CachePath + ".target"
			if err := os.WriteFile(target, fixture.core, 0o600); err != nil {
				return assetdownload.Result{}, err
			}
			if err := os.Symlink(target, opts.CachePath); err != nil {
				return assetdownload.Result{}, err
			}
			return assetdownload.Result{OutputPath: opts.OutputPath, Bytes: int64(len(fixture.core))}, nil
		},
		CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
			commandCalled = true
			return successfulTestCommand(ctx, path, args...)
		},
	}
	if err := app.Run(t.Context(), fixture.args(windowsEntryTestCacheDir(t))); err == nil {
		t.Fatal("cache file replacement during download was accepted")
	}
	if commandCalled {
		t.Fatal("runtime command was created after cache post-validation failed")
	}
}
