package main

import (
	"reflect"
	"testing"
)

func TestHostArgsDefaultsToHostServeForFlags(t *testing.T) {
	got := hostArgs([]string{"--mode", "temporary"})
	want := []string{"host", "serve", "--mode", "temporary"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hostArgs() = %#v, want %#v", got, want)
	}
}

func TestHostArgsPreservesExplicitHostCommand(t *testing.T) {
	got := hostArgs([]string{"host", "serve", "--mode", "temporary"})
	want := []string{"host", "serve", "--mode", "temporary"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hostArgs() = %#v, want %#v", got, want)
	}
}

func TestHostArgsMapsHostSubcommands(t *testing.T) {
	got := hostArgs([]string{"service-status", "--platform", "macos"})
	want := []string{"host", "service-status", "--platform", "macos"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hostArgs() = %#v, want %#v", got, want)
	}
}
