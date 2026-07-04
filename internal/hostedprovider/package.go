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
const RuntimeContractSchemaVersion = "rdev.hosted-provider-runtime-contract.v1"

type Options struct {
	OutDir          string
	Name            string
	StorageProvider string
	AuthProvider    string
	GeneratedAt     time.Time
	Force           bool
}

type Package struct {
	SchemaVersion    string          `json:"schema_version"`
	Name             string          `json:"name"`
	GeneratedAt      time.Time       `json:"generated_at"`
	ExternalMutation bool            `json:"external_mutation"`
	ProductionClaim  string          `json:"production_claim"`
	Storage          Provider        `json:"storage"`
	Auth             Provider        `json:"auth"`
	Runtime          RuntimeContract `json:"runtime"`
	GatewayArgs      []string        `json:"gateway_args"`
	Environment      []EnvVar        `json:"environment"`
	Files            []PackageFile   `json:"files"`
	Checks           []Check         `json:"checks"`
	AgentRules       []string        `json:"agent_rules"`
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

type RuntimeContract struct {
	SchemaVersion            string                       `json:"schema_version"`
	StorageProvider          string                       `json:"storage_provider"`
	AuthProvider             string                       `json:"auth_provider"`
	RuntimeStatus            string                       `json:"runtime_status"`
	GatewayLauncher          []string                     `json:"gateway_launcher"`
	RequiredEvidence         []RuntimeEvidenceRequirement `json:"required_evidence"`
	OperatorApprovalRequired []string                     `json:"operator_approval_required"`
	UnsupportedClaims        []string                     `json:"unsupported_claims"`
}

type RuntimeEvidenceRequirement struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Required       bool   `json:"required"`
	Description    string `json:"description"`
	ExampleCommand string `json:"example_command,omitempty"`
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
		Runtime:          runtimeContract(storageProvider, authProvider),
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
		{"HOSTED_PROVIDER_RUNTIME.md", "runtime-documentation", []byte(renderRuntimeReadme(pkg))},
		{"gateway.env.example", "env-template", []byte(renderEnvTemplate(pkg))},
	}
	runtimeContent, err := json.MarshalIndent(pkg.Runtime, "", "  ")
	if err != nil {
		return Package{}, err
	}
	files = append(files, struct {
		path    string
		kind    string
		content []byte
	}{"runtime-contract.json", "runtime-contract", append(runtimeContent, '\n')})
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
	add("runtime_contract_present", pkg.Runtime.SchemaVersion == RuntimeContractSchemaVersion, pkg.Runtime.SchemaVersion)
	add("runtime_contract_matches_providers", pkg.Runtime.StorageProvider == pkg.Storage.Kind && pkg.Runtime.AuthProvider == pkg.Auth.Kind, pkg.Runtime.StorageProvider+"/"+pkg.Runtime.AuthProvider)
	add("runtime_evidence_declared", len(pkg.Runtime.RequiredEvidence) >= 6, fmt.Sprintf("%d", len(pkg.Runtime.RequiredEvidence)))
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
	case "postgres":
		return Provider{
			Kind:             "postgres",
			Schema:           "rdev.hosted-storage.postgres.v1",
			Implementation:   "provider-runtime-package",
			RuntimeStatus:    "durable-runtime-evidence-required",
			RequiredSettings: []string{"RDEV_POSTGRES_DSN_SECRET_REF", "RDEV_POSTGRES_TLS_MODE", "RDEV_POSTGRES_MIGRATIONS_REF", "RDEV_POSTGRES_BACKUP_COMMAND_REF"},
			VerifyCommand:    "rdev hosted-provider verify --package <provider-package> && <operator-reviewed-postgres-health-probe>",
			Notes:            []string{"Use a secret reference for the DSN; do not store hostnames, usernames, passwords, or database names in this package.", "Runtime evidence must prove schema migration, snapshot load/save, backup, restore, retention, and rejected-credential behavior."},
		}
	case "s3-compatible":
		return Provider{
			Kind:             "s3-compatible",
			Schema:           "rdev.hosted-storage.s3-compatible.v1",
			Implementation:   "built-in-aws-cli-runtime",
			RuntimeStatus:    "durable-runtime-evidence-required",
			RequiredSettings: []string{"RDEV_GATEWAY_STORAGE_PATH", "RDEV_S3_ENDPOINT_SECRET_REF", "RDEV_S3_BUCKET_SECRET_REF", "RDEV_S3_REGION", "RDEV_S3_KMS_POLICY_REF", "RDEV_S3_RETENTION_POLICY_REF"},
			VerifyCommand:    "rdev gateway storage verify --provider s3-compatible --path <s3-bucket-prefix-from-secret-ref>",
			Notes:            []string{"Use a secret reference for endpoint, bucket, access key, and secret key material; expose the final s3://bucket/prefix only at runtime.", "The built-in runtime uses aws s3api to store and load the current gateway snapshot object.", "Production evidence must still prove versioning or backup, restore, retention, denied credential behavior, role mapping, failure modes, and audit redaction."},
		}
	case "redis-stream":
		return Provider{
			Kind:             "redis-stream",
			Schema:           "rdev.hosted-storage.redis-stream.v1",
			Implementation:   "built-in-redis-cli-runtime",
			RuntimeStatus:    "durable-runtime-evidence-required",
			RequiredSettings: []string{"RDEV_REDIS_URL_SECRET_REF", "RDEV_REDIS_STREAM_PREFIX", "RDEV_REDIS_TLS_MODE", "RDEV_REDIS_RETENTION_POLICY_REF"},
			VerifyCommand:    "rdev gateway storage verify --provider redis-stream --path <redis-url-from-secret-ref>",
			Notes:            []string{"Use a secret reference for the Redis URL; do not include hostnames or credentials in this package.", "The built-in runtime uses redis-cli, stores the current gateway snapshot key, and appends snapshot/probe events to a Redis stream.", "Production evidence must still prove persistence/replication, retention, backup/restore or replay policy, role mapping, failure modes, and audit redaction."},
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
	case "oidc-jwks":
		return Provider{
			Kind:             "oidc-jwks",
			Schema:           "rdev.hosted-auth.oidc-jwks.v1",
			Implementation:   "provider-runtime-package",
			RuntimeStatus:    "durable-runtime-evidence-required",
			RequiredSettings: []string{"RDEV_OIDC_ISSUER", "RDEV_OIDC_AUDIENCE", "RDEV_OIDC_JWKS_REF", "RDEV_OIDC_ROLE_CLAIM", "RDEV_OIDC_CLOCK_SKEW_SECONDS"},
			VerifyCommand:    "rdev hosted-provider verify --package <provider-package> && <operator-reviewed-oidc-jwks-probe>",
			Notes:            []string{"Keep tenant domains and JWKS URLs in runtime config or secret references, not in the public package.", "Runtime evidence must prove valid token acceptance, invalid issuer/audience/key rejection, role mapping, and key rotation behavior."},
		}
	case "saml-assertion":
		return Provider{
			Kind:             "saml-assertion",
			Schema:           "rdev.hosted-auth.saml-assertion.v1",
			Implementation:   "provider-runtime-package",
			RuntimeStatus:    "durable-runtime-evidence-required",
			RequiredSettings: []string{"RDEV_SAML_METADATA_REF", "RDEV_SAML_AUDIENCE", "RDEV_SAML_ROLE_ATTRIBUTE", "RDEV_SAML_CLOCK_SKEW_SECONDS"},
			VerifyCommand:    "rdev hosted-provider verify --package <provider-package> && <operator-reviewed-saml-probe>",
			Notes:            []string{"Keep IdP metadata and certificate material in reviewed runtime config or secret references, not in this package.", "Runtime evidence must prove signed assertion acceptance, unsigned/expired/wrong-audience rejection, and role mapping."},
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
	if authProvider == "hosted-ed25519-jwt" && (storageProvider == "file" || storageProvider == "postgres" || storageProvider == "redis-stream" || storageProvider == "s3-compatible") {
		args := []string{"rdev", "gateway", "serve", "--storage-provider", storageProvider, "--storage-path", "${RDEV_GATEWAY_STORAGE_PATH}"}
		args = append(args, "--hosted-operator-auth", "${RDEV_HOSTED_OPERATOR_AUTH_FILE}")
		return args
	}
	if storageProvider != "file" || authProvider != "hosted-ed25519-jwt" {
		return []string{"operator-reviewed-hosted-gateway-launcher", "--provider-package", "${RDEV_HOSTED_PROVIDER_PACKAGE}", "--runtime-config", "${RDEV_HOSTED_RUNTIME_CONFIG}"}
	}
	return []string{"rdev", "gateway", "serve", "--storage-provider", storageProvider, "--storage-path", "${RDEV_GATEWAY_STORAGE_PATH}"}
}

func environment(storageProvider, authProvider string) []EnvVar {
	var env []EnvVar
	if storageProvider == "file" {
		env = append(env, EnvVar{Name: "RDEV_GATEWAY_STORAGE_PATH", Required: true, Description: "Path to the gateway snapshot JSON file for the built-in file provider.", Secret: false})
	} else {
		env = append(env, storageEnvironment(storageProvider)...)
	}
	if authProvider == "hosted-ed25519-jwt" {
		env = append(env, EnvVar{Name: "RDEV_HOSTED_OPERATOR_AUTH_FILE", Required: true, Description: "Path to rdev.hosted-operator-auth.v1 public-key verifier config.", Secret: false})
	} else {
		env = append(env, authEnvironment(authProvider)...)
	}
	if storageProvider != "file" || authProvider != "hosted-ed25519-jwt" {
		env = append(env, EnvVar{Name: "RDEV_HOSTED_PROVIDER_PACKAGE", Required: true, Description: "Path to the verified hosted-provider package directory or hosted-provider.json.", Secret: false})
		env = append(env, EnvVar{Name: "RDEV_HOSTED_RUNTIME_CONFIG", Required: true, Description: "Path to the reviewed runtime config assembled by the operator for this provider package.", Secret: false})
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
		{Name: "runtime_contract_declared", Passed: pkg.Runtime.SchemaVersion == RuntimeContractSchemaVersion, Detail: pkg.Runtime.SchemaVersion},
		{Name: "runtime_contract_provider_match", Passed: pkg.Runtime.StorageProvider == pkg.Storage.Kind && pkg.Runtime.AuthProvider == pkg.Auth.Kind, Detail: pkg.Runtime.StorageProvider + "/" + pkg.Runtime.AuthProvider},
		{Name: "runtime_evidence_requirements_declared", Passed: len(pkg.Runtime.RequiredEvidence) >= 6, Detail: fmt.Sprintf("%d", len(pkg.Runtime.RequiredEvidence))},
		{Name: "environment_declared", Passed: len(pkg.Environment) > 0, Detail: fmt.Sprintf("%d", len(pkg.Environment))},
		{Name: "no_secret_environment_values", Passed: envHasNoSecretValues(pkg.Environment), Detail: fmt.Sprintf("%d", len(pkg.Environment))},
	}
}

func runtimeContract(storageProvider, authProvider string) RuntimeContract {
	status := "single-node-smoke"
	if storageProvider != "file" || authProvider != "hosted-ed25519-jwt" {
		status = "durable-runtime-evidence-required"
	}
	return RuntimeContract{
		SchemaVersion:   RuntimeContractSchemaVersion,
		StorageProvider: storageProvider,
		AuthProvider:    authProvider,
		RuntimeStatus:   status,
		GatewayLauncher: gatewayArgs(storageProvider, authProvider),
		RequiredEvidence: []RuntimeEvidenceRequirement{
			{Name: "gateway-startup", Kind: "transcript", Required: true, Description: "Gateway startup or deployment transcript using this verified provider package."},
			{Name: "storage-verification", Kind: "json", Required: true, Description: "Storage verification with ok=true for the selected storage provider.", ExampleCommand: storageDescriptor(storageProvider).VerifyCommand},
			{Name: "auth-verification", Kind: "json", Required: true, Description: "Hosted auth verification with ok=true for the selected auth provider.", ExampleCommand: authDescriptor(authProvider).VerifyCommand},
			{Name: "backup-evidence", Kind: "transcript", Required: true, Description: "Backup, versioning, export, or snapshot evidence appropriate for the selected storage provider."},
			{Name: "restore-evidence", Kind: "transcript", Required: true, Description: "Restore drill evidence proving state can be recovered into a clean gateway runtime."},
			{Name: "retention-evidence", Kind: "transcript", Required: true, Description: "Retention policy evidence covering audit/state retention and deletion expectations."},
			{Name: "role-mapping-evidence", Kind: "json", Required: true, Description: "Authorization probes with at least one allowed and one denied decision."},
			{Name: "failure-mode-evidence", Kind: "json", Required: true, Description: "Failure-mode probes for unavailable storage/auth or rejected credentials."},
			{Name: "audit", Kind: "transcript", Required: true, Description: "Redacted audit transcript covering startup, storage/auth probes, role probes, failure probes, and cleanup."},
		},
		OperatorApprovalRequired: []string{
			"Creating or mutating external databases, buckets, streams, identity applications, DNS, certificates, cloud accounts, or paid resources.",
			"Adding real endpoints, tenant identifiers, credentials, keys, certificates, IdP metadata, or organization-specific values to runtime config.",
			"Changing backup, restore, retention, lifecycle, role-mapping, or break-glass policy.",
		},
		UnsupportedClaims: []string{
			"This package alone does not prove a deployed hosted gateway.",
			"This package does not include credentials, private endpoints, tenant IDs, cloud resources, or a running database/object store/identity provider.",
			"Production claims require a passing rdev.acceptance-package.hosted-provider-runtime.v1 evidence bundle from a real deployment.",
		},
	}
}

func storageEnvironment(provider string) []EnvVar {
	switch provider {
	case "postgres":
		return []EnvVar{
			{Name: "RDEV_GATEWAY_STORAGE_PATH", Required: true, Description: "libpq connection info or service name for the built-in Postgres gateway state store. Do not include inline passwords; use PGSERVICE, .pgpass, or an operator-approved secret injector.", Secret: false},
			{Name: "RDEV_POSTGRES_DSN_SECRET_REF", Required: true, Description: "Secret-manager reference for the PostgreSQL connection info used to populate RDEV_GATEWAY_STORAGE_PATH at runtime.", Secret: true},
			{Name: "RDEV_POSTGRES_TLS_MODE", Required: true, Description: "Reviewed PostgreSQL TLS mode, for example require or verify-full.", Secret: false},
			{Name: "RDEV_POSTGRES_MIGRATIONS_REF", Required: true, Description: "Reviewed migration bundle or schema bootstrap reference.", Secret: false},
			{Name: "RDEV_POSTGRES_BACKUP_COMMAND_REF", Required: true, Description: "Reviewed backup command or managed backup policy reference.", Secret: false},
		}
	case "s3-compatible":
		return []EnvVar{
			{Name: "RDEV_GATEWAY_STORAGE_PATH", Required: true, Description: "s3://bucket/prefix for the built-in S3-compatible gateway state store. Do not include endpoint query strings, credentials, or fragments; use AWS_PROFILE, AWS_* environment, endpoint config, or an operator-approved secret injector.", Secret: false},
			{Name: "RDEV_S3_ENDPOINT_SECRET_REF", Required: true, Description: "Secret-manager reference for the S3-compatible endpoint.", Secret: true},
			{Name: "RDEV_S3_BUCKET_SECRET_REF", Required: true, Description: "Secret-manager reference for bucket and access policy configuration.", Secret: true},
			{Name: "RDEV_S3_REGION", Required: true, Description: "Reviewed region or placement label.", Secret: false},
			{Name: "RDEV_S3_KMS_POLICY_REF", Required: true, Description: "Reviewed server-side encryption or KMS policy reference.", Secret: false},
			{Name: "RDEV_S3_RETENTION_POLICY_REF", Required: true, Description: "Reviewed lifecycle, versioning, and retention policy reference.", Secret: false},
		}
	case "redis-stream":
		return []EnvVar{
			{Name: "RDEV_GATEWAY_STORAGE_PATH", Required: true, Description: "Redis URL for the built-in redis-stream gateway state store. Do not include inline credentials; use REDISCLI_AUTH or an operator-approved secret injector.", Secret: false},
			{Name: "RDEV_REDIS_URL_SECRET_REF", Required: true, Description: "Secret-manager reference for the Redis URL.", Secret: true},
			{Name: "RDEV_REDIS_STREAM_PREFIX", Required: true, Description: "Reviewed stream key prefix for gateway state and audit events.", Secret: false},
			{Name: "RDEV_REDIS_TLS_MODE", Required: true, Description: "Reviewed Redis TLS mode.", Secret: false},
			{Name: "RDEV_REDIS_RETENTION_POLICY_REF", Required: true, Description: "Reviewed stream trimming, persistence, replication, and retention policy reference.", Secret: false},
		}
	default:
		return []EnvVar{
			{Name: "RDEV_HOSTED_STORAGE_CONFIG", Required: true, Description: "Path to the reviewed external hosted storage provider configuration.", Secret: false},
			{Name: "RDEV_HOSTED_STORAGE_SECRET_REF", Required: true, Description: "Reference to provider credentials in an operator-approved secret manager.", Secret: true},
		}
	}
}

func authEnvironment(provider string) []EnvVar {
	switch provider {
	case "oidc-jwks":
		return []EnvVar{
			{Name: "RDEV_OIDC_ISSUER", Required: true, Description: "Reviewed issuer identifier or runtime config reference.", Secret: false},
			{Name: "RDEV_OIDC_AUDIENCE", Required: true, Description: "Reviewed audience value for gateway operator tokens.", Secret: false},
			{Name: "RDEV_OIDC_JWKS_REF", Required: true, Description: "Reviewed JWKS URL/config reference or pinned key-set reference.", Secret: false},
			{Name: "RDEV_OIDC_ROLE_CLAIM", Required: true, Description: "Reviewed claim name used for operator role mapping.", Secret: false},
		}
	case "saml-assertion":
		return []EnvVar{
			{Name: "RDEV_SAML_METADATA_REF", Required: true, Description: "Reviewed SAML IdP metadata or certificate reference.", Secret: false},
			{Name: "RDEV_SAML_AUDIENCE", Required: true, Description: "Reviewed SAML audience for gateway operator assertions.", Secret: false},
			{Name: "RDEV_SAML_ROLE_ATTRIBUTE", Required: true, Description: "Reviewed SAML attribute used for operator role mapping.", Secret: false},
			{Name: "RDEV_SAML_CLOCK_SKEW_SECONDS", Required: true, Description: "Reviewed maximum assertion clock skew in seconds.", Secret: false},
		}
	default:
		return []EnvVar{{Name: "RDEV_HOSTED_AUTH_CONFIG", Required: true, Description: "Path to the reviewed external hosted auth provider configuration.", Secret: false}}
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

Runtime contract:

%s

External database, identity-provider, cloud, DNS, paid-resource, and retention
policy changes require operator approval before deployment.
`, PackageSchemaVersion, pkg.Name, pkg.Storage.Kind, pkg.Auth.Kind, pkg.Storage.VerifyCommand, pkg.Auth.VerifyCommand, strings.Join(pkg.GatewayArgs, " "), pkg.Runtime.SchemaVersion)
}

func renderRuntimeReadme(pkg Package) string {
	var builder strings.Builder
	builder.WriteString("# Hosted Provider Runtime Contract\n\n")
	builder.WriteString("Schema: ")
	builder.WriteString(pkg.Runtime.SchemaVersion)
	builder.WriteString("\n\n")
	builder.WriteString("Storage provider: ")
	builder.WriteString(pkg.Runtime.StorageProvider)
	builder.WriteString("\n")
	builder.WriteString("Auth provider: ")
	builder.WriteString(pkg.Runtime.AuthProvider)
	builder.WriteString("\n")
	builder.WriteString("Runtime status: ")
	builder.WriteString(pkg.Runtime.RuntimeStatus)
	builder.WriteString("\n\n")
	builder.WriteString("Required evidence:\n\n")
	for _, evidence := range pkg.Runtime.RequiredEvidence {
		builder.WriteString("- ")
		builder.WriteString(evidence.Name)
		builder.WriteString(" (")
		builder.WriteString(evidence.Kind)
		builder.WriteString("): ")
		builder.WriteString(evidence.Description)
		if evidence.ExampleCommand != "" {
			builder.WriteString(" Example: `")
			builder.WriteString(evidence.ExampleCommand)
			builder.WriteString("`.")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\nOperator approval is required before external database, object storage, identity, DNS, certificate, paid-resource, backup, restore, retention, or role-mapping changes.\n")
	builder.WriteString("Do not publish production hosted claims until the runtime evidence is packaged with `rdev acceptance package-hosted-provider-runtime` and verified.\n")
	return builder.String()
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
