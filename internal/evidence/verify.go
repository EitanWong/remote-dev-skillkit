package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
)

const VerificationSchemaVersion = "rdev.evidence-verification.v1"

type VerificationReport struct {
	SchemaVersion string              `json:"schema_version"`
	BundleDir     string              `json:"bundle_dir"`
	Manifest      Manifest            `json:"manifest"`
	Checks        []VerificationCheck `json:"checks"`
}

type VerificationCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func (r VerificationReport) OK() bool {
	if len(r.Checks) == 0 {
		return false
	}
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func VerifyDirectory(dir string) (VerificationReport, error) {
	if strings.TrimSpace(dir) == "" {
		return VerificationReport{}, fmt.Errorf("bundle directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return VerificationReport{}, err
	}
	report := VerificationReport{
		SchemaVersion: VerificationSchemaVersion,
		BundleDir:     abs,
	}
	manifestPath := filepath.Join(abs, "manifest.json")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(content, &report.Manifest); err != nil {
		return report, err
	}

	report.add("manifest_schema", report.Manifest.SchemaVersion == BundleSchemaVersion, report.Manifest.SchemaVersion)
	report.add("manifest_job_id", strings.TrimSpace(report.Manifest.JobID) != "", report.Manifest.JobID)
	report.add("manifest_omits_self", !manifestIncludesPath(report.Manifest, "manifest.json"), "")

	entriesByPath, duplicatePaths := indexManifestFiles(report.Manifest.Files)
	report.add("manifest_paths_unique", len(duplicatePaths) == 0, strings.Join(duplicatePaths, ","))
	report.add("manifest_has_checksums", entriesByPath["checksums.txt"] != nil, "")

	fileFailures := verifyManifestFiles(abs, report.Manifest.Files)
	report.add("manifest_files_verified", len(fileFailures) == 0, strings.Join(fileFailures, "; "))

	checksumFailures := verifyChecksumsFile(abs, report.Manifest.Files)
	report.add("checksums_file_verified", len(checksumFailures) == 0, strings.Join(checksumFailures, "; "))

	artifactFailures := verifyArtifactIndex(abs, entriesByPath)
	report.add("artifact_index_verified", len(artifactFailures) == 0, strings.Join(artifactFailures, "; "))

	auditOK, auditDetail := verifyAuditEvidence(abs, report.Manifest)
	report.add("audit_chain_verified", auditOK, auditDetail)

	return report, nil
}

func (r *VerificationReport) add(name string, passed bool, detail string) {
	r.Checks = append(r.Checks, VerificationCheck{Name: name, Passed: passed, Detail: detail})
}

func manifestIncludesPath(manifest Manifest, path string) bool {
	for _, entry := range manifest.Files {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func indexManifestFiles(entries []FileEntry) (map[string]*FileEntry, []string) {
	index := map[string]*FileEntry{}
	var duplicates []string
	for i := range entries {
		entry := entries[i]
		if existing := index[entry.Path]; existing != nil {
			duplicates = append(duplicates, entry.Path)
			continue
		}
		index[entry.Path] = &entries[i]
	}
	sort.Strings(duplicates)
	return index, duplicates
}

func verifyManifestFiles(root string, entries []FileEntry) []string {
	var failures []string
	for _, entry := range entries {
		if err := validateBundlePath(entry.Path); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.Path, err))
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", entry.Path, err))
			continue
		}
		if got := sha256String(content); got != entry.SHA256 {
			failures = append(failures, fmt.Sprintf("%s sha256 mismatch", entry.Path))
		}
		if len(content) != entry.SizeBytes {
			failures = append(failures, fmt.Sprintf("%s size mismatch", entry.Path))
		}
		if strings.TrimSpace(entry.Kind) == "" {
			failures = append(failures, fmt.Sprintf("%s missing kind", entry.Path))
		}
	}
	return failures
}

func verifyChecksumsFile(root string, entries []FileEntry) []string {
	content, err := os.ReadFile(filepath.Join(root, "checksums.txt"))
	if err != nil {
		return []string{err.Error()}
	}
	expected := map[string]string{}
	for _, entry := range entries {
		if entry.Path == "checksums.txt" {
			continue
		}
		expected[entry.Path] = strings.TrimPrefix(entry.SHA256, "sha256:")
	}
	actual := map[string]string{}
	for lineNumber, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return []string{fmt.Sprintf("line %d has invalid checksum format", lineNumber+1)}
		}
		actual[parts[1]] = parts[0]
	}
	var failures []string
	for path, expectedHash := range expected {
		if actual[path] != expectedHash {
			failures = append(failures, fmt.Sprintf("%s checksum mismatch", path))
		}
	}
	for path := range actual {
		if _, ok := expected[path]; !ok {
			failures = append(failures, fmt.Sprintf("%s unexpected checksum entry", path))
		}
	}
	sort.Strings(failures)
	return failures
}

func verifyArtifactIndex(root string, entriesByPath map[string]*FileEntry) []string {
	content, err := os.ReadFile(filepath.Join(root, "artifacts.json"))
	if err != nil {
		return []string{err.Error()}
	}
	var records []ArtifactRecord
	if err := json.Unmarshal(content, &records); err != nil {
		return []string{err.Error()}
	}
	var failures []string
	for _, record := range records {
		entry := entriesByPath[record.Path]
		if entry == nil {
			failures = append(failures, fmt.Sprintf("%s missing manifest entry", record.Path))
			continue
		}
		if entry.SHA256 != record.ContentSHA256 {
			failures = append(failures, fmt.Sprintf("%s artifact sha mismatch", record.Path))
		}
		if entry.SizeBytes != record.ContentBytes {
			failures = append(failures, fmt.Sprintf("%s artifact size mismatch", record.Path))
		}
		if record.JobID == "" || record.ID == "" {
			failures = append(failures, fmt.Sprintf("%s missing artifact identity", record.Path))
		}
	}
	sort.Strings(failures)
	return failures
}

func verifyAuditEvidence(root string, manifest Manifest) (bool, string) {
	events, err := audit.ReadJSONL(filepath.Join(root, "audit-slice.jsonl"))
	if err != nil {
		return false, err.Error()
	}
	if len(events) != manifest.AuditEventCount {
		return false, fmt.Sprintf("audit slice count %d != manifest count %d", len(events), manifest.AuditEventCount)
	}
	chain, err := audit.ReadChain(filepath.Join(root, "audit-chain.json"))
	if err != nil {
		return false, err.Error()
	}
	if err := audit.VerifyChain(chain); err != nil {
		return false, err.Error()
	}
	if chain.EventCount != manifest.AuditEventCount {
		return false, fmt.Sprintf("audit chain count %d != manifest count %d", chain.EventCount, manifest.AuditEventCount)
	}
	if chain.RootHash != manifest.AuditRootHash {
		return false, "audit root hash mismatch"
	}
	return true, chain.RootHash
}

func validateBundlePath(path string) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return fmt.Errorf("invalid bundle path")
	}
	if filepath.ToSlash(clean) != path {
		return fmt.Errorf("bundle path is not clean")
	}
	return nil
}

func sha256String(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
