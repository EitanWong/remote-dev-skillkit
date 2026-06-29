package main

import (
	"reflect"
	"testing"
)

func TestGatewayArgsDefaultsToGatewayServeForFlags(t *testing.T) {
	got := gatewayArgs([]string{"--dev", "--addr", "127.0.0.1:8787"})
	want := []string{"gateway", "serve", "--dev", "--addr", "127.0.0.1:8787"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gatewayArgs() = %#v, want %#v", got, want)
	}
}

func TestGatewayArgsMapsServeSubcommand(t *testing.T) {
	got := gatewayArgs([]string{"serve", "--dev"})
	want := []string{"gateway", "serve", "--dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gatewayArgs() = %#v, want %#v", got, want)
	}
}
