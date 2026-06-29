package trustref

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func Encode(keyID string, publicKey ed25519.PublicKey) string {
	return keyID + ":" + base64.RawURLEncoding.EncodeToString(publicKey)
}

func Parse(value string) (model.TrustBundle, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return model.TrustBundle{}, fmt.Errorf("trust root public key must be formatted key_id:base64url_public_key")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return model.TrustBundle{}, fmt.Errorf("decode trust root public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return model.TrustBundle{}, fmt.Errorf("invalid trust root public key length %d", len(publicKey))
	}
	return model.NewTrustBundle(parts[0], ed25519.PublicKey(publicKey)), nil
}
