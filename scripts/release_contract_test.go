package scripts_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionentry"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

const maxReleaseWindowsLayeredHandoffBytes int64 = 1 << 20

func TestReleaseBuildDefaultsIncludeBootstrap(t *testing.T) {
	script := readReleaseScriptForTest(t, "release/build-artifacts.sh")
	defaultCommands := shellAssignmentForTest(t, script, "commands")
	for _, command := range strings.Split(defaultCommands, ",") {
		if strings.TrimSpace(command) == "rdev-bootstrap" {
			return
		}
	}
	t.Fatalf("default release commands omit rdev-bootstrap: %q", defaultCommands)
}

func TestReleaseBootstrapUsesOnlyDocumentedSizeFlags(t *testing.T) {
	script := readReleaseScriptForTest(t, "release/build-artifacts.sh")
	for _, required := range []string{
		"-trimpath",
		"-gcflags=all=-l",
		"-buildid=",
		"-tags=rdev_bootstrap_focused",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("focused bootstrap build is missing documented flag %q", required)
		}
	}
	for _, undocumented := range []string{"-funcalign=1", "rdev_bootstrap_focused,purego"} {
		if strings.Contains(script, undocumented) {
			t.Errorf("focused bootstrap build uses undocumented tuning %q", undocumented)
		}
	}
}

func TestReleaseWindowsBootstrapFocusedDependencyBoundary(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "-deps", "-tags=rdev_bootstrap_focused", "./cmd/rdev-bootstrap")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("list focused Windows bootstrap dependencies: %v", err)
	}
	dependencies := make(map[string]bool)
	for _, dependency := range strings.Fields(string(output)) {
		dependencies[dependency] = true
	}
	for _, forbidden := range []string{
		"net/http",
		"crypto/tls",
		"encoding/json",
		"golang.org/x/sys/windows",
		"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd",
		"github.com/EitanWong/remote-dev-skillkit/internal/model",
		"github.com/EitanWong/remote-dev-skillkit/internal/trustref",
	} {
		if dependencies[forbidden] {
			t.Errorf("focused Windows bootstrap linked forbidden dependency %q", forbidden)
		}
	}
}

