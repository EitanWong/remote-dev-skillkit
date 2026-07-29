package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewayUpdatesSignedTrustBundleAndAudits(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewMemoryGatewayWithSigningKey(clock, "gateway-test", publicKey, privateKey)
	initial := gw.SignedTrustBundle()
	initialHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}

	nextUnsigned, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           "gateway-test-next",
		Sequence:           initial.Sequence + 1,
		PreviousBundleHash: initialHash,
		SigningKeyID:       "gateway-test",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-test", publicKey, model.TrustKeyStatusActive, now.Add(-time.Minute)),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err := nextUnsigned.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := gw.UpdateSignedTrustBundle(next)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Sequence != next.Sequence || gw.SignedTrustBundle().Sequence != next.Sequence {
		t.Fatalf("trust bundle sequence = %d, want %d", updated.Sequence, next.Sequence)
	}
	audit := gw.AuditEvents()
	if len(audit) != 1 || audit[0].Action != "trust_bundle.update" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMemoryGatewayRenewsSignedTrustBundle(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	gw := NewMemoryGatewayWithClock(clock)
	initial := gw.SignedTrustBundle()
	initialHash, err := initial.Hash()
	if err != nil {
		t.Fatal(err)
	}

	unchanged, renewed, err := gw.RenewSignedTrustBundle()
	if err != nil {
		t.Fatal(err)
	}
	if renewed || unchanged.Sequence != initial.Sequence {
		t.Fatalf("healthy bundle renewal = renewed:%t sequence:%d", renewed, unchanged.Sequence)
	}

	now = initial.NotAfter.Add(time.Second)
	next, renewed, err := gw.RenewSignedTrustBundle()
	if err != nil {
		t.Fatal(err)
	}
	if !renewed {
		t.Fatal("expired bundle was not renewed")
	}
	if next.Sequence != initial.Sequence+1 || next.PreviousBundleHash != initialHash {
		t.Fatalf("renewed bundle sequence/hash = %d/%q", next.Sequence, next.PreviousBundleHash)
	}
	if !next.NotAfter.After(now) {
		t.Fatalf("renewed bundle expired at %s", next.NotAfter)
	}
	if audit := gw.AuditEvents(); len(audit) != 1 || audit[0].Action != "trust_bundle.renew" {
		t.Fatalf("audit = %#v", audit)
	}
}
