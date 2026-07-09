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

const ManagedMacServicePlanVerificationSchemaVersion = "rdev.acceptance-verification.managed-mac-service-plan.v1"

type ManagedMacServicePlanVerification struct {
	SchemaVersion      string    `json:"schema_version"`
	PlanPath           string    `json:"plan_path"`
	PlanSchema         string    `json:"plan_schema"`
	GeneratedAt        time.Time `json:"generated_at"`
	Checks             []Check   `json:"checks"`
	RecommendedActions []string  `json:"recommended_actions,omitempty"`
}

func (v ManagedMacServicePlanVerification) OK() bool {
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

func VerifyManagedMacServicePlan(planPath string) (ManagedMacServicePlanVerification, error) {
	if strings.TrimSpace(planPath) == "" {
		return ManagedMacServicePlanVerification{}, fmt.Errorf("plan path is required")
	}
	abs, err := filepath.Abs(planPath)
	if err != nil {
		return ManagedMacServicePlanVerification{}, err
	}
	plan, err := readManagedMacServicePlan(abs)
	if err != nil {
		return ManagedMacServicePlanVerification{}, err
	}
	verification := ManagedMacServicePlanVerification{
		SchemaVersion: ManagedMacServicePlanVerificationSchemaVersion,
		PlanPath:      abs,
		PlanSchema:    plan.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	args := strings.Join(plan.LaunchAgent.ProgramArguments, "\x00")
	joinedCommands := strings.ToLower(managedMacServiceJoinedCommands(plan))
	plistStatus, plistErr := service.InspectMacOSLaunchAgent(plan.PlistPath)
	plistArgs := strings.Join(plistStatus.ProgramArguments, "\x00")

	add("plan_schema", plan.SchemaVersion == ManagedMacServicePlanSchemaVersion, plan.SchemaVersion)
	add("plan_checks_passed", allChecksPassed(plan.Checks), failedCheckNames(plan.Checks))
	add("platform_macos", plan.Platform == "macos", plan.Platform)
	add("plist_path_present", strings.TrimSpace(plan.PlistPath) != "", plan.PlistPath)
	add("plist_file_readable", plistErr == nil, errorString(plistErr))
	add("plist_file_exists", plistErr == nil && plistStatus.Exists, plan.PlistPath)
	add("plist_mode_0600", plistErr == nil && plistStatus.Mode == "0600", plistStatus.Mode)
	add("plist_label_matches", plistErr == nil && plistStatus.Label == plan.LaunchAgent.Label, plistStatus.Label)
	add("plist_run_at_load", plistErr == nil && plistStatus.RunAtLoad, "")
	add("plist_keep_alive", plistErr == nil && plistStatus.KeepAlive, "")
	add("plist_exec_managed", plistErr == nil && strings.Contains(plistArgs, "--mode\x00managed") && strings.Contains(plistArgs, "--once=false"), strings.Join(plistStatus.ProgramArguments, " "))
	add("plist_exec_release_gate", plistErr == nil && strings.Contains(plistArgs, "--release-bundle\x00") && strings.Contains(plistArgs, "--release-root-public-key\x00") && strings.Contains(plistArgs, "--release-require-artifacts\x00"), strings.Join(plistStatus.ProgramArguments, " "))
	add("managed_mode_arg", strings.Contains(args, "--mode\x00managed"), "")
	add("once_false_arg", strings.Contains(args, "--once=false"), "")
	add("release_bundle_arg", strings.Contains(args, "--release-bundle\x00"), "")
	add("release_root_arg", strings.Contains(args, "--release-root-public-key\x00"), "")
	add("release_required_artifacts_arg", strings.Contains(args, "--release-require-artifacts\x00"), "")
	add("workspace_lock_store_arg", strings.Contains(args, "--workspace-lock-store\x00"), "")
	add("identity_trust_stores", strings.Contains(args, "--identity-store\x00") && strings.Contains(args, "--trust-store\x00"), "")
	add("review_plist_command_present", serviceCommandContains(plan.Commands, "plutil", "-lint"), "")
	add("service_start_command_present", serviceCommandContains(plan.Commands, "service-control", "--platform", "macos", "--action", "start", "--execute"), "")
	add("service_inspect_command_present", serviceCommandContains(plan.Commands, "service-control", "--platform", "macos", "--action", "inspect", "--execute"), "")
	add("managed_acceptance_command_present", serviceCommandContains(plan.Commands, "acceptance", "managed-mac"), "")
	add("managed_verify_command_present", serviceCommandContains(plan.Commands, "acceptance", "verify"), "")
	add("service_stop_command_present", serviceCommandContains(plan.Commands, "service-control", "--platform", "macos", "--action", "stop", "--execute"), "")
	add("uninstall_command_present", serviceCommandContains(plan.Commands, "uninstall-service", "--platform", "macos"), "")
	add("commands_manual", allServiceCommandsManual(plan.Commands), "")
	add("no_policy_weakening_commands", !containsForbiddenManagedMacServiceOperation(joinedCommands), forbiddenManagedMacServiceDetail(joinedCommands))
	add("required_evidence_complete", managedMacServiceRequiredEvidenceComplete(plan.RequiredEvidence), missingManagedMacServiceEvidence(plan.RequiredEvidence))

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the managed Mac service acceptance plan in a fresh output directory.",
			"Include a release-bundle startup gate before claiming service-backed managed Mac support.",
			"Do not publish managed Mac service support until this verifier and the real evidence package both pass.",
		}
	}
	return verification, nil
}

func readManagedMacServicePlan(path string) (ManagedMacServicePlan, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ManagedMacServicePlan{}, err
	}
	var plan ManagedMacServicePlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return ManagedMacServicePlan{}, err
	}
	return plan, nil
}

func serviceCommandContains(commands []ServiceCommand, values ...string) bool {
	for _, command := range commands {
		joined := strings.ToLower(command.Shell + "\x00" + strings.Join(command.Argv, "\x00"))
		matched := true
		for _, value := range values {
			if !strings.Contains(joined, strings.ToLower(value)) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func managedMacServiceJoinedCommands(plan ManagedMacServicePlan) string {
	var builder strings.Builder
	for _, command := range plan.Commands {
		builder.WriteString(command.Shell)
		builder.WriteByte('\n')
		builder.WriteString(strings.Join(command.Argv, " "))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func containsForbiddenManagedMacServiceOperation(commands string) bool {
	return forbiddenManagedMacServiceDetail(commands) != ""
}

func forbiddenManagedMacServiceDetail(commands string) string {
	lower := strings.ToLower(commands)
	for _, pattern := range []string{
		"sudo ",
		"sudo\t",
		"launchdaemon",
		"/library/launchdaemons",
		"chmod 777",
		"chmod -r 777",
		"chown root",
		"spctl --master-disable",
		"csrutil disable",
		"crontab",
		"/etc/cron",
		"pfctl",
		"socketfilterfw --setglobalstate off",
	} {
		if strings.Contains(lower, pattern) {
			return pattern
		}
	}
	return ""
}
