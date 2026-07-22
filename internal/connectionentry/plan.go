package connectionentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/acceptance"
	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	MaterializationPlanSchemaVersion = "rdev.connection-entry.materialization-plan.v1"
	EntryPackagePlanSchemaVersion    = "rdev.connection-entry.package-plan.v1"
)

var connectionEntryMaterializationFailureHook func(phase string) error

type Options struct {
	InviteJSON                          string
	InvitePath                          string
	OutDir                              string
	TargetOS                            string
	Ownership                           string
	SessionMode                         string
	ReleaseBundleRequiredArtifacts      string
	ReleaseBundlePath                   string
	ReleaseRootPublicKey                string
	ManagedBinaryPath                   string
	ManagedServiceName                  string
	ManagedServiceLabel                 string
	ManagedUnitName                     string
	WindowsBootstrapBinaryPath          string
	WindowsBootstrapReleaseManifestPath string
	LayeredAssetsManifestURL            string
	LayeredReleaseVersion               string
	HostName                            string
	TargetArch                          string
	BootstrapCommand                    string
	Force                               bool
	Now                                 time.Time
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
	RunnerManifestSchema   string            `json:"runner_manifest_schema"`
	RunnerPlan             *RunnerPlan       `json:"runner_plan,omitempty"`
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
	ArchivePath         string             `json:"archive_path,omitempty"`
	ArchiveSHA256       string             `json:"archive_sha256,omitempty"`
	ArchiveSizeBytes    int64              `json:"archive_size_bytes,omitempty"`
	PlatformPlanSchema  string             `json:"platform_plan_schema,omitempty"`
	PlatformPlanKind    string             `json:"platform_plan_kind,omitempty"`
	HumanEntryPoint     string             `json:"human_entry_point,omitempty"`
	AgentOnlyParameters []string           `json:"agent_only_parameters"`
	Checks              []acceptance.Check `json:"checks"`
}

type RunnerPlan struct {
	SchemaVersion   string                            `json:"schema_version"`
	ManifestPath    string                            `json:"manifest_path,omitempty"`
	LauncherPath    string                            `json:"launcher_path,omitempty"`
	PlanPath        string                            `json:"plan_path,omitempty"`
	TargetOS        string                            `json:"target_os"`
	TargetArch      string                            `json:"target_arch"`
	PackageMode     string                            `json:"package_mode"`
	OK              bool                              `json:"ok"`
	SelectionOrder  []string                          `json:"selection_order"`
	ConnectionPaths []connectionrunner.ConnectionPath `json:"connection_paths"`
	HelperPolicy    connectionrunner.HelperPolicy     `json:"helper_policy"`
	Checks          []acceptance.Check                `json:"checks"`
}

func FromInvite(opts Options) (_ Plan, resultErr error) {
	var pendingArchive *pendingWindowsLayeredArchive
	defer func() {
		if pendingArchive == nil {
			return
		}
		if err := pendingArchive.discard(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("discard pending Windows layered archive: %w", err))
		}
	}()

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
	materializationDir := ""
	if outDir != "" {
		abs, err := filepath.Abs(outDir)
		if err != nil {
			return Plan{}, err
		}
		outDir = abs
		if err := prepareOutDir(outDir); err != nil {
			return Plan{}, err
		}
		materializationDir, err = createMaterializationStagingDir(outDir)
		if err != nil {
			return Plan{}, err
		}
		defer os.RemoveAll(materializationDir)
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
		RunnerManifestSchema:   connectionrunner.ManifestSchemaVersion,
	}
	plan.Checks = buildChecks(invite, plan, entryCommand)
	layeredHandoff, err := prepareWindowsLayeredHandoff(plan, invite, opts, materializationDir)
	if err != nil {
		return Plan{}, err
	}
	addRunnerPlan(&plan, invite, opts, materializationDir)
	if targetOS == "windows" && sessionMode == string(model.HostModeAttendedTemporary) {
		if layeredHandoff == nil {
			plan.MissingInputs = append(plan.MissingInputs, windowsLayeredMissingInputs(opts)...)
			plan.Checks = append(plan.Checks, Check{Name: "windows_layered_handoff", Passed: false, Detail: "missing_inputs"})
		} else {
			pendingArchive, err = materializeWindowsLayeredHandoff(&plan, layeredHandoff, materializationDir)
			if err != nil {
				return Plan{}, err
			}
		}
	}
	if sessionMode == string(model.HostModeManaged) {
		addManagedEntryPackagePlan(&plan, invite, opts, materializationDir)
	}
	if outDir != "" {
		if err := rewriteMaterializedPaths(&plan, materializationDir, outDir); err != nil {
			return Plan{}, err
		}
		if err := writeMaterializedFiles(&plan, materializationDir); err != nil {
			return Plan{}, err
		}
		var finalize func() error
		if pendingArchive != nil {
			finalize = func() error {
				if err := runConnectionEntryMaterializationFailureHook("before_windows_layered_archive_publish"); err != nil {
					return pendingArchive.publicationFailure(fmt.Errorf("before Windows layered archive publication: %w", err))
				}
				if _, err := pendingArchive.publish(filepath.Join(outDir, windowsLayeredArchiveName)); err != nil {
					return err
				}
				pendingArchive = nil
				return nil
			}
		}
		if err := publishMaterializedFiles(materializationDir, outDir, finalize); err != nil {
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
		return "break-glass mode selected only for an explicitly authorized emergency session"
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
		surface = append(surface, "reviewed managed-service entry package after owned-host activation")
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
		steps = append(steps, "Authorize the reviewed managed service lifecycle only after confirming this is an operator-owned machine.")
	} else {
		steps = append(steps, "Keep the visible temporary session open until the Agent reports completion.")
	}
	return append(steps, invite.ConnectionEntry.RevocationInstructions...)
}

