package agentinvite

import (
	"strings"
	"testing"
)

func TestHostServeCommandUsesContinuousTaskLoop(t *testing.T) {
	command := hostServeCommand("rdev", "https://gateway.test/manifest", "manifest-dev:key", "auto", false)
	if !strings.Contains(command, "--once=false") {
		t.Fatalf("expected continuous host command, got %q", command)
	}
	if !strings.Contains(command, "--max-tasks 0") {
		t.Fatalf("expected unlimited task loop, got %q", command)
	}
}
