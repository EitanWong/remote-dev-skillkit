package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd"
	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd/windowsentry"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const WindowsTemporaryPlanVerificationSchemaVersion = "rdev.acceptance-verification.windows-temporary-plan.v1"
const WindowsLayeredEntryEvidenceSchemaVersion = "rdev.acceptance.windows-layered-entry-evidence.v1"

const maxWindowsLayeredRunReportBytes = 64 << 10

var windowsLayeredRunStageNames = []string{
	"manifest-fetch",
	"signature-verification",
	"runtime-download",
	"runtime-launch-preparation",
}

type windowsLayeredRunReport struct {
	SchemaVersion *string                         `json:"schema_version"`
	AssetID       *string                         `json:"asset_id"`
	FromCache     *bool                           `json:"from_cache"`
	Resumed       *bool                           `json:"resumed"`
	Bytes         *int64                          `json:"bytes"`
	Stages        *[]windowsLayeredRunReportStage `json:"stages"`
}

type windowsLayeredRunReportStage struct {
	Name       *string `json:"name"`
	DurationMS *int64  `json:"duration_ms"`
}

type windowsLayeredEntryEvidence struct {
	SchemaVersion           string   `json:"schema_version"`
	WindowsRelease          string   `json:"windows_release"`
	Architecture            string   `json:"architecture"`
	HandoffZIPSizeBytes     int64    `json:"handoff_zip_size_bytes"`
	HandoffZIPSHA256        string   `json:"handoff_zip_sha256"`
	SelectedLauncher        string   `json:"selected_launcher"`
	FallbackAttempts        []string `json:"fallback_attempts"`
	CoreStartCount          int      `json:"core_start_count"`
	NetworkBytes            int64    `json:"network_bytes"`
	RegistrationDurationMS  int64    `json:"registration_duration_ms"`
	CacheHit                bool     `json:"cache_hit"`
	RangeInterrupted        bool     `json:"range_interrupted"`
	RangeResumed            bool     `json:"range_resumed"`
	RangeBytes              int64    `json:"range_bytes"`
	PrivateACL              bool     `json:"private_acl"`
	UNCRejected             bool     `json:"unc_rejected"`
	ReparseRejected         bool     `json:"reparse_rejected"`
	DefenderLockVerified    bool     `json:"defender_lock_verified"`
	ActiveRouteFailed       bool     `json:"active_route_failed"`
	RouteReselected         bool     `json:"route_reselected"`
	RegistrationCount       int      `json:"registration_count"`
	SessionIdentityStable   bool     `json:"session_identity_stable"`
	EventCursorStable       bool     `json:"event_cursor_stable"`
	ArchiveRecoveryExecuted bool     `json:"archive_recovery_executed"`
	BootstrapOnly           bool     `json:"bootstrap_only"`
	PersistenceResidue      []string `json:"persistence_residue"`
	CleanupComplete         bool     `json:"cleanup_complete"`
}

type WindowsTemporaryPlanVerification struct {
	SchemaVersion      string    `json:"schema_version"`
	PlanPath           string    `json:"plan_path"`
	PlanSchema         string    `json:"plan_schema"`
	GeneratedAt        time.Time `json:"generated_at"`
	Checks             []Check   `json:"checks"`
	RecommendedActions []string  `json:"recommended_actions,omitempty"`
}

