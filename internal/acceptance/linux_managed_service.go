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

const LinuxManagedServicePlanSchemaVersion = "rdev.acceptance.linux-managed-service-plan.v1"

type LinuxManagedServiceOptions struct {
	OutDir                   string
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	UnitName                 string
	UnitOut                  string
	IdentityStore            string
	TrustStore               string
	NonceStore               string
	ApprovalStore            string
	WorkspaceLockStore       string
	LogDir                   string
	ReleaseBundle            string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
	RestartSec               string
	Force                    bool
	Now                      time.Time
}

type LinuxManagedServicePlan struct {
	SchemaVersion      string                           `json:"schema_version"`
	GeneratedAt        time.Time                        `json:"generated_at"`
	Platform           string                           `json:"platform"`
	OutDir             string                           `json:"out_dir"`
	UnitPath           string                           `json:"unit_path"`
	Unit               service.SystemdUserService       `json:"unit"`
	Status             service.SystemdUserServiceStatus `json:"status"`
	Start              service.SystemdControlPlan       `json:"start"`
	Inspect            service.SystemdControlPlan       `json:"inspect"`
	Stop               service.SystemdControlPlan       `json:"stop"`
	Checks             []Check                          `json:"checks"`
	Commands           []ServiceCommand                 `json:"commands"`
	RequiredEvidence   []string                         `json:"required_evidence"`
	RecommendedActions []string                         `json:"recommended_actions"`
}

func RunLinuxManagedServicePlan(opts LinuxManagedServiceOptions) (LinuxManagedServicePlan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return LinuxManagedServicePlan{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return LinuxManagedServicePlan{}, err
	}
	resolved, err := resolveLinuxManagedServiceOptions(outDir, opts)
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	unit, err := service.NewLinuxSystemdUserService(service.SystemdUserServiceOptions{
		UnitName:                 resolved.UnitName,
		BinaryPath:               resolved.BinaryPath,
		GatewayURL:               resolved.GatewayURL,
		TicketCode:               resolved.TicketCode,
		ManifestURL:              resolved.ManifestURL,
		IdentityStorePath:        resolved.IdentityStore,
		TrustStorePath:           resolved.TrustStore,
		NonceStorePath:           resolved.NonceStore,
		ApprovalStorePath:        resolved.ApprovalStore,
		WorkspaceLockStorePath:   resolved.WorkspaceLockStore,
		ReleaseBundlePath:        resolved.ReleaseBundle,
		ReleaseRootPublicKey:     resolved.ReleaseRootPublicKey,
		ReleaseRequiredArtifacts: resolved.ReleaseRequiredArtifacts,
		LogDir:                   resolved.LogDir,
		Transport:                resolved.Transport,
		LongPollTimeout:          resolved.LongPollTimeout,
		RestartSec:               resolved.RestartSec,
	})
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	content, err := service.RenderLinuxSystemdUserService(unit)
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	if err := writeAcceptanceFile(resolved.UnitOut, content, opts.Force); err != nil {
		return LinuxManagedServicePlan{}, err
	}
	status, err := service.InspectLinuxSystemdUserService(resolved.UnitOut)
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	start, err := service.NewLinuxSystemdControlPlan(service.SystemdControlOptions{
		Action:   "start",
		UnitName: unit.UnitName,
		User:     true,
	})
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	inspect, err := service.NewLinuxSystemdControlPlan(service.SystemdControlOptions{
		Action:   "inspect",
		UnitName: unit.UnitName,
		User:     true,
	})
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	stop, err := service.NewLinuxSystemdControlPlan(service.SystemdControlOptions{
		Action:   "stop",
		UnitName: unit.UnitName,
		User:     true,
	})
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	plan := LinuxManagedServicePlan{
		SchemaVersion: LinuxManagedServicePlanSchemaVersion,
		GeneratedAt:   now.UTC(),
		Platform:      "linux",
		OutDir:        outDir,
		UnitPath:      resolved.UnitOut,
		Unit:          unit,
		Status:        status,
		Start:         start,
		Inspect:       inspect,
		Stop:          stop,
		Commands:      linuxManagedServiceCommands(outDir, resolved, start, inspect, stop),
		RequiredEvidence: []string{
			"Generated linux-managed-service-plan.json, written systemd user unit, and verifier output.",
			"systemctl --user daemon-reload transcript before service enablement.",
			"systemctl --user enable --now transcript proving the managed host user service was started.",
			"systemctl --user status transcript after start.",
			"journalctl --user -u transcript or equivalent service log excerpt.",
			"rdev host startup output proving the release-bundle gate passed before registration.",
			"Gateway host registration, approval, trust refresh, and managed host audit events.",
			"Reconnect evidence after logout or reboot, including any separately approved linger/setup transcript if required.",
			"Managed coding or repair job evidence bundle with diff, logs, cancellation, and approval-required artifacts.",
			"systemctl --user disable --now transcript and uninstall transcript.",
		},
		RecommendedActions: []string{
			"Review the generated systemd user unit and plan JSON before running any systemctl command.",
			"Start the unit only through rdev host service-control --platform linux --execute on an owned or formally managed Linux host.",
			"Capture status, logs, release-gate output, gateway audit, reconnect proof, and managed job evidence before claiming acceptance.",
			"Stop and uninstall the unit after acceptance unless this is a deliberate managed enrollment.",
			"Do not publish this as Linux managed-service support until a real Linux host produces start/reboot/reconnect/stop/uninstall evidence.",
		},
	}
	plan.Checks = linuxManagedServiceChecks(plan, resolved)
	if err := writeLinuxManagedServicePlan(filepath.Join(outDir, "linux-managed-service-plan.json"), plan); err != nil {
		return LinuxManagedServicePlan{}, err
	}
	return plan, nil
}

