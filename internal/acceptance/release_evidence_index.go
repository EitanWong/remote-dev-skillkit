package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ReleaseEvidenceIndexSchemaVersion = "rdev.acceptance-release-evidence-index.v1"

type ReleaseEvidenceIndexOptions struct {
	OutDir                           string
	HostedProviderRuntimePackagePath string
	RelayAdapterPackagePaths         []string
	PostReleaseDownloadPackagePath   string
	Now                              time.Time
}

type ReleaseEvidenceIndex struct {
	SchemaVersion         string                     `json:"schema_version"`
	GeneratedAt           time.Time                  `json:"generated_at"`
	OK                    bool                       `json:"ok"`
	OutDir                string                     `json:"out_dir"`
	HostedProviderRuntime *ReleaseEvidenceIndexItem  `json:"hosted_provider_runtime,omitempty"`
	RelayAdapters         []ReleaseEvidenceIndexItem `json:"relay_adapters,omitempty"`
	PostReleaseDownload   *ReleaseEvidenceIndexItem  `json:"post_release_download,omitempty"`
	Checks                []Check                    `json:"checks"`
	IndexPath             string                     `json:"index_path"`
	ChecksumsPath         string                     `json:"checksums_path"`
	RequiredEvidence      []string                   `json:"required_evidence"`
	RecommendedActions    []string                   `json:"recommended_actions,omitempty"`
}

type ReleaseEvidenceIndexItem struct {
	Kind                        string   `json:"kind"`
	Present                     bool     `json:"present"`
	OK                          bool     `json:"ok"`
	PackageSchema               string   `json:"package_schema,omitempty"`
	VerificationSchema          string   `json:"verification_schema,omitempty"`
	PackageManifestSHA256       string   `json:"package_manifest_sha256,omitempty"`
	PackageManifestSizeBytes    int64    `json:"package_manifest_size_bytes,omitempty"`
	StorageProvider             string   `json:"storage_provider,omitempty"`
	AuthProvider                string   `json:"auth_provider,omitempty"`
	RuntimeClaim                string   `json:"runtime_claim,omitempty"`
	SelectedPath                string   `json:"selected_path,omitempty"`
	AcceptedPaths               []string `json:"accepted_paths,omitempty"`
	Repo                        string   `json:"repo,omitempty"`
	Tag                         string   `json:"tag,omitempty"`
	PlatformTargets             []string `json:"platform_targets,omitempty"`
	SkillkitIncluded            bool     `json:"skillkit_included,omitempty"`
	CheckCount                  int      `json:"check_count"`
	FailedCheckCount            int      `json:"failed_check_count"`
	FileCheckCount              int      `json:"file_check_count"`
	FailedFileCheckCount        int      `json:"failed_file_check_count"`
	FailedChecks                []string `json:"failed_checks,omitempty"`
	Checks                      []Check  `json:"checks,omitempty"`
	RecommendedActions          []string `json:"recommended_actions,omitempty"`
	VerificationError           string   `json:"verification_error,omitempty"`
	OperatorRunEvidenceRequired bool     `json:"operator_run_evidence_required"`
	ExternalMutationPerformed   bool     `json:"external_mutation_performed"`
}

