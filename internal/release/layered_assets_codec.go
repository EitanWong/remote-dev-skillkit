//go:build !rdev_bootstrap_focused

package release

import (
	"encoding/json"
	"sort"
)

func canonicalUnsignedLayeredAssetManifest(manifest LayeredAssetManifest) ([]byte, error) {
	canonical := cloneLayeredAssetManifest(manifest)
	canonical.Signature = ""
	sort.Slice(canonical.Assets, func(i, j int) bool {
		return canonical.Assets[i].ID < canonical.Assets[j].ID
	})
	for index := range canonical.Assets {
		sort.Strings(canonical.Assets[index].Capabilities)
	}
	return json.Marshal(canonical)
}