func agentSteps(invite agentinvite.Invite, sessionMode string) []string {
	steps := []string{
		"Do not ask the target-side human to assemble ticket, manifest root, gateway, or transport flags.",
		"Watch rdev.sessions.status/events until the expected endpoint appears.",
		"Create small scoped rdev.sessions.task requests and review evidence before claiming completion.",
	}
	if sessionMode == string(model.HostModeManaged) {
		steps = append(steps, "Generate a reviewed managed service plan with release gates, renewal, revocation refresh, reconnect proof, and uninstall instructions.")
	} else {
		steps = append(steps, "Revoke the temporary ticket and run no-persistence checks when finished.")
	}
	return append(steps, invite.AgentNextActions...)
}

func buildChecks(invite agentinvite.Invite, plan Plan, entryCommand string) []Check {
	entryURLValid := validURL(invite.ConnectionEntry.EntryURL)
	manifestURLValid := validURL(invite.ManifestURL)
	checks := []Check{
		{Name: "entry_url", Passed: entryURLValid, Detail: validationStatusDetail(invite.ConnectionEntry.EntryURL, entryURLValid)},
		{Name: "manifest_url", Passed: manifestURLValid, Detail: validationStatusDetail(invite.ManifestURL, manifestURLValid)},
		{Name: "manifest_root_public_key", Passed: strings.TrimSpace(invite.ManifestRootPublicKey) != "", Detail: presenceStatusDetail(invite.ManifestRootPublicKey)},
		{Name: "ticket_code", Passed: strings.TrimSpace(invite.Ticket.Code) != "", Detail: presenceStatusDetail(invite.Ticket.Code)},
		{Name: "session_mode_selected", Passed: plan.SessionMode != "", Detail: plan.SessionMode},
		{Name: "entry_command_available", Passed: entryCommand != "", Detail: plan.TargetOS},
		{Name: "human_steps_no_raw_flag_assembly", Passed: true, Detail: "connection entry carries ticket/root/gateway/transport metadata for the Agent and package; humans receive a link, script, or package"},
	}
	if plan.SessionMode == string(model.HostModeManaged) {
		checks = append(checks, Check{Name: "owned_mode_requires_owned_inventory", Passed: plan.Ownership == "owned", Detail: plan.Ownership})
	}
	return checks
}

func validationStatusDetail(value string, valid bool) string {
	if strings.TrimSpace(value) == "" {
		return "missing"
	}
	if valid {
		return "valid"
	}
	return "invalid"
}

func presenceStatusDetail(value string) string {
	if strings.TrimSpace(value) == "" {
		return "missing"
	}
	return "present"
}

func validURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func addRunnerPlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	pkg, err := connectionrunner.Build(connectionrunner.Options{
		Invite:                      invite,
		OutDir:                      runnerOutDir(outDir),
		TargetOS:                    plan.TargetOS,
		TargetArch:                  opts.TargetArch,
		SessionMode:                 plan.SessionMode,
		BootstrapCommand:            opts.BootstrapCommand,
		LayeredAssetsManifestURL:    opts.LayeredAssetsManifestURL,
		LayeredReleaseRootPublicKey: opts.ReleaseRootPublicKey,
		LayeredReleaseVersion:       opts.LayeredReleaseVersion,
		HostName:                    opts.HostName,
		GeneratedAt:                 plan.GeneratedAt,
		WritePackage:                outDir != "",
	})
	if err != nil {
		plan.MissingInputs = append(plan.MissingInputs, "connection_entry_runner_error: "+err.Error())
		plan.Checks = append(plan.Checks, Check{Name: "connection_entry_runner", Passed: false, Detail: "failed"})
		return
	}
	checks := runnerChecksToAcceptance(pkg.Checks)
	plan.RunnerPlan = &RunnerPlan{
		SchemaVersion:   pkg.Manifest.SchemaVersion,
		ManifestPath:    pkg.ManifestPath,
		LauncherPath:    pkg.LauncherPath,
		PlanPath:        pkg.PlanPath,
		TargetOS:        pkg.Manifest.TargetOS,
		TargetArch:      pkg.Manifest.TargetArch,
		PackageMode:     "self-contained-connection-entry-runner",
		OK:              allAcceptanceChecksPassed(checks),
		SelectionOrder:  pkg.Plan.SelectionOrder,
		ConnectionPaths: pkg.Manifest.ConnectionPaths,
		HelperPolicy:    pkg.Manifest.HelperPolicy,
		Checks:          checks,
	}
	if outDir != "" {
		plan.GeneratedFiles = append(plan.GeneratedFiles,
			GeneratedFile{Path: pkg.ManifestPath, Purpose: "self-contained Connection Entry runner manifest with agent-only connection metadata"},
			GeneratedFile{Path: pkg.LauncherPath, Purpose: "visible target-side launcher that runs the Connection Entry runner"},
			GeneratedFile{Path: pkg.PlanPath, Purpose: "reviewable Connection Entry runner package plan and helper policy"},
		)
	}
	plan.Checks = append(plan.Checks, Check{Name: "connection_entry_runner", Passed: plan.RunnerPlan.OK, Detail: planArtifactStatus(plan.RunnerPlan.OK)})
	if plan.EntryPackagePlan == nil && outDir != "" {
		plan.EntryPackagePlan = &EntryPackagePlan{
			SchemaVersion:      EntryPackagePlanSchemaVersion,
			TargetOS:           plan.TargetOS,
			SessionMode:        plan.SessionMode,
			PackageMode:        "self-contained-connection-entry-runner",
			OK:                 plan.RunnerPlan.OK,
			PlanPath:           pkg.PlanPath,
			LauncherPath:       pkg.LauncherPath,
			PlatformPlanSchema: pkg.Manifest.SchemaVersion,
			PlatformPlanKind:   "connection-entry-runner",
			HumanEntryPoint:    "run the visible Connection Entry launcher; the runner probes and selects direct, proxy, relay, mesh, VPN, or SSH-assisted connectivity",
			AgentOnlyParameters: []string{
				"manifest_url",
				"manifest_root_public_key",
				"gateway_url",
				"ticket_code",
				"transport_preference",
				"relay_or_mesh_credentials",
				"ssh_identity",
			},
			Checks: checks,
		}
	}
}

func runnerOutDir(outDir string) string {
	if outDir == "" {
		return ""
	}
	return filepath.Join(outDir, "connection-entry-runner")
}

func runnerChecksToAcceptance(checks []connectionrunner.Check) []acceptance.Check {
	values := make([]acceptance.Check, 0, len(checks))
	for _, check := range checks {
		values = append(values, acceptance.Check{Name: check.Name, Passed: check.Passed, Detail: planArtifactStatus(check.Passed)})
	}
	return values
}

func addManagedEntryPackagePlan(plan *Plan, invite agentinvite.Invite, opts Options, outDir string) {
	missing := managedMissingInputs(plan.TargetOS, opts)
	plan.MissingInputs = append(plan.MissingInputs, missing...)
	if outDir == "" || len(missing) > 0 {
		plan.Checks = append(plan.Checks, Check{
			Name:   "managed_service_package_plan",
			Passed: false,
			Detail: "missing_inputs",
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
		plan.Checks = append(plan.Checks, Check{Name: "managed_service_package_plan", Passed: false, Detail: "unsupported"})
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
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: planArtifactStatus(plan.EntryPackagePlan.OK)})
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
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: planArtifactStatus(plan.EntryPackagePlan.OK)})
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
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: planArtifactStatus(plan.EntryPackagePlan.OK)})
}

