package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/service"
)

const LinuxManagedServicePlanVerificationSchemaVersion = "rdev.acceptance-verification.linux-managed-service-plan.v1"

type LinuxManagedServicePlanVerification struct {
	SchemaVersion      string    `json:"schema_version"`
	PlanPath           string    `json:"plan_path"`
	PlanSchema         string    `json:"plan_schema"`
	GeneratedAt        time.Time `json:"generated_at"`
	Checks             []Check   `json:"checks"`
	RecommendedActions []string  `json:"recommended_actions,omitempty"`
}

func (v LinuxManagedServicePlanVerification) OK() bool {
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

func VerifyLinuxManagedServicePlan(planPath string) (LinuxManagedServicePlanVerification, error) {
	if strings.TrimSpace(planPath) == "" {
		return LinuxManagedServicePlanVerification{}, fmt.Errorf("plan path is required")
	}
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return LinuxManagedServicePlanVerification{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return LinuxManagedServicePlanVerification{}, err
	}
	var plan LinuxManagedServicePlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return LinuxManagedServicePlanVerification{}, err
	}
	verification := LinuxManagedServicePlanVerification{
		SchemaVersion: LinuxManagedServicePlanVerificationSchemaVersion,
		PlanPath:      abs,
		PlanSchema:    plan.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	args := strings.Join(plan.Unit.ExecStart, "\x00")
	joinedCommands := strings.ToLower(linuxManagedJoinedCommands(plan))
	unitStatus, unitErr := service.InspectLinuxSystemdUserService(plan.UnitPath)
	unitExecStart := unitStatus.ExecStart

	add("plan_schema", plan.SchemaVersion == LinuxManagedServicePlanSchemaVersion, plan.SchemaVersion)
	add("plan_checks_passed", allChecksPassed(plan.Checks), failedCheckNames(plan.Checks))
	add("platform_linux", plan.Platform == "linux", plan.Platform)
	add("unit_path_present", strings.TrimSpace(plan.UnitPath) != "", plan.UnitPath)
	add("unit_file_readable", unitErr == nil, errorString(unitErr))
	add("unit_file_exists", unitErr == nil && unitStatus.Exists, plan.UnitPath)
	add("unit_mode_0600", unitErr == nil && unitStatus.Mode == "0600", unitStatus.Mode)
	add("unit_name_safe", validLinuxManagedUnitName(plan.Unit.UnitName), plan.Unit.UnitName)
	add("unit_name_matches_file", unitErr == nil && unitStatus.UnitName == plan.Unit.UnitName, unitStatus.UnitName)
	add("unit_exec_managed", unitErr == nil && strings.Contains(unitExecStart, "--mode") && strings.Contains(unitExecStart, "managed") && strings.Contains(unitExecStart, "--once=false"), unitExecStart)
	add("unit_exec_release_gate", unitErr == nil && strings.Contains(unitExecStart, "--release-bundle") && strings.Contains(unitExecStart, "--release-root-public-key") && strings.Contains(unitExecStart, "--release-require-artifacts"), unitExecStart)
	add("unit_exec_workspace_lock", unitErr == nil && strings.Contains(unitExecStart, "--workspace-lock-store"), unitExecStart)
	add("managed_mode_arg", strings.Contains(args, "--mode\x00managed"), "")
	add("once_false_arg", strings.Contains(args, "--once=false"), "")
	add("release_bundle_arg", strings.Contains(args, "--release-bundle\x00"), "")
	add("release_root_arg", strings.Contains(args, "--release-root-public-key\x00"), "")
	add("release_required_artifacts_arg", strings.Contains(args, "--release-require-artifacts\x00"), "")
	add("workspace_lock_store_arg", strings.Contains(args, "--workspace-lock-store\x00"), "")
	add("identity_trust_nonce_approval_stores", strings.Contains(args, "--identity-store\x00") && strings.Contains(args, "--trust-store\x00") && strings.Contains(args, "--nonce-store\x00") && strings.Contains(args, "--approval-store\x00"), "")
	add("restart_on_failure", plan.Unit.Restart == "on-failure" && unitStatus.Restart == "on-failure", plan.Unit.Restart)
	add("restart_sec_present", plan.Unit.RestartSec != "" && unitStatus.RestartSec != "", plan.Unit.RestartSec)
	add("wanted_by_default", plan.Unit.WantedBy == "default.target" && unitStatus.WantedBy == "default.target", plan.Unit.WantedBy)
	add("no_new_privileges", plan.Unit.NoNewPrivileges && unitStatus.NoNewPrivileges, "")
	add("private_tmp", plan.Unit.PrivateTmp && unitStatus.PrivateTmp, "")
	add("systemctl_daemon_reload_present", linuxCommandContains(plan.Start.Commands, "systemctl", "--user", "daemon-reload"), "")
	add("systemctl_enable_now_present", linuxCommandContains(plan.Start.Commands, "systemctl", "--user", "enable", "--now"), "")
	add("systemctl_status_present", linuxCommandContains(plan.Inspect.Commands, "systemctl", "--user", "status"), "")
	add("systemctl_disable_now_present", linuxCommandContains(plan.Stop.Commands, "systemctl", "--user", "disable", "--now"), "")
	add("commands_manual", allServiceCommandsManual(plan.Commands), "")
	add("no_policy_weakening_commands", !containsForbiddenLinuxManagedOperation(joinedCommands), forbiddenLinuxManagedDetail(joinedCommands))
	add("required_evidence_complete", linuxManagedRequiredEvidenceComplete(plan.RequiredEvidence), missingLinuxManagedEvidence(plan.RequiredEvidence))

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the Linux managed service acceptance plan in a fresh output directory.",
			"Keep this as reviewed planning only until a real Linux host produces start/status/reboot/reconnect/stop/uninstall evidence.",
			"Do not publish Linux managed-service support until this verifier and the real acceptance package both pass.",
		}
	}
	return verification, nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