type linuxManagedServiceResolvedOptions struct {
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	UnitName                 string
	UnitOut                  string
	IdentityStore            string
	TrustStore               string
	NonceStore               string
	ApprovalStore            string
	WorkspaceLockStore       string
	LogDir                   string
	ReleaseBundle            string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
	RestartSec               string
}

func resolveLinuxManagedServiceOptions(outDir string, opts LinuxManagedServiceOptions) (linuxManagedServiceResolvedOptions, error) {
	binaryPath := strings.TrimSpace(opts.BinaryPath)
	if binaryPath == "" {
		return linuxManagedServiceResolvedOptions{}, fmt.Errorf("Linux managed service acceptance requires --binary with the target Linux rdev path")
	}
	if !filepath.IsAbs(binaryPath) {
		return linuxManagedServiceResolvedOptions{}, fmt.Errorf("Linux binary path must be absolute")
	}
	unitName := firstNonEmptyString(opts.UnitName, service.DefaultLinuxSystemdUnitName)
	unitOut := opts.UnitOut
	if strings.TrimSpace(unitOut) == "" {
		unitOut = filepath.Join(outDir, unitName)
	}
	if !filepath.IsAbs(unitOut) {
		unitOut = filepath.Join(outDir, unitOut)
	}
	hostDir := filepath.Join(outDir, "host-state")
	releaseRequired := append([]string(nil), opts.ReleaseRequiredArtifacts...)
	if strings.TrimSpace(opts.ReleaseBundle) != "" && len(releaseRequired) == 0 {
		releaseRequired = []string{"rdev", "rdev-host", "rdev-verify"}
	}
	return linuxManagedServiceResolvedOptions{
		BinaryPath:               binaryPath,
		GatewayURL:               strings.TrimSpace(opts.GatewayURL),
		TicketCode:               strings.TrimSpace(opts.TicketCode),
		ManifestURL:              strings.TrimSpace(opts.ManifestURL),
		UnitName:                 unitName,
		UnitOut:                  filepath.Clean(unitOut),
		IdentityStore:            firstNonEmptyString(opts.IdentityStore, filepath.Join(hostDir, "identity.json")),
		TrustStore:               firstNonEmptyString(opts.TrustStore, filepath.Join(hostDir, "trust-bundle.json")),
		NonceStore:               firstNonEmptyString(opts.NonceStore, filepath.Join(hostDir, "nonces.json")),
		ApprovalStore:            firstNonEmptyString(opts.ApprovalStore, filepath.Join(hostDir, "approvals.json")),
		WorkspaceLockStore:       firstNonEmptyString(opts.WorkspaceLockStore, filepath.Join(outDir, "workspace-locks")),
		LogDir:                   firstNonEmptyString(opts.LogDir, filepath.Join(outDir, "logs")),
		ReleaseBundle:            strings.TrimSpace(opts.ReleaseBundle),
		ReleaseRootPublicKey:     strings.TrimSpace(opts.ReleaseRootPublicKey),
		ReleaseRequiredArtifacts: releaseRequired,
		Transport:                firstNonEmptyString(opts.Transport, "long-poll"),
		LongPollTimeout:          firstNonEmptyString(opts.LongPollTimeout, "25s"),
		RestartSec:               firstNonEmptyString(opts.RestartSec, "5s"),
	}, nil
}

