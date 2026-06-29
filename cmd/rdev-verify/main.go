package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rdev-verify", flag.ContinueOnError)
	artifactPath := fs.String("artifact", "", "artifact path to verify")
	manifestPath := fs.String("manifest", "", "release manifest path")
	bundlePath := fs.String("bundle", "", "release bundle index path to verify")
	requiredArtifacts := fs.String("require-artifacts", "", "comma-separated artifact ids that must be present in the bundle")
	rootPublicKey := fs.String("root-public-key", "", "release trust root, formatted key_id:base64url_public_key")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := trustref.Parse(*rootPublicKey)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*bundlePath) != "" {
		if strings.TrimSpace(*artifactPath) != "" || strings.TrimSpace(*manifestPath) != "" {
			return fmt.Errorf("bundle verification cannot be combined with artifact or manifest verification")
		}
		return verifyBundle(*bundlePath, requiredArtifactList(*requiredArtifacts), root, stdout)
	}

	if *artifactPath == "" {
		return fmt.Errorf("artifact is required")
	}
	if *manifestPath == "" {
		return fmt.Errorf("manifest is required")
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
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func verifyBundle(bundlePath string, requiredArtifacts []string, root model.TrustBundle, stdout io.Writer) error {
	verification, err := release.VerifyBundle(bundlePath, root, requiredArtifacts)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":                  verification.OK(),
		"schema":              verification.SchemaVersion,
		"bundle":              verification.BundlePath,
		"root_key_id":         verification.RootKeyID,
		"checks":              verification.Checks,
		"artifacts":           verification.Artifacts,
		"recommended_actions": verification.RecommendedActions,
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if !verification.OK() {
		return fmt.Errorf("release bundle verification failed")
	}
	return nil
}

func requiredArtifactList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
