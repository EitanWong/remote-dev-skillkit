package codexadapter

import (
	"reflect"
	"testing"
)

func TestCodexArgvAddsIsolatedRuntimeProfileBeforeExecution(t *testing.T) {
	argv := codexArgv("/workspace", "implement", Spec{CodexProfile: "rdev", CodexCommand: "codex"})
	want := []string{"codex", "--profile", "rdev", "exec", "-C", "/workspace", "--sandbox", "workspace-write", "--json", "implement"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("codex argv = %#v, want %#v", argv, want)
	}
}