func (v WindowsTemporaryPlanVerification) OK() bool {
	if len(v.Checks) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func VerifyWindowsTemporaryPlan(planPath string) (WindowsTemporaryPlanVerification, error) {
	if strings.TrimSpace(planPath) == "" {
		return WindowsTemporaryPlanVerification{}, fmt.Errorf("plan path is required")
	}
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return WindowsTemporaryPlanVerification{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return WindowsTemporaryPlanVerification{}, err
	}
	plan, err := decodeWindowsTemporaryPlan(content)
	if err != nil {
		return WindowsTemporaryPlanVerification{}, err
	}
	baseDir := filepath.Dir(abs)
	archivePath := filepath.Join(baseDir, "Windows-ConnectionEntry.zip")
	archivePathSafe := plan.HandoffArchivePath == "Windows-ConnectionEntry.zip"
	var archiveSHA256 string
	var archiveSize int64
	var archiveErr error
	if archivePathSafe {
		_, _, archiveSHA256, archiveSize, archiveErr = inspectWindowsLayeredAcceptanceArchive(archivePath)
	} else {
		archiveErr = fmt.Errorf("handoff archive path is not the generated basename")
	}

	verification := WindowsTemporaryPlanVerification{
		SchemaVersion: WindowsTemporaryPlanVerificationSchemaVersion,
		PlanPath:      filepath.Base(abs),
		PlanSchema:    plan.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	add("plan_schema", plan.SchemaVersion == WindowsTemporaryPlanSchemaVersion, plan.SchemaVersion)
	add("plan_checks_passed", allChecksPassed(plan.Checks), failedCheckNames(plan.Checks))
	add("platform_windows_amd64", plan.Platform == "windows/amd64", plan.Platform)
	add("handoff_archive_path_safe", archivePathSafe, plan.HandoffArchivePath)
	add("handoff_archive_exists", archiveErr == nil, filepath.Base(archivePath))
	add("handoff_archive_private_file_mode", archiveErr == nil && fileModePrivate(archivePath), filepath.Base(archivePath))
	add("handoff_archive_sha256_matches", archiveErr == nil && strings.EqualFold(archiveSHA256, plan.HandoffArchiveSHA256), plan.HandoffArchiveSHA256)
	add("handoff_archive_size_matches", archiveErr == nil && archiveSize == plan.HandoffArchiveSizeBytes && archiveSize <= maxWindowsTemporaryHandoffBytes, fmt.Sprintf("%d", plan.HandoffArchiveSizeBytes))
	add("powershell_launcher_preferred", plan.PowerShellLauncher == "Start-ConnectionEntry.ps1" && plan.PreferredLauncher == "powershell", plan.PreferredLauncher)
	add("command_launcher_present", plan.CommandLauncher == "Start-ConnectionEntry.cmd", plan.CommandLauncher)
	add("fallback_order", slices.Equal(plan.FallbackOrder, []string{"powershell", "powershell-bypass", "cmd"}), strings.Join(plan.FallbackOrder, ","))
	add("bootstrap_only", plan.BootstrapCommand == "rdev-bootstrap layered-run", plan.BootstrapCommand)
	add("archive_recovery_not_automatic", !plan.ArchiveRecoveryAutomatic, "")
	add("foreground_command_present", commandNamed(plan.Commands, "run_foreground_temporary_host"), "")
	add("transcript_commands_present", commandNamed(plan.Commands, "start_transcript") && commandNamed(plan.Commands, "stop_transcript"), "")
	add("no_persistence_checks_complete", windowsNoPersistenceChecksComplete(plan.NoPersistenceChecks), missingWindowsNoPersistenceChecks(plan.NoPersistenceChecks))
	add("denial_probes_complete", windowsDenialProbesComplete(plan.DenialProbes), missingWindowsDenialProbes(plan.DenialProbes))
	add("required_evidence_complete", windowsTemporaryRequiredEvidenceComplete(plan.RequiredEvidence), missingWindowsTemporaryRequiredEvidence(plan.RequiredEvidence))

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the Windows temporary acceptance plan in a fresh output directory.",
			"Inspect the measured handoff archive and its two visible launchers before sending it to a target user.",
			"Do not run or publish this Windows acceptance plan until verification passes.",
		}
	}
	return verification, nil
}

