package windowsentry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/release"
)

type managedCacheFile struct {
	path     string
	maxBytes int64
}

func validateOptionalPrivateCacheFile(path string, expectedSize int64) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return validatePrivateCacheFile(path, expectedSize)
}

func cachePaths(cacheDir string, asset release.LayeredAsset) (string, string, error) {
	rawRoot := strings.TrimSpace(cacheDir)
	if rawRoot == "" || strings.HasPrefix(rawRoot, `\\`) || strings.HasPrefix(rawRoot, "//") {
		return "", "", fmt.Errorf("layered cache must use a local path")
	}
	root, err := filepath.Abs(rawRoot)
	if err != nil {
		return "", "", err
	}
	digest := strings.TrimPrefix(asset.SHA256, "sha256:")
	name := filepath.Base(filepath.FromSlash(asset.RelativePath))
	if !validWindowsCacheBasename(name) {
		return "", "", fmt.Errorf("layered asset has an unsafe Windows filename")
	}
	runtimeDir := filepath.Join(root, "runtime", digest)
	contentDir := filepath.Join(root, "content")
	outputPath := filepath.Join(runtimeDir, name)
	contentPath := filepath.Join(contentDir, digest)
	if err := preparePrivateCache(
		root,
		[]string{root, filepath.Join(root, "runtime"), runtimeDir, contentDir},
		[]managedCacheFile{
			{path: outputPath, maxBytes: asset.SizeBytes},
			{path: outputPath + ".part", maxBytes: asset.SizeBytes},
			{path: outputPath + ".tmp", maxBytes: asset.SizeBytes},
			{path: contentPath, maxBytes: asset.SizeBytes},
			{path: contentPath + ".tmp", maxBytes: asset.SizeBytes},
		},
	); err != nil {
		return "", "", err
	}
	return outputPath, contentPath, nil
}

func validWindowsCacheBasename(name string) bool {
	if name == "" || len(name) > 128 || strings.TrimRight(name, ". ") != name {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._-", rune(character)) {
			continue
		}
		return false
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" {
		return false
	}
	return !(len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9')
}
