package model

import "testing"

func TestHostModeValid(t *testing.T) {
	for _, mode := range []HostMode{HostModeAttendedTemporary, HostModeManaged, HostModeBreakGlass} {
		if !mode.Valid() {
			t.Fatalf("expected valid mode %q", mode)
		}
	}
	if HostMode("unknown").Valid() {
		t.Fatal("unknown mode must be invalid")
	}
}
