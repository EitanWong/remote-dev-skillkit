package hostcap

import (
	"context"
	"runtime"
	"testing"
)

func TestDetectReturnsHostInventory(t *testing.T) {
	inventory := Detect(context.Background())
	if inventory.Name == "" {
		t.Fatal("host name must be set")
	}
	if inventory.OS != runtime.GOOS {
		t.Fatalf("expected OS %q, got %q", runtime.GOOS, inventory.OS)
	}
	if inventory.Arch != runtime.GOARCH {
		t.Fatalf("expected arch %q, got %q", runtime.GOARCH, inventory.Arch)
	}
	if len(inventory.TemporaryCapabilities) == 0 {
		t.Fatal("temporary capabilities must be populated")
	}
	if _, ok := inventory.Executables["git"]; !ok {
		t.Fatal("expected git executable status")
	}
	if _, ok := inventory.Executables["powershell"]; !ok {
		t.Fatal("expected powershell executable status")
	}
}
