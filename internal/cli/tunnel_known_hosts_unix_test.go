//go:build !windows

package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func protectKnownHostsTestFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestStartSSHTunnelPassesOnlySafeRelativeKnownHostsName(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	knownHostsDir := filepath.Join(root, "reviewed pins %h ${HOME}")
	if err := os.Mkdir(knownHostsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	knownHostsPath := filepath.Join(knownHostsDir, "known_hosts")
	if err := os.WriteFile(knownHostsPath, []byte("localhost.run ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sshPath := filepath.Join(binDir, "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" > \"$RDEV_TEST_SSH_CWD\"\nprintf '%s\\n' \"$@\" > \"$RDEV_TEST_SSH_ARGS\"\necho 'ready https://abc.lhr.life'\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cwdCapture := filepath.Join(root, "cwd.txt")
	argsCapture := filepath.Join(root, "args.txt")
	t.Setenv("PATH", binDir)
	t.Setenv("RDEV_TEST_SSH_CWD", cwdCapture)
	t.Setenv("RDEV_TEST_SSH_ARGS", argsCapture)

	started, err := startSSHTunnel(context.Background(), io.Discard, tunnel.ProviderLocalhostRun, sshTunnelSpec{
		Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:8787",
	}, knownHostsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer started.cancel()

	gotCWD, err := os.ReadFile(cwdCapture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(gotCWD)) != knownHostsDir {
		t.Fatalf("ssh working directory = %q, want %q", strings.TrimSpace(string(gotCWD)), knownHostsDir)
	}
	gotArgs, err := os.ReadFile(argsCapture)
	if err != nil {
		t.Fatal(err)
	}
	argsText := string(gotArgs)
	if !strings.Contains(argsText, "UserKnownHostsFile=known_hosts") || strings.Contains(argsText, knownHostsDir) {
		t.Fatalf("ssh argv did not isolate known_hosts path: %q", argsText)
	}
}

func TestStartSSHTunnelPinsRelativeSSHLookupBeforeChangingDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	knownHostsDir := filepath.Join(root, "pins")
	if err := os.Mkdir(knownHostsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	knownHostsPath := filepath.Join(knownHostsDir, "known_hosts")
	if err := os.WriteFile(knownHostsPath, []byte("localhost.run ssh-ed25519 dGVzdA==\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sshPath := filepath.Join(binDir, "ssh")
	if err := os.WriteFile(sshPath, []byte("#!/bin/sh\necho 'ready https://abc.lhr.life'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "bin")
	t.Setenv("GODEBUG", "execerrdot=0")

	started, err := startSSHTunnel(context.Background(), io.Discard, tunnel.ProviderLocalhostRun, sshTunnelSpec{
		Destination: "nokey@localhost.run", Port: 22, RemoteForward: "80:localhost:8787",
	}, knownHostsPath)
	if err != nil {
		t.Fatalf("relative PATH lookup was retargeted by SSH working directory: %v", err)
	}
	defer started.cancel()
}
