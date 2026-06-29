package trustref

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestEncodeParseRoundTrip(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ref := Encode("release-root", publicKey)
	bundle, err := Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.SigningKeyID != "release-root" {
		t.Fatalf("unexpected key id %q", bundle.SigningKeyID)
	}
	decoded, err := bundle.Ed25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(publicKey) {
		t.Fatal("decoded public key should match")
	}
}

func TestParseRejectsMalformedRef(t *testing.T) {
	if _, err := Parse("not-a-ref"); err == nil {
		t.Fatal("expected malformed ref to fail")
	}
}
