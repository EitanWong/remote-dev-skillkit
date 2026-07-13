package desktopadapter

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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

func TestNormalizeWindowQueryStripsExecutableSuffix(t *testing.T) {
	if got := normalizeWindowQuery("  Notepad.exe  "); got != "notepad" {
		t.Fatalf("normalizeWindowQuery() = %q, want notepad", got)
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
	if !strings.Contains(result.ArtifactContent(), "native desktop backend is not available") {
		t.Fatalf("desktop error detail was not preserved in artifact: %s", result.ArtifactContent())
	}
}

func TestEncodePNGWithinBudgetKeepsValidPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1024, 768))
	for y := 0; y < 768; y++ {
		for x := 0; x < 1024; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	pngBytes, err := encodePNGWithinBudget(img, 12000)
	if err != nil {
		t.Fatal(err)
	}
	if len(pngBytes) > 12000 {
		t.Fatalf("PNG size %d exceeds budget", len(pngBytes))
	}
	decoded, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("bounded PNG is invalid: %v", err)
	}
	if decoded.Bounds().Dx() == 0 || decoded.Bounds().Dy() == 0 {
		t.Fatalf("bounded PNG has empty dimensions: %v", decoded.Bounds())
	}
}

func TestPersistDesktopArtifactReturnsRelativeMetadata(t *testing.T) {
	root := t.TempDir()
	artifact, err := persistDesktopArtifact(root, ".rdev/desktop-artifacts/capture.png", "image/png", []byte("png-data"))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Path != ".rdev/desktop-artifacts/capture.png" || artifact.ContentType != "image/png" || artifact.Bytes != 8 || artifact.SHA256 == "" {
		t.Fatalf("unexpected artifact metadata: %#v", artifact)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifact.Path)))
	if err != nil || string(data) != "png-data" {
		t.Fatalf("expected persisted artifact, data=%q err=%v", data, err)
	}
}

func TestPersistDesktopArtifactRejectsPathEscape(t *testing.T) {
	if _, err := persistDesktopArtifact(t.TempDir(), "../capture.png", "image/png", []byte("x")); err == nil {
		t.Fatal("expected artifact path escape to be rejected")
	}
}
