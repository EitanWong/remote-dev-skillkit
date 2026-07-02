package connectionentry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/acceptance"
	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	MaterializationPlanSchemaVersion = "rdev.connection-entry.materialization-plan.v1"
	EntryPackagePlanSchemaVersion    = "rdev.connection-entry.package-plan.v1"
)

type Options struct {
	InviteJSON                     string
	InvitePath                     string
	OutDir                         string
	TargetOS                       string
	Ownership                      string
	SessionMode                    string
	ReleaseBundleURL               string
	ReleaseBundleRequiredArtifacts string
	ReleaseBundlePath              string
	ReleaseRootPublicKey           string
	ManagedBinaryPath              string
	ManagedServiceName             string
	ManagedServiceLabel            string
	ManagedUnitName                string
	WindowsHostDownloadURL         string
	WindowsHostExpectedSHA256      string
	WindowsVerifierDownloadURL     string
	WindowsVerifierExpectedSHA256  string
	WindowsBootstrapScriptURL      string
	WindowsBootstrapScriptSHA256   string
	WindowsBootstrapScriptPath     string
	HostName                       string
	Force                          bool
	Now                            time.Time
}

type Plan struct {
	SchemaVersion          string            `json:"schema_version"`
	ConnectionEntryName    string            `json:"connection_entry_name"`
	EntryPackagePlanSchema string            `json:"entry_package_plan_schema"`
	GeneratedAt            time.Time         `json:"generated_at"`
	OutDir                 string            `json:"out_dir,omitempty"`
	InviteSchemaVersion    string            `json:"invite_schema_version"`
	EntrySchemaVersion     string            `json:"entry_schema_version"`
	EntryPlanSchemaVersion string            `json:"entry_plan_schema_version"`
	TargetOS               string            `json:"target_os"`
	Ownership              string            `json:"ownership"`
	SessionMode            string            `json:"session_mode"`
	ModeDecision           string            `json:"mode_decision"`
	EntryURL               string            `json:"entry_url"`
	EntryCommand           string            `json:"entry_command,omitempty"`
	HumanSurface           []string          `json:"human_surface"`
	AgentMetadata          []string          `json:"agent_metadata"`
	HandoffContract        []string          `json:"handoff_contract"`
	HumanMessagePath       string            `json:"human_message_path,omitempty"`
	HumanSteps             []string          `json:"human_steps"`
	AgentSteps             []string          `json:"agent_steps"`
	NetworkStrategy        []string          `json:"network_strategy"`
	Checks                 []Check           `json:"checks"`
	MissingInputs          []string          `json:"missing_inputs,omitempty"`
	GeneratedFiles         []GeneratedFile   `json:"generated_files,omitempty"`
	EntryPackagePlan       *EntryPackagePlan `json:"entry_package_plan,omitempty"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type GeneratedFile struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

type EntryPackagePlan struct {
	SchemaVersion       string             `json:"schema_version"`
	TargetOS            string             `json:"target_os"`
	SessionMode         string             `json:"session_mode"`
	PackageMode         string             `json:"package_mode"`
	OK                  bool               `json:"ok"`
	PlanPath            string             `json:"plan_path"`
	LauncherPath        string             `json:"launcher_path,omitempty"`
	PlatformPlanSchema  string             `json:"platform_plan_schema,omitempty"`
	PlatformPlanKind    string             `json:"platform_plan_kind,omitempty"`
	HumanEntryPoint     string             `json:"human_entry_point,omitempty"`
	AgentOnlyParameters []string           `json:"agent_only_parameters"`
	Checks              []acceptance.Check `json:"checks"`
}

func FromInvite(opts Options) (Plan, error) {
	invite, err := readInvite(opts)
	if err != nil {
		return Plan{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	targetOS := normalizeTargetOS(opts.TargetOS)
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	ownership := normalizeOwnership(opts.Ownership)
	if ownership == "" {
		ownership = inferOwnership(invite)
	}
	sessionMode, err := selectSessionMode(invite, ownership, opts.SessionMode)
	if err != nil {
		return Plan{}, err
	}
	outDir := strings.TrimSpace(opts.OutDir)
	if outDir != "" {
		abs, err := filepath.Abs(outDir)
		if err != nil {
			return Plan{}, err
		}
		outDir = abs
		if err := prepareOutDir(outDir); err != nil {
			return Plan{}, err
		}
	}
	entryCommand := commandForOS(invite.ConnectionEntry.OneLineCommands, targetOS)
	plan := Plan{
		SchemaVersion:          MaterializationPlanSchemaVersion,
		ConnectionEntryName:    "Connection Entry",
		EntryPackagePlanSchema: EntryPackagePlanSchemaVersion,
		GeneratedAt:            now.UTC(),
		OutDir:                 outDir,
		InviteSchemaVersion:    invite.SchemaVersion,
		EntrySchemaVersion:     invite.ConnectionEntry.SchemaVersion,
		EntryPlanSchemaVersion: invite.ConnectionEntryPlan.SchemaVersion,
		TargetOS:               targetOS,
		Ownership:              ownership,
		SessionMode:            sessionMode,
		ModeDecision:           modeDecision(ownership, sessionMode),
		EntryURL:               invite.ConnectionEntry.EntryURL,
		EntryCommand:           entryCommand,
		HumanSurface:           humanSurface(targetOS, sessionMode),
		AgentMetadata:          agentMetadata(),
		HandoffContract:        handoffContract(),
		HumanSteps:             humanSteps(invite, sessionMode, targetOS),
		AgentSteps:             agentSteps(invite, sessionMode),
		NetworkStrategy:        invite.ConnectionEntryPlan.NetworkStrategy,
	}
	plan.Checks = buildChecks(invite, plan, entryCommand)
	if targetOS == "windows" && sessionMode == string(model.HostModeAttendedTemporary) {
		addWindowsEntryPackagePlan(&plan, invite, opts, outDir)
	}
	if sessionMode == string(model.HostModeManaged) {
		addManagedEntryPackagePlan(&plan, invite, opts, outDir)
	}
	if outDir != "" {
		if err := writeMaterializedFiles(&plan); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

func readInvite(opts Options) (agentinvite.Invite, error) {
	content := strings.TrimSpace(opts.InviteJSON)
	if content == "" && strings.TrimSpace(opts.InvitePath) != "" {
		raw, err := os.ReadFile(opts.InvitePath)
		if err != nil {
			return agentinvite.Invite{}, err
		}
		content = string(raw)
	}
	if content == "" {
		return agentinvite.Invite{}, fmt.Errorf("invite JSON or invite path is required")
	}
	var invite agentinvite.Invite
	if err := json.Unmarshal([]byte(content), &invite); err != nil {
		return agentinvite.Invite{}, fmt.Errorf("decode invite JSON: %w", err)
	}
	if invite.SchemaVersion != agentinvite.SchemaVersion {
		return agentinvite.Invite{}, fmt.Errorf("unsupported invite schema %q", invite.SchemaVersion)
	}
	return invite, nil
}

func normalizeTargetOS(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "win", "windows":
		return "windows"
	case "mac", "macos", "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeOwnership(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "owned", "personal", "fleet", "operator-owned", "self":
		return "owned"
	case "third-party", "temporary", "external", "customer":
		return "third-party"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func inferOwnership(invite agentinvite.Invite) string {
	if invite.Ticket.Mode == model.HostModeManaged {
		return "owned"
	}
	return "third-party"
}

func selectSessionMode(invite agentinvite.Invite, ownership, requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		mode := model.HostMode(strings.TrimSpace(requested))
		if !mode.Valid() {
			return "", fmt.Errorf("unsupported session mode %q", requested)
		}
		if mode == model.HostModeManaged && ownership != "owned" {
			return "", fmt.Errorf("managed connection entries require ownership=owned or explicit owned inventory")
		}
		return string(mode), nil
	}
	if invite.Ticket.Mode == model.HostModeManaged || ownership == "owned" {
		return string(model.HostModeManaged), nil
	}
	return string(model.HostModeAttendedTemporary), nil
}

func modeDecision(ownership, sessionMode string) string {
	if sessionMode == string(model.HostModeManaged) {
		return "owned target selected managed mode for durable Agent work with an explicit service lifecycle"
	}
	if ownership == "owned" {
		return "owned target selected attended-temporary mode because the invite or operator requested a non-persistent session"
	}
	if sessionMode == string(model.HostModeBreakGlass) {
		return "break-glass mode selected only for an explicitly approved emergency session"
	}
	return "third-party or one-off target selected attended-temporary mode with no persistence by default"
}

func commandForOS(commands map[string]string, targetOS string) string {
	if commands == nil {
		return ""
	}
	if targetOS == "windows" {
		return strings.TrimSpace(commands["windows_powershell"])
	}
	return strings.TrimSpace(commands["macos_linux_sh"])
}

func humanSurface(targetOS, sessionMode string) []string {
	surface := []string{
		"connection_entry.entry_url",
		"platform connection entry package",
	}
	if targetOS == "windows" {
		surface = append(surface, "visible PowerShell launcher generated from the package plan")
	}
	if sessionMode == string(model.HostModeManaged) {
		surface = append(surface, "reviewed managed-service entry package after owned-host approval")
	}
	return surface
}

func agentMetadata() []string {
	return []string{
		"ticket code",
		"manifest URL",
		"manifest root public key",
		"gateway URL",
		"transport preference and fallback order",
		"release bundle URL and release root public key when packaging is requested",
	}
}

func handoffContract() []string {
	return []string{
		"Connection Entry is the universal target-side handoff for every new remote host.",
		"The target side receives only a link, visible script, or signed package.",
		"Ticket, gateway, manifest root, transport, release, and checksum values stay in Agent/package metadata.",
		"Agents must use rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions.",
		"Owned recurring machines use managed planning; third-party or one-off machines use attended-temporary planning by default.",
	}
}

func humanSteps(invite agentinvite.Invite, sessionMode, targetOS string) []string {
	steps := []string{
		"Open " + invite.ConnectionEntry.EntryURL + " on the target machine.",
	}
	if command := commandForOS(invite.ConnectionEntry.OneLineCommands, targetOS); command != "" {
		steps = append(steps, "Run the generated visible connection entry for "+targetOS+": "+command)
	}
	if sessionMode == string(model.HostModeManaged) {
		steps = append(steps, "Approve the reviewed managed service lifecycle only after confirming this is an operator-owned machine.")
	} else {
		steps = append(steps, "Keep the visible temporary session open until the Agent reports completion.")
	}
	return append(steps, invite.ConnectionEntry.RevocationInstructions...)
}

func agentSteps(invite agentinvite.Invite, sessionMode string) []string {
	steps := []string{
		"Do not ask the target-side human to assemble ticket, manifest root, gateway, or transport flags.",
		"Poll rdev.hosts.list until the expected host appears, then approve only the scoped host.",
		"Create small signed jobs and review evidence before claiming completion.",
	}
	if sessionMode == string(model.HostModeManaged) {
		steps = append(steps, "Generate a reviewed managed service plan with release gates, renewal, revocation refresh, reconnect proof, and uninstall instructions.")
	} else {
		steps = append(steps, "Revoke the temporary ticket and run no-persistence checks when finished.")
	}
	return append(steps, invite.AgentNextActions...)
}

func buildChecks(invite agentinvite.Invite, plan Plan, entryCommand string) []Check {
	checks := []Check{
		{Name: "entry_url", Passed: validURL(invite.ConnectionEntry.EntryURL), Detail: invite.ConnectionEntry.EntryURL},
		{Name: "manifest_url", Passed: validURL(invite.ManifestURL), Detail: invite.ManifestURL},
		{Name: "manifest_root_public_key", Passed: strings.TrimSpace(invite.ManifestRootPublicKey) != "", Detail: invite.ManifestRootPublicKey},
		{Name: "ticket_code", Passed: strings.TrimSpace(invite.Ticket.Code) != "", Detail: invite.Ticket.Code},
		{Name: "session_mode_selected", Passed: plan.SessionMode != "", Detail: plan.SessionMode},
		{Name: "entry_command_available", Passed: entryCommand != "", Detail: plan.TargetOS},
		{Name: "human_steps_no_raw_flag_assembly", Passed: true, Detail: "connection entry carries ticket/root/gateway/transport metadata for the Agent and package; humans receive a link, script, or package"},
	}
	if plan.SessionMode == string(model.HostModeManaged) {
		checks = append(checks, Check{Name: "owned_mode_requires_owned_inventory", Passed: plan.Ownership == "owned", Detail: plan.Ownership})
	}
	return checks
}

func validURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func addWindowsEntryPackagePlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	missing := windowsMissingInputs(opts)
	plan.MissingInputs = append(plan.MissingInputs, missing...)
	if outDir == "" || len(missing) > 0 {
		plan.Checks = append(plan.Checks, Check{
			Name:   "windows_temporary_acceptance_plan",
			Passed: false,
			Detail: "requires out_dir and release artifact inputs before generating the Windows launcher",
		})
		return
	}
	winPlan, err := acceptance.RunWindowsTemporaryPlan(acceptance.WindowsTemporaryOptions{
		OutDir:                         filepath.Join(outDir, "windows-temporary"),
		GatewayURL:                     invite.GatewayURL,
		TicketCode:                     invite.Ticket.Code,
		DownloadURL:                    strings.TrimSpace(opts.WindowsHostDownloadURL),
		ExpectedSHA256:                 strings.TrimSpace(opts.WindowsHostExpectedSHA256),
		BootstrapScriptPath:            strings.TrimSpace(opts.WindowsBootstrapScriptPath),
		BootstrapScriptURL:             strings.TrimSpace(opts.WindowsBootstrapScriptURL),
		BootstrapScriptExpectedSHA256:  strings.TrimSpace(opts.WindowsBootstrapScriptSHA256),
		ManifestURL:                    invite.ManifestURL,
		ManifestRootPublicKey:          invite.ManifestRootPublicKey,
		ReleaseBundleURL:               strings.TrimSpace(opts.ReleaseBundleURL),
		ReleaseBundleRequiredArtifacts: firstNonEmpty(strings.TrimSpace(opts.ReleaseBundleRequiredArtifacts), "rdev-host.exe,rdev-verify.exe"),
		ReleaseRootPublicKey:           strings.TrimSpace(opts.ReleaseRootPublicKey),
		VerifierDownloadURL:            strings.TrimSpace(opts.WindowsVerifierDownloadURL),
		VerifierExpectedSHA256:         strings.TrimSpace(opts.WindowsVerifierExpectedSHA256),
		HostName:                       strings.TrimSpace(opts.HostName),
		Force:                          opts.Force,
		Now:                            plan.GeneratedAt,
	})
	if err != nil {
		plan.MissingInputs = append(plan.MissingInputs, "entry_package_plan_error: "+err.Error())
		plan.Checks = append(plan.Checks, Check{Name: "windows_temporary_acceptance_plan", Passed: false, Detail: err.Error()})
		return
	}
	plan.EntryPackagePlan = &EntryPackagePlan{
		SchemaVersion:      EntryPackagePlanSchemaVersion,
		TargetOS:           plan.TargetOS,
		SessionMode:        plan.SessionMode,
		PackageMode:        "visible-self-contained-connection-entry",
		OK:                 allAcceptanceChecksPassed(winPlan.Checks),
		PlanPath:           filepath.Join(winPlan.OutDir, "windows-temporary-plan.json"),
		LauncherPath:       winPlan.LauncherPath,
		PlatformPlanSchema: winPlan.SchemaVersion,
		PlatformPlanKind:   "windows-temporary-acceptance-plan",
		HumanEntryPoint:    "run the visible PowerShell launcher from this package plan",
		AgentOnlyParameters: []string{
			"manifest_url",
			"manifest_root_public_key",
			"gateway_url",
			"ticket_code",
			"transport",
			"release_bundle_url",
			"release_root_public_key",
		},
		Checks: winPlan.Checks,
	}
	plan.GeneratedFiles = append(plan.GeneratedFiles,
		GeneratedFile{Path: plan.EntryPackagePlan.PlanPath, Purpose: "reviewable Windows temporary acceptance plan inside the generic connection entry package plan"},
		GeneratedFile{Path: plan.EntryPackagePlan.LauncherPath, Purpose: "visible PowerShell connection entry launcher"},
	)
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: plan.EntryPackagePlan.PlanPath})
}

func addManagedEntryPackagePlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	missing := managedMissingInputs(plan.TargetOS, opts)
	plan.MissingInputs = append(plan.MissingInputs, missing...)
	if outDir == "" || len(missing) > 0 {
		plan.Checks = append(plan.Checks, Check{
			Name:   "managed_service_package_plan",
			Passed: false,
			Detail: "requires out_dir, managed binary path, release bundle path, and release root before generating the managed-service package plan",
		})
		return
	}
	switch plan.TargetOS {
	case "darwin":
		addManagedMacEntryPackagePlan(plan, invite, opts, outDir)
	case "linux":
		addLinuxManagedEntryPackagePlan(plan, invite, opts, outDir)
	case "windows":
		addWindowsManagedEntryPackagePlan(plan, invite, opts, outDir)
	default:
		plan.MissingInputs = append(plan.MissingInputs, "managed_service_unsupported_target_os: "+plan.TargetOS)
		plan.Checks = append(plan.Checks, Check{Name: "managed_service_package_plan", Passed: false, Detail: "unsupported target OS for managed service package plan: " + plan.TargetOS})
	}
}

func addManagedMacEntryPackagePlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	macPlan, err := acceptance.RunManagedMacServicePlan(context.Background(), acceptance.ManagedMacServiceOptions{
		OutDir:                   filepath.Join(outDir, "managed-macos"),
		BinaryPath:               strings.TrimSpace(opts.ManagedBinaryPath),
		GatewayURL:               invite.GatewayURL,
		TicketCode:               invite.Ticket.Code,
		Label:                    strings.TrimSpace(opts.ManagedServiceLabel),
		ReleaseBundle:            strings.TrimSpace(opts.ReleaseBundlePath),
		ReleaseRootPublicKey:     strings.TrimSpace(opts.ReleaseRootPublicKey),
		ReleaseRequiredArtifacts: splitCSV(opts.ReleaseBundleRequiredArtifacts),
		Transport:                managedTransport(invite.Transport),
		Force:                    opts.Force,
		Now:                      plan.GeneratedAt,
	})
	if err != nil {
		addManagedEntryPackagePlanError(plan, err)
		return
	}
	plan.EntryPackagePlan = &EntryPackagePlan{
		SchemaVersion:       EntryPackagePlanSchemaVersion,
		TargetOS:            plan.TargetOS,
		SessionMode:         plan.SessionMode,
		PackageMode:         "reviewed-managed-service-connection-entry",
		OK:                  allAcceptanceChecksPassed(macPlan.Checks),
		PlanPath:            filepath.Join(macPlan.OutDir, "service-plan.json"),
		LauncherPath:        macPlan.PlistPath,
		PlatformPlanSchema:  macPlan.SchemaVersion,
		PlatformPlanKind:    "managed-mac-service-plan",
		HumanEntryPoint:     "review the generated LaunchAgent plist and run the listed service-control commands only on the owned Mac",
		AgentOnlyParameters: managedAgentOnlyParameters(),
		Checks:              macPlan.Checks,
	}
	plan.GeneratedFiles = append(plan.GeneratedFiles,
		GeneratedFile{Path: plan.EntryPackagePlan.PlanPath, Purpose: "reviewable macOS managed-service plan inside the generic connection entry package plan"},
		GeneratedFile{Path: plan.EntryPackagePlan.LauncherPath, Purpose: "reviewable LaunchAgent plist for owned managed host enrollment"},
	)
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: plan.EntryPackagePlan.PlanPath})
}

func addLinuxManagedEntryPackagePlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	linuxPlan, err := acceptance.RunLinuxManagedServicePlan(acceptance.LinuxManagedServiceOptions{
		OutDir:                   filepath.Join(outDir, "managed-linux"),
		BinaryPath:               strings.TrimSpace(opts.ManagedBinaryPath),
		GatewayURL:               invite.GatewayURL,
		TicketCode:               invite.Ticket.Code,
		UnitName:                 strings.TrimSpace(opts.ManagedUnitName),
		ReleaseBundle:            strings.TrimSpace(opts.ReleaseBundlePath),
		ReleaseRootPublicKey:     strings.TrimSpace(opts.ReleaseRootPublicKey),
		ReleaseRequiredArtifacts: splitCSV(opts.ReleaseBundleRequiredArtifacts),
		Transport:                managedTransport(invite.Transport),
		Force:                    opts.Force,
		Now:                      plan.GeneratedAt,
	})
	if err != nil {
		addManagedEntryPackagePlanError(plan, err)
		return
	}
	plan.EntryPackagePlan = &EntryPackagePlan{
		SchemaVersion:       EntryPackagePlanSchemaVersion,
		TargetOS:            plan.TargetOS,
		SessionMode:         plan.SessionMode,
		PackageMode:         "reviewed-managed-service-connection-entry",
		OK:                  allAcceptanceChecksPassed(linuxPlan.Checks),
		PlanPath:            filepath.Join(linuxPlan.OutDir, "linux-managed-service-plan.json"),
		LauncherPath:        linuxPlan.UnitPath,
		PlatformPlanSchema:  linuxPlan.SchemaVersion,
		PlatformPlanKind:    "linux-managed-service-plan",
		HumanEntryPoint:     "review the generated systemd user unit and run the listed service-control commands only on the owned Linux host",
		AgentOnlyParameters: managedAgentOnlyParameters(),
		Checks:              linuxPlan.Checks,
	}
	plan.GeneratedFiles = append(plan.GeneratedFiles,
		GeneratedFile{Path: plan.EntryPackagePlan.PlanPath, Purpose: "reviewable Linux managed-service plan inside the generic connection entry package plan"},
		GeneratedFile{Path: plan.EntryPackagePlan.LauncherPath, Purpose: "reviewable systemd user unit for owned managed host enrollment"},
	)
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: plan.EntryPackagePlan.PlanPath})
}

func addWindowsManagedEntryPackagePlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	winPlan, err := acceptance.RunWindowsManagedServicePlan(acceptance.WindowsManagedServiceOptions{
		OutDir:                   filepath.Join(outDir, "managed-windows"),
		BinaryPath:               strings.TrimSpace(opts.ManagedBinaryPath),
		GatewayURL:               invite.GatewayURL,
		TicketCode:               invite.Ticket.Code,
		ServiceName:              strings.TrimSpace(opts.ManagedServiceName),
		ReleaseBundle:            strings.TrimSpace(opts.ReleaseBundlePath),
		ReleaseRootPublicKey:     strings.TrimSpace(opts.ReleaseRootPublicKey),
		ReleaseRequiredArtifacts: splitCSV(opts.ReleaseBundleRequiredArtifacts),
		Transport:                managedTransport(invite.Transport),
		Force:                    opts.Force,
		Now:                      plan.GeneratedAt,
	})
	if err != nil {
		addManagedEntryPackagePlanError(plan, err)
		return
	}
	plan.EntryPackagePlan = &EntryPackagePlan{
		SchemaVersion:       EntryPackagePlanSchemaVersion,
		TargetOS:            plan.TargetOS,
		SessionMode:         plan.SessionMode,
		PackageMode:         "reviewed-managed-service-connection-entry",
		OK:                  allAcceptanceChecksPassed(winPlan.Checks),
		PlanPath:            filepath.Join(winPlan.OutDir, "windows-managed-service-plan.json"),
		PlatformPlanSchema:  winPlan.SchemaVersion,
		PlatformPlanKind:    "windows-managed-service-plan",
		HumanEntryPoint:     "review the generated Windows Service plan and run the listed service-control commands only on the owned Windows host",
		AgentOnlyParameters: managedAgentOnlyParameters(),
		Checks:              winPlan.Checks,
	}
	plan.GeneratedFiles = append(plan.GeneratedFiles,
		GeneratedFile{Path: plan.EntryPackagePlan.PlanPath, Purpose: "reviewable Windows managed-service plan inside the generic connection entry package plan"},
	)
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: plan.EntryPackagePlan.PlanPath})
}

func addManagedEntryPackagePlanError(plan *Plan, err error) {
	plan.MissingInputs = append(plan.MissingInputs, "managed_service_package_plan_error: "+err.Error())
	plan.Checks = append(plan.Checks, Check{Name: "managed_service_package_plan", Passed: false, Detail: err.Error()})
}

func windowsMissingInputs(opts Options) []string {
	var missing []string
	required := map[string]string{
		"windows_host_download_url":        opts.WindowsHostDownloadURL,
		"windows_host_expected_sha256":     opts.WindowsHostExpectedSHA256,
		"release_bundle_url":               opts.ReleaseBundleURL,
		"release_root_public_key":          opts.ReleaseRootPublicKey,
		"windows_verifier_download_url":    opts.WindowsVerifierDownloadURL,
		"windows_verifier_expected_sha256": opts.WindowsVerifierExpectedSHA256,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func managedMissingInputs(targetOS string, opts Options) []string {
	required := map[string]string{
		"managed_binary_path":     opts.ManagedBinaryPath,
		"release_bundle_path":     opts.ReleaseBundlePath,
		"release_root_public_key": opts.ReleaseRootPublicKey,
	}
	var missing []string
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if targetOS == "windows" && strings.TrimSpace(opts.ManagedBinaryPath) != "" && !strings.Contains(opts.ManagedBinaryPath, `:\`) && !strings.HasPrefix(opts.ManagedBinaryPath, `\\`) {
		missing = append(missing, "managed_binary_path_absolute_windows")
	}
	return missing
}

func managedAgentOnlyParameters() []string {
	return []string{
		"manifest_url",
		"manifest_root_public_key",
		"gateway_url",
		"ticket_code",
		"transport",
		"managed_binary_path",
		"release_bundle_path",
		"release_root_public_key",
	}
}

func managedTransport(value string) string {
	switch strings.TrimSpace(value) {
	case "poll":
		return "poll"
	default:
		return "long-poll"
	}
}

func splitCSV(value string) []string {
	var values []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func writeMaterializedFiles(plan *Plan) error {
	humanPath := filepath.Join(plan.OutDir, "CONNECTION_ENTRY.md")
	if err := os.WriteFile(humanPath, []byte(renderHumanMessage(*plan)), 0o600); err != nil {
		return err
	}
	plan.HumanMessagePath = humanPath
	plan.GeneratedFiles = append(plan.GeneratedFiles, GeneratedFile{Path: humanPath, Purpose: "target-side human connection instructions"})
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(filepath.Join(plan.OutDir, "connection-entry-plan.json"), content, 0o600)
}

func renderHumanMessage(plan Plan) string {
	var builder strings.Builder
	builder.WriteString("# Remote Dev Skillkit Connection Entry\n\n")
	builder.WriteString("Open this on the target machine:\n\n")
	builder.WriteString(plan.EntryURL)
	builder.WriteString("\n\n")
	if plan.EntryCommand != "" {
		builder.WriteString("Or run this visible command:\n\n```text\n")
		builder.WriteString(plan.EntryCommand)
		builder.WriteString("\n```\n\n")
	}
	builder.WriteString("Keep the session visible. The Agent will approve scoped access, run signed jobs, collect evidence, and revoke or stop the session when finished.\n")
	return builder.String()
}

func prepareOutDir(outDir string) error {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("out directory must be empty: %s", outDir)
	}
	return nil
}

func allAcceptanceChecksPassed(checks []acceptance.Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
