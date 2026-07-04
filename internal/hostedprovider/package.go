package hostedprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const PackageSchemaVersion = "rdev.hosted-provider-package.v1"
const VerificationSchemaVersion = "rdev.hosted-provider-package-verification.v1"

type Options struct {
	OutDir          string
	Name            string
	StorageProvider string
	AuthProvider    string
	GeneratedAt     time.Time
	Force           bool
}

type Package struct {
	SchemaVersion    string        `json:"schema_version"`
	Name             string        `json:"name"`
	GeneratedAt      time.Time     `json:"generated_at"`
	ExternalMutation bool          `json:"external_mutation"`
	ProductionClaim  string        `json:"production_claim"`
	Storage          Provider      `json:"storage"`
	Auth             Provider      `json:"auth"`
	GatewayArgs      []string      `json:"gateway_args"`
	Environment      []EnvVar      `json:"environment"`
	Files            []PackageFile `json:"files"`
	Checks           []Check       `json:"checks"`
	AgentRules       []string      `json:"agent_rules"`
}

type Provider struct {
	Kind             string   `json:"kind"`
	Schema           string   `json:"schema"`
	Implementation   string   `json:"implementation"`
	RuntimeStatus    string   `json:"runtime_status"`
	RequiredSettings []string `json:"required_settings"`
	VerifyCommand    string   `json:"verify_command"`
	Notes            []string `json:"notes,omitempty"`
}

type EnvVar struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Secret      bool   `json:"secret"`
}

type PackageFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Kind      string `json:"kind"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type Verification struct {
	SchemaVersion      string      `json:"schema_version"`
	PackagePath        string      `json:"package_path"`
	PackageDir         string      `json:"package_dir"`
	Name               string      `json:"name,omitempty"`
	StorageProvider    string      `json:"storage_provider,omitempty"`
	AuthProvider       string      `json:"auth_provider,omitempty"`
	Checks             []Check     `json:"checks"`
	Files              []FileCheck `json:"files"`
	RecommendedActions []string    `json:"recommended_actions,omitempty"`
}

type FileCheck struct {
	Path           string  `json:"path"`
	Kind           string  `json:"kind"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	ActualSHA256   string  `json:"actual_sha256,omitempty"`
	ExpectedSize   int64   `json:"expected_size"`
	ActualSize     int64   `json:"actual_size,omitempty"`
	Checks         []Check `json:"checks"`
}