func addManagedEntryPackagePlanError(plan *Plan, err error) {
	plan.MissingInputs = append(plan.MissingInputs, "managed_service_package_plan_error: "+err.Error())
	plan.Checks = append(plan.Checks, Check{Name: "managed_service_package_plan", Passed: false, Detail: "failed"})
}

func planArtifactStatus(passed bool) string {
	if passed {
		return "ready"
	}
	return "failed"
}

func windowsLayeredMissingInputs(opts Options) []string {
	required := []struct {
		name  string
		value string
	}{
		{name: "windows_bootstrap_binary", value: opts.WindowsBootstrapBinaryPath},
		{name: "windows_bootstrap_release_manifest", value: opts.WindowsBootstrapReleaseManifestPath},
		{name: "layered_assets_manifest_url", value: opts.LayeredAssetsManifestURL},
		{name: "layered_release_version", value: opts.LayeredReleaseVersion},
		{name: "release_root_public_key", value: opts.ReleaseRootPublicKey},
	}
	missing := make([]string, 0, len(required))
	for _, input := range required {
		if strings.TrimSpace(input.value) == "" {
			missing = append(missing, input.name)
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

func writeMaterializedFiles(plan *Plan, materializationDir string) error {
	if err := runConnectionEntryMaterializationFailureHook("before_top_level_files"); err != nil {
		return err
	}
	humanPath := filepath.Join(plan.OutDir, "CONNECTION_ENTRY.md")
	if err := os.WriteFile(filepath.Join(materializationDir, "CONNECTION_ENTRY.md"), []byte(renderHumanMessage(*plan)), 0o600); err != nil {
		return err
	}
	plan.HumanMessagePath = humanPath
	plan.GeneratedFiles = append(plan.GeneratedFiles, GeneratedFile{Path: humanPath, Purpose: "target-side human connection instructions"})
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.WriteFile(filepath.Join(materializationDir, "connection-entry-plan.json"), content, 0o600); err != nil {
		return err
	}
	return runConnectionEntryMaterializationFailureHook("after_top_level_files")
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
	builder.WriteString("Keep the session visible. The Agent will authorize scoped access, run signed session tasks, collect evidence, and revoke or stop the session when finished.\n")
	return builder.String()
}

func prepareOutDir(outDir string) error {
	entries, err := os.ReadDir(outDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("out directory must be empty: %s", outDir)
	}
	return nil
}

func createMaterializationStagingDir(outDir string) (string, error) {
	parentDir := filepath.Dir(outDir)
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return "", err
	}
	stagingDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(outDir)+".staging-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(stagingDir, 0o700); err != nil {
		os.RemoveAll(stagingDir)
		return "", err
	}
	return stagingDir, nil
}

func rewriteMaterializedPaths(plan *Plan, stagingDir, outDir string) error {
	if err := filepath.WalkDir(stagingDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}
		return rewriteMaterializedJSONFile(path, stagingDir, outDir)
	}); err != nil {
		return err
	}

	plan.OutDir = outDir
	plan.HumanMessagePath = rewriteMaterializedPath(plan.HumanMessagePath, stagingDir, outDir)
	for index := range plan.GeneratedFiles {
		plan.GeneratedFiles[index].Path = rewriteMaterializedPath(plan.GeneratedFiles[index].Path, stagingDir, outDir)
	}
	for index := range plan.Checks {
		plan.Checks[index].Detail = rewriteMaterializedPath(plan.Checks[index].Detail, stagingDir, outDir)
	}
	for index := range plan.MissingInputs {
		plan.MissingInputs[index] = rewriteMaterializedPath(plan.MissingInputs[index], stagingDir, outDir)
	}
	if plan.RunnerPlan != nil {
		plan.RunnerPlan.ManifestPath = rewriteMaterializedPath(plan.RunnerPlan.ManifestPath, stagingDir, outDir)
		plan.RunnerPlan.LauncherPath = rewriteMaterializedPath(plan.RunnerPlan.LauncherPath, stagingDir, outDir)
		plan.RunnerPlan.PlanPath = rewriteMaterializedPath(plan.RunnerPlan.PlanPath, stagingDir, outDir)
		for index := range plan.RunnerPlan.Checks {
			plan.RunnerPlan.Checks[index].Detail = rewriteMaterializedPath(plan.RunnerPlan.Checks[index].Detail, stagingDir, outDir)
		}
	}
	if plan.EntryPackagePlan != nil {
		plan.EntryPackagePlan.PlanPath = rewriteMaterializedPath(plan.EntryPackagePlan.PlanPath, stagingDir, outDir)
		plan.EntryPackagePlan.LauncherPath = rewriteMaterializedPath(plan.EntryPackagePlan.LauncherPath, stagingDir, outDir)
		plan.EntryPackagePlan.ArchivePath = rewriteMaterializedPath(plan.EntryPackagePlan.ArchivePath, stagingDir, outDir)
		for index := range plan.EntryPackagePlan.Checks {
			plan.EntryPackagePlan.Checks[index].Detail = rewriteMaterializedPath(plan.EntryPackagePlan.Checks[index].Detail, stagingDir, outDir)
		}
	}
	return nil
}