func decodeWindowsTemporaryPlan(content []byte) (WindowsTemporaryPlan, error) {
	if len(content) == 0 {
		return WindowsTemporaryPlan{}, fmt.Errorf("Windows temporary plan is empty")
	}
	if err := rejectDuplicateJSONKeys(content); err != nil {
		return WindowsTemporaryPlan{}, fmt.Errorf("invalid Windows temporary plan JSON: %w", err)
	}
	if detail := windowsLayeredRunPrivateContentDetail(content); detail != "" {
		return WindowsTemporaryPlan{}, fmt.Errorf("Windows temporary plan contains %s", detail)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var plan WindowsTemporaryPlan
	if err := decoder.Decode(&plan); err != nil {
		return WindowsTemporaryPlan{}, fmt.Errorf("decode Windows temporary plan: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	return plan, nil
}

func validateWindowsLayeredRunReport(content []byte, expectedFromCache bool) error {
	if len(content) == 0 {
		return fmt.Errorf("layered run report is empty")
	}
	if len(content) > maxWindowsLayeredRunReportBytes {
		return fmt.Errorf("layered run report exceeds %d bytes", maxWindowsLayeredRunReportBytes)
	}
	if err := rejectDuplicateJSONKeys(content); err != nil {
		return fmt.Errorf("invalid layered run report JSON: %w", err)
	}
	if detail := windowsLayeredRunPrivateContentDetail(content); detail != "" {
		return fmt.Errorf("layered run report contains %s", detail)
	}
	reportSchema, err := validateWindowsLayeredRunFieldNames(content)
	if err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var report windowsLayeredRunReport
	if err := decoder.Decode(&report); err != nil {
		return fmt.Errorf("decode layered run report: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	if report.SchemaVersion == nil || *report.SchemaVersion != reportSchema {
		return fmt.Errorf("unsupported layered run report schema")
	}
	if report.AssetID == nil || *report.AssetID != "rdev-host-windows-amd64" {
		return fmt.Errorf("unexpected Windows layered runtime asset_id")
	}
	if report.FromCache == nil {
		return fmt.Errorf("from_cache is required")
	}
	if *report.FromCache != expectedFromCache {
		return fmt.Errorf("from_cache=%t, want %t", *report.FromCache, expectedFromCache)
	}
	if report.Resumed == nil {
		return fmt.Errorf("resumed is required")
	}
	if expectedFromCache && *report.Resumed {
		return fmt.Errorf("resumed must be false when from_cache=true")
	}
	if report.Bytes == nil || *report.Bytes <= 0 {
		return fmt.Errorf("bytes must be positive")
	}
	if reportSchema == windowsentry.RunReportSchemaVersion {
		return nil
	}
	if report.Stages == nil {
		return fmt.Errorf("stages are required")
	}
	stages := *report.Stages
	for index, expectedName := range windowsLayeredRunStageNames {
		if index >= len(stages) {
			return fmt.Errorf("stage %d must be %q: missing", index, expectedName)
		}
		stage := stages[index]
		if stage.Name == nil || *stage.Name != expectedName {
			return fmt.Errorf("stage %d must be %q", index, expectedName)
		}
		if stage.DurationMS == nil {
			return fmt.Errorf("stage %q duration_ms is required", expectedName)
		}
		if *stage.DurationMS < 0 {
			return fmt.Errorf("stage %q duration_ms must be nonnegative", expectedName)
		}
	}
	if len(stages) != len(windowsLayeredRunStageNames) {
		return fmt.Errorf("layered run report must contain exactly %d stages", len(windowsLayeredRunStageNames))
	}
	return nil
}

func validateWindowsLayeredEntryEvidence(content []byte, plan WindowsTemporaryPlan) error {
	if len(content) == 0 || len(content) > maxWindowsLayeredRunReportBytes {
		return fmt.Errorf("layered entry evidence is empty or exceeds %d bytes", maxWindowsLayeredRunReportBytes)
	}
	if err := rejectDuplicateJSONKeys(content); err != nil {
		return fmt.Errorf("invalid layered entry evidence JSON: %w", err)
	}
	if detail := windowsLayeredRunPrivateContentDetail(content); detail != "" {
		return fmt.Errorf("layered entry evidence contains %s", detail)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil || fields == nil {
		return fmt.Errorf("layered entry evidence must be a JSON object")
	}
	if err := requireExactJSONFields(fields, []string{
		"schema_version",
		"windows_release",
		"architecture",
		"handoff_zip_size_bytes",
		"handoff_zip_sha256",
		"selected_launcher",
		"fallback_attempts",
		"core_start_count",
		"network_bytes",
		"registration_duration_ms",
		"cache_hit",
		"range_interrupted",
		"range_resumed",
		"range_bytes",
		"private_acl",
		"unc_rejected",
		"reparse_rejected",
		"defender_lock_verified",
		"active_route_failed",
		"route_reselected",
		"registration_count",
		"session_identity_stable",
		"event_cursor_stable",
		"archive_recovery_executed",
		"bootstrap_only",
		"persistence_residue",
		"cleanup_complete",
	}, "layered entry evidence"); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var evidence windowsLayeredEntryEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return fmt.Errorf("decode layered entry evidence: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	if evidence.SchemaVersion != WindowsLayeredEntryEvidenceSchemaVersion {
		return fmt.Errorf("unsupported layered entry evidence schema")
	}
	if evidence.WindowsRelease != "10" && evidence.WindowsRelease != "11" {
		return fmt.Errorf("windows_release must be 10 or 11")
	}
	if evidence.Architecture != "amd64" {
		return fmt.Errorf("architecture must be amd64")
	}
	if evidence.HandoffZIPSizeBytes <= 0 || evidence.HandoffZIPSizeBytes > maxWindowsTemporaryHandoffBytes ||
		evidence.HandoffZIPSizeBytes != plan.HandoffArchiveSizeBytes {
		return fmt.Errorf("handoff ZIP size does not match the measured plan")
	}
	if !isHexSHA256(evidence.HandoffZIPSHA256) || !strings.EqualFold(evidence.HandoffZIPSHA256, plan.HandoffArchiveSHA256) {
		return fmt.Errorf("handoff ZIP SHA-256 does not match the measured plan")
	}
	expectedAttempts := map[string][]string{
		"powershell":        {"powershell"},
		"powershell-bypass": {"powershell", "powershell-bypass"},
		"cmd":               {"powershell", "powershell-bypass", "cmd"},
	}[evidence.SelectedLauncher]
	if expectedAttempts == nil || !slices.Equal(evidence.FallbackAttempts, expectedAttempts) {
		return fmt.Errorf("fallback attempts are unordered or do not end at selected_launcher")
	}
	if evidence.CoreStartCount != 1 {
		return fmt.Errorf("core_start_count must be exactly one")
	}
	if evidence.NetworkBytes < 0 {
		return fmt.Errorf("network_bytes must be nonnegative")
	}
	if evidence.RegistrationDurationMS < 0 {
		return fmt.Errorf("registration duration must be nonnegative")
	}
	if !evidence.RangeInterrupted || !evidence.RangeResumed || evidence.RangeBytes <= 0 {
		return fmt.Errorf("Range interruption and resume evidence is required")
	}
	if !evidence.PrivateACL {
		return fmt.Errorf("private ACL evidence is required")
	}
	if !evidence.UNCRejected {
		return fmt.Errorf("UNC rejection evidence is required")
	}
	if !evidence.ReparseRejected {
		return fmt.Errorf("reparse rejection evidence is required")
	}
	if !evidence.DefenderLockVerified {
		return fmt.Errorf("Defender/file-lock evidence is required")
	}
	if !evidence.ActiveRouteFailed || !evidence.RouteReselected || !evidence.SessionIdentityStable || !evidence.EventCursorStable {
		return fmt.Errorf("route reselection continuity evidence is required")
	}
	if evidence.RegistrationCount != 1 {
		return fmt.Errorf("registration_count must be exactly one")
	}
	if evidence.ArchiveRecoveryExecuted {
		return fmt.Errorf("archive recovery must not execute automatically")
	}
	if !evidence.BootstrapOnly {
		return fmt.Errorf("bootstrap_only must be true")
	}
	if len(evidence.PersistenceResidue) != 0 {
		return fmt.Errorf("persistence residue must be empty")
	}
	if !evidence.CleanupComplete {
		return fmt.Errorf("cleanup_complete must be true")
	}
	return nil
}

func decodeValidatedWindowsLayeredRunReport(content []byte, expectedFromCache bool) (windowsLayeredRunReport, error) {
	if err := validateWindowsLayeredRunReport(content, expectedFromCache); err != nil {
		return windowsLayeredRunReport{}, err
	}
	var report windowsLayeredRunReport
	if err := json.Unmarshal(content, &report); err != nil {
		return windowsLayeredRunReport{}, fmt.Errorf("decode validated layered run report")
	}
	return report, nil
}

func validateWindowsLayeredRunFieldNames(content []byte) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil || fields == nil {
		return "", fmt.Errorf("layered run report must be a JSON object")
	}
	var schema string
	if err := json.Unmarshal(fields["schema_version"], &schema); err != nil {
		return "", fmt.Errorf("layered run report schema_version must be a string")
	}
	required := []string{
		"schema_version",
		"asset_id",
		"from_cache",
		"resumed",
		"bytes",
	}
	switch schema {
	case bootstrapcmd.LayeredRunReportSchemaVersion:
		required = append(required, "stages")
	case windowsentry.RunReportSchemaVersion:
	default:
		return "", fmt.Errorf("unsupported layered run report schema")
	}
	if err := requireExactJSONFields(fields, required, "layered run report"); err != nil {
		return "", err
	}
	if schema == windowsentry.RunReportSchemaVersion {
		return schema, nil
	}

	var stages []json.RawMessage
	if err := json.Unmarshal(fields["stages"], &stages); err != nil {
		return "", fmt.Errorf("layered run report stages must be an array")
	}
	for _, content := range stages {
		var stageFields map[string]json.RawMessage
		if err := json.Unmarshal(content, &stageFields); err != nil || stageFields == nil {
			return "", fmt.Errorf("layered run report stage must be a JSON object")
		}
		if err := requireExactJSONFields(stageFields, []string{"name", "duration_ms"}, "layered run report stage"); err != nil {
			return "", err
		}
	}
	return schema, nil
}

func requireExactJSONFields(fields map[string]json.RawMessage, required []string, context string) error {
	allowed := make(map[string]bool, len(required))
	for _, name := range required {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return fmt.Errorf("unknown field in %s", context)
		}
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing %s field %q", context, name)
		}
	}
	return nil
}

func rejectDuplicateJSONKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := walkUniqueJSONValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func walkUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON object key")
			}
			seen[key] = true
			if err := walkUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("layered run report contains trailing JSON")
		}
		return fmt.Errorf("decode trailing layered run report content: %w", err)
	}
	return nil
}