func (p Package) OK() bool {
	if len(p.Checks) == 0 || len(p.Files) == 0 {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (v Verification) OK() bool {
	if len(v.Checks) == 0 || len(v.Files) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	for _, file := range v.Files {
		for _, check := range file.Checks {
			if !check.Passed {
				return false
			}
		}
	}
	return true
}

func Build(opts Options) (Package, error) {
	if strings.TrimSpace(opts.OutDir) == "" {
		return Package{}, fmt.Errorf("out is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return Package{}, err
	}
	if err := prepareOut(outDir, opts.Force); err != nil {
		return Package{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "rdev-hosted-provider"
	}
	storageProvider := normalizeProvider(opts.StorageProvider, "file")
	authProvider := normalizeProvider(opts.AuthProvider, "hosted-ed25519-jwt")

	pkg := Package{
		SchemaVersion:    PackageSchemaVersion,
		Name:             name,
		GeneratedAt:      now.UTC(),
		ExternalMutation: false,
		ProductionClaim:  "provider-package-surface-only",
		Storage:          storageDescriptor(storageProvider),
		Auth:             authDescriptor(authProvider),
		GatewayArgs:      gatewayArgs(storageProvider, authProvider),
		Environment:      environment(storageProvider, authProvider),
		AgentRules: []string{
			"Verify this package before using it to configure a hosted gateway.",
			"Do not place secrets, private endpoints, organization IDs, or local machine paths in this package.",
			"Ask the operator for one missing value at a time when provider credentials or deployment URLs are unclear.",
			"Treat external database, object storage, identity provider, DNS, cloud, and paid resource changes as approval-required.",
		},
	}
	pkg.Checks = packageChecks(pkg)

	files := []struct {
		path    string
		kind    string
		content []byte
	}{
		{"HOSTED_PROVIDER.md", "documentation", []byte(renderReadme(pkg))},
		{"gateway.env.example", "env-template", []byte(renderEnvTemplate(pkg))},
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(outDir, file.path), file.content, 0o644); err != nil {
			return Package{}, err
		}
	}
	for _, file := range files {
		entry, err := packageFile(outDir, file.path, file.kind)
		if err != nil {
			return Package{}, err
		}
		pkg.Files = append(pkg.Files, entry)
	}
	sort.Slice(pkg.Files, func(i, j int) bool { return pkg.Files[i].Path < pkg.Files[j].Path })
	manifest, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return Package{}, err
	}
	manifest = append(manifest, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "hosted-provider.json"), manifest, 0o644); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

func Verify(path string) (Verification, error) {
	manifestPath, dir, err := resolveManifest(path)
	if err != nil {
		return Verification{}, err
	}
	v := Verification{
		SchemaVersion: VerificationSchemaVersion,
		PackagePath:   "hosted-provider.json",
		PackageDir:    ".",
	}
	add := func(name string, passed bool, detail string) {
		v.Checks = append(v.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	content, err := os.ReadFile(manifestPath)
	add("manifest_exists", err == nil, "hosted-provider.json")
	if err != nil {
		v.RecommendedActions = failureActions()
		return v, nil
	}
	var pkg Package
	err = json.Unmarshal(content, &pkg)
	add("manifest_json_valid", err == nil, errorDetail(err))
	if err != nil {
		v.RecommendedActions = failureActions()
		return v, nil
	}
	v.Name = pkg.Name
	v.StorageProvider = pkg.Storage.Kind
	v.AuthProvider = pkg.Auth.Kind
	add("schema_version", pkg.SchemaVersion == PackageSchemaVersion, pkg.SchemaVersion)
	add("external_mutation_false", !pkg.ExternalMutation, fmt.Sprintf("%t", pkg.ExternalMutation))
	add("production_claim_is_scoped", pkg.ProductionClaim == "provider-package-surface-only", pkg.ProductionClaim)
	add("storage_provider_supported", supportedStorageProvider(pkg.Storage.Kind), pkg.Storage.Kind)
	add("auth_provider_supported", supportedAuthProvider(pkg.Auth.Kind), pkg.Auth.Kind)
	add("gateway_args_present", len(pkg.GatewayArgs) > 0, strings.Join(pkg.GatewayArgs, " "))
	add("environment_declared", len(pkg.Environment) > 0, fmt.Sprintf("%d", len(pkg.Environment)))
	add("agent_rules_present", len(pkg.AgentRules) >= 3, fmt.Sprintf("%d", len(pkg.AgentRules)))
	add("no_private_surface", noPrivateSurface(content), "manifest")
	for _, check := range packageChecks(pkg) {
		v.Checks = append(v.Checks, check)
	}
	v.Files = verifyFiles(dir, pkg.Files)
	if unlisted := unlistedFiles(dir, pkg.Files); len(unlisted) > 0 {
		add("no_unlisted_files", false, strings.Join(unlisted, ","))
	} else {
		add("no_unlisted_files", true, "")
	}
	if !v.OK() {
		v.RecommendedActions = failureActions()
	}
	return v, nil
}

func normalizeProvider(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func storageDescriptor(provider string) Provider {
	switch provider {
	case "file":
		return Provider{
			Kind:             "file",
			Schema:           "rdev.gateway-snapshot.v1",
			Implementation:   "built-in",
			RuntimeStatus:    "available-single-node",
			RequiredSettings: []string{"RDEV_GATEWAY_STORAGE_PATH"},
			VerifyCommand:    "rdev gateway storage verify --provider file --path <storage-path>",
			Notes:            []string{"Use for single-node self-hosted gateways and release smoke. It is not a multi-node hosted database."},
		}
	default:
		return Provider{
			Kind:             provider,
			Schema:           "external-provider-contract-required",
			Implementation:   "external-package",
			RuntimeStatus:    "not-bundled",
			RequiredSettings: []string{"provider endpoint", "provider credentials", "backup and retention policy"},
			VerifyCommand:    "rdev hosted-provider verify --package <provider-package>",
			Notes:            []string{"External provider packages must pass this verifier before gateway deployment."},
		}
	}
}

func authDescriptor(provider string) Provider {
	switch provider {
	case "hosted-ed25519-jwt":
		return Provider{
			Kind:             "hosted-ed25519-jwt",
			Schema:           "rdev.hosted-operator-auth.v1",
			Implementation:   "built-in",
			RuntimeStatus:    "available",
			RequiredSettings: []string{"RDEV_HOSTED_OPERATOR_AUTH_FILE"},
			VerifyCommand:    "rdev operator-auth verify-hosted --auth <hosted-auth-file>",
			Notes:            []string{"Provider-neutral EdDSA JWT verification for issuer, audience, key id, expiry, not-before, and role claims."},
		}
	default:
		return Provider{
			Kind:             provider,
			Schema:           "external-auth-contract-required",
			Implementation:   "external-package",
			RuntimeStatus:    "not-bundled",
			RequiredSettings: []string{"issuer", "audience", "keys or JWKS", "role mapping"},
			VerifyCommand:    "rdev hosted-provider verify --package <provider-package>",
			Notes:            []string{"External auth packages must keep secret material outside the package manifest."},
		}
	}
}

func gatewayArgs(storageProvider, authProvider string) []string {
	if storageProvider != "file" || authProvider != "hosted-ed25519-jwt" {
		return []string{"external-provider-runtime", "must-supply-reviewed-gateway-launcher-after-package-verification"}
	}
	args := []string{"rdev", "gateway", "serve", "--storage-provider", storageProvider, "--storage-path", "${RDEV_GATEWAY_STORAGE_PATH}"}
	if authProvider == "hosted-ed25519-jwt" {
		args = append(args, "--hosted-operator-auth", "${RDEV_HOSTED_OPERATOR_AUTH_FILE}")
	}
	return args
}

func environment(storageProvider, authProvider string) []EnvVar {
	var env []EnvVar
	if storageProvider == "file" {
		env = append(env, EnvVar{Name: "RDEV_GATEWAY_STORAGE_PATH", Required: true, Description: "Path to the gateway snapshot JSON file for the built-in file provider.", Secret: false})
	} else {
		env = append(env, EnvVar{Name: "RDEV_HOSTED_STORAGE_CONFIG", Required: true, Description: "Path to the reviewed external hosted storage provider configuration.", Secret: false})
		env = append(env, EnvVar{Name: "RDEV_HOSTED_STORAGE_SECRET_REF", Required: true, Description: "Reference to provider credentials in an operator-approved secret manager.", Secret: true})
	}
	if authProvider == "hosted-ed25519-jwt" {
		env = append(env, EnvVar{Name: "RDEV_HOSTED_OPERATOR_AUTH_FILE", Required: true, Description: "Path to rdev.hosted-operator-auth.v1 public-key verifier config.", Secret: false})
	} else {
		env = append(env, EnvVar{Name: "RDEV_HOSTED_AUTH_CONFIG", Required: true, Description: "Path to the reviewed external hosted auth provider configuration.", Secret: false})
	}
	return env
}

func packageChecks(pkg Package) []Check {
	return []Check{
		{Name: "schema_version", Passed: pkg.SchemaVersion == PackageSchemaVersion, Detail: pkg.SchemaVersion},
		{Name: "external_mutation_false", Passed: !pkg.ExternalMutation, Detail: fmt.Sprintf("%t", pkg.ExternalMutation)},
		{Name: "production_claim_scoped", Passed: pkg.ProductionClaim == "provider-package-surface-only", Detail: pkg.ProductionClaim},
		{Name: "storage_provider_declared", Passed: strings.TrimSpace(pkg.Storage.Kind) != "", Detail: pkg.Storage.Kind},
		{Name: "auth_provider_declared", Passed: strings.TrimSpace(pkg.Auth.Kind) != "", Detail: pkg.Auth.Kind},
		{Name: "storage_provider_supported", Passed: supportedStorageProvider(pkg.Storage.Kind), Detail: pkg.Storage.Kind},
		{Name: "auth_provider_supported", Passed: supportedAuthProvider(pkg.Auth.Kind), Detail: pkg.Auth.Kind},
		{Name: "storage_verify_command_declared", Passed: strings.TrimSpace(pkg.Storage.VerifyCommand) != "", Detail: pkg.Storage.VerifyCommand},
		{Name: "auth_verify_command_declared", Passed: strings.TrimSpace(pkg.Auth.VerifyCommand) != "", Detail: pkg.Auth.VerifyCommand},
		{Name: "environment_declared", Passed: len(pkg.Environment) > 0, Detail: fmt.Sprintf("%d", len(pkg.Environment))},
		{Name: "no_secret_environment_values", Passed: envHasNoSecretValues(pkg.Environment), Detail: fmt.Sprintf("%d", len(pkg.Environment))},
	}
}

func prepareOut(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 && !force {
			return fmt.Errorf("output directory must be empty: %s", dir)
		}
		if force {
			for _, entry := range entries {
				if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func packageFile(root, path, kind string) (PackageFile, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return PackageFile{}, err
	}
	sum := sha256.Sum256(content)
	return PackageFile{
		Path:      path,
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(content)),
		Kind:      kind,
	}, nil
}

func renderReadme(pkg Package) string {
	return fmt.Sprintf(`# Remote Dev Skillkit Hosted Provider Package

Schema: %s
Name: %s

This package describes a hosted gateway provider boundary for Remote Dev
Skillkit. It contains no credentials, private endpoints, organization IDs, or
machine-local paths.

Storage provider: %s
Auth provider: %s

Verify before use:

%s
%s

Gateway command template:

%s

External database, identity-provider, cloud, DNS, paid-resource, and retention
policy changes require operator approval before deployment.
`, PackageSchemaVersion, pkg.Name, pkg.Storage.Kind, pkg.Auth.Kind, pkg.Storage.VerifyCommand, pkg.Auth.VerifyCommand, strings.Join(pkg.GatewayArgs, " "))
}

func renderEnvTemplate(pkg Package) string {
	var builder strings.Builder
	for _, env := range pkg.Environment {
		builder.WriteString("# ")
		builder.WriteString(env.Description)
		builder.WriteByte('\n')
		builder.WriteString(env.Name)
		builder.WriteString("=\n\n")
	}
	return builder.String()
}

func resolveManifest(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("package is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(abs, "hosted-provider.json"), abs, nil
	}
	return abs, filepath.Dir(abs), nil
}

func verifyFiles(root string, files []PackageFile) []FileCheck {
	seen := map[string]bool{}
	var out []FileCheck
	for _, file := range files {
		result := FileCheck{
			Path:           file.Path,
			Kind:           file.Kind,
			ExpectedSHA256: file.SHA256,
			ExpectedSize:   file.SizeBytes,
		}
		add := func(name string, passed bool, detail string) {
			result.Checks = append(result.Checks, Check{Name: name, Passed: passed, Detail: detail})
		}
		safe := safePath(file.Path)
		add("file_path_safe", safe, file.Path)
		add("file_path_unique", !seen[file.Path], file.Path)
		seen[file.Path] = true
		add("expected_sha256_format", strings.HasPrefix(file.SHA256, "sha256:") && len(strings.TrimPrefix(file.SHA256, "sha256:")) == 64, file.SHA256)
		if safe {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
			add("file_exists", err == nil, errorDetail(err))
			if err == nil {
				sum := sha256.Sum256(content)
				result.ActualSHA256 = "sha256:" + hex.EncodeToString(sum[:])
				result.ActualSize = int64(len(content))
				add("file_sha256_matches", result.ActualSHA256 == file.SHA256, file.SHA256)
				add("file_size_matches", result.ActualSize == file.SizeBytes, fmt.Sprintf("%d", file.SizeBytes))
				add("file_has_no_private_surface", noPrivateSurface(content), file.Path)
			}
		}
		out = append(out, result)
	}
	return out
}

func unlistedFiles(root string, files []PackageFile) []string {
	listed := map[string]bool{}
	for _, file := range files {
		listed[file.Path] = true
	}
	listed["hosted-provider.json"] = true
	var unlisted []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !listed[key] {
			unlisted = append(unlisted, key)
		}
		return nil
	})
	sort.Strings(unlisted)
	return unlisted
}

func safePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func supportedStorageProvider(provider string) bool {
	switch provider {
	case "file", "postgres", "s3-compatible", "redis-stream":
		return true
	default:
		return false
	}
}

func supportedAuthProvider(provider string) bool {
	switch provider {
	case "hosted-ed25519-jwt", "oidc-jwks", "saml-assertion":
		return true
	default:
		return false
	}
}

func envHasNoSecretValues(env []EnvVar) bool {
	for _, item := range env {
		if strings.Contains(item.Name, "=") || strings.Contains(item.Description, "sk-") {
			return false
		}
	}
	return true
}

func noPrivateSurface(content []byte) bool {
	text := string(content)
	for _, forbidden := range []string{
		"/Users/",
		"192.168.",
		"10.0.",
		"BEGIN PRIVATE KEY",
		"PRIVATE KEY",
		"password=",
		"secret=",
		"token=",
		"sk-",
	} {
		if strings.Contains(text, forbidden) {
			return false
		}
	}
	return true
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func failureActions() []string {
	return []string{
		"Regenerate the hosted provider package from a clean directory.",
		"Keep credentials, private endpoints, local paths, and organization-specific values outside the package.",
		"Do not deploy a hosted gateway from this package until verification passes.",
	}
}
