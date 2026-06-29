package bootstrap_test

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsTemporaryBootstrapWiresReleaseVerifier(t *testing.T) {
	content, err := os.ReadFile("windows-temporary.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	required := []string{
		"$ReleaseManifestUrl",
		"$ReleaseBundleUrl",
		"$ReleaseBundleRequiredArtifacts",
		"$ReleaseRootPublicKey",
		"$VerifierDownloadUrl",
		"$VerifierExpectedSha256",
		"Assert-ReleaseSignature",
		"Assert-ReleaseBundle",
		"--root-public-key",
		"--bundle",
		"rdev-verify.exe",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected bootstrap script to contain %q", needle)
		}
	}
	verifierHash := strings.Index(script, "Assert-Sha256 -Path $verifierExe")
	releaseVerify := strings.Index(script, "Assert-ReleaseSignature -VerifierExe $verifierExe")
	if verifierHash < 0 || releaseVerify < 0 {
		t.Fatal("expected verifier hash and release signature steps")
	}
	if verifierHash > releaseVerify {
		t.Fatal("verifier must be SHA256-pinned before it verifies the host artifact")
	}
	bundleVerify := strings.Index(script, "Assert-ReleaseBundle -VerifierExe $verifierExe")
	if bundleVerify < 0 {
		t.Fatal("expected release bundle verification step")
	}
	if verifierHash > bundleVerify {
		t.Fatal("verifier must be SHA256-pinned before it verifies the release bundle")
	}
}
