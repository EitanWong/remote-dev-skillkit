package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const WindowsManagedServicePlanVerificationSchemaVersion = "rdev.acceptance-verification.windows-managed-service-plan.v1"

type WindowsManagedServicePlanVerification struct {
	SchemaVersion      string    `json:"schema_version"`
	PlanPath           string    `json:"plan_path"`
	PlanSchema         string    `json:"plan_schema"`
	GeneratedAt        time.Time `json:"generated_at"`
	Checks             []Check   `json:"checks"`
	RecommendedActions []string  `json:"recommended_actions,omitempty"`
}

func (v WindowsManagedServicePlanVerification) OK() bool {
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

func VerifyWindowsManagedServicePlan(planPath string) (WindowsManagedServicePlanVerification, error) {
	if strings.TrimSpace(planPath) == "" {
		return WindowsManagedServicePlanVerification{}, fmt.Errorf("plan path is required")
	}
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return WindowsManagedServicePlanVerification{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return WindowsManagedServicePlanVerification{}, err
	}
	var plan WindowsManagedServicePlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return WindowsManagedServicePlanVerification{}, err
	}
	verification := WindowsManagedServicePlanVerification{
		SchemaVersion: WindowsManagedServicePlanVerificationSchemaVersion,
		PlanPath:      abs,
		PlanSchema:    plan.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	args := strings.Join(plan.Service.Args, "\x00")
	joinedCommands := strings.ToLower(windowsManagedJoinedCommands(plan))

	add("plan_schema", plan.SchemaVersion == WindowsManagedServicePlanSchemaVersion, plan.SchemaVersion)
	add("plan_checks_passed", allChecksPassed(plan.Checks), failedCheckNames(plan.Checks))
	add("platform_windows", plan.Platform == "windows", plan.Platform)
	add("service_name_safe", validWindowsManagedServiceName(plan.Service.ServiceName), plan.Service.ServiceName)
	add("start_type_demand", plan.Service.StartType == "demand", plan.Service.StartType)
	add("managed_mode_arg", strings.Contains(args, "--mode\x00managed"), "")
	add("once_false_arg", strings.Contains(args, "--once=false"), "")
	add("release_bundle_arg", strings.Contains(args, "--release-bundle\x00"), "")
	add("release_root_arg", strings.Contains(args, "--release-root-public-key\x00"), "")
	add("release_required_artifacts_arg", strings.Contains(args, "--release-require-artifacts\x00"), "")
	add("workspace_lock_store_arg", strings.Contains(args, "--workspace-lock-store\x00"), "")
	add("identity_trust_nonce_approval_stores", strings.Contains(args, "--identity-store\x00") && strings.Contains(args, "--trust-store\x00") && strings.Contains(args, "--nonce-store\x00") && strings.Contains(args, "--approval-store\x00"), "")
	add("sc_create_present", windowsCommandContains(plan.Service.Commands, "sc.exe", "create"), "")
	add("sc_description_present", windowsCommandContains(plan.Service.Commands, "sc.exe", "description"), "")
	add("sc_query_present", windowsCommandContains(plan.Status.Commands, "sc.exe", "query") && windowsCommandContains(plan.Inspect.Commands, "sc.exe", "query"), "")
	add("sc_qc_present", windowsCommandContains(plan.Status.Commands, "sc.exe", "qc") && windowsCommandContains(plan.Inspect.Commands, "sc.exe", "qc"), "")
	add("sc_start_present", windowsCommandContains(plan.Start.Commands, "sc.exe", "start"), "")
	add("sc_stop_present", windowsCommandContains(plan.Stop.Commands, "sc.exe", "stop") && windowsCommandContains(plan.Uninstall.Commands, "sc.exe", "stop"), "")
	add("sc_delete_present", windowsCommandContains(plan.Uninstall.Commands, "sc.exe", "delete"), "")
	add("commands_manual", allServiceCommandsManual(plan.Commands), "")
	add("no_policy_weakening_commands", !containsForbiddenWindowsManagedOperation(joinedCommands), forbiddenWindowsManagedDetail(joinedCommands))
	add("required_evidence_complete", windowsManagedRequiredEvidenceComplete(plan.RequiredEvidence), missingWindowsManagedEvidence(plan.RequiredEvidence))

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the Windows managed service acceptance plan in a fresh output directory.",
			"Keep this as reviewed planning only until a real Windows host produces create/status/start/reconnect/stop/delete evidence.",
			"Do not publish Windows Service support until this verifier and the real acceptance package both pass.",
		}
	}
	return verification, nil
}
