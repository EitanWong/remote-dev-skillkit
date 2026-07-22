package acceptance

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

	"github.com/EitanWong/remote-dev-skillkit/internal/relayadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const RelayAdapterPackageSchemaVersion = "rdev.acceptance-package.relay-adapter.v1"
const RelayAdapterPackageVerificationSchemaVersion = "rdev.acceptance-verification.relay-adapter-package.v1"

var standardConnectivityAdapterPaths = []string{
	"existing-frp-or-chisel-relay",
	"existing-ssh-tunnel",
	"existing-headscale-tailscale-mesh",
	"existing-wireguard-vpn",
}

type RelayAdapterPackageOptions struct {
	RelayAdapterPackagePath string
	OutDir                  string
	EvidenceDirPath         string
	RunnerResultPath        string
	HelperTranscriptPath    string
	GatewayStatusPath       string
	HostStatusPath          string
	AuditPath               string
	ConnectionStatusPath    string
	NotesPath               string
	Now                     time.Time
}

type RelayAdapterAcceptancePackage struct {
	SchemaVersion       string                    `json:"schema_version"`
	GeneratedAt         time.Time                 `json:"generated_at"`
	OutDir              string                    `json:"out_dir"`
	RelayAdapterPackage string                    `json:"relay_adapter_package"`
	RelayAdapterSchema  string                    `json:"relay_adapter_schema"`
	RelayVerification   relayadapter.Verification `json:"relay_adapter_verification"`
	SelectedPath        string                    `json:"selected_path,omitempty"`
	AcceptedPaths       []string                  `json:"accepted_paths"`
	Checks              []Check                   `json:"checks"`
	Files               []AcceptancePackageFile   `json:"files"`
	RedactionRuleCounts map[string]int            `json:"redaction_rule_counts,omitempty"`
	RequiredEvidence    []string                  `json:"required_evidence"`
	RecommendedActions  []string                  `json:"recommended_actions,omitempty"`
}

type RelayAdapterAcceptanceVerification struct {
	SchemaVersion      string                  `json:"schema_version"`
	PackagePath        string                  `json:"package_path"`
	PackageSchema      string                  `json:"package_schema"`
	GeneratedAt        time.Time               `json:"generated_at"`
	SelectedPath       string                  `json:"selected_path,omitempty"`
	AcceptedPaths      []string                `json:"accepted_paths"`
	Checks             []Check                 `json:"checks"`
	Files              []RelayPackageFileCheck `json:"files"`
	RecommendedActions []string                `json:"recommended_actions,omitempty"`
}