func rewriteMaterializedJSONFile(path, stagingDir, outDir string) error {
	if filepath.Ext(path) != ".json" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	rewritten, changed := rewriteMaterializedJSONValue(value, stagingDir, outDir)
	if !changed {
		return nil
	}
	content, err = json.MarshalIndent(rewritten, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func rewriteMaterializedJSONValue(value any, stagingDir, outDir string) (any, bool) {
	switch typed := value.(type) {
	case string:
		rewritten := rewriteMaterializedPath(typed, stagingDir, outDir)
		return rewritten, rewritten != typed
	case []any:
		rewritten := make([]any, len(typed))
		changed := false
		for index, child := range typed {
			var childChanged bool
			rewritten[index], childChanged = rewriteMaterializedJSONValue(child, stagingDir, outDir)
			changed = changed || childChanged
		}
		return rewritten, changed
	case map[string]any:
		rewritten := make(map[string]any, len(typed))
		changed := false
		for key, child := range typed {
			var childChanged bool
			rewritten[key], childChanged = rewriteMaterializedJSONValue(child, stagingDir, outDir)
			changed = changed || childChanged
		}
		return rewritten, changed
	default:
		return value, false
	}
}

func rewriteMaterializedPath(value, stagingDir, outDir string) string {
	rewritten := strings.ReplaceAll(value, stagingDir, outDir)
	stagingSlash := filepath.ToSlash(stagingDir)
	if stagingSlash != stagingDir {
		rewritten = strings.ReplaceAll(rewritten, stagingSlash, filepath.ToSlash(outDir))
	}
	return rewritten
}

func publishMaterializedFiles(stagingDir, outDir string, finalize func() error) error {
	originalOutDirExists := false
	placeholder := ""
	entries, err := os.ReadDir(outDir)
	if os.IsNotExist(err) {
		if err := os.Rename(stagingDir, outDir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if len(entries) != 0 {
		return fmt.Errorf("out directory must be empty: %s", outDir)
	} else {
		originalOutDirExists = true
		parentDir := filepath.Dir(outDir)
		placeholder, err = os.MkdirTemp(parentDir, "."+filepath.Base(outDir)+".empty-")
		if err != nil {
			return err
		}
		if err := os.Remove(placeholder); err != nil {
			_ = os.RemoveAll(placeholder)
			return err
		}
		if err := os.Rename(outDir, placeholder); err != nil {
			return err
		}
		if err := os.Rename(stagingDir, outDir); err != nil {
			if rollbackErr := os.Rename(placeholder, outDir); rollbackErr != nil {
				return errors.Join(
					fmt.Errorf("publish materialized output: %w", err),
					fmt.Errorf("restore empty output: %w", rollbackErr),
				)
			}
			return err
		}
	}

	if finalize != nil {
		if err := finalize(); err != nil {
			rollbackErr := rollbackPublishedMaterializedFiles(outDir, placeholder, originalOutDirExists)
			return errors.Join(err, rollbackErr)
		}
	}
	if placeholder != "" {
		if err := os.RemoveAll(placeholder); err != nil {
			return fmt.Errorf("remove replaced empty output directory: %w", err)
		}
	}
	return nil
}

func rollbackPublishedMaterializedFiles(outDir, placeholder string, restoreOriginal bool) error {
	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("remove failed materialized output: %w", err)
	}
	if !restoreOriginal {
		return nil
	}
	if err := os.Rename(placeholder, outDir); err != nil {
		return fmt.Errorf("restore empty output: %w", err)
	}
	return nil
}

func runConnectionEntryMaterializationFailureHook(phase string) error {
	if connectionEntryMaterializationFailureHook == nil {
		return nil
	}
	return connectionEntryMaterializationFailureHook(phase)
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
