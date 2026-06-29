package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const WindowsTemporaryPlanVerificationSchemaVersion = "rdev.acceptance-verification.windows-temporary-plan.v1"

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
	add("approval_probes_complete", windowsApprovalProbesComplete(plan.ApprovalProbes), missingWindowsApprovalProbes(plan.ApprovalProbes))
	add("required_evidence_complete", len(plan.RequiredEvidence) >= 5, "")

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the Windows temporary acceptance plan in a fresh output directory.",
			"Inspect run-windows-temporary.ps1 for unexpected side effects before sending it to a target user.",
			"Do not run or publish this Windows acceptance plan until verification passes.",
		}
	}
	return verification, nil
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

func windowsApprovalProbesComplete(probes []WindowsApprovalProbe) bool {
	return missingWindowsApprovalProbes(probes) == ""
}

func missingWindowsApprovalProbes(probes []WindowsApprovalProbe) string {
	seen := map[string]bool{}
	for _, probe := range probes {
		if probe.ExpectedArtifact == "rdev.approval-required.v1" {
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
