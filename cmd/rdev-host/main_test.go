package main

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
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
