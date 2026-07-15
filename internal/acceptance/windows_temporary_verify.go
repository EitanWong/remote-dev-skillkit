package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/bootstrapcmd"
	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const WindowsTemporaryPlanVerificationSchemaVersion = "rdev.acceptance-verification.windows-temporary-plan.v1"

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
	var plan WindowsTemporaryPlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return WindowsTemporaryPlanVerification{}, err
	}
	baseDir := filepath.Dir(abs)
	launcherPath := resolvePlanPath(baseDir, plan.LauncherPath, "run-windows-temporary.ps1")
	launcherContent, launcherErr := os.ReadFile(launcherPath)
	launcherText := string(launcherContent)

	verification := WindowsTemporaryPlanVerification{
		SchemaVersion: WindowsTemporaryPlanVerificationSchemaVersion,
		PlanPath:      abs,
		PlanSchema:    plan.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	add("plan_schema", plan.SchemaVersion == WindowsTemporaryPlanSchemaVersion, plan.SchemaVersion)
	add("plan_checks_passed", allChecksPassed(plan.Checks), failedCheckNames(plan.Checks))
	add("platform_windows", plan.Platform == "windows", plan.Platform)
	add("launcher_exists", launcherErr == nil, launcherPath)
	add("launcher_private_file_mode", fileModePrivate(launcherPath), launcherPath)
	add("launcher_matches_plan", launcherErr == nil && launcherMatchesWindowsPlan(launcherText, plan), "")
	add("launcher_has_no_forbidden_side_effects", launcherErr == nil && !containsForbiddenWindowsLauncherOperation(launcherText), forbiddenWindowsLauncherDetail(launcherText))
	add("bootstrap_hash_pinned_or_matches", bootstrapHashPinnedOrMatches(plan), plan.BootstrapScriptSHA256)
	add("host_sha256_valid", isHexSHA256(plan.HostExpectedSHA256), plan.HostExpectedSHA256)
	add("verifier_sha256_valid", isHexSHA256(plan.VerifierExpectedSHA256), plan.VerifierExpectedSHA256)
	add("release_manifest_or_bundle_present", plan.ReleaseManifestURL != "" || plan.ReleaseBundleURL != "", firstNonEmptyString(plan.ReleaseBundleURL, plan.ReleaseManifestURL))
	add("release_bundle_required_artifacts_present", plan.ReleaseBundleURL == "" || plan.ReleaseBundleRequiredArtifacts != "", plan.ReleaseBundleRequiredArtifacts)
	add("release_root_present", plan.ReleaseRootPublicKey != "", "")
	add("verifier_download_present", plan.VerifierDownloadURL != "", plan.VerifierDownloadURL)
	add("foreground_command_present", commandNamed(plan.Commands, "run_foreground_temporary_host"), "")
	add("transcript_commands_present", commandNamed(plan.Commands, "start_transcript") && commandNamed(plan.Commands, "stop_transcript"), "")
	add("no_persistence_checks_complete", windowsNoPersistenceChecksComplete(plan.NoPersistenceChecks), missingWindowsNoPersistenceChecks(plan.NoPersistenceChecks))
	add("denial_probes_complete", windowsDenialProbesComplete(plan.DenialProbes), missingWindowsDenialProbes(plan.DenialProbes))
	add("required_evidence_complete", windowsTemporaryRequiredEvidenceComplete(plan.RequiredEvidence), missingWindowsTemporaryRequiredEvidence(plan.RequiredEvidence))

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the Windows temporary acceptance plan in a fresh output directory.",
			"Inspect run-windows-temporary.ps1 for unexpected side effects before sending it to a target user.",
			"Do not run or publish this Windows acceptance plan until verification passes.",
		}
	}
	return verification, nil
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
	if err := validateWindowsLayeredRunFieldNames(content); err != nil {
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
	if report.SchemaVersion == nil || *report.SchemaVersion != bootstrapcmd.LayeredRunReportSchemaVersion {
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

func validateWindowsLayeredRunFieldNames(content []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil || fields == nil {
		return fmt.Errorf("layered run report must be a JSON object")
	}
	if err := requireExactJSONFields(fields, []string{
		"schema_version",
		"asset_id",
		"from_cache",
		"resumed",
		"bytes",
		"stages",
	}, "layered run report"); err != nil {
		return err
	}

	var stages []json.RawMessage
	if err := json.Unmarshal(fields["stages"], &stages); err != nil {
		return fmt.Errorf("layered run report stages must be an array")
	}
	for _, content := range stages {
		var stageFields map[string]json.RawMessage
		if err := json.Unmarshal(content, &stageFields); err != nil || stageFields == nil {
			return fmt.Errorf("layered run report stage must be a JSON object")
		}
		if err := requireExactJSONFields(stageFields, []string{"name", "duration_ms"}, "layered run report stage"); err != nil {
			return err
		}
	}
	return nil
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
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o077 == 0
}

func launcherMatchesWindowsPlan(launcher string, plan WindowsTemporaryPlan) bool {
	required := []string{
		"-GatewayUrl " + powershellQuote(plan.GatewayURL),
		"-TicketCode " + powershellQuote(plan.TicketCode),
		"-DownloadUrl " + powershellQuote(plan.HostDownloadURL),
		"-ExpectedSha256 " + powershellQuote(plan.HostExpectedSHA256),
		"-ReleaseRootPublicKey " + powershellQuote(plan.ReleaseRootPublicKey),
		"-VerifierDownloadUrl " + powershellQuote(plan.VerifierDownloadURL),
		"-VerifierExpectedSha256 " + powershellQuote(plan.VerifierExpectedSHA256),
	}
	if plan.ReleaseManifestURL != "" {
		required = append(required, "-ReleaseManifestUrl "+powershellQuote(plan.ReleaseManifestURL))
	}
	if plan.ReleaseBundleURL != "" {
		required = append(required, "-ReleaseBundleUrl "+powershellQuote(plan.ReleaseBundleURL))
		if plan.ReleaseBundleRequiredArtifacts != "" {
			required = append(required, "-ReleaseBundleRequiredArtifacts "+powershellQuote(plan.ReleaseBundleRequiredArtifacts))
		}
	}
	for _, value := range required {
		if !strings.Contains(launcher, value) {
			return false
		}
	}
	return strings.Contains(launcher, "& $bootstrap")
}

func containsForbiddenWindowsLauncherOperation(launcher string) bool {
	return forbiddenWindowsLauncherDetail(launcher) != ""
}

func forbiddenWindowsLauncherDetail(launcher string) string {
	lower := strings.ToLower(launcher)
	for _, pattern := range []string{
		"set-executionpolicy",
		"new-service",
		"sc.exe create",
		"register-scheduledtask",
		"new-itemproperty",
		"set-itemproperty",
		"currentversion\\run",
		"startup\\",
		"new-netfirewallrule",
		"netsh advfirewall firewall add",
		"-verb runas",
	} {
		if strings.Contains(lower, pattern) {
			return pattern
		}
	}
	return ""
}

func bootstrapHashPinnedOrMatches(plan WindowsTemporaryPlan) bool {
	if strings.TrimSpace(plan.BootstrapScriptSHA256) == "" {
		return false
	}
	if strings.TrimSpace(plan.BootstrapScriptPath) != "" {
		hash, err := fileSHA256(plan.BootstrapScriptPath)
		if err == nil {
			return strings.EqualFold(hash, plan.BootstrapScriptSHA256)
		}
	}
	return strings.TrimSpace(plan.BootstrapScriptURL) != "" && isHexSHA256(plan.BootstrapScriptSHA256)
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
