package release

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestLayeredAssetManifestSignsVerifiesAndSelectsWindowsCore(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}

	manifest := LayeredAssetManifest{
		SchemaVersion: LayeredAssetManifestSchemaVersion,
		Version:       "v0.2.0",
		GeneratedAt:   now,
		Assets: []LayeredAsset{
			{
				ID:           "rdev-host-windows-amd64",
				Platform:     "windows-amd64",
				Kind:         "core-runtime",
				RelativePath: "windows/amd64/rdev-host.exe",
				SHA256:       "sha256:" + strings.Repeat("a", 64),
				SizeBytes:    4096,
				Capabilities: []string{"shell", "file-transfer"},
			},
			{
				ID:           "rdev-verify-windows-amd64",
				Platform:     "windows-amd64",
				Kind:         "optional-helper",
				RelativePath: "windows/amd64/rdev-verify.exe",
				SHA256:       "sha256:" + strings.Repeat("b", 64),
				SizeBytes:    2048,
				Capabilities: []string{"verify"},
			},
		},
	}
	original := cloneLayeredAssetManifestForTest(manifest)

	signed, err := SignLayeredAssetManifest(manifest, key)
	if err != nil {
		t.Fatal(err)
	}
	if signed.SigningKeyID != key.ID {
		t.Fatalf("unexpected signing key id %q", signed.SigningKeyID)
	}
	if signed.Signature == "" {
		t.Fatal("signature should be present")
	}
	if !reflect.DeepEqual(manifest, original) {
		t.Fatal("signing mutated the input manifest")
	}

	root := model.NewTrustBundle(key.ID, key.PublicKey)
	signedBeforeVerify := cloneLayeredAssetManifestForTest(signed)
	if err := VerifyLayeredAssetManifest(signed, root, now); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
	if !reflect.DeepEqual(signed, signedBeforeVerify) {
		t.Fatal("verification mutated the signed manifest")
	}

	selected, err := SelectLayeredAsset(signed, "windows-amd64", "core-runtime", []string{"file-transfer"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "rdev-host-windows-amd64" {
		t.Fatalf("unexpected selected asset %q", selected.ID)
	}
	selected.Capabilities[0] = "mutated"
	if signed.Assets[0].Capabilities[0] == "mutated" {
		t.Fatal("selection returned capabilities aliased to the manifest")
	}

	reordered := cloneLayeredAssetManifestForTest(manifest)
	reordered.Assets[0], reordered.Assets[1] = reordered.Assets[1], reordered.Assets[0]
	reordered.Assets[1].Capabilities[0], reordered.Assets[1].Capabilities[1] =
		reordered.Assets[1].Capabilities[1], reordered.Assets[1].Capabilities[0]
	reorderedSigned, err := SignLayeredAssetManifest(reordered, key)
	if err != nil {
		t.Fatal(err)
	}
	if reorderedSigned.Signature != signed.Signature {
		t.Fatal("canonical signature changed with asset or capability order")
	}

	tests := []struct {
		name           string
		mutate         func(*LayeredAssetManifest)
		invalidSigOnly bool
	}{
		{
			name: "zero size",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].SizeBytes = 0
			},
		},
		{
			name: "non-sha256 digest",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].SHA256 = strings.Repeat("a", 64)
			},
		},
		{
			name: "non-canonical uppercase digest",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].SHA256 = "sha256:" + strings.Repeat("A", 64)
			},
		},
		{
			name: "duplicate id",
			mutate: func(candidate *LayeredAssetManifest) {
				duplicate := candidate.Assets[0]
				duplicate.Platform = "linux-amd64"
				duplicate.RelativePath = "linux/amd64/rdev-host"
				candidate.Assets = append(candidate.Assets, duplicate)
			},
		},
		{
			name: "unknown kind",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].Kind = "bootstrapper"
			},
		},
		{
			name: "empty version",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Version = ""
			},
		},
		{
			name: "absolute path",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "/tmp/rdev-host.exe"
			},
		},
		{
			name: "parent traversal",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "../rdev-host.exe"
			},
		},
		{
			name: "bare parent path",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = ".."
			},
		},
		{
			name: "percent-encoded traversal",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "windows/%2e%2e/rdev-host.exe"
			},
		},
		{
			name: "backslash",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = `windows\amd64/rdev-host.exe`
			},
		},
		{
			name: "query",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "windows/amd64/rdev-host.exe?download=1"
			},
		},
		{
			name: "empty query delimiter",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "windows/amd64/rdev-host.exe?"
			},
		},
		{
			name: "fragment",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "windows/amd64/rdev-host.exe#payload"
			},
		},
		{
			name: "empty fragment delimiter",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Assets[0].RelativePath = "windows/amd64/rdev-host.exe#"
			},
		},
		{
			name: "invalid signature",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Signature = strings.Repeat("A", len(candidate.Signature))
			},
			invalidSigOnly: true,
		},
		{
			name: "padded signature",
			mutate: func(candidate *LayeredAssetManifest) {
				candidate.Signature += "="
			},
			invalidSigOnly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.invalidSigOnly {
				unsigned := cloneLayeredAssetManifestForTest(manifest)
				tt.mutate(&unsigned)
				if _, err := SignLayeredAssetManifest(unsigned, key); err == nil {
					t.Fatal("expected signing to reject invalid manifest")
				}
			}

			candidate := cloneLayeredAssetManifestForTest(signed)
			tt.mutate(&candidate)
			if err := VerifyLayeredAssetManifest(candidate, root, now); err == nil {
				t.Fatal("expected verification to reject invalid manifest")
			}
		})
	}

	ambiguous := cloneLayeredAssetManifestForTest(signed)
	secondCore := ambiguous.Assets[0]
	secondCore.ID = "rdev-host-windows-amd64-alt"
	secondCore.RelativePath = "windows/amd64/rdev-host-alt.exe"
	ambiguous.Assets = append(ambiguous.Assets, secondCore)
	if _, err := SelectLayeredAsset(ambiguous, "windows-amd64", "core-runtime", []string{"file-transfer"}); err == nil {
		t.Fatal("expected ambiguous core-runtime selection to fail")
	}

	future := cloneLayeredAssetManifestForTest(manifest)
	future.GeneratedAt = now.Add(time.Second)
	futureSigned, err := SignLayeredAssetManifest(future, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyLayeredAssetManifest(futureSigned, root, now); err == nil {
		t.Fatal("expected future generated_at to fail verification")
	}
	if err := VerifyLayeredAssetManifest(signed, model.NewTrustBundle("other-root", key.PublicKey), now); err == nil {
		t.Fatal("expected trust root key id mismatch to fail verification")
	}

	invalidKey := key
	invalidKey.PrivateKey = invalidKey.PrivateKey[:len(invalidKey.PrivateKey)-1]
	if _, err := SignLayeredAssetManifest(manifest, invalidKey); err == nil {
		t.Fatal("expected invalid Ed25519 private key length to fail signing")
	}
	invalidKey = key
	invalidKey.PublicKey = invalidKey.PublicKey[:len(invalidKey.PublicKey)-1]
	if _, err := SignLayeredAssetManifest(manifest, invalidKey); err == nil {
		t.Fatal("expected invalid Ed25519 public key length to fail signing")
	}
}

func cloneLayeredAssetManifestForTest(manifest LayeredAssetManifest) LayeredAssetManifest {
	cloned := manifest
	cloned.Assets = make([]LayeredAsset, len(manifest.Assets))
	for index, asset := range manifest.Assets {
		cloned.Assets[index] = asset
		cloned.Assets[index].Capabilities = append([]string(nil), asset.Capabilities...)
	}
	return cloned
}
