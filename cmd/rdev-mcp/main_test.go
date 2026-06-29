package main

import (
	"reflect"
	"testing"
)

func TestMCPArgsDefaultsToServeForEmptyArgs(t *testing.T) {
	got := mcpArgs(nil)
	want := []string{"mcp", "serve"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mcpArgs() = %#v, want %#v", got, want)
	}
}

func TestMCPArgsMapsToolsSubcommand(t *testing.T) {
	got := mcpArgs([]string{"tools"})
	want := []string{"mcp", "tools"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mcpArgs() = %#v, want %#v", got, want)
	}
}
