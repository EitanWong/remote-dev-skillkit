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

const WindowsManagedServicePlanSchemaVersion = "rdev.acceptance.windows-managed-service-plan.v1"

type WindowsManagedServiceOptions struct {
	OutDir                   string
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	ServiceName              string
	DisplayName              string
	Description              string
	IdentityStore            string
	TrustStore               string
	WorkspaceLockStore       string
	ReleaseBundle            string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
	Force                    bool
	Now                      time.Time
}

type WindowsManagedServicePlan struct {
	SchemaVersion      string                            `json:"schema_version"`
	GeneratedAt        time.Time                         `json:"generated_at"`
	Platform           string                            `json:"platform"`
	OutDir             string                            `json:"out_dir"`
	Service            service.WindowsService            `json:"service"`
	Status             service.WindowsServiceStatus      `json:"status"`
	Start              service.WindowsServiceControlPlan `json:"start"`
	Inspect            service.WindowsServiceControlPlan `json:"inspect"`
	Stop               service.WindowsServiceControlPlan `json:"stop"`
	Uninstall          service.WindowsServiceControlPlan `json:"uninstall"`
	Checks             []Check                           `json:"checks"`
	Commands           []ServiceCommand                  `json:"commands"`
	RequiredEvidence   []string                          `json:"required_evidence"`
	RecommendedActions []string                          `json:"recommended_actions"`
}

func RunWindowsManagedServicePlan(opts WindowsManagedServiceOptions) (WindowsManagedServicePlan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return WindowsManagedServicePlan{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return WindowsManagedServicePlan{}, err
	}
	resolved, err := resolveWindowsManagedServiceOptions(outDir, opts)
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	winService, err := service.NewWindowsService(service.WindowsServiceOptions{
		ServiceName:              resolved.ServiceName,
		DisplayName:              resolved.DisplayName,
		Description:              resolved.Description,
		BinaryPath:               resolved.BinaryPath,
		GatewayURL:               resolved.GatewayURL,
		TicketCode:               resolved.TicketCode,
		ManifestURL:              resolved.ManifestURL,
		IdentityStorePath:        resolved.IdentityStore,
		TrustStorePath:           resolved.TrustStore,
		WorkspaceLockStorePath:   resolved.WorkspaceLockStore,
		ReleaseBundlePath:        resolved.ReleaseBundle,
		ReleaseRootPublicKey:     resolved.ReleaseRootPublicKey,
		ReleaseRequiredArtifacts: resolved.ReleaseRequiredArtifacts,
		Transport:                resolved.Transport,
		LongPollTimeout:          resolved.LongPollTimeout,
	})
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	status, err := service.NewWindowsServiceStatus(winService.ServiceName)
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	start, err := service.NewWindowsServiceControlPlan(service.WindowsServiceControlOptions{
		Action:      "start",
		ServiceName: winService.ServiceName,
	})
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	inspect, err := service.NewWindowsServiceControlPlan(service.WindowsServiceControlOptions{
		Action:      "inspect",
		ServiceName: winService.ServiceName,
	})
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	stop, err := service.NewWindowsServiceControlPlan(service.WindowsServiceControlOptions{
		Action:      "stop",
		ServiceName: winService.ServiceName,
	})
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	uninstall, err := service.NewWindowsServiceUninstallPlan(winService.ServiceName)
	if err != nil {
		return WindowsManagedServicePlan{}, err
	}
	plan := WindowsManagedServicePlan{
		SchemaVersion: WindowsManagedServicePlanSchemaVersion,
		GeneratedAt:   now.UTC(),
		Platform:      "windows",
		OutDir:        outDir,
		Service:       winService,
		Status:        status,
		Start:         start,
		Inspect:       inspect,
		Stop:          stop,
		Uninstall:     uninstall,
		Commands:      windowsManagedServiceCommands(outDir, winService, status, start, inspect, stop, uninstall),
		RequiredEvidence: []string{
			"Generated windows-managed-service-plan.json and verifier output.",
			"Elevated PowerShell transcript for reviewed sc.exe create and description commands.",
			"sc.exe query and sc.exe qc transcript after creation.",
			"rdev host startup output proving the release-bundle gate passed before registration.",
			"Gateway session join, endpoint trust refresh, and managed host audit events.",
			"Service start transcript and reconnect evidence after login or reboot.",
			"Managed coding or repair session evidence with diff, logs, cancellation, and host-denial probe artifacts.",
			"Service stop transcript.",
			"sc.exe delete uninstall transcript.",
			"Evidence that attended-temporary mode remains non-persistent and is not represented by this managed service.",
		},
		RecommendedActions: []string{
			"Review the plan JSON and every sc.exe command before running anything on a Windows host.",
			"Create the service only from an elevated PowerShell session on an owned or formally managed Windows host.",
			"Start the service through rdev host service-control --execute only after the create/status transcript is captured.",
			"After proving reconnect, stop and uninstall the service unless this is a deliberate managed enrollment.",
			"Do not publish this as Windows Service support until a real Windows acceptance package includes the required evidence.",
		},
	}
	plan.Checks = windowsManagedServiceChecks(plan, resolved)
	if err := writeWindowsManagedServicePlan(filepath.Join(outDir, "windows-managed-service-plan.json"), plan); err != nil {
		return WindowsManagedServicePlan{}, err
	}
	return plan, nil
}

