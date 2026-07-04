package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostedprovider"
)

func TestPackageAndVerifyHostedProviderRuntimeEvidence(t *testing.T) {
	root := t.TempDir()
	providerDir := filepath.Join(root, "hosted-provider")
	if _, err := hostedprovider.Build(hostedprovider.Options{
		OutDir:          providerDir,
		Name:            "single-node-smoke",
		StorageProvider: "file",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForTest(t, root, "file", "hosted-ed25519-jwt")
	pkg, err := PackageHostedProviderRuntimeEvidence(HostedProviderRuntimePackageOptions{
		HostedProviderPackagePath: providerDir,
		OutDir:                    filepath.Join(root, "package"),
		GatewayStartupPath:        evidence.gatewayStartup,
		StorageVerificationPath:   evidence.storageVerification,
		AuthVerificationPath:      evidence.authVerification,
		BackupEvidencePath:        evidence.backupEvidence,
		RestoreEvidencePath:       evidence.restoreEvidence,
		RetentionEvidencePath:     evidence.retentionEvidence,
		RoleMappingEvidencePath:   evidence.roleMappingEvidence,
		FailureModeEvidencePath:   evidence.failureModeEvidence,
		AuditPath:                 evidence.audit,
		Now:                       time.Date(2026, 7, 4, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected hosted provider runtime package ok: %#v", pkg.Checks)
	}
	if pkg.RuntimeClaim != "single-node-hosted-smoke" {
		t.Fatalf("unexpected runtime claim %q", pkg.RuntimeClaim)
	}
	if pkg.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github token redaction, got %#v", pkg.RedactionRuleCounts)
	}

	verification, err := VerifyHostedProviderRuntimeAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
}

func TestPackageAndVerifyExternalHostedProviderRuntimeEvidence(t *testing.T) {
	root := t.TempDir()
	providerDir := filepath.Join(root, "hosted-provider")
	if _, err := hostedprovider.Build(hostedprovider.Options{
		OutDir:          providerDir,
		Name:            "external-hosted-runtime",
		StorageProvider: "postgres",
		AuthProvider:    "oidc-jwks",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForTest(t, root, "postgres", "oidc-jwks")
	pkg, err := PackageHostedProviderRuntimeEvidence(HostedProviderRuntimePackageOptions{
		HostedProviderPackagePath: providerDir,
		OutDir:                    filepath.Join(root, "package"),
		GatewayStartupPath:        evidence.gatewayStartup,
		StorageVerificationPath:   evidence.storageVerification,
		AuthVerificationPath:      evidence.authVerification,
		BackupEvidencePath:        evidence.backupEvidence,
		RestoreEvidencePath:       evidence.restoreEvidence,
		RetentionEvidencePath:     evidence.retentionEvidence,
		RoleMappingEvidencePath:   evidence.roleMappingEvidence,
		FailureModeEvidencePath:   evidence.failureModeEvidence,
		AuditPath:                 evidence.audit,
		Now:                       time.Date(2026, 7, 4, 12, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected external hosted runtime package ok: %#v", pkg.Checks)
	}
	if pkg.RuntimeClaim != "external-durable-hosted-runtime-evidence" ||
		pkg.StorageProvider != "postgres" ||
		pkg.AuthProvider != "oidc-jwks" {
		t.Fatalf("unexpected external runtime package: %#v", pkg)
	}
	verification, err := VerifyHostedProviderRuntimeAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected verification ok: %#v", verification.Checks)
	}
	if verification.RuntimeClaim != "external-durable-hosted-runtime-evidence" {
		t.Fatalf("unexpected verification runtime claim %q", verification.RuntimeClaim)
	}
}

func TestVerifyHostedProviderRuntimeRejectsMissingDurabilityEvidence(t *testing.T) {
	root := t.TempDir()
	providerDir := filepath.Join(root, "hosted-provider")
	if _, err := hostedprovider.Build(hostedprovider.Options{
		OutDir:          providerDir,
		StorageProvider: "file",
		AuthProvider:    "hosted-ed25519-jwt",
		GeneratedAt:     time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForTest(t, root, "file", "hosted-ed25519-jwt")
	pkg, err := PackageHostedProviderRuntimeEvidence(HostedProviderRuntimePackageOptions{
		HostedProviderPackagePath: providerDir,
		OutDir:                    filepath.Join(root, "package"),
		GatewayStartupPath:        evidence.gatewayStartup,
		StorageVerificationPath:   evidence.storageVerification,
		AuthVerificationPath:      evidence.authVerification,
		RoleMappingEvidencePath:   evidence.roleMappingEvidence,
		FailureModeEvidencePath:   evidence.failureModeEvidence,
		AuditPath:                 evidence.audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatal("expected package to fail without backup, restore, and retention evidence")
	}
	verification, err := VerifyHostedProviderRuntimeAcceptancePackage(pkg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() {
		t.Fatal("expected verification to fail")
	}
	failures := failedHostedRuntimeAcceptanceChecks(verification.Checks)
	for _, expected := range []string{
		"package_checks_passed",
		"backup_evidence_present",
		"restore_evidence_present",
		"retention_evidence_present",
	} {
		if !strings.Contains(failures, expected) {
			t.Fatalf("expected failure %q in %s", expected, failures)
		}
	}
}

type hostedProviderRuntimeEvidenceForTest struct {
	gatewayStartup      string
	storageVerification string
	authVerification    string
	backupEvidence      string
	restoreEvidence     string
	retentionEvidence   string
	roleMappingEvidence string
	failureModeEvidence string
	audit               string
}

func writeHostedProviderRuntimeEvidenceForTest(t *testing.T, root, storageProvider, authProvider string) hostedProviderRuntimeEvidenceForTest {
	t.Helper()
	dir := filepath.Join(root, "runtime-evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) string {
		t.Helper()
		path := filepath.Join(dir, name)
		var content []byte
		switch typed := value.(type) {
		case string:
			content = []byte(typed)
		default:
			var err error
			content, err = json.MarshalIndent(value, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			content = append(content, '\n')
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return hostedProviderRuntimeEvidenceForTest{
		gatewayStartup:      write("gateway-startup.txt", "gateway started with hosted provider package\nexported token ghp_abcdefghijklmnopqrstuvwx\n"),
		storageVerification: write("storage-verification.json", map[string]any{"ok": true, "provider": storageProvider}),
		authVerification:    write("auth-verification.json", map[string]any{"ok": true, "provider": authProvider}),
		backupEvidence:      write("backup-evidence.txt", "snapshot copied to reviewed backup location\n"),
		restoreEvidence:     write("restore-evidence.txt", "restored snapshot and verified audit chain\n"),
		retentionEvidence:   write("retention-evidence.txt", "retention policy reviewed for release smoke\n"),
		roleMappingEvidence: write("role-mapping-evidence.json", map[string]any{
			"probes": []map[string]any{
				{"role": "operator", "authorized": true},
				{"role": "viewer", "authorized": false},
			},
		}),
		failureModeEvidence: write("failure-mode-evidence.json", map[string]any{"ok": true, "failure_mode_tested": true, "mode": "invalid auth rejected"}),
		audit:               write("audit.txt", "gateway_start\nstorage_verify\nauth_verify\nrole_probe\nfailure_probe\ncleanup\n"),
	}
}

func failedHostedRuntimeAcceptanceChecks(checks []Check) string {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}