type RelayPackageFileCheck struct {
	Path           string  `json:"path"`
	Kind           string  `json:"kind"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	ActualSHA256   string  `json:"actual_sha256,omitempty"`
	ExpectedSize   int     `json:"expected_size"`
	ActualSize     int     `json:"actual_size,omitempty"`
	Checks         []Check `json:"checks"`
}

func (p RelayAdapterAcceptancePackage) OK() bool {
	if len(p.Checks) == 0 || !p.RelayVerification.OK() {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (v RelayAdapterAcceptanceVerification) OK() bool {
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

func PackageRelayAdapterEvidence(opts RelayAdapterPackageOptions) (RelayAdapterAcceptancePackage, error) {
	if strings.TrimSpace(opts.RelayAdapterPackagePath) == "" {
		return RelayAdapterAcceptancePackage{}, fmt.Errorf("relay adapter package is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return RelayAdapterAcceptancePackage{}, fmt.Errorf("output directory is required")
	}
	var err error
	opts, err = resolveRelayEvidenceDir(opts)
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	relayPackagePath, relayPackageDir, err := resolveRelayAdapterPackage(opts.RelayAdapterPackagePath)
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	verification, err := relayadapter.Verify(relayPackageDir)
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	relayManifestContent, err := os.ReadFile(relayPackagePath)
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	var relayPkg relayadapter.Package
	if err := json.Unmarshal(relayManifestContent, &relayPkg); err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	pkg := RelayAdapterAcceptancePackage{
		SchemaVersion:       RelayAdapterPackageSchemaVersion,
		GeneratedAt:         now.UTC(),
		OutDir:              outDir,
		RelayAdapterPackage: relayPackagePath,
		RelayAdapterSchema:  relayPkg.SchemaVersion,
		RelayVerification:   verification,
		AcceptedPaths:       append([]string(nil), standardConnectivityAdapterPaths...),
		RequiredEvidence: []string{
			"verified relay-adapter.json",
			"Connection Entry runner dry-run or run result selecting a standard connectivity adapter path",
			"helper startup transcript or process supervisor evidence",
			"gateway status or health evidence",
			"host status or registration evidence",
			"support-session status or connection supervision evidence",
			"audit transcript covering helper start, host registration, and cleanup",
		},
	}
	add := func(name string, passed bool, detail string) {
		pkg.Checks = append(pkg.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	redactor := shelladapter.NewArtifactRedactor()
	add("relay_adapter_verification_ok", verification.OK(), failedRelayAdapterCheckNames(verification.Checks))

	var files []AcceptancePackageFile
	if entries, err := copyRelayAdapterPackageFiles(outDir, relayPackageDir, redactor); err != nil {
		add("relay_adapter_package_copied", false, err.Error())
	} else {
		files = append(files, entries...)
		add("relay_adapter_package_copied", len(entries) >= 3, fmt.Sprintf("%d", len(entries)))
	}
	if content, err := json.MarshalIndent(verification, "", "  "); err != nil {
		return RelayAdapterAcceptancePackage{}, err
	} else if entry, err := writePackageContent(outDir, "relay-adapter/verification.json", "relay-adapter-verification", append(content, '\n'), ""); err != nil {
		return RelayAdapterAcceptancePackage{}, err
	} else {
		files = append(files, entry)
	}

	files = append(files, copyRunnerResultEvidence(outDir, opts.RunnerResultPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/helper-transcript.txt", "helper-transcript", opts.HelperTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/gateway-status.json", "gateway-status", opts.GatewayStatusPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/host-status.json", "host-status", opts.HostStatusPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/connection-status.json", "connection-status", opts.ConnectionStatusPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/audit.jsonl", "audit", opts.AuditPath, redactor, add)...)
	files = append(files, copyNotesEvidence(outDir, opts.NotesPath, redactor, add)...)

	selectedPath := runnerResultSelectedConnectivityPath(outDir, "evidence/runner-result.json")
	pkg.SelectedPath = selectedPath
	add("runner_result_present", fileEntryKindPresent(files, "runner-result"), opts.RunnerResultPath)
	add("runner_selected_standard_connectivity_path", selectedPath != "", selectedPath)
	add("helper_transcript_present", fileEntryKindPresent(files, "helper-transcript"), opts.HelperTranscriptPath)
	add("gateway_status_present", fileEntryKindPresent(files, "gateway-status"), opts.GatewayStatusPath)
	add("host_status_present", fileEntryKindPresent(files, "host-status"), opts.HostStatusPath)
	add("connection_status_present", fileEntryKindPresent(files, "connection-status"), opts.ConnectionStatusPath)
	add("connection_status_connected", connectionStatusConnected(outDir, "evidence/connection-status.json"), opts.ConnectionStatusPath)
	add("audit_present", fileEntryKindPresent(files, "audit"), opts.AuditPath)
	add("relay_files_have_no_private_surface", relayPackageFilesHaveNoPrivateSurface(outDir, files), "")

	if redactor.Redacted() {
		pkg.RedactionRuleCounts = redactor.Counts()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	checksums, checksumEntry, err := writePackageChecksums(outDir, files)
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	files = append(files, checksumEntry)
	pkg.Files = files
	add("checksums_written", len(checksums) > 0, "checksums.txt")
	add("package_files_written", len(pkg.Files) >= 10, fmt.Sprintf("%d", len(pkg.Files)))
	if !pkg.OK() {
		pkg.RecommendedActions = []string{
			"Collect missing relay adapter evidence from the real restrictive-network run.",
			"Re-run package-relay-adapter after redacting helper transcripts, status output, and audit logs.",
			"Confirm the runner result selects one of the accepted standard connectivity adapter paths before publishing release evidence.",
		}
	}
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	content = append(content, '\n')
	if _, err := writePackageContent(outDir, "package.json", "package-manifest", content, ""); err != nil {
		return RelayAdapterAcceptancePackage{}, err
	}
	return pkg, nil
}

func copyRunnerResultEvidence(root, source string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) []AcceptancePackageFile {
	if strings.TrimSpace(source) == "" {
		add("runner-result_copied", false, "missing")
		return nil
	}
	content, err := os.ReadFile(source)
	if err == nil && acceptanceEvidencePath("evidence/runner-result.json") && evidenceContentIsPlaceholder(content) {
		err = fmt.Errorf("evidence placeholder must be replaced before packaging: %s", source)
	}
	if err == nil && redactor != nil {
		content = []byte(redactor.Redact(string(content)))
	}
	if err == nil {
		content, err = sanitizeRunnerResultEvidence(content)
	}
	if err != nil {
		add("runner-result_copied", false, packageEvidenceErrorDetail(err))
		return nil
	}
	entry, err := writePackageContent(root, "evidence/runner-result.json", "runner-result", content, source)
	if err != nil {
		add("runner-result_copied", false, packageEvidenceErrorDetail(err))
		return nil
	}
	add("runner-result_copied", true, entry.Path)
	return []AcceptancePackageFile{entry}
}

func sanitizeRunnerResultEvidence(content []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, fmt.Errorf("decode runner result evidence: %w", err)
	}
	value = sanitizeRunnerResultValue(value, "")
	clean, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode runner result evidence: %w", err)
	}
	return append(clean, '\n'), nil
}

func sanitizeRunnerResultValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for childKey, child := range typed {
			if runnerResultSensitiveKey(childKey) {
				clean[childKey] = runnerResultRedaction(childKey)
				continue
			}
			clean[childKey] = sanitizeRunnerResultValue(child, childKey)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, child := range typed {
			clean[index] = sanitizeRunnerResultValue(child, key)
		}
		return clean
	case string:
		return sanitizeRunnerResultString(typed, key)
	default:
		return value
	}
}

func runnerResultSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "selected_path" || lower == "path_id" {
		return false
	}
	return lower == "manifest_path" || lower == "workspace_root" || lower == "path" ||
		strings.HasSuffix(lower, "_path") || strings.HasSuffix(lower, "_url")
}

func runnerResultRedaction(key string) string {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), "_url") {
		return "[REDACTED:gateway_url]"
	}
	return "[REDACTED:private_path]"
}

func sanitizeRunnerResultString(value, key string) string {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), "_url") ||
		strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "[REDACTED:gateway_url]"
	}
	if runnerResultLooksLikePath(trimmed) || strings.Contains(lower, "/users/") ||
		strings.Contains(lower, "/home/") || strings.Contains(lower, `\users\`) {
		return "[REDACTED:private_path]"
	}
	if strings.Contains(lower, "://") {
		return "[REDACTED:gateway_url]"
	}
	return value
}

func runnerResultLooksLikePath(value string) bool {
	if value == "" || filepath.IsAbs(value) {
		return value != ""
	}
	return len(value) >= 3 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) &&
		value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func resolveRelayEvidenceDir(opts RelayAdapterPackageOptions) (RelayAdapterPackageOptions, error) {
	if strings.TrimSpace(opts.EvidenceDirPath) == "" {
		return opts, nil
	}
	dir, err := filepath.Abs(opts.EvidenceDirPath)
	if err != nil {
		return RelayAdapterPackageOptions{}, err
	}
	if info, err := os.Stat(dir); err != nil {
		return RelayAdapterPackageOptions{}, err
	} else if !info.IsDir() {
		return RelayAdapterPackageOptions{}, fmt.Errorf("relay evidence path is not a directory: %s", dir)
	}
	fill := func(current, name string) string {
		if strings.TrimSpace(current) != "" {
			return current
		}
		return filepath.Join(dir, name)
	}
	opts.RunnerResultPath = fill(opts.RunnerResultPath, "runner-result.json")
	opts.HelperTranscriptPath = fill(opts.HelperTranscriptPath, "helper-transcript.txt")
	opts.GatewayStatusPath = fill(opts.GatewayStatusPath, "gateway-status.json")
	opts.HostStatusPath = fill(opts.HostStatusPath, "host-status.json")
	opts.ConnectionStatusPath = fill(opts.ConnectionStatusPath, "connection-status.json")
	opts.AuditPath = fill(opts.AuditPath, "audit.jsonl")
	return opts, nil
}

func VerifyRelayAdapterAcceptancePackage(packagePath string) (RelayAdapterAcceptanceVerification, error) {
	manifestPath, dir, err := resolveAcceptancePackageManifest(packagePath)
	if err != nil {
		return RelayAdapterAcceptanceVerification{}, err
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return RelayAdapterAcceptanceVerification{}, err
	}
	var pkg RelayAdapterAcceptancePackage
	if err := json.Unmarshal(content, &pkg); err != nil {
		return RelayAdapterAcceptanceVerification{}, err
	}
	verification := RelayAdapterAcceptanceVerification{
		SchemaVersion: RelayAdapterPackageVerificationSchemaVersion,
		PackagePath:   manifestPath,
		PackageSchema: pkg.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		SelectedPath:  pkg.SelectedPath,
		AcceptedPaths: append([]string(nil), standardConnectivityAdapterPaths...),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	add("package_schema", pkg.SchemaVersion == RelayAdapterPackageSchemaVersion, pkg.SchemaVersion)
	add("package_checks_passed", allChecksPassed(pkg.Checks), failedCheckNames(pkg.Checks))
	add("relay_adapter_verification_ok", pkg.RelayVerification.OK(), failedRelayAdapterCheckNames(pkg.RelayVerification.Checks))
	add("required_evidence_declared", len(pkg.RequiredEvidence) >= 6, fmt.Sprintf("%d", len(pkg.RequiredEvidence)))
	verification.Files = verifyAcceptancePackageFiles(dir, pkg.Files)
	add("checksums_file_present", packagePathExists(pkg.Files, "checksums.txt"), "")
	add("runner_result_present", packageKindPresent(pkg.Files, "runner-result"), "")
	add("helper_transcript_present", packageKindPresent(pkg.Files, "helper-transcript"), "")
	add("gateway_status_present", packageKindPresent(pkg.Files, "gateway-status"), "")
	add("host_status_present", packageKindPresent(pkg.Files, "host-status"), "")
	add("connection_status_present", packageKindPresent(pkg.Files, "connection-status"), "")
	add("audit_present", packageKindPresent(pkg.Files, "audit"), "")
	selectedPath := runnerResultSelectedConnectivityPath(dir, "evidence/runner-result.json")
	if verification.SelectedPath == "" {
		verification.SelectedPath = selectedPath
	}
	add("runner_selected_standard_connectivity_path", selectedPath != "", selectedPath)
	add("connection_status_connected", connectionStatusConnected(dir, "evidence/connection-status.json"), "")
	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the relay adapter acceptance package from a complete real run.",
			"Confirm the runner result selects a standard connectivity adapter path and connection status reports connected=true.",
			"Do not attach this package to release evidence until verification passes.",
		}
	}
	return verification, nil
}

func resolveRelayAdapterPackage(path string) (string, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(abs, "relay-adapter.json"), abs, nil
	}
	return abs, filepath.Dir(abs), nil
}

func copyRelayAdapterPackageFiles(outDir, packageDir string, redactor *shelladapter.ArtifactRedactor) ([]AcceptancePackageFile, error) {
	var files []AcceptancePackageFile
	for _, name := range []string{"relay-adapter.json", "RELAY_ADAPTER.md", "runner.env.example"} {
		entry, err := copyPackageFile(outDir, filepath.ToSlash(filepath.Join("relay-adapter", name)), "relay-adapter", filepath.Join(packageDir, name), redactor)
		if err != nil {
			return files, err
		}
		files = append(files, entry)
	}
	return files, nil
}

func resolveAcceptancePackageManifest(path string) (string, string, error) {
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
		return filepath.Join(abs, "package.json"), abs, nil
	}
	return abs, filepath.Dir(abs), nil
}

func verifyAcceptancePackageFiles(root string, files []AcceptancePackageFile) []RelayPackageFileCheck {
	var checks []RelayPackageFileCheck
	seen := map[string]bool{}
	for _, file := range files {
		item := RelayPackageFileCheck{
			Path:           file.Path,
			Kind:           file.Kind,
			ExpectedSHA256: file.SHA256,
			ExpectedSize:   file.SizeBytes,
		}
		add := func(name string, passed bool, detail string) {
			item.Checks = append(item.Checks, Check{Name: name, Passed: passed, Detail: detail})
		}
		safe := safeAcceptancePath(file.Path)
		add("file_path_safe", safe, file.Path)
		add("file_path_unique", !seen[file.Path], file.Path)
		seen[file.Path] = true
		add("expected_sha256_format", strings.HasPrefix(file.SHA256, "sha256:") && len(strings.TrimPrefix(file.SHA256, "sha256:")) == 64, file.SHA256)
		if safe {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
			add("file_exists", err == nil, errorString(err))
			if err == nil {
				sum := fileSHA256Bytes(content)
				item.ActualSHA256 = "sha256:" + sum
				item.ActualSize = len(content)
				add("file_sha256_matches", item.ActualSHA256 == file.SHA256, file.SHA256)
				add("file_size_matches", item.ActualSize == file.SizeBytes, fmt.Sprintf("%d", file.SizeBytes))
				add("file_has_no_private_surface", relayAcceptanceNoPrivateSurface(content), file.Path)
				if acceptanceEvidencePath(file.Path) {
					add("file_not_placeholder", !evidenceContentIsPlaceholder(content), file.Path)
				}
			}
		}
		checks = append(checks, item)
	}
	return checks
}

func runnerResultSelectedConnectivityPath(root, path string) string {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	for _, accepted := range standardConnectivityAdapterPaths {
		if jsonContainsStringField(content, "selected_path", accepted) {
			return accepted
		}
	}
	return ""
}

func connectionStatusConnected(root, path string) bool {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		compact := strings.Join(strings.Fields(string(content)), "")
		return strings.Contains(compact, `"connected":true`) || strings.Contains(compact, `"event":"connected"`)
	}
	return jsonHasBoolField(value, "connected", true) || jsonHasStringField(value, "event", "connected")
}

func jsonContainsStringField(content []byte, key, expected string) bool {
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return strings.Contains(string(content), `"`+key+`"`) && strings.Contains(string(content), expected)
	}
	return jsonHasStringField(value, key, expected)
}

func jsonHasStringField(value any, key, expected string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[key].(string); ok && current == expected {
			return true
		}
		for _, child := range typed {
			if jsonHasStringField(child, key, expected) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonHasStringField(child, key, expected) {
				return true
			}
		}
	}
	return false
}

func jsonHasBoolField(value any, key string, expected bool) bool {
	switch typed := value.(type) {
	case map[string]any:
		if current, ok := typed[key].(bool); ok && current == expected {
			return true
		}
		for _, child := range typed {
			if jsonHasBoolField(child, key, expected) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonHasBoolField(child, key, expected) {
				return true
			}
		}
	}
	return false
}

func relayPackageFilesHaveNoPrivateSurface(root string, files []AcceptancePackageFile) bool {
	for _, file := range files {
		if file.Kind != "relay-adapter" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil || !relayAcceptanceNoPrivateSurface(content) {
			return false
		}
	}
	return true
}

func packageKindPresent(files []AcceptancePackageFile, kind string) bool {
	for _, file := range files {
		if file.Kind == kind && file.SizeBytes > 0 {
			return true
		}
	}
	return false
}

func safeAcceptancePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func relayAcceptanceNoPrivateSurface(content []byte) bool {
	lower := strings.ToLower(string(content))
	for _, marker := range []string{
		"begin private key",
		"api_key",
		"apikey",
		"password=",
		"secret=",
		"token=",
		"sk-",
		"192.168.",
		"10.0.",
		"10.1.",
		"172.16.",
		"/users/",
		"c:\\users\\",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func fileSHA256Bytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func failedRelayAdapterCheckNames(checks []relayadapter.Check) string {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}
