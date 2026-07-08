package desktopadapter

import (
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeActionAliases(t *testing.T) {
	cases := map[string]string{
		"windows":    "window.inspect",
		"screenshot": "screen.screenshot",
		"record":     "screen.record",
		"keyboard":   "input.keyboard",
		"mouse":      "input.mouse",
		"launch":     "app.launch",
		"open_url":   "url.open",
	}
	for input, want := range cases {
		if got := NormalizeAction(input); got != want {
			t.Fatalf("NormalizeAction(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExecuteFailClosedWhenNativeDesktopUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows backend has native desktop integration tests outside this package")
	}
	result, err := Execute(Spec{Action: "screen.screenshot"})
	if err == nil {
		t.Fatal("expected non-Windows desktop backend to fail closed")
	}
	if result.DesktopSessionState != "desktop_session_unavailable" {
		t.Fatalf("expected explicit unavailable state, got %#v", result)
	}
	if !strings.Contains(err.Error(), "desktop_session_unavailable") {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}
