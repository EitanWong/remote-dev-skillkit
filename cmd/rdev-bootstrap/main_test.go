package main

import (
	"os"
	"strings"
	"testing"
)

func TestRdevBootstrapEntrypointAvoidsFullRuntimeImports(t *testing.T) {
	content, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{
		"internal/cli",
		"internal/hostcmd",
		"internal/hostrunner",
		"internal/mcpstdio",
		"internal/desktopadapter",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("rdev-bootstrap entrypoint must not import full runtime dependency %q:\n%s", forbidden, source)
		}
	}
	if !strings.Contains(source, "internal/bootstrapcmd") {
		t.Fatalf("rdev-bootstrap entrypoint should delegate to internal/bootstrapcmd:\n%s", source)
	}
	if !strings.Contains(source, "Stdin: os.Stdin") {
		t.Fatalf("rdev-bootstrap entrypoint should pass inherited stdin to foreground commands:\n%s", source)
	}
}
