package scripts_test

import (
	"os"
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
