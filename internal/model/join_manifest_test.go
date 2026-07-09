package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func TestJoinManifestSignsAndVerifies(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewJoinManifest(ticket, JoinManifestSpec{
		GatewayURL:   "http://127.0.0.1:8787",
		JoinURL:      "http://127.0.0.1:8787/join/" + ticket.Code,
		Trust:        NewTrustBundle("test-key", publicKey),
		SigningKeyID: "test-key",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(now.Add(time.Second)); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
	if manifest.TrustFingerprint == "" {
		t.Fatal("trust fingerprint should be set")
	}
	if manifest.PackageCatalog.SchemaVersion != ConnectionEntryPackageCatalogSchemaVersion {
		t.Fatalf("expected package catalog schema, got %#v", manifest.PackageCatalog)
	}
	if len(manifest.PackageCatalog.Candidates) == 0 {
		t.Fatalf("expected package catalog candidates, got %#v", manifest.PackageCatalog)
	}
	if manifest.PackageCatalog.Candidates[0].FallbackScriptURL == "" {
		t.Fatalf("expected fallback script URL, got %#v", manifest.PackageCatalog.Candidates[0])
	}
}

func TestJoinManifestSignsGatewayCandidates(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewJoinManifest(ticket, JoinManifestSpec{
		GatewayURL: "http://192.0.2.10:8787",
		GatewayCandidates: []JoinManifestGatewayCandidate{
			{URL: "https://relay.example.test/rdev", Kind: "relay", Scope: "configured-relay", Recommended: true},
			{URL: "http://192.0.2.10:8787", Kind: "lan-private", Scope: "same-lan"},
		},
		Trust:        NewTrustBundle("test-key", publicKey),
		SigningKeyID: "test-key",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(now); err != nil {
		t.Fatalf("expected manifest to verify: %v", err)
	}
	if len(manifest.GatewayCandidates) != 2 || manifest.GatewayCandidates[0].Kind != "relay" {
		t.Fatalf("expected signed relay candidate first, got %#v", manifest.GatewayCandidates)
	}
	manifest.GatewayCandidates[0].URL = "https://evil.example.test"
	if err := manifest.Verify(now); !errors.Is(err, ErrJoinManifestSignature) {
		t.Fatalf("expected candidate tampering to fail signature, got %v", err)
	}
}

func TestJoinManifestVerifiesWithSeparateTrustRoot(t *testing.T) {
	gatewayPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPublicKey, rootPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongRootPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewJoinManifest(ticket, JoinManifestSpec{
		GatewayURL:   "http://127.0.0.1:8787",
		Trust:        NewTrustBundle("gateway-tasks", gatewayPublicKey),
		SigningKeyID: "release-root",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(rootPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.VerifyWithRoot(NewTrustBundle("release-root", rootPublicKey), now); err != nil {
		t.Fatalf("expected manifest to verify with release root: %v", err)
	}
	if err := manifest.Verify(now); !errors.Is(err, ErrJoinManifestInvalid) {
		t.Fatalf("expected embedded gateway trust not to verify release-root manifest, got %v", err)
	}
	if err := manifest.VerifyWithRoot(NewTrustBundle("release-root", wrongRootPublicKey), now); !errors.Is(err, ErrJoinManifestSignature) {
		t.Fatalf("expected wrong release root to fail, got %v", err)
	}
}

func TestJoinManifestRejectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewJoinManifest(ticket, JoinManifestSpec{
		GatewayURL:   "http://127.0.0.1:8787",
		JoinURL:      "http://127.0.0.1:8787/join/" + ticket.Code,
		Trust:        NewTrustBundle("test-key", publicKey),
		SigningKeyID: "test-key",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest.GatewayURL = "http://evil.example"
	if err := manifest.Verify(now); !errors.Is(err, ErrJoinManifestSignature) {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestJoinManifestRejectsPackageCatalogTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewJoinManifest(ticket, JoinManifestSpec{
		GatewayURL:   "http://127.0.0.1:8787",
		JoinURL:      "http://127.0.0.1:8787/join/" + ticket.Code,
		Trust:        NewTrustBundle("test-key", publicKey),
		SigningKeyID: "test-key",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest.PackageCatalog.Candidates[0].PackageStatus = "available"
	if err := manifest.Verify(now); !errors.Is(err, ErrJoinManifestSignature) {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestJoinManifestRejectsExpired(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 60, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewJoinManifest(ticket, JoinManifestSpec{
		GatewayURL:   "http://127.0.0.1:8787",
		Trust:        NewTrustBundle("test-key", publicKey),
		SigningKeyID: "test-key",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifest.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(now.Add(61 * time.Second)); !errors.Is(err, ErrJoinManifestExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}
}
