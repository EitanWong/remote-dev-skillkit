package service

import (
	"fmt"
	"strings"
)

func appendReleaseGateArgs(args *[]string, bundlePath, rootPublicKey string, requiredArtifacts []string) {
	if strings.TrimSpace(bundlePath) == "" {
		return
	}
	*args = append(*args, "--release-bundle", bundlePath, "--release-root-public-key", rootPublicKey)
	required := cleanRequiredArtifacts(requiredArtifacts)
	if len(required) > 0 {
		*args = append(*args, "--release-require-artifacts", strings.Join(required, ","))
	}
}

func cleanRequiredArtifacts(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validateReleaseGateOptions(bundlePath, rootPublicKey string, requiredArtifacts []string) error {
	if strings.TrimSpace(bundlePath) == "" {
		if strings.TrimSpace(rootPublicKey) != "" || len(cleanRequiredArtifacts(requiredArtifacts)) > 0 {
			return fmt.Errorf("release bundle is required when release verification options are provided")
		}
		return nil
	}
	if strings.TrimSpace(rootPublicKey) == "" {
		return fmt.Errorf("release root public key is required with release bundle")
	}
	return nil
}
