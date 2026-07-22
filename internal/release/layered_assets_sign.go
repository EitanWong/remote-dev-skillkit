//go:build !rdev_bootstrap_focused

package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func SignLayeredAssetManifest(manifest LayeredAssetManifest, key signing.Key) (LayeredAssetManifest, error) {
	if err := validateLayeredAssetSigningKey(key); err != nil {
		return LayeredAssetManifest{}, err
	}

	signed := cloneLayeredAssetManifest(manifest)
	signed.SigningKeyID = key.ID
	signed.Signature = ""
	if err := validateLayeredAssetManifest(signed); err != nil {
		return LayeredAssetManifest{}, err
	}
	message, err := canonicalUnsignedLayeredAssetManifest(signed)
	if err != nil {
		return LayeredAssetManifest{}, err
	}
	signed.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key.PrivateKey, message))
	return signed, nil
}

func validateLayeredAssetSigningKey(key signing.Key) error {
	if strings.TrimSpace(key.ID) == "" || len(key.PublicKey) != ed25519.PublicKeySize || len(key.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("valid Ed25519 release signing key is required")
	}
	derived, ok := key.PrivateKey.Public().(ed25519.PublicKey)
	if !ok || !derived.Equal(key.PublicKey) {
		return fmt.Errorf("valid Ed25519 release signing key is required")
	}
	return nil
}
