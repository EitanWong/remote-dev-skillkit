package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func TestSignedTrustBundleSignsAndVerifies(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	rootPublic, rootPrivate := testKeyPair(t)
	gatewayPublic, _ := testKeyPair(t)

	bundle, err := NewSignedTrustBundle(SignedTrustBundleSpec{
		BundleID:     "managed-hosts",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "root",
		Keys: []TrustKey{
			NewTrustKey("root", rootPublic, TrustKeyStatusActive, now),
			NewTrustKey("gateway-1", gatewayPublic, TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = bundle.Sign(rootPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Verify(NewTrustBundle("root", rootPublic), now.Add(time.Minute)); err != nil {
		t.Fatalf("expected bundle to verify: %v", err)
	}
	trust, err := bundle.ActiveTrustBundle("gateway-1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if trust.SigningKeyID != "gateway-1" {
		t.Fatalf("unexpected key id %q", trust.SigningKeyID)
	}
}

func TestSignedTrustBundleVerifiesRotationUpdate(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	rootPublic, rootPrivate := testKeyPair(t)
	oldGatewayPublic, _ := testKeyPair(t)
	newGatewayPublic, _ := testKeyPair(t)

	first := signedBundle(t, SignedTrustBundleSpec{
		BundleID:     "managed-hosts",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(2 * time.Hour),
		SigningKeyID: "root",
		Keys: []TrustKey{
			NewTrustKey("root", rootPublic, TrustKeyStatusActive, now),
			NewTrustKey("gateway-old", oldGatewayPublic, TrustKeyStatusActive, now),
		},
	}, rootPrivate, now)
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	retiredOld := NewTrustKey("gateway-old", oldGatewayPublic, TrustKeyStatusRetired, now)
	retiredUntil := now.Add(24 * time.Hour)
	retiredOld.NotAfter = &retiredUntil
	second := signedBundle(t, SignedTrustBundleSpec{
		BundleID:           "managed-hosts",
		Sequence:           2,
		NotBefore:          now.Add(time.Minute),
		NotAfter:           now.Add(3 * time.Hour),
		PreviousBundleHash: firstHash,
		SigningKeyID:       "root",
		Keys: []TrustKey{
			NewTrustKey("root", rootPublic, TrustKeyStatusActive, now),
			retiredOld,
			NewTrustKey("gateway-new", newGatewayPublic, TrustKeyStatusActive, now.Add(time.Minute)),
		},
	}, rootPrivate, now.Add(time.Minute))

	if err := second.VerifyUpdate(first, NewTrustBundle("root", rootPublic), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("expected rotation update to verify: %v", err)
	}
	if _, err := second.ActiveTrustBundle("gateway-old", now.Add(2*time.Minute)); err == nil {
		t.Fatal("retired gateway key should not be active for new jobs")
	}
	if _, err := second.ActiveTrustBundle("gateway-new", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("new gateway key should be active: %v", err)
	}
}

func TestSignedTrustBundleRejectsRollbackAndWrongPreviousHash(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	rootPublic, rootPrivate := testKeyPair(t)
	gatewayPublic, _ := testKeyPair(t)
	first := signedBundle(t, SignedTrustBundleSpec{
		BundleID:     "managed-hosts",
		Sequence:     2,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "root",
		Keys: []TrustKey{
			NewTrustKey("root", rootPublic, TrustKeyStatusActive, now),
			NewTrustKey("gateway", gatewayPublic, TrustKeyStatusActive, now),
		},
	}, rootPrivate, now)
	rollback := signedBundle(t, SignedTrustBundleSpec{
		BundleID:           "managed-hosts",
		Sequence:           1,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: "sha256:wrong",
		SigningKeyID:       "root",
		Keys: []TrustKey{
			NewTrustKey("root", rootPublic, TrustKeyStatusActive, now),
			NewTrustKey("gateway", gatewayPublic, TrustKeyStatusActive, now),
		},
	}, rootPrivate, now)
	if err := rollback.VerifyUpdate(first, NewTrustBundle("root", rootPublic), now.Add(time.Minute)); err == nil {
		t.Fatal("expected rollback sequence to fail")
	}
	wrongHash := rollback
	wrongHash.Sequence = 3
	if err := wrongHash.VerifyUpdate(first, NewTrustBundle("root", rootPublic), now.Add(time.Minute)); err == nil {
		t.Fatal("expected wrong previous hash to fail")
	}
}

func TestSignedTrustBundleRejectsRevokedKeys(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	rootPublic, rootPrivate := testKeyPair(t)
	gatewayPublic, _ := testKeyPair(t)
	revokedAt := now.Add(time.Minute)
	revoked := NewTrustKey("gateway", gatewayPublic, TrustKeyStatusRevoked, now)
	revoked.RevokedAt = &revokedAt
	revoked.RevokedReason = "suspected compromise"

	bundle := signedBundle(t, SignedTrustBundleSpec{
		BundleID:     "managed-hosts",
		Sequence:     1,
		NotBefore:    now,
		NotAfter:     now.Add(time.Hour),
		SigningKeyID: "root",
		Keys: []TrustKey{
			NewTrustKey("root", rootPublic, TrustKeyStatusActive, now),
			revoked,
		},
	}, rootPrivate, now)
	if _, err := bundle.ActiveTrustBundle("gateway", now.Add(2*time.Minute)); !errors.Is(err, ErrTrustKeyRevoked) {
		t.Fatalf("expected revoked key error, got %v", err)
	}
}

func signedBundle(t *testing.T, spec SignedTrustBundleSpec, privateKey ed25519.PrivateKey, now time.Time) SignedTrustBundle {
	t.Helper()
	bundle, err := NewSignedTrustBundle(spec, now)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = bundle.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}
