package model

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

type TrustBundle struct {
	SigningKeyID string `json:"signing_key_id"`
	SigningAlg   string `json:"signing_alg"`
	PublicKey    string `json:"public_key"`
}

func NewTrustBundle(signingKeyID string, publicKey ed25519.PublicKey) TrustBundle {
	return TrustBundle{
		SigningKeyID: signingKeyID,
		SigningAlg:   JobEnvelopeSigningAlg,
		PublicKey:    base64.RawURLEncoding.EncodeToString(publicKey),
	}
}

func (b TrustBundle) Ed25519PublicKey() (ed25519.PublicKey, error) {
	if b.SigningAlg != JobEnvelopeSigningAlg {
		return nil, fmt.Errorf("unsupported signing algorithm %q", b.SigningAlg)
	}
	key, err := base64.RawURLEncoding.DecodeString(b.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length %d", len(key))
	}
	return ed25519.PublicKey(key), nil
}