func windowsLayeredRunPrivateContentDetail(content []byte) string {
	lower := strings.ToLower(string(content))
	for _, forbidden := range []struct {
		detail   string
		patterns []string
	}{
		{detail: "ticket pattern", patterns: []string{`"ticket`, "ticket_code=", "ticket-code=", "ticket="}},
		{detail: "gateway pattern", patterns: []string{`"gateway`, "gateway_url=", "gateway-url=", "gateway="}},
		{detail: "token pattern", patterns: []string{`"token`, `"access_token`, "token=", "access_token="}},
		{detail: "private path", patterns: []string{`c:\\users\\`, `:\\users\\`, `:\\documents and settings\\`, "/users/", "/home/", `\\\\`}},
		{detail: "redacted content", patterns: []string{"[redacted:", `"redacted"`, `"redaction_counts"`}},
	} {
		for _, pattern := range forbidden.patterns {
			if strings.Contains(lower, pattern) {
				return forbidden.detail
			}
		}
	}
	redactor := shelladapter.NewArtifactRedactor()
	_ = redactor.Redact(string(content))
	if redactor.Redacted() {
		return "token or secret pattern"
	}
	return ""
}

func windowsTemporaryRequiredEvidenceComplete(evidence []string) bool {
	required := windowsTemporaryRequiredEvidence()
	return len(evidence) == len(required) && missingWindowsTemporaryRequiredEvidence(evidence) == ""
}

