package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestTrustBundleEncodesPublicKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bundle := NewTrustBundle("test-key", publicKey)
	decoded, err := bundle.Ed25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(publicKey) {
		t.Fatal("decoded public key should match")
	}
}

func TestTrustBundleRejectsWrongAlgorithm(t *testing.T) {
	bundle := TrustBundle{SigningAlg: "rsa", PublicKey: "x"}
	if _, err := bundle.Ed25519PublicKey(); err == nil {
		t.Fatal("expected unsupported algorithm error")
	}
}
