//go:build !rdev_bootstrap_focused

package release

import (
	"fmt"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func VerifyLayeredAssetManifest(manifest LayeredAssetManifest, root model.TrustBundle, now time.Time) error {
	if err := validateLayeredAssetManifest(manifest); err != nil {
		return err
	}
	publicKey, err := root.Ed25519PublicKey()
	if err != nil {
		return fmt.Errorf("%w: layered asset manifest trust root: %v", ErrManifestInvalid, err)
	}
	return verifyLayeredAssetManifest(manifest, LayeredTrustRoot{
		SigningKeyID: root.SigningKeyID,
		PublicKey:    publicKey,
	}, now)
}