func linuxManagedServiceChecks(plan LinuxManagedServicePlan, opts linuxManagedServiceResolvedOptions) []Check {
	args := strings.Join(plan.Unit.ExecStart, "\x00")
	joinedCommands := strings.ToLower(linuxManagedJoinedCommands(plan))
	return []Check{
		{Name: "platform_linux", Passed: plan.Platform == "linux", Detail: plan.Platform},
		{Name: "unit_written", Passed: plan.Status.Exists, Detail: plan.UnitPath},
		{Name: "unit_mode_0600", Passed: plan.Status.Mode == "0600", Detail: plan.Status.Mode},
		{Name: "unit_name_safe", Passed: validLinuxManagedUnitName(plan.Unit.UnitName), Detail: plan.Unit.UnitName},
		{Name: "unit_name_matches_file", Passed: plan.Status.UnitName == plan.Unit.UnitName, Detail: plan.Status.UnitName},
		{Name: "unit_exec_managed", Passed: strings.Contains(plan.Status.ExecStart, "--mode") && strings.Contains(plan.Status.ExecStart, "managed") && strings.Contains(plan.Status.ExecStart, "--once=false"), Detail: plan.Status.ExecStart},
		{Name: "binary_absolute_linux_path", Passed: filepath.IsAbs(opts.BinaryPath), Detail: opts.BinaryPath},
		{Name: "managed_mode_arg", Passed: strings.Contains(args, "--mode\x00managed")},
		{Name: "once_false_arg", Passed: strings.Contains(args, "--once=false")},
		{Name: "transport_arg", Passed: strings.Contains(args, "--transport\x00"+opts.Transport), Detail: opts.Transport},
		{Name: "workspace_lock_store_arg", Passed: strings.Contains(args, "--workspace-lock-store\x00"+opts.WorkspaceLockStore), Detail: opts.WorkspaceLockStore},
		{Name: "identity_store_arg", Passed: strings.Contains(args, "--identity-store\x00"+opts.IdentityStore), Detail: opts.IdentityStore},
		{Name: "trust_store_arg", Passed: strings.Contains(args, "--trust-store\x00"+opts.TrustStore), Detail: opts.TrustStore},
		{Name: "nonce_store_arg", Passed: strings.Contains(args, "--nonce-store\x00"+opts.NonceStore), Detail: opts.NonceStore},
		{Name: "approval_store_arg", Passed: strings.Contains(args, "--approval-store\x00"+opts.ApprovalStore), Detail: opts.ApprovalStore},
		{Name: "enrollment_arg", Passed: enrollmentConfigured(args), Detail: "ticket-code or manifest-url"},
		{Name: "release_bundle_arg", Passed: strings.Contains(args, "--release-bundle\x00"+opts.ReleaseBundle), Detail: opts.ReleaseBundle},
		{Name: "release_root_arg", Passed: opts.ReleaseRootPublicKey != "" && strings.Contains(args, "--release-root-public-key\x00"+opts.ReleaseRootPublicKey)},
		{Name: "release_required_artifacts_arg", Passed: len(opts.ReleaseRequiredArtifacts) > 0 && strings.Contains(args, "--release-require-artifacts\x00"+strings.Join(opts.ReleaseRequiredArtifacts, ",")), Detail: strings.Join(opts.ReleaseRequiredArtifacts, ",")},
		{Name: "restart_on_failure", Passed: plan.Unit.Restart == "on-failure", Detail: plan.Unit.Restart},
		{Name: "restart_sec", Passed: plan.Unit.RestartSec == opts.RestartSec, Detail: plan.Unit.RestartSec},
		{Name: "wanted_by_default", Passed: plan.Unit.WantedBy == "default.target", Detail: plan.Unit.WantedBy},
		{Name: "no_new_privileges", Passed: plan.Unit.NoNewPrivileges},
		{Name: "private_tmp", Passed: plan.Unit.PrivateTmp},
		{Name: "systemctl_daemon_reload_present", Passed: linuxCommandContains(plan.Start.Commands, "systemctl", "--user", "daemon-reload")},
		{Name: "systemctl_enable_now_present", Passed: linuxCommandContains(plan.Start.Commands, "systemctl", "--user", "enable", "--now")},
		{Name: "systemctl_status_present", Passed: linuxCommandContains(plan.Inspect.Commands, "systemctl", "--user", "status")},
		{Name: "systemctl_disable_now_present", Passed: linuxCommandContains(plan.Stop.Commands, "systemctl", "--user", "disable", "--now")},
		{Name: "commands_manual", Passed: allServiceCommandsManual(plan.Commands)},
		{Name: "no_policy_weakening_commands", Passed: !containsForbiddenLinuxManagedOperation(joinedCommands), Detail: forbiddenLinuxManagedDetail(joinedCommands)},
		{Name: "required_evidence_complete", Passed: linuxManagedRequiredEvidenceComplete(plan.RequiredEvidence), Detail: missingLinuxManagedEvidence(plan.RequiredEvidence)},
	}
}