func (i ReleaseEvidenceIndex) Complete() bool {
	if len(i.Checks) == 0 || i.HostedProviderRuntime == nil || i.PostReleaseDownload == nil || len(i.RelayAdapters) == 0 {
		return false
	}
	for _, check := range i.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func BuildReleaseEvidenceIndex(opts ReleaseEvidenceIndexOptions) (ReleaseEvidenceIndex, error) {
	if strings.TrimSpace(opts.OutDir) == "" {
		return ReleaseEvidenceIndex{}, fmt.Errorf("output directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ReleaseEvidenceIndex{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return ReleaseEvidenceIndex{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	index := ReleaseEvidenceIndex{
		SchemaVersion: ReleaseEvidenceIndexSchemaVersion,
		GeneratedAt:   now.UTC(),
		OutDir:        ".",
		IndexPath:     "release-evidence-index.json",
		ChecksumsPath: "checksums.txt",
		RequiredEvidence: []string{
			"verified hosted provider runtime acceptance package",
			"one or more verified relay/connectivity adapter acceptance packages from real restrictive-network runs",
			"verified post-release download acceptance package from public GitHub Release assets",
		},
	}
	add := func(name string, passed bool, detail string) {
		index.Checks = append(index.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	hosted := releaseEvidenceHostedProviderRuntimeItem(opts.HostedProviderRuntimePackagePath)
	index.HostedProviderRuntime = &hosted
	add("hosted_provider_runtime_package_present", hosted.Present, "")
	add("hosted_provider_runtime_package_verified", hosted.OK, strings.Join(hosted.FailedChecks, ","))

	relayPaths := compactReleaseEvidencePaths(opts.RelayAdapterPackagePaths)
	add("relay_adapter_package_present", len(relayPaths) > 0, fmt.Sprintf("%d", len(relayPaths)))
	allRelayOK := len(relayPaths) > 0
	for _, path := range relayPaths {
		item := releaseEvidenceRelayAdapterItem(path)
		index.RelayAdapters = append(index.RelayAdapters, item)
		if !item.OK {
			allRelayOK = false
		}
	}
	add("relay_adapter_packages_verified", allRelayOK, releaseEvidenceFailedKinds(index.RelayAdapters))

	postRelease := releaseEvidencePostReleaseDownloadItem(opts.PostReleaseDownloadPackagePath)
	index.PostReleaseDownload = &postRelease
	add("post_release_download_package_present", postRelease.Present, "")
	add("post_release_download_package_verified", postRelease.OK, strings.Join(postRelease.FailedChecks, ","))
	add("external_mutation_absent", true, "local verification/index only")
	add("release_evidence_index_written", true, index.IndexPath)
	add("checksums_written", true, index.ChecksumsPath)

	index.OK = index.Complete()
	if !index.OK {
		index.RecommendedActions = []string{
			"Collect the missing real acceptance packages and rerun release-evidence-index.",
			"Require hosted provider, relay/connectivity, and post-release download package verification to pass before making production release claims.",
			"Do not use this index as public release evidence until ok=true.",
		}
	}
	content, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return ReleaseEvidenceIndex{}, err
	}
	content = append(content, '\n')
	indexPath := filepath.Join(outDir, index.IndexPath)
	if err := os.WriteFile(indexPath, content, 0o600); err != nil {
		return ReleaseEvidenceIndex{}, err
	}
	sum := sha256.Sum256(content)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), index.IndexPath)
	if err := os.WriteFile(filepath.Join(outDir, index.ChecksumsPath), []byte(checksums), 0o600); err != nil {
		return ReleaseEvidenceIndex{}, err
	}
	return index, nil
}

func releaseEvidenceHostedProviderRuntimeItem(path string) ReleaseEvidenceIndexItem {
	item := releaseEvidenceBaseItem("hosted-provider-runtime", path)
	if !item.Present {
		return item
	}
	verification, err := VerifyHostedProviderRuntimeAcceptancePackage(path)
	if err != nil {
		item.VerificationError = err.Error()
		item.Checks = []Check{{Name: "verification_error", Passed: false, Detail: err.Error()}}
		item.FailedChecks = []string{"verification_error"}
		item.FailedCheckCount = 1
		return item
	}
	item.OK = verification.OK()
	item.PackageSchema = verification.PackageSchema
	item.VerificationSchema = verification.SchemaVersion
	item.StorageProvider = verification.StorageProvider
	item.AuthProvider = verification.AuthProvider
	item.RuntimeClaim = verification.RuntimeClaim
	item.Checks = verification.Checks
	item.RecommendedActions = verification.RecommendedActions
	item.CheckCount, item.FailedCheckCount, item.FailedChecks = releaseEvidenceCheckSummary(verification.Checks)
	item.FileCheckCount, item.FailedFileCheckCount = releaseEvidenceFileCheckSummary(verification.Files)
	if item.FailedFileCheckCount > 0 {
		item.OK = false
	}
	return item
}

func releaseEvidenceRelayAdapterItem(path string) ReleaseEvidenceIndexItem {
	item := releaseEvidenceBaseItem("relay-adapter", path)
	if !item.Present {
		return item
	}
	verification, err := VerifyRelayAdapterAcceptancePackage(path)
	if err != nil {
		item.VerificationError = err.Error()
		item.Checks = []Check{{Name: "verification_error", Passed: false, Detail: err.Error()}}
		item.FailedChecks = []string{"verification_error"}
		item.FailedCheckCount = 1
		return item
	}
	item.OK = verification.OK()
	item.PackageSchema = verification.PackageSchema
	item.VerificationSchema = verification.SchemaVersion
	item.SelectedPath = verification.SelectedPath
	item.AcceptedPaths = append([]string(nil), verification.AcceptedPaths...)
	item.Checks = verification.Checks
	item.RecommendedActions = verification.RecommendedActions
	item.CheckCount, item.FailedCheckCount, item.FailedChecks = releaseEvidenceCheckSummary(verification.Checks)
	item.FileCheckCount, item.FailedFileCheckCount = releaseEvidenceFileCheckSummary(verification.Files)
	if item.FailedFileCheckCount > 0 {
		item.OK = false
	}
	return item
}

func releaseEvidencePostReleaseDownloadItem(path string) ReleaseEvidenceIndexItem {
	item := releaseEvidenceBaseItem("post-release-download", path)
	if !item.Present {
		return item
	}
	verification, err := VerifyPostReleaseDownloadAcceptancePackage(path)
	if err != nil {
		item.VerificationError = err.Error()
		item.Checks = []Check{{Name: "verification_error", Passed: false, Detail: err.Error()}}
		item.FailedChecks = []string{"verification_error"}
		item.FailedCheckCount = 1
		return item
	}
	item.OK = verification.OK()
	item.PackageSchema = verification.PackageSchema
	item.VerificationSchema = verification.SchemaVersion
	item.Repo = verification.Repo
	item.Tag = verification.Tag
	item.PlatformTargets = append([]string(nil), verification.PlatformTargets...)
	item.SkillkitIncluded = verification.SkillkitIncluded
	item.Checks = verification.Checks
	item.RecommendedActions = verification.RecommendedActions
	item.CheckCount, item.FailedCheckCount, item.FailedChecks = releaseEvidenceCheckSummary(verification.Checks)
	item.FileCheckCount, item.FailedFileCheckCount = releaseEvidenceFileCheckSummary(verification.Files)
	if item.FailedFileCheckCount > 0 {
		item.OK = false
	}
	return item
}

func releaseEvidenceBaseItem(kind, path string) ReleaseEvidenceIndexItem {
	item := ReleaseEvidenceIndexItem{
		Kind:                        kind,
		OperatorRunEvidenceRequired: true,
		ExternalMutationPerformed:   false,
	}
	if strings.TrimSpace(path) == "" {
		item.Checks = []Check{{Name: kind + "_package_path_present", Passed: false}}
		item.FailedChecks = []string{kind + "_package_path_present"}
		item.FailedCheckCount = 1
		return item
	}
	manifestPath, _, err := resolveAcceptancePackageManifest(path)
	if err != nil {
		item.Present = false
		item.VerificationError = err.Error()
		item.Checks = []Check{{Name: kind + "_package_manifest_present", Passed: false, Detail: err.Error()}}
		item.FailedChecks = []string{kind + "_package_manifest_present"}
		item.FailedCheckCount = 1
		return item
	}
	item.Present = true
	if stat, err := os.Stat(manifestPath); err == nil {
		item.PackageManifestSizeBytes = stat.Size()
	}
	if content, err := os.ReadFile(manifestPath); err == nil {
		sum := sha256.Sum256(content)
		item.PackageManifestSHA256 = "sha256:" + hex.EncodeToString(sum[:])
	}
	return item
}

func releaseEvidenceCheckSummary(checks []Check) (int, int, []string) {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return len(checks), len(failed), failed
}

func releaseEvidenceFileCheckSummary(files []RelayPackageFileCheck) (int, int) {
	total := 0
	failed := 0
	for _, file := range files {
		for _, check := range file.Checks {
			total++
			if !check.Passed {
				failed++
			}
		}
	}
	return total, failed
}

func compactReleaseEvidencePaths(paths []string) []string {
	var out []string
	for _, path := range paths {
		for _, part := range strings.Split(path, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func releaseEvidenceFailedKinds(items []ReleaseEvidenceIndexItem) string {
	var failed []string
	for _, item := range items {
		if !item.OK {
			failed = append(failed, item.Kind)
		}
	}
	return strings.Join(failed, ",")
}
