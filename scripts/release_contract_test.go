package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