func linuxManagedServiceCommands(outDir string, opts linuxManagedServiceResolvedOptions, start, inspect, stop service.SystemdControlPlan) []ServiceCommand {
	planPath := filepath.Join(outDir, "linux-managed-service-plan.json")
	gatewayURL := firstNonEmptyString(opts.GatewayURL, "<gateway-url>")
	commands := []ServiceCommand{
		{
			Name:    "review_unit",
			Purpose: "Inspect the generated systemd user unit before running service-manager commands.",
			Shell:   "cat " + shellQuote(opts.UnitOut),
			Argv:    []string{"cat", opts.UnitOut},
			Manual:  true,
		},
	}
	commands = append(commands, linuxSystemdPlanCommands("start_service", "Start the managed host user service after review.", start.Commands)...)
	commands = append(commands,
		ServiceCommand{
			Name:    "start_service_via_rdev",
			Purpose: "Start the managed host service through rdev's explicit service-control command.",
			Shell:   "rdev host service-control --platform linux --action start --label " + shellQuote(opts.UnitName) + " --unit " + shellQuote(opts.UnitOut) + " --execute",
			Manual:  true,
		},
	)
	commands = append(commands, linuxSystemdPlanCommands("inspect_running_service", "Capture user-service status after start.", inspect.Commands)...)
	commands = append(commands,
		ServiceCommand{
			Name:    "capture_logs",
			Purpose: "Capture recent user-service logs for the acceptance package.",
			Shell:   "journalctl --user -u " + shellQuote(opts.UnitName) + " -n 100 --no-pager",
			Argv:    []string{"journalctl", "--user", "-u", opts.UnitName, "-n", "100", "--no-pager"},
			Manual:  true,
		},
		ServiceCommand{
			Name:    "capture_evidence_bundle",
			Purpose: "Export the managed coding or repair job evidence bundle after running a signed job.",
			Shell:   "rdev evidence export --gateway " + shellQuote(gatewayURL) + " --job-id <managed-job-id> --out " + shellQuote(filepath.Join(outDir, "linux-managed-evidence")),
			Manual:  true,
		},
	)
	commands = append(commands, linuxSystemdPlanCommands("stop_service", "Stop the managed host user service after acceptance evidence is captured.", stop.Commands)...)
	commands = append(commands,
		ServiceCommand{
			Name:    "stop_service_via_rdev",
			Purpose: "Stop the managed host service through rdev's explicit service-control command.",
			Shell:   "rdev host service-control --platform linux --action stop --label " + shellQuote(opts.UnitName) + " --unit " + shellQuote(opts.UnitOut) + " --execute",
			Manual:  true,
		},
		ServiceCommand{
			Name:    "uninstall_unit",
			Purpose: "Remove the generated user unit after acceptance or revocation.",
			Shell:   "rdev host uninstall-service --platform linux --label " + shellQuote(opts.UnitName) + " --unit " + shellQuote(opts.UnitOut),
			Manual:  true,
		},
		ServiceCommand{
			Name:    "verify_plan",
			Purpose: "Verify the plan invariants before treating it as acceptance-run input.",
			Shell:   "rdev acceptance verify-linux-managed-service --plan " + shellQuote(planPath),
			Manual:  true,
		},
	)
	return commands
}