func missingWindowsTemporaryRequiredEvidence(evidence []string) string {
	seen := make(map[string]bool, len(evidence))
	for _, item := range evidence {
		seen[item] = true
	}
	var missing []string
	requiredEvidence := windowsTemporaryRequiredEvidence()
	for _, required := range requiredEvidence {
		if !seen[required] {
			missing = append(missing, requiredEvidenceName(required))
		}
	}
	if len(evidence) != len(requiredEvidence) && len(missing) == 0 {
		missing = append(missing, "unexpected-or-duplicate-evidence")
	}
	return strings.Join(missing, ",")
}

func requiredEvidenceName(requirement string) string {
	if strings.Contains(requirement, "cold-layered-run.json") {
		return "cold-layered-run.json"
	}
	if strings.Contains(requirement, "warm-layered-run.json") {
		return "warm-layered-run.json"
	}
	words := strings.Fields(requirement)
	if len(words) == 0 {
		return "required-evidence"
	}
	return strings.Trim(words[0], "`.,")
}

func resolvePlanPath(baseDir, path, fallback string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(path) != "" {
		return filepath.Join(baseDir, path)
	}
	return filepath.Join(baseDir, fallback)
}

func fileModePrivate(path string) bool {
	return windowsentry.ValidatePrivatePath(path, false) == nil
}

func commandNamed(commands []WindowsAcceptanceCommand, name string) bool {
	for _, command := range commands {
		if command.Name == name {
			return true
		}
	}
	return false
}

func windowsNoPersistenceChecksComplete(commands []WindowsAcceptanceCommand) bool {
	return missingWindowsNoPersistenceChecks(commands) == ""
}

func missingWindowsNoPersistenceChecks(commands []WindowsAcceptanceCommand) string {
	return missingNames(windowsCommandNames(commands), []string{
		"services",
		"scheduled_tasks",
		"hkcu_run_key",
		"hklm_run_key",
		"startup_folders",
		"firewall_rules",
	})
}

func windowsDenialProbesComplete(probes []WindowsDenialProbe) bool {
	return missingWindowsDenialProbes(probes) == ""
}

func missingWindowsDenialProbes(probes []WindowsDenialProbe) string {
	seen := map[string]bool{}
	for _, probe := range probes {
		if probe.ExpectedArtifact == "rdev.host-denial.v1" {
			seen[probe.Operation] = true
		}
	}
	var missing []string
	for _, required := range []string{
		"package.install",
		"elevation.request",
		"service.manage",
		"gui.control",
		"credential.change",
	} {
		if !seen[required] {
			missing = append(missing, required)
		}
	}
	return strings.Join(missing, ",")
}

func windowsCommandNames(commands []WindowsAcceptanceCommand) map[string]bool {
	names := map[string]bool{}
	for _, command := range commands {
		names[command.Name] = true
	}
	return names
}

func missingNames(seen map[string]bool, required []string) string {
	var missing []string
	for _, name := range required {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	return strings.Join(missing, ",")
}