type windowsManagedServiceResolvedOptions struct {
	BinaryPath               string
	GatewayURL               string
	TicketCode               string
	ManifestURL              string
	ServiceName              string
	DisplayName              string
	Description              string
	IdentityStore            string
	TrustStore               string
	WorkspaceLockStore       string
	ReleaseBundle            string
	ReleaseRootPublicKey     string
	ReleaseRequiredArtifacts []string
	Transport                string
	LongPollTimeout          string
}

func resolveWindowsManagedServiceOptions(outDir string, opts WindowsManagedServiceOptions) (windowsManagedServiceResolvedOptions, error) {
	binaryPath := strings.TrimSpace(opts.BinaryPath)
	if binaryPath == "" {
		return windowsManagedServiceResolvedOptions{}, fmt.Errorf("Windows managed service acceptance requires --binary with the target Windows rdev.exe path")
	}
	if !isWindowsManagedAbsolutePath(binaryPath) {
		return windowsManagedServiceResolvedOptions{}, fmt.Errorf("Windows binary path must be an absolute Windows path")
	}
	serviceName := firstNonEmptyString(opts.ServiceName, service.DefaultWindowsServiceName)
	programData := `C:\ProgramData\rdev`
	releaseRequired := append([]string(nil), opts.ReleaseRequiredArtifacts...)
	if strings.TrimSpace(opts.ReleaseBundle) != "" && len(releaseRequired) == 0 {
		releaseRequired = []string{"rdev.exe", "rdev-host.exe", "rdev-verify.exe"}
	}
	return windowsManagedServiceResolvedOptions{
		BinaryPath:               binaryPath,
		GatewayURL:               strings.TrimSpace(opts.GatewayURL),
		TicketCode:               strings.TrimSpace(opts.TicketCode),
		ManifestURL:              strings.TrimSpace(opts.ManifestURL),
		ServiceName:              serviceName,
		DisplayName:              firstNonEmptyString(opts.DisplayName, "Remote Dev Skillkit Host"),
		Description:              firstNonEmptyString(opts.Description, "Remote Dev Skillkit managed host"),
		IdentityStore:            firstNonEmptyString(opts.IdentityStore, programData+`\identity.json`),
		TrustStore:               firstNonEmptyString(opts.TrustStore, programData+`\trust-bundle.json`),
		WorkspaceLockStore:       firstNonEmptyString(opts.WorkspaceLockStore, programData+`\workspace-locks`),
		ReleaseBundle:            strings.TrimSpace(opts.ReleaseBundle),
		ReleaseRootPublicKey:     strings.TrimSpace(opts.ReleaseRootPublicKey),
		ReleaseRequiredArtifacts: releaseRequired,
		Transport:                firstNonEmptyString(opts.Transport, "long-poll"),
		LongPollTimeout:          firstNonEmptyString(opts.LongPollTimeout, "25s"),
	}, nil
}