func linuxSystemdPlanCommands(name, purpose string, commands [][]string) []ServiceCommand {
	serviceCommands := make([]ServiceCommand, 0, len(commands))
	for index, command := range commands {
		serviceCommands = append(serviceCommands, ServiceCommand{
			Name:    fmt.Sprintf("%s_%d", name, index+1),
			Purpose: purpose,
			Shell:   strings.Join(command, " "),
			Argv:    append([]string(nil), command...),
			Manual:  true,
		})
	}
	return serviceCommands
}

func writeLinuxManagedServicePlan(path string, plan LinuxManagedServicePlan) error {
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func validLinuxManagedUnitName(unitName string) bool {
	if !strings.HasSuffix(unitName, ".service") || strings.Contains(unitName, "/") || strings.Contains(unitName, "\\") {
		return false
	}
	if len(unitName) <= len(".service") {
		return false
	}
	for _, r := range unitName {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '@':
		default:
			return false
		}
	}
	return true
}

func linuxCommandContains(commands [][]string, values ...string) bool {
	for _, command := range commands {
		joined := strings.ToLower(strings.Join(command, "\x00"))
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

func linuxManagedJoinedCommands(plan LinuxManagedServicePlan) string {
	var builder strings.Builder
	appendLinuxCommandMatrix(&builder, plan.Start.Commands)
	appendLinuxCommandMatrix(&builder, plan.Inspect.Commands)
	appendLinuxCommandMatrix(&builder, plan.Stop.Commands)
	for _, shell := range plan.Start.Shell {
		builder.WriteString(shell)
		builder.WriteByte('\n')
	}
	for _, shell := range plan.Inspect.Shell {
		builder.WriteString(shell)
		builder.WriteByte('\n')
	}
	for _, shell := range plan.Stop.Shell {
		builder.WriteString(shell)
		builder.WriteByte('\n')
	}
	for _, command := range plan.Commands {
		builder.WriteString(command.Shell)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func appendLinuxCommandMatrix(builder *strings.Builder, commands [][]string) {
	for _, command := range commands {
		builder.WriteString(strings.Join(command, " "))
		builder.WriteByte('\n')
	}
}

func containsForbiddenLinuxManagedOperation(commands string) bool {
	return forbiddenLinuxManagedDetail(commands) != ""
}

func forbiddenLinuxManagedDetail(commands string) string {
	lower := strings.ToLower(commands)
	for _, pattern := range []string{
		"sudo ",
		"sudo\t",
		"chmod 777",
		"chmod -r 777",
		"chown root",
		"/etc/systemd/system",
		"systemctl --system",
		"systemctl enable ",
		"systemctl disable ",
		"loginctl enable-linger",
		"crontab",
		"/etc/cron",
		"iptables",
		"ufw ",
		"firewall-cmd",
		"setcap",
	} {
		if strings.Contains(lower, pattern) {
			return pattern
		}
	}
	return ""
}

func linuxManagedRequiredEvidenceComplete(evidence []string) bool {
	return missingLinuxManagedEvidence(evidence) == ""
}

func missingLinuxManagedEvidence(evidence []string) string {
	joined := strings.ToLower(strings.Join(evidence, "\n"))
	required := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now",
		"systemctl --user status",
		"journalctl --user",
		"release-bundle gate",
		"reconnect",
		"evidence bundle",
		"approval-required",
		"systemctl --user disable --now",
		"uninstall transcript",
	}
	var missing []string
	for _, value := range required {
		if !strings.Contains(joined, value) {
			missing = append(missing, value)
		}
	}
	return strings.Join(missing, ",")
}
