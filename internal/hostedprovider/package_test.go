package hostedprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildAndVerifyHostedProviderPackage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	pkg, err := Build(Options{
		OutDir:          out,
		Name:            "self-hosted-rdev",
		StorageProvider: "file",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package ok: %#v", pkg.Checks)
	}
	for _, path := range []string{"hosted-provider.json", "HOSTED_PROVIDER.md", "HOSTED_PROVIDER_RUNTIME.md", "gateway.env.example", "runtime-contract.json"} {
		if _, err := os.Stat(filepath.Join(out, path)); err != nil {
			t.Fatalf("expected hosted provider file %s: %v", path, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(out, "hosted-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), filepath.Dir(out)) || strings.Contains(string(content), "BEGIN PRIVATE KEY") {
		t.Fatalf("hosted provider package leaked private material: %s", string(content))
	}

	verification, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
	if verification.SchemaVersion != VerificationSchemaVersion ||
		verification.StorageProvider != "file" ||
		verification.AuthProvider != "hosted-ed25519-jwt" {
		t.Fatalf("unexpected verification: %#v", verification)
	}
}

func TestBuildExternalHostedProviderRuntimeContract(t *testing.T) {
	cases := []struct {
		name             string
		storageProvider  string
		authProvider     string
		requiredEnv      []string
		requiredEvidence []string
	}{
		{
			name:            "postgres oidc",
			storageProvider: "postgres",
			authProvider:    "oidc-jwks",
			requiredEnv:     []string{"RDEV_POSTGRES_DSN_SECRET_REF", "RDEV_POSTGRES_BACKUP_COMMAND_REF", "RDEV_OIDC_JWKS_REF", "RDEV_HOSTED_RUNTIME_CONFIG"},
			requiredEvidence: []string{
				"storage-verification",
				"auth-verification",
				"backup-evidence",
				"restore-evidence",
				"retention-evidence",
				"failure-mode-evidence",
			},
		},
		{
			name:            "s3 saml",
			storageProvider: "s3-compatible",
			authProvider:    "saml-assertion",
			requiredEnv:     []string{"RDEV_S3_BUCKET_SECRET_REF", "RDEV_S3_RETENTION_POLICY_REF", "RDEV_SAML_METADATA_REF", "RDEV_HOSTED_PROVIDER_PACKAGE"},
			requiredEvidence: []string{
				"gateway-startup",
				"role-mapping-evidence",
				"audit",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "provider")
			pkg, err := Build(Options{
				OutDir:          out,
				Name:            "external-provider",
				StorageProvider: tc.storageProvider,
				AuthProvider:    tc.authProvider,
				GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !pkg.OK() {
				t.Fatalf("expected package ok: %#v", pkg.Checks)
			}
			if pkg.Runtime.SchemaVersion != RuntimeContractSchemaVersion ||
				pkg.Runtime.StorageProvider != tc.storageProvider ||
				pkg.Runtime.AuthProvider != tc.authProvider ||
				pkg.Runtime.RuntimeStatus != "durable-runtime-evidence-required" {
				t.Fatalf("unexpected runtime contract: %#v", pkg.Runtime)
			}
			envNames := map[string]bool{}
			for _, env := range pkg.Environment {
				envNames[env.Name] = true
			}
			for _, expected := range tc.requiredEnv {
				if !envNames[expected] {
					t.Fatalf("missing env %q in %#v", expected, pkg.Environment)
				}
			}
			evidenceNames := map[string]bool{}
			for _, evidence := range pkg.Runtime.RequiredEvidence {
				evidenceNames[evidence.Name] = true
			}
			for _, expected := range tc.requiredEvidence {
				if !evidenceNames[expected] {
					t.Fatalf("missing evidence %q in %#v", expected, pkg.Runtime.RequiredEvidence)
				}
			}
			if !slices.Contains(pkg.GatewayArgs, "--provider-package") ||
				!slices.Contains(pkg.GatewayArgs, "${RDEV_HOSTED_RUNTIME_CONFIG}") {
				t.Fatalf("expected reviewed external gateway launcher, got %#v", pkg.GatewayArgs)
			}
			if !slices.ContainsFunc(pkg.Files, func(file PackageFile) bool {
				return file.Path == "runtime-contract.json" && file.Kind == "runtime-contract"
			}) {
				t.Fatalf("missing runtime contract file: %#v", pkg.Files)
			}
			content, err := os.ReadFile(filepath.Join(out, "runtime-contract.json"))
			if err != nil {
				t.Fatal(err)
			}
			var contract RuntimeContract
			if err := json.Unmarshal(content, &contract); err != nil {
				t.Fatal(err)
			}
			if contract.SchemaVersion != RuntimeContractSchemaVersion {
				t.Fatalf("unexpected runtime contract file: %#v", contract)
			}
			verification, err := Verify(out)
			if err != nil {
				t.Fatal(err)
			}
			if !verification.OK() {
				t.Fatalf("expected verification ok: %#v", verification)
			}
		})
	}
}

func TestBuildPostgresHostedJWTProviderUsesBuiltInGatewayRuntime(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	pkg, err := Build(Options{
		OutDir:          out,
		Name:            "postgres-hosted-jwt",
		StorageProvider: "postgres",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package ok: %#v", pkg.Checks)
	}
	expectedArgs := "rdev gateway serve --storage-provider postgres --storage-path ${RDEV_GATEWAY_STORAGE_PATH} --hosted-operator-auth ${RDEV_HOSTED_OPERATOR_AUTH_FILE}"
	if strings.Join(pkg.GatewayArgs, " ") != expectedArgs {
		t.Fatalf("expected built-in postgres gateway args %q, got %#v", expectedArgs, pkg.GatewayArgs)
	}
	if slices.Contains(pkg.GatewayArgs, "operator-reviewed-hosted-gateway-launcher") {
		t.Fatalf("postgres hosted JWT package should not use placeholder launcher: %#v", pkg.GatewayArgs)
	}
	envNames := map[string]bool{}
	for _, env := range pkg.Environment {
		envNames[env.Name] = true
	}
	for _, expected := range []string{"RDEV_GATEWAY_STORAGE_PATH", "RDEV_POSTGRES_DSN_SECRET_REF", "RDEV_HOSTED_OPERATOR_AUTH_FILE"} {
		if !envNames[expected] {
			t.Fatalf("missing env %q in %#v", expected, pkg.Environment)
		}
	}
}

func TestBuildRedisHostedJWTProviderUsesBuiltInGatewayRuntime(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	pkg, err := Build(Options{
		OutDir:          out,
		Name:            "redis-hosted-jwt",
		StorageProvider: "redis-stream",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 20, 45, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package ok: %#v", pkg.Checks)
	}
	expectedArgs := "rdev gateway serve --storage-provider redis-stream --storage-path ${RDEV_GATEWAY_STORAGE_PATH} --hosted-operator-auth ${RDEV_HOSTED_OPERATOR_AUTH_FILE}"
	if strings.Join(pkg.GatewayArgs, " ") != expectedArgs {
		t.Fatalf("expected built-in redis gateway args %q, got %#v", expectedArgs, pkg.GatewayArgs)
	}
	if slices.Contains(pkg.GatewayArgs, "operator-reviewed-hosted-gateway-launcher") {
		t.Fatalf("redis hosted JWT package should not use placeholder launcher: %#v", pkg.GatewayArgs)
	}
	envNames := map[string]bool{}
	for _, env := range pkg.Environment {
		envNames[env.Name] = true
	}
	for _, expected := range []string{"RDEV_GATEWAY_STORAGE_PATH", "RDEV_REDIS_URL_SECRET_REF", "RDEV_REDIS_STREAM_PREFIX", "RDEV_HOSTED_OPERATOR_AUTH_FILE"} {
		if !envNames[expected] {
			t.Fatalf("missing env %q in %#v", expected, pkg.Environment)
		}
	}
}

func TestVerifyHostedProviderPackageDetectsTamperedFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	_, err := Build(Options{
		OutDir:          out,
		StorageProvider: "file",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "gateway.env.example"), []byte("RDEV_SECRET=sk-private\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	verification, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected tampered hosted provider package to fail")
	}
	failures := failedNames(verification)
	if !strings.Contains(failures, "gateway.env.example:file_sha256_matches") ||
		!strings.Contains(failures, "gateway.env.example:file_has_no_private_surface") {
		t.Fatalf("expected checksum and private-surface failures, got %s", failures)
	}
}

func failedNames(verification Verification) string {
	var failed []string
	for _, check := range verification.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	for _, file := range verification.Files {
		for _, check := range file.Checks {
			if !check.Passed {
				failed = append(failed, file.Path+":"+check.Name)
			}
		}
	}
	return strings.Join(failed, ",")
}
