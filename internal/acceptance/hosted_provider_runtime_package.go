package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostedprovider"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const HostedProviderRuntimePackageSchemaVersion = "rdev.acceptance-package.hosted-provider-runtime.v1"
const HostedProviderRuntimeVerificationSchemaVersion = "rdev.acceptance-verification.hosted-provider-runtime-package.v1"

type HostedProviderRuntimePackageOptions struct {
	HostedProviderPackagePath string
	OutDir                    string
	EvidenceDirPath           string
	GatewayStartupPath        string
	StorageVerificationPath   string
	AuthVerificationPath      string
	BackupEvidencePath        string
	RestoreEvidencePath       string
	RetentionEvidencePath     string
	RoleMappingEvidencePath   string
	FailureModeEvidencePath   string
	AuditPath                 string
	NotesPath                 string
	Now                       time.Time
}

type HostedProviderRuntimeAcceptancePackage struct {
	SchemaVersion              string                      `json:"schema_version"`
	GeneratedAt                time.Time                   `json:"generated_at"`
	OutDir                     string                      `json:"out_dir"`
	HostedProviderPackage      string                      `json:"hosted_provider_package"`
	HostedProviderSchema       string                      `json:"hosted_provider_schema"`
	StorageProvider            string                      `json:"storage_provider"`
	AuthProvider               string                      `json:"auth_provider"`
	RuntimeClaim               string                      `json:"runtime_claim"`
	HostedProviderVerification hostedprovider.Verification `json:"hosted_provider_verification"`
	Checks                     []Check                     `json:"checks"`
	Files                      []AcceptancePackageFile     `json:"files"`
	RedactionRuleCounts        map[string]int              `json:"redaction_rule_counts,omitempty"`
	RequiredEvidence           []string                    `json:"required_evidence"`
	RecommendedActions         []string                    `json:"recommended_actions,omitempty"`
}

type HostedProviderRuntimeAcceptanceVerification struct {
	SchemaVersion      string                  `json:"schema_version"`
	PackagePath        string                  `json:"package_path"`
	PackageSchema      string                  `json:"package_schema"`
	StorageProvider    string                  `json:"storage_provider,omitempty"`
	AuthProvider       string                  `json:"auth_provider,omitempty"`
	RuntimeClaim       string                  `json:"runtime_claim,omitempty"`
	GeneratedAt        time.Time               `json:"generated_at"`
	Checks             []Check                 `json:"checks"`
	Files              []RelayPackageFileCheck `json:"files"`
	RecommendedActions []string                `json:"recommended_actions,omitempty"`
}

