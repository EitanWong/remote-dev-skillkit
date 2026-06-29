package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	artifactPath := flag.String("artifact", "", "artifact path to verify")
	manifestPath := flag.String("manifest", "", "release manifest path")
	rootPublicKey := flag.String("root-public-key", "", "release trust root, formatted key_id:base64url_public_key")
	flag.Parse()

	if *artifactPath == "" {
		return fmt.Errorf("artifact is required")
	}
	if *manifestPath == "" {
		return fmt.Errorf("manifest is required")
	}
	root, err := trustref.Parse(*rootPublicKey)
	if err != nil {
		return err
	}
	manifest, err := release.ReadManifest(*manifestPath)
	if err != nil {
		return err
	}
	if err := manifest.VerifyArtifact(*artifactPath, root); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":       true,
		"artifact": *artifactPath,
		"manifest": *manifestPath,
		"sha256":   manifest.ArtifactSHA256,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
