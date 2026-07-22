//go:build rdev_bootstrap_focused

package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const focusedManifestJSON = `{"schema_version":"rdev.layered-assets.v1","version":"v2-test","generated_at":"2026-07-17T08:00:00Z","expires_at":"2026-07-17T09:00:00Z","signing_key_id":"release-root","assets":[{"id":"windows-core","platform":"windows/amd64","kind":"core-runtime","relative_path":"rdev-core.exe","sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":4}],"signature":"AA"}`

func TestFocusedLayeredManifestDecodeIsStrict(t *testing.T) {
	manifest, err := DecodeLayeredAssetManifest([]byte(focusedManifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v2-test" || len(manifest.Assets) != 1 || manifest.Assets[0].SizeBytes != 4 {
		t.Fatalf("unexpected focused manifest: %#v", manifest)
	}
	for _, content := range []string{
		strings.Replace(focusedManifestJSON, `"version":"v2-test"`, `"version":"v2-test","version":"duplicate"`, 1),
		strings.Replace(focusedManifestJSON, `"signature":"AA"`, `"unknown":true,"signature":"AA"`, 1),
		focusedManifestJSON + ` {}`,
	} {
		if _, err := DecodeLayeredAssetManifest([]byte(content)); err == nil {
			t.Fatalf("focused decoder accepted malformed manifest: %s", content)
		}
	}
}

func TestFocusedLayeredManifestRejectsNonCanonicalTime(t *testing.T) {
	content := strings.Replace(focusedManifestJSON, "2026-07-17T08:00:00Z", "2026-07-17T09:00:00+01:00", 1)
	if _, err := DecodeLayeredAssetManifest([]byte(content)); err == nil {
		t.Fatal("focused decoder accepted a non-canonical UTC timestamp")
	}
}

func TestFocusedLayeredManifestBoundsAssetCount(t *testing.T) {
	assetStart := strings.Index(focusedManifestJSON, `{"id":`)
	assetEnd := strings.Index(focusedManifestJSON[assetStart:], `}],"signature"`)
	if assetStart < 0 || assetEnd < 0 {
		t.Fatal("focused fixture asset not found")
	}
	asset := focusedManifestJSON[assetStart : assetStart+assetEnd+1]
	assets := strings.TrimSuffix(strings.Repeat(asset+",", maxFocusedLayeredAssets+1), ",")
	content := focusedManifestJSON[:assetStart] + assets + focusedManifestJSON[assetStart+assetEnd+1:]
	if _, err := DecodeLayeredAssetManifest([]byte(content)); err == nil {
		t.Fatal("focused decoder accepted an unbounded asset list")
	}
}

func TestFocusedLayeredManifestCanonicalEncoding(t *testing.T) {
	manifest, err := DecodeLayeredAssetManifest([]byte(focusedManifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = "ignored"
	encoded, err := canonicalUnsignedLayeredAssetManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(focusedManifestJSON, `"signature":"AA"`, `"signature":""`, 1)
	if string(encoded) != want {
		t.Fatalf("focused canonical encoding mismatch:\n got %s\nwant %s", encoded, want)
	}
	if manifest.GeneratedAt != time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected focused generated_at: %s", manifest.GeneratedAt)
	}
}

func TestFocusedLayeredManifestRootVerification(t *testing.T) {
	manifest, err := DecodeLayeredAssetManifest([]byte(focusedManifestJSON))
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	canonical, err := canonicalUnsignedLayeredAssetManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, canonical))
	root, err := ParseLayeredTrustRoot("release-root:" + base64.RawURLEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyLayeredAssetManifestRoot(manifest, root, time.Date(2026, 7, 17, 8, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}
