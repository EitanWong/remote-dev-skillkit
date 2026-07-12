package main

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostcmd"
)

func TestRdevHostEntrypointDoesNotImportFullCLI(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if path == "github.com/EitanWong/remote-dev-skillkit/internal/cli" {
			t.Fatalf("rdev-host entrypoint must not import the full CLI package; use a host-only command package to keep target helpers small")
		}
	}
}

func TestRdevHostEntrypointMapsPermanentJoinFailureExitCode(t *testing.T) {
	err := hostcmd.NewJoinSessionResponseError(
		404,
		"404 Not Found",
		[]byte(`{"error":{"schema_version":"rdev.error.v1","code":"invalid_join_code","message":"join code is invalid","recoverable":false,"retry_after_ms":0,"user_summary":"The support-session entry is invalid or no longer active.","agent_next_action":"create a fresh support-session entry"}}`),
		nil,
	)
	if got := commandExitCode(err); got != hostcmd.PermanentJoinFailureExitCode {
		t.Fatalf("commandExitCode() = %d, want %d", got, hostcmd.PermanentJoinFailureExitCode)
	}
}