func windowsManagedServiceChecks(plan WindowsManagedServicePlan, opts windowsManagedServiceResolvedOptions) []Check {
	args := strings.Join(plan.Service.Args, "\x00")
	joinedCommands := strings.ToLower(windowsManagedJoinedCommands(plan))
	return []Check{
		{Name: "platform_windows", Passed: plan.Platform == "windows", Detail: plan.Platform},
		{Name: "service_name_safe", Passed: validWindowsManagedServiceName(plan.Service.ServiceName), Detail: plan.Service.ServiceName},
		{Name: "binary_absolute_windows_path", Passed: isWindowsManagedAbsolutePath(opts.BinaryPath), Detail: opts.BinaryPath},
		{Name: "managed_mode_arg", Passed: strings.Contains(args, "--mode\x00managed")},
		{Name: "once_false_arg", Passed: strings.Contains(args, "--once=false")},
		{Name: "transport_arg", Passed: strings.Contains(args, "--transport\x00"+opts.Transport), Detail: opts.Transport},
		{Name: "workspace_lock_store_arg", Passed: strings.Contains(args, "--workspace-lock-store\x00"+opts.WorkspaceLockStore), Detail: opts.WorkspaceLockStore},
		{Name: "identity_store_arg", Passed: strings.Contains(args, "--identity-store\x00"+opts.IdentityStore), Detail: opts.IdentityStore},
		{Name: "trust_store_arg", Passed: strings.Contains(args, "--trust-store\x00"+opts.TrustStore), Detail: opts.TrustStore},
		{Name: "enrollment_arg", Passed: enrollmentConfigured(args), Detail: "ticket-code or manifest-url"},
		{Name: "release_bundle_arg", Passed: strings.Contains(args, "--release-bundle\x00"+opts.ReleaseBundle), Detail: opts.ReleaseBundle},
		{Name: "release_root_arg", Passed: opts.ReleaseRootPublicKey != "" && strings.Contains(args, "--release-root-public-key\x00"+opts.ReleaseRootPublicKey)},
		{Name: "release_required_artifacts_arg", Passed: len(opts.ReleaseRequiredArtifacts) > 0 && strings.Contains(args, "--release-require-artifacts\x00"+strings.Join(opts.ReleaseRequiredArtifacts, ",")), Detail: strings.Join(opts.ReleaseRequiredArtifacts, ",")},
		{Name: "start_type_demand", Passed: plan.Service.StartType == "demand", Detail: plan.Service.StartType},
		{Name: "sc_create_present", Passed: windowsCommandContains(plan.Service.Commands, "sc.exe", "create")},
		{Name: "sc_description_present", Passed: windowsCommandContains(plan.Service.Commands, "sc.exe", "description")},
		{Name: "sc_status_present", Passed: windowsCommandContains(plan.Status.Commands, "sc.exe", "query") && windowsCommandContains(plan.Status.Commands, "sc.exe", "qc")},
		{Name: "sc_start_present", Passed: windowsCommandContains(plan.Start.Commands, "sc.exe", "start")},
		{Name: "sc_stop_present", Passed: windowsCommandContains(plan.Stop.Commands, "sc.exe", "stop")},
		{Name: "sc_delete_present", Passed: windowsCommandContains(plan.Uninstall.Commands, "sc.exe", "delete")},
		{Name: "commands_manual", Passed: allServiceCommandsManual(plan.Commands)},
		{Name: "no_policy_weakening_commands", Passed: !containsForbiddenWindowsManagedOperation(joinedCommands), Detail: forbiddenWindowsManagedDetail(joinedCommands)},
		{Name: "required_evidence_complete", Passed: windowsManagedRequiredEvidenceComplete(plan.RequiredEvidence), Detail: missingWindowsManagedEvidence(plan.RequiredEvidence)},
	}
}

func windowsManagedServiceCommands(outDir string, winService service.WindowsService, status service.WindowsServiceStatus, start, inspect, stop, uninstall service.WindowsServiceControlPlan) []ServiceCommand {
	planPath := filepath.Join(outDir, "windows-managed-service-plan.json")
	commands := []ServiceCommand{
		{
			Name:    "review_plan",
			Purpose: "Inspect the generated Windows managed service acceptance plan before running service-manager commands.",
			Shell:   "Get-Content -LiteralPath " + powershellQuote(planPath),
			Argv:    []string{"powershell.exe", "-NoProfile", "-Command", "Get-Content -LiteralPath " + powershellQuote(planPath)},
			Manual:  true,
		},
	}
	commands = append(commands, windowsServicePlanCommands("create_service", "Create the Windows Service from an elevated PowerShell session after review.", winService.Commands)...)
	commands = append(commands, windowsServicePlanCommands("status_after_create", "Capture service status and configuration after create.", status.Commands)...)
	commands = append(commands,
		ServiceCommand{
			Name:    "start_service",
			Purpose: "Start the managed host service through rdev's explicit service-control command.",
			Shell:   "rdev host service-control --platform windows --action start --label " + winService.ServiceName + " --execute",
			Manual:  true,
		},
	)
	commands = append(commands, windowsServicePlanCommands("inspect_running_service", "Capture service status and configuration after start.", inspect.Commands)...)
	commands = append(commands,
		ServiceCommand{
			Name:    "stop_service",
			Purpose: "Stop the managed host service after acceptance evidence is captured.",
			Shell:   "rdev host service-control --platform windows --action stop --label " + winService.ServiceName + " --execute",
			Manual:  true,
		},
		ServiceCommand{
			Name:    "review_uninstall_plan",
			Purpose: "Generate the uninstall command plan before deleting the service.",
			Shell:   "rdev host uninstall-service --platform windows --label " + winService.ServiceName,
			Manual:  true,
		},
	)
	commands = append(commands, windowsServicePlanCommands("uninstall_service", "Stop and delete the Windows Service after acceptance or revocation.", uninstall.Commands)...)
	commands = append(commands,
		ServiceCommand{
			Name:    "verify_plan",
			Purpose: "Verify the plan invariants before treating it as acceptance-run input.",
			Shell:   "rdev acceptance verify-windows-managed-service --plan " + shellQuote(planPath),
			Manual:  true,
		},
	)
	return commands
}