func TestReleaseBuildLimitsBootstrapToWindowsAMD64(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeGo := `#!/bin/sh
set -eu
if [ "$1" = "env" ]; then
  case "$2" in
    GOOS) printf '%s\n' darwin ;;
    GOARCH) printf '%s\n' amd64 ;;
    *) exit 2 ;;
  esac
  exit 0
fi
if [ "$1" = "build" ]; then
  shift
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then
      printf '%s' fake-binary > "$2"
      exit 0
    fi
    shift
  done
fi
exit 2
`
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	cmd := exec.Command("bash", "scripts/release/build-artifacts.sh",
		"--out", outDir,
		"--targets", "linux/amd64,windows/amd64",
		"--commands", "rdev-bootstrap",
	)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-artifacts failed: %v\n%s", err, output)
	}
	content, err := os.ReadFile(filepath.Join(outDir, "build-artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Artifacts []struct {
			Command string `json:"command"`
			Target  string `json:"target"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Command != "rdev-bootstrap" || manifest.Artifacts[0].Target != "windows/amd64" {
		t.Fatalf("rdev-bootstrap must be emitted only for windows/amd64: %+v", manifest.Artifacts)
	}
}

func TestReleaseWindowsLayeredHandoffSize(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	cmd := exec.Command("bash", "scripts/release/build-artifacts.sh",
		"--out", outDir,
		"--version", "v2.0.0-size-test",
		"--targets", "windows/amd64",
		"--commands", "rdev-bootstrap",
		"--clean",
	)
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cross-build focused Windows bootstrap: %v\n%s", err, output)
	}
	bootstrapPath := filepath.Join(outDir, "windows-amd64", "rdev-bootstrap.exe")
	bootstrapInfo, err := os.Stat(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := signing.Generate("release-root-size-test")
	if err != nil {
		t.Fatal(err)
	}
	generatedAt := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	manifest, err := release.SignArtifactForRelease(bootstrapPath, key, generatedAt, "v2.0.0-size-test", "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(outDir, "rdev-bootstrap.exe.rdev-release.json")
	if err := release.WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	ticket := model.Ticket{
		ID: "tkt_size", Code: "SIZE-TEST", Mode: model.HostModeAttendedTemporary,
		Status: model.TicketStatusActive, TTLSeconds: 600, Capabilities: []string{"shell.user"},
		Reason: "release size gate", CreatedAt: generatedAt, ExpiresAt: generatedAt.Add(10 * time.Minute),
	}
	invite, err := agentinvite.New(agentinvite.Options{
		GatewayURL:            "https://gateway.example.test/v1",
		JoinURL:               "https://gateway.example.test/join/SIZE-TEST",
		ManifestURL:           "https://gateway.example.test/v1/tickets/SIZE-TEST/manifest",
		ManifestRootPublicKey: "manifest-root:" + strings.Repeat("d", 43),
		Ticket:                ticket,
		Transport:             "auto",
		NetworkScope:          "auto",
		AuthorityProfile:      "max-control",
		CreatedAt:             generatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	inviteJSON, err := json.Marshal(invite)
	if err != nil {
		t.Fatal(err)
	}
	entryDir := filepath.Join(outDir, "entry")
	rootPublicKey := key.ID + ":" + base64.RawURLEncoding.EncodeToString(key.PublicKey)
	plan, err := connectionentry.FromInvite(connectionentry.Options{
		InviteJSON:                          string(inviteJSON),
		OutDir:                              entryDir,
		TargetOS:                            "windows",
		TargetArch:                          "amd64",
		Ownership:                           "third-party",
		SessionMode:                         string(model.HostModeAttendedTemporary),
		WindowsBootstrapBinaryPath:          bootstrapPath,
		WindowsBootstrapReleaseManifestPath: manifestPath,
		LayeredAssetsManifestURL:            "https://downloads.example.test/layered-assets.json",
		LayeredReleaseVersion:               "v2.0.0-size-test",
		WindowsBootstrapScriptPath:          filepath.Join(repoRoot, "scripts", "bootstrap", "windows-temporary.ps1"),
		WindowsHostDownloadURL:              "https://downloads.example.test/rdev-host.exe",
		WindowsHostExpectedSHA256:           strings.Repeat("a", 64),
		ReleaseBundleURL:                    "https://downloads.example.test/release-bundle.json",
		ReleaseRootPublicKey:                rootPublicKey,
		WindowsVerifierDownloadURL:          "https://downloads.example.test/rdev-verify.exe",
		WindowsVerifierExpectedSHA256:       strings.Repeat("b", 64),
		Now:                                 generatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(entryDir, "Windows-ConnectionEntry.zip")
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	closedSize := archiveInfo.Size()
	t.Logf("Windows focused bootstrap PE size_bytes=%d; closed representative ZIP size_bytes=%d", bootstrapInfo.Size(), closedSize)
	if closedSize > maxReleaseWindowsLayeredHandoffBytes {
		t.Fatalf("closed Windows-ConnectionEntry.zip size %d exceeds %d bytes (PE %d bytes)", closedSize, maxReleaseWindowsLayeredHandoffBytes, bootstrapInfo.Size())
	}
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.ArchiveSizeBytes != closedSize {
		t.Fatalf("materialized archive report does not match closed file: %#v", plan.EntryPackagePlan)
	}
}

func TestPreparePlatformCandidatesForwardsTargetPlatform(t *testing.T) {
	script := readReleaseScriptForTest(t, "release/prepare-platform-candidates.sh")
	start := strings.Index(script, "go run ./cmd/rdev release prepare-candidate")
	if start < 0 {
		t.Fatal("prepare-platform-candidates script does not invoke release prepare-candidate")
	}
	invocation := script[start:]
	if end := strings.Index(invocation, ") > \"$prepare_json\""); end >= 0 {
		invocation = invocation[:end]
	}
	if !strings.Contains(invocation, "--target-platform \"$target\"") {
		t.Fatalf("release prepare-candidate invocation does not forward the selected target platform:\n%s", invocation)
	}
}

func readReleaseScriptForTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func shellAssignmentForTest(t *testing.T, script, name string) string {
	t.Helper()
	prefix := name + "="
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), "\"'")
		}
	}
	t.Fatalf("shell assignment %q not found", name)
	return ""
}