func (p HostedProviderRuntimeAcceptancePackage) OK() bool {
	if len(p.Checks) == 0 || !p.HostedProviderVerification.OK() {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (v HostedProviderRuntimeAcceptanceVerification) OK() bool {
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

func PackageHostedProviderRuntimeEvidence(opts HostedProviderRuntimePackageOptions) (HostedProviderRuntimeAcceptancePackage, error) {
	if strings.TrimSpace(opts.HostedProviderPackagePath) == "" {
		return HostedProviderRuntimeAcceptancePackage{}, fmt.Errorf("hosted provider package is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return HostedProviderRuntimeAcceptancePackage{}, fmt.Errorf("output directory is required")
	}
	var err error
	opts, err = resolveHostedRuntimeEvidenceDir(opts)
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	providerManifestPath, providerDir, err := resolveHostedProviderPackage(opts.HostedProviderPackagePath)
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	providerVerification, err := hostedprovider.Verify(providerDir)
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	providerContent, err := os.ReadFile(providerManifestPath)
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	var providerPkg hostedprovider.Package
	if err := json.Unmarshal(providerContent, &providerPkg); err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	runtimeClaim := hostedProviderRuntimeClaim(providerPkg)
	pkg := HostedProviderRuntimeAcceptancePackage{
		SchemaVersion:              HostedProviderRuntimePackageSchemaVersion,
		GeneratedAt:                now.UTC(),
		OutDir:                     outDir,
		HostedProviderPackage:      providerManifestPath,
		HostedProviderSchema:       providerPkg.SchemaVersion,
		StorageProvider:            providerPkg.Storage.Kind,
		AuthProvider:               providerPkg.Auth.Kind,
		RuntimeClaim:               runtimeClaim,
		HostedProviderVerification: providerVerification,
		RequiredEvidence: []string{
			"verified hosted-provider.json",
			"gateway startup or deployment transcript",
			"storage provider verification output",
			"hosted auth verification output",
			"backup evidence or single-node smoke backup note",
			"restore evidence or single-node smoke restore note",
			"retention policy evidence",
			"role mapping and authorization probe evidence",
			"failure-mode evidence for unavailable storage/auth or rejected credentials",
			"audit transcript covering startup, authz probes, storage checks, and cleanup",
		},
	}
	add := func(name string, passed bool, detail string) {
		pkg.Checks = append(pkg.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	redactor := shelladapter.NewArtifactRedactor()
	add("hosted_provider_verification_ok", providerVerification.OK(), failedHostedProviderCheckNames(providerVerification))

	var files []AcceptancePackageFile
	if entries, err := copyHostedProviderPackageFiles(outDir, providerDir, redactor); err != nil {
		add("hosted_provider_package_copied", false, err.Error())
	} else {
		files = append(files, entries...)
		add("hosted_provider_package_copied", len(entries) >= 3, fmt.Sprintf("%d", len(entries)))
	}
	if content, err := json.MarshalIndent(providerVerification, "", "  "); err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	} else if entry, err := writePackageContent(outDir, "hosted-provider/verification.json", "hosted-provider-verification", append(content, '\n'), ""); err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	} else {
		files = append(files, entry)
	}

	files = append(files, copyOptionalEvidence(outDir, "evidence/gateway-startup.txt", "gateway-startup", opts.GatewayStartupPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/storage-verification.json", "storage-verification", opts.StorageVerificationPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/auth-verification.json", "auth-verification", opts.AuthVerificationPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/backup-evidence.txt", "backup-evidence", opts.BackupEvidencePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/restore-evidence.txt", "restore-evidence", opts.RestoreEvidencePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/retention-evidence.txt", "retention-evidence", opts.RetentionEvidencePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/role-mapping-evidence.json", "role-mapping-evidence", opts.RoleMappingEvidencePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/failure-mode-evidence.json", "failure-mode-evidence", opts.FailureModeEvidencePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/audit.jsonl", "audit", opts.AuditPath, redactor, add)...)
	files = append(files, copyNotesEvidence(outDir, opts.NotesPath, redactor, add)...)

	add("gateway_startup_present", fileEntryKindPresent(files, "gateway-startup"), opts.GatewayStartupPath)
	add("storage_verification_present", fileEntryKindPresent(files, "storage-verification"), opts.StorageVerificationPath)
	add("storage_verification_ok", hostedEvidenceOK(outDir, "evidence/storage-verification.json"), opts.StorageVerificationPath)
	add("auth_verification_present", fileEntryKindPresent(files, "auth-verification"), opts.AuthVerificationPath)
	add("auth_verification_ok", hostedEvidenceOK(outDir, "evidence/auth-verification.json"), opts.AuthVerificationPath)
	add("backup_evidence_present", fileEntryKindPresent(files, "backup-evidence"), opts.BackupEvidencePath)
	add("restore_evidence_present", fileEntryKindPresent(files, "restore-evidence"), opts.RestoreEvidencePath)
	add("retention_evidence_present", fileEntryKindPresent(files, "retention-evidence"), opts.RetentionEvidencePath)
	add("role_mapping_evidence_present", fileEntryKindPresent(files, "role-mapping-evidence"), opts.RoleMappingEvidencePath)
	add("role_mapping_authorized_and_denied", roleMappingEvidenceProvesAuthz(outDir, "evidence/role-mapping-evidence.json"), opts.RoleMappingEvidencePath)
	add("failure_mode_evidence_present", fileEntryKindPresent(files, "failure-mode-evidence"), opts.FailureModeEvidencePath)
	add("failure_mode_probe_passed", failureModeEvidencePassed(outDir, "evidence/failure-mode-evidence.json"), opts.FailureModeEvidencePath)
	add("audit_present", fileEntryKindPresent(files, "audit"), opts.AuditPath)
	add("hosted_provider_files_have_no_private_surface", hostedProviderFilesHaveNoPrivateSurface(outDir, files), "")

	if redactor.Redacted() {
		pkg.RedactionRuleCounts = redactor.Counts()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	checksums, checksumEntry, err := writePackageChecksums(outDir, files)
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	files = append(files, checksumEntry)
	pkg.Files = files
	add("checksums_written", len(checksums) > 0, "checksums.txt")
	add("package_files_written", len(pkg.Files) >= 13, fmt.Sprintf("%d", len(pkg.Files)))
	if !pkg.OK() {
		pkg.RecommendedActions = []string{
			"Collect missing hosted provider runtime evidence from the deployed gateway run.",
			"Re-run package-hosted-provider-runtime after redacting startup, storage, auth, backup, restore, role, failure, and audit evidence.",
			"Do not publish this hosted provider runtime package as release evidence until every check passes.",
		}
	}
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	content = append(content, '\n')
	if _, err := writePackageContent(outDir, "package.json", "package-manifest", content, ""); err != nil {
		return HostedProviderRuntimeAcceptancePackage{}, err
	}
	return pkg, nil
}

func resolveHostedRuntimeEvidenceDir(opts HostedProviderRuntimePackageOptions) (HostedProviderRuntimePackageOptions, error) {
	if strings.TrimSpace(opts.EvidenceDirPath) == "" {
		return opts, nil
	}
	dir, err := filepath.Abs(opts.EvidenceDirPath)
	if err != nil {
		return HostedProviderRuntimePackageOptions{}, err
	}
	if info, err := os.Stat(dir); err != nil {
		return HostedProviderRuntimePackageOptions{}, err
	} else if !info.IsDir() {
		return HostedProviderRuntimePackageOptions{}, fmt.Errorf("hosted runtime evidence path is not a directory: %s", dir)
	}
	fill := func(current, name string) string {
		if strings.TrimSpace(current) != "" {
			return current
		}
		return filepath.Join(dir, name)
	}
	opts.GatewayStartupPath = fill(opts.GatewayStartupPath, "gateway-startup.txt")
	opts.StorageVerificationPath = fill(opts.StorageVerificationPath, "storage-verification.json")
	opts.AuthVerificationPath = fill(opts.AuthVerificationPath, "auth-verification.json")
	opts.BackupEvidencePath = fill(opts.BackupEvidencePath, "backup-evidence.txt")
	opts.RestoreEvidencePath = fill(opts.RestoreEvidencePath, "restore-evidence.txt")
	opts.RetentionEvidencePath = fill(opts.RetentionEvidencePath, "retention-evidence.txt")
	opts.RoleMappingEvidencePath = fill(opts.RoleMappingEvidencePath, "role-mapping-evidence.json")
	opts.FailureModeEvidencePath = fill(opts.FailureModeEvidencePath, "failure-mode-evidence.json")
	opts.AuditPath = fill(opts.AuditPath, "audit.jsonl")
	return opts, nil
}

func VerifyHostedProviderRuntimeAcceptancePackage(packagePath string) (HostedProviderRuntimeAcceptanceVerification, error) {
	manifestPath, dir, err := resolveAcceptancePackageManifest(packagePath)
	if err != nil {
		return HostedProviderRuntimeAcceptanceVerification{}, err
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return HostedProviderRuntimeAcceptanceVerification{}, err
	}
	var pkg HostedProviderRuntimeAcceptancePackage
	if err := json.Unmarshal(content, &pkg); err != nil {
		return HostedProviderRuntimeAcceptanceVerification{}, err
	}
	verification := HostedProviderRuntimeAcceptanceVerification{
		SchemaVersion:   HostedProviderRuntimeVerificationSchemaVersion,
		PackagePath:     manifestPath,
		PackageSchema:   pkg.SchemaVersion,
		StorageProvider: pkg.StorageProvider,
		AuthProvider:    pkg.AuthProvider,
		RuntimeClaim:    pkg.RuntimeClaim,
		GeneratedAt:     time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	add("package_schema", pkg.SchemaVersion == HostedProviderRuntimePackageSchemaVersion, pkg.SchemaVersion)
	add("package_checks_passed", allChecksPassed(pkg.Checks), failedCheckNames(pkg.Checks))
	add("hosted_provider_verification_ok", pkg.HostedProviderVerification.OK(), failedHostedProviderCheckNames(pkg.HostedProviderVerification))
	add("runtime_claim_scoped", pkg.RuntimeClaim == hostedProviderRuntimeClaimFromKinds(pkg.StorageProvider, pkg.AuthProvider), pkg.RuntimeClaim)
	add("required_evidence_declared", len(pkg.RequiredEvidence) >= 9, fmt.Sprintf("%d", len(pkg.RequiredEvidence)))
	verification.Files = verifyAcceptancePackageFiles(dir, pkg.Files)
	add("checksums_file_present", packagePathExists(pkg.Files, "checksums.txt"), "")
	add("gateway_startup_present", packageKindPresent(pkg.Files, "gateway-startup"), "")
	add("storage_verification_present", packageKindPresent(pkg.Files, "storage-verification"), "")
	add("auth_verification_present", packageKindPresent(pkg.Files, "auth-verification"), "")
	add("backup_evidence_present", packageKindPresent(pkg.Files, "backup-evidence"), "")
	add("restore_evidence_present", packageKindPresent(pkg.Files, "restore-evidence"), "")
	add("retention_evidence_present", packageKindPresent(pkg.Files, "retention-evidence"), "")
	add("role_mapping_evidence_present", packageKindPresent(pkg.Files, "role-mapping-evidence"), "")
	add("failure_mode_evidence_present", packageKindPresent(pkg.Files, "failure-mode-evidence"), "")
	add("audit_present", packageKindPresent(pkg.Files, "audit"), "")
	add("storage_verification_ok", hostedEvidenceOK(dir, "evidence/storage-verification.json"), "")
	add("auth_verification_ok", hostedEvidenceOK(dir, "evidence/auth-verification.json"), "")
	add("role_mapping_authorized_and_denied", roleMappingEvidenceProvesAuthz(dir, "evidence/role-mapping-evidence.json"), "")
	add("failure_mode_probe_passed", failureModeEvidencePassed(dir, "evidence/failure-mode-evidence.json"), "")
	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the hosted provider runtime package from complete deployed gateway evidence.",
			"Confirm storage and auth verification report ok=true, role probes include an allowed and denied decision, and failure-mode probes passed.",
			"Do not attach this package to release evidence until verification passes.",
		}
	}
	return verification, nil
}

func resolveHostedProviderPackage(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("hosted provider package is required")
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

func copyHostedProviderPackageFiles(outDir, packageDir string, redactor *shelladapter.ArtifactRedactor) ([]AcceptancePackageFile, error) {
	var files []AcceptancePackageFile
	for _, name := range []string{"hosted-provider.json", "HOSTED_PROVIDER.md", "gateway.env.example"} {
		entry, err := copyPackageFile(outDir, filepath.ToSlash(filepath.Join("hosted-provider", name)), "hosted-provider", filepath.Join(packageDir, name), redactor)
		if err != nil {
			return files, err
		}
		files = append(files, entry)
	}
	return files, nil
}

func hostedProviderRuntimeClaim(pkg hostedprovider.Package) string {
	return hostedProviderRuntimeClaimFromKinds(pkg.Storage.Kind, pkg.Auth.Kind)
}

func hostedProviderRuntimeClaimFromKinds(storageProvider, authProvider string) string {
	if storageProvider == "file" && authProvider == "hosted-ed25519-jwt" {
		return "single-node-hosted-smoke"
	}
	return "external-durable-hosted-runtime-evidence"
}

func hostedEvidenceOK(root, path string) bool {
	return releaseVerificationOK(root, path)
}

func roleMappingEvidenceProvesAuthz(root, path string) bool {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		compact := strings.ToLower(strings.Join(strings.Fields(string(content)), ""))
		return strings.Contains(compact, `"authorized":true`) && strings.Contains(compact, `"authorized":false`)
	}
	return jsonHasBoolField(value, "authorized", true) && jsonHasBoolField(value, "authorized", false)
}

func failureModeEvidencePassed(root, path string) bool {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		compact := strings.ToLower(strings.Join(strings.Fields(string(content)), ""))
		return strings.Contains(compact, `"failure_mode_tested":true`) && failureModeTextHasNegativeProbe(compact)
	}
	return jsonHasBoolField(value, "failure_mode_tested", true) && jsonHasFailureModeNegativeProbe(value)
}

func jsonHasFailureModeNegativeProbe(value any) bool {
	for _, probe := range []struct {
		key      string
		expected bool
	}{
		{"rejected", true},
		{"denied", true},
		{"unavailable", true},
		{"outage", true},
		{"accepted", false},
		{"authorized", false},
		{"ok", false},
	} {
		if jsonHasBoolField(value, probe.key, probe.expected) {
			return true
		}
	}
	return false
}

func failureModeTextHasNegativeProbe(compact string) bool {
	for _, marker := range []string{
		`"rejected":true`,
		`"denied":true`,
		`"unavailable":true`,
		`"outage":true`,
		`"accepted":false`,
		`"authorized":false`,
		`"ok":false`,
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func hostedProviderFilesHaveNoPrivateSurface(root string, files []AcceptancePackageFile) bool {
	for _, file := range files {
		if file.Kind != "hosted-provider" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil || !relayAcceptanceNoPrivateSurface(content) {
			return false
		}
	}
	return true
}

func failedHostedProviderCheckNames(verification hostedprovider.Verification) string {
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