func windowsServicePlanCommands(name, purpose string, commands [][]string) []ServiceCommand {
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

func writeWindowsManagedServicePlan(path string, plan WindowsManagedServicePlan) error {
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func isWindowsManagedAbsolutePath(path string) bool {
	if len(path) >= 3 {
		drive := path[0]
		if ((drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
			return true
		}
	}
	return strings.HasPrefix(path, `\\`)
}

func validWindowsManagedServiceName(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func windowsCommandContains(commands [][]string, values ...string) bool {
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

func allServiceCommandsManual(commands []ServiceCommand) bool {
	if len(commands) == 0 {
		return false
	}
	for _, command := range commands {
		if !command.Manual {
			return false
		}
	}
	return true
}

func windowsManagedJoinedCommands(plan WindowsManagedServicePlan) string {
	var builder strings.Builder
	appendWindowsCommandMatrix(&builder, plan.Service.Commands)
	appendWindowsCommandMatrix(&builder, plan.Status.Commands)
	appendWindowsCommandMatrix(&builder, plan.Start.Commands)
	appendWindowsCommandMatrix(&builder, plan.Inspect.Commands)
	appendWindowsCommandMatrix(&builder, plan.Stop.Commands)
	appendWindowsCommandMatrix(&builder, plan.Uninstall.Commands)
	for _, shell := range plan.Service.Shell {
		builder.WriteString(shell)
		builder.WriteByte('\n')
	}
	for _, shell := range plan.Status.Shell {
		builder.WriteString(shell)
		builder.WriteByte('\n')
	}
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
	for _, shell := range plan.Uninstall.Shell {
		builder.WriteString(shell)
		builder.WriteByte('\n')
	}
	for _, command := range plan.Commands {
		builder.WriteString(command.Shell)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func appendWindowsCommandMatrix(builder *strings.Builder, commands [][]string) {
	for _, command := range commands {
		builder.WriteString(strings.Join(command, " "))
		builder.WriteByte('\n')
	}
}

func containsForbiddenWindowsManagedOperation(commands string) bool {
	return forbiddenWindowsManagedDetail(commands) != ""
}

func forbiddenWindowsManagedDetail(commands string) string {
	lower := strings.ToLower(commands)
	for _, pattern := range []string{
		"set-executionpolicy",
		"register-scheduledtask",
		"new-itemproperty",
		"set-itemproperty",
		"currentversion\\run",
		"startup\\",
		"new-netfirewallrule",
		"netsh advfirewall firewall add",
		"-verb runas",
		"new-service",
	} {
		if strings.Contains(lower, pattern) {
			return pattern
		}
	}
	return ""
}

func windowsManagedRequiredEvidenceComplete(evidence []string) bool {
	return missingWindowsManagedEvidence(evidence) == ""
}

func missingWindowsManagedEvidence(evidence []string) string {
	joined := strings.ToLower(strings.Join(evidence, "\n"))
	required := []string{
		"sc.exe create",
		"sc.exe query",
		"sc.exe qc",
		"release-bundle gate",
		"reconnect",
		"session evidence",
		"host-denial",
		"sc.exe delete",
		"temporary mode",
	}
	var missing []string
	for _, value := range required {
		if !strings.Contains(joined, value) {
			missing = append(missing, value)
		}
	}
	return strings.Join(missing, ",")
}
