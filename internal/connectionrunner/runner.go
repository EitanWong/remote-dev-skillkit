package connectionrunner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const (
	ManifestSchemaVersion = "rdev.connection-entry.runner.v1"
	PlanSchemaVersion     = "rdev.connection-entry.runner-plan.v1"
	EvidenceSchemaVersion = "rdev.connection-entry.runner-evidence.v1"
)

type Options struct {
	Invite       agentinvite.Invite
	OutDir       string
	TargetOS     string
	TargetArch   string
	SessionMode  string
	RdevCommand  string
	HostName     string
	GeneratedAt  time.Time
	WritePackage bool
}

type Manifest struct {
	SchemaVersion          string           `json:"schema_version"`
	GeneratedAt            time.Time        `json:"generated_at"`
	ConnectionEntryName    string           `json:"connection_entry_name"`
	TargetOS               string           `json:"target_os"`
	TargetArch             string           `json:"target_arch"`
	SessionMode            string           `json:"session_mode"`
	Mode                   model.HostMode   `json:"mode"`
	HostName               string           `json:"host_name,omitempty"`
	ManifestURL            string           `json:"manifest_url"`
	ManifestRootPublicKey  string           `json:"manifest_root_public_key"`
	GatewayURL             string           `json:"gateway_url"`
	JoinURL                string           `json:"join_url"`
	TransportPreference    string           `json:"transport_preference"`
	Once                   bool             `json:"once"`
	ConnectionPaths        []ConnectionPath `json:"connection_paths"`
	RuntimeProbes          []RuntimeProbe   `json:"runtime_probes"`
	HelperPolicy           HelperPolicy     `json:"helper_policy"`
	HumanConsent           []string         `json:"human_consent"`
	AgentOnlyParameters    []string         `json:"agent_only_parameters"`
	StopAndCleanup         []string         `json:"stop_and_cleanup"`
	MissingInputs          []string         `json:"missing_inputs,omitempty"`
	NoManualAssembly       bool             `json:"no_manual_assembly"`
	NoPersistenceByDefault bool             `json:"no_persistence_by_default"`
}

type ConnectionPath struct {
	ID                             string   `json:"id"`
	Priority                       int      `json:"priority"`
	Kind                           string   `json:"kind"`
	Status                         string   `json:"status"`
	BestFor                        string   `json:"best_for"`
	Probe                          []string `json:"probe"`
	RequiredTools                  []string `json:"required_tools,omitempty"`
	GatewayEnvVars                 []string `json:"gateway_env_vars,omitempty"`
	DependencyInstallActionEnvVars []string `json:"dependency_install_action_env_vars,omitempty"`
	HelperStartArgvEnvVars         []string `json:"helper_start_argv_env_vars,omitempty"`
	UsesHostServe                  bool     `json:"uses_host_serve"`
	GatewayOverride                string   `json:"gateway_override,omitempty"`
	TransportOverride              string   `json:"transport_override,omitempty"`
	ExecuteWhen                    []string `json:"execute_when"`
	ApprovalRequired               []string `json:"approval_required,omitempty"`
	Evidence                       []string `json:"evidence"`
}

type RuntimeProbe struct {
	Name       string   `json:"name"`
	Intent     string   `json:"intent"`
	Commands   []string `json:"commands"`
	NoSecrets  bool     `json:"no_secrets"`
	CanExecute bool     `json:"can_execute_without_approval"`
}

type HelperPolicy struct {
	SchemaVersion            string   `json:"schema_version"`
	AutoExecuteAllowed       []string `json:"auto_execute_allowed"`
	ApprovalRequired         []string `json:"approval_required"`
	PreferredOpenSourceFirst []string `json:"preferred_open_source_first"`
	Disallowed               []string `json:"disallowed"`
}

type DependencyInstallAction struct {
	SchemaVersion     string   `json:"schema_version,omitempty"`
	Tool              string   `json:"tool"`
	Argv              []string `json:"argv"`
	Scope             string   `json:"scope,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	ExpectedSHA256    string   `json:"expected_sha256,omitempty"`
	RequiresElevation bool     `json:"requires_elevation,omitempty"`
}

type Package struct {
	SchemaVersion  string     `json:"schema_version"`
	OutDir         string     `json:"out_dir,omitempty"`
	ManifestPath   string     `json:"manifest_path,omitempty"`
	LauncherPath   string     `json:"launcher_path,omitempty"`
	LauncherSHA256 string     `json:"launcher_sha256,omitempty"`
	PlanPath       string     `json:"plan_path,omitempty"`
	Checks         []Check    `json:"checks"`
	Manifest       Manifest   `json:"manifest"`
	Plan           RunnerPlan `json:"plan"`
}

type RunnerPlan struct {
	SchemaVersion       string             `json:"schema_version"`
	GeneratedAt         time.Time          `json:"generated_at"`
	SelectedOS          string             `json:"selected_os"`
	SelectedArch        string             `json:"selected_arch"`
	ManifestPath        string             `json:"manifest_path,omitempty"`
	LauncherPath        string             `json:"launcher_path,omitempty"`
	ExecutionContract   []string           `json:"execution_contract"`
	SelectionOrder      []string           `json:"selection_order"`
	ConnectivityHelpers []ConnectivityTool `json:"connectivity_helpers"`
	FallbackBehavior    []string           `json:"fallback_behavior"`
	EvidenceRequired    []string           `json:"evidence_required"`
}

type ConnectivityTool struct {
	ID               string   `json:"id"`
	ToolNames        []string `json:"tool_names"`
	Status           string   `json:"status"`
	BestFor          string   `json:"best_for"`
	AutoUseWhen      []string `json:"auto_use_when"`
	ApprovalRequired []string `json:"approval_required"`
	Notes            []string `json:"notes"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type RunOptions struct {
	ManifestPath        string
	RdevCommand         string
	DryRun              bool
	ProbeTimeout        time.Duration
	ExtraHostArgs       []string
	Now                 time.Time
	LookPath            func(string) (string, error)
	CommandRunner       func(string, []string) error
	DependencyInstaller func(DependencyInstallAction) (DependencyInstallResult, error)
	HelperStarter       func([]string) (func() error, error)
	HTTPProbe           func(string, time.Duration) error
}

type RunResult struct {
	SchemaVersion               string        `json:"schema_version"`
	ManifestPath                string        `json:"manifest_path"`
	DryRun                      bool          `json:"dry_run"`
	StartedAt                   time.Time     `json:"started_at"`
	SelectedPath                string        `json:"selected_path,omitempty"`
	SelectedTransport           string        `json:"selected_transport,omitempty"`
	SelectedGatewayURL          string        `json:"selected_gateway_url,omitempty"`
	HostServeArgs               []string      `json:"host_serve_args,omitempty"`
	Executed                    bool          `json:"executed"`
	DependencyInstallConfigured bool          `json:"dependency_install_configured"`
	DependencyInstalled         bool          `json:"dependency_installed"`
	DependencyInstallTool       string        `json:"dependency_install_tool,omitempty"`
	HelperStartConfigured       bool          `json:"helper_start_configured"`
	HelperStarted               bool          `json:"helper_started"`
	HelperStartTool             string        `json:"helper_start_tool,omitempty"`
	HelperCleanupAttempted      bool          `json:"helper_cleanup_attempted,omitempty"`
	HelperCleanupSucceeded      bool          `json:"helper_cleanup_succeeded,omitempty"`
	HelperTranscript            []string      `json:"helper_transcript,omitempty"`
	ProbeResults                []ProbeResult `json:"probe_results"`
	ToolResults                 []ToolResult  `json:"tool_results"`
	ApprovalRequired            []string      `json:"approval_required,omitempty"`
	ManualActionRequired        []string      `json:"manual_action_required,omitempty"`
}

type ProbeResult struct {
	PathID string `json:"path_id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type ToolResult struct {
	Name  string `json:"name"`
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`
}

type DependencyInstallResult struct {
	InstalledBinary string `json:"installed_binary,omitempty"`
}

type EvidenceReport struct {
	SchemaVersion    string    `json:"schema_version"`
	GeneratedAt      time.Time `json:"generated_at"`
	Directory        string    `json:"directory"`
	RunnerResult     string    `json:"runner_result"`
	HelperTranscript string    `json:"helper_transcript"`
	GatewayStatus    string    `json:"gateway_status"`
	HostStatus       string    `json:"host_status"`
	ConnectionStatus string    `json:"connection_status"`
	Audit            string    `json:"audit"`
	SelectedPath     string    `json:"selected_path,omitempty"`
	Connected        bool      `json:"connected"`
}

func Build(opts Options) (Package, error) {
	if opts.Invite.SchemaVersion != agentinvite.SchemaVersion {
		return Package{}, fmt.Errorf("unsupported invite schema %q", opts.Invite.SchemaVersion)
	}
	targetOS := normalizeOS(opts.TargetOS)
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	targetArch := normalizeArch(opts.TargetArch)
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
	sessionMode := strings.TrimSpace(opts.SessionMode)
	if sessionMode == "" {
		sessionMode = string(opts.Invite.Ticket.Mode)
	}
	mode := model.HostMode(sessionMode)
	if !mode.Valid() {
		return Package{}, fmt.Errorf("unsupported runner session mode %q", sessionMode)
	}
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	manifest := Manifest{
		SchemaVersion:         ManifestSchemaVersion,
		GeneratedAt:           generatedAt.UTC(),
		ConnectionEntryName:   "Connection Entry",
		TargetOS:              targetOS,
		TargetArch:            targetArch,
		SessionMode:           sessionMode,
		Mode:                  mode,
		HostName:              strings.TrimSpace(opts.HostName),
		ManifestURL:           opts.Invite.ManifestURL,
		ManifestRootPublicKey: opts.Invite.ManifestRootPublicKey,
		GatewayURL:            opts.Invite.GatewayURL,
		JoinURL:               opts.Invite.JoinURL,
		TransportPreference:   firstNonEmpty(opts.Invite.Transport, "auto"),
		Once:                  false,
		ConnectionPaths:       connectionPaths(opts.Invite),
		RuntimeProbes:         runtimeProbes(targetOS),
		HelperPolicy:          helperPolicy(),
		HumanConsent: []string{
			"This visible Connection Entry starts an rdev host session for the support or development request.",
			"Closing the launcher stops an attended temporary session.",
			"Managed service installation is separate and requires explicit operator approval.",
		},
		AgentOnlyParameters: []string{
			"manifest_url",
			"manifest_root_public_key",
			"gateway_url",
			"ticket_code",
			"transport_preference",
			"relay_or_mesh_credentials",
			"ssh_identity",
		},
		StopAndCleanup: []string{
			"stop the visible process for attended temporary sessions",
			"revoke the ticket at the gateway when the work is complete",
			"remove temporary package files if the target-side user requests cleanup",
		},
		NoManualAssembly:       true,
		NoPersistenceByDefault: mode != model.HostModeManaged,
	}
	if manifest.ManifestURL == "" {
		manifest.MissingInputs = append(manifest.MissingInputs, "manifest_url")
	}
	if manifest.ManifestRootPublicKey == "" {
		manifest.MissingInputs = append(manifest.MissingInputs, "manifest_root_public_key")
	}
	if manifest.GatewayURL == "" {
		manifest.MissingInputs = append(manifest.MissingInputs, "gateway_url")
	}

	plan := RunnerPlan{
		SchemaVersion: PlanSchemaVersion,
		GeneratedAt:   generatedAt.UTC(),
		SelectedOS:    targetOS,
		SelectedArch:  targetArch,
		ExecutionContract: []string{
			"the target side runs one visible launcher or package runner",
			"the runner verifies and consumes the signed join manifest before host registration",
			"the runner probes direct gateway reachability before trying helper connectivity",
			"the runner invokes rdev host serve with --transport auto unless a selected path requires a narrower fallback",
			"connectivity helpers provide routing only; rdev ticket, host approval, signed jobs, and policy authorize work",
		},
		SelectionOrder: []string{
			"native-direct-gateway",
			"native-lan-gateway",
			"proxy-aware-https",
			"existing-ssh-tunnel",
			"existing-frp-or-chisel-relay",
			"existing-headscale-tailscale-mesh",
			"existing-wireguard-vpn",
		},
		ConnectivityHelpers: connectivityTools(),
		FallbackBehavior: []string{
			"start with WSS, then HTTPS long-poll, then short polling through rdev host serve --transport auto",
			"use proxy environment variables automatically when present",
			"use existing non-privileged relay, mesh, VPN, or SSH tooling only when the route and credential choice are unambiguous",
			"report a machine-readable manual_action_required item instead of guessing when route, credential, enrollment, or privilege is unclear",
		},
		EvidenceRequired: []string{
			"runner manifest checksum",
			"selected connection path",
			"gateway probe result",
			"helper tool detection result",
			"host registration result",
			"transport fallback attempts from rdev host serve",
		},
	}

	pkg := Package{
		SchemaVersion: PlanSchemaVersion,
		OutDir:        strings.TrimSpace(opts.OutDir),
		Checks:        runnerChecks(manifest),
		Manifest:      manifest,
		Plan:          plan,
	}
	if !opts.WritePackage || strings.TrimSpace(opts.OutDir) == "" {
		return pkg, nil
	}
	if err := writePackage(&pkg, rdevCommand); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

func LoadManifest(path string) (Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return Manifest{}, fmt.Errorf("runner manifest is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode runner manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported runner manifest schema %q", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.ManifestURL) == "" {
		return fmt.Errorf("runner manifest missing manifest_url")
	}
	if strings.TrimSpace(manifest.ManifestRootPublicKey) == "" {
		return fmt.Errorf("runner manifest missing manifest_root_public_key")
	}
	if strings.TrimSpace(manifest.GatewayURL) == "" {
		return fmt.Errorf("runner manifest missing gateway_url")
	}
	if !manifest.Mode.Valid() {
		return fmt.Errorf("runner manifest has invalid mode %q", manifest.Mode)
	}
	if len(manifest.ConnectionPaths) == 0 {
		return fmt.Errorf("runner manifest has no connection paths")
	}
	return nil
}

func Run(opts RunOptions) (RunResult, error) {
	manifest, err := LoadManifest(opts.ManifestPath)
	if err != nil {
		return RunResult{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	probeTimeout := opts.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 5 * time.Second
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	httpProbe := opts.HTTPProbe
	if httpProbe == nil {
		httpProbe = probeGatewayHTTP
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	result := RunResult{
		SchemaVersion: "rdev.connection-entry.runner-result.v1",
		ManifestPath:  opts.ManifestPath,
		DryRun:        opts.DryRun,
		StartedAt:     now.UTC(),
	}
	toolNames := requiredToolNames(manifest)
	for _, name := range toolNames {
		path, err := lookPath(name)
		item := ToolResult{Name: name, Found: err == nil, Path: path}
		result.ToolResults = append(result.ToolResults, item)
	}
	selected, probes := selectPath(manifest, result.ToolResults, httpProbe, probeTimeout)
	result.ProbeResults = probes
	if selected == nil {
		result.ManualActionRequired = append(result.ManualActionRequired, "no usable direct, proxy, relay, mesh, VPN, or SSH path was detected; provide a gateway, relay, mesh, VPN, SSH, or hosted route for this Connection Entry")
		appendHelperTranscript(&result, "manual_action_required no_usable_connection_path")
		return result, nil
	}
	result.SelectedPath = selected.ID
	result.SelectedTransport = firstNonEmpty(selected.TransportOverride, manifest.TransportPreference, "auto")
	result.SelectedGatewayURL = firstNonEmpty(selected.GatewayOverride, manifest.GatewayURL)
	result.ApprovalRequired = append(result.ApprovalRequired, selected.ApprovalRequired...)
	result.HostServeArgs = hostServeArgs(manifest, result.SelectedGatewayURL, result.SelectedTransport, opts.ExtraHostArgs)
	appendHelperTranscript(&result, "selected_path "+selected.ID)
	appendHelperTranscript(&result, "selected_transport "+result.SelectedTransport)
	installAction, installTool, installConfigured, err := dependencyInstallAction(*selected, manifest.Mode)
	if err != nil {
		return result, err
	}
	result.DependencyInstallConfigured = installConfigured
	result.DependencyInstallTool = installTool
	if installConfigured {
		appendHelperTranscript(&result, "dependency_install_configured tool="+installTool)
	}
	helperArgv, helperTool, helperStartConfigured, err := helperStartArgv(*selected)
	if err != nil {
		return result, err
	}
	result.HelperStartConfigured = helperStartConfigured
	result.HelperStartTool = helperTool
	if helperStartConfigured {
		appendHelperTranscript(&result, "helper_start_configured tool="+helperTool)
	}
	if len(result.ApprovalRequired) > 0 && selected.Status != "auto-executable-when-already-configured" {
		result.ManualActionRequired = append(result.ManualActionRequired, result.ApprovalRequired...)
		appendHelperTranscript(&result, "manual_action_required approval_required")
		return result, nil
	}
	if opts.DryRun {
		appendHelperTranscript(&result, "dry_run no_execution")
		return result, nil
	}
	commandRunner := opts.CommandRunner
	if commandRunner == nil {
		commandRunner = runCommand
	}
	if installConfigured && !requiredToolsFoundInResults(selected.RequiredTools, result.ToolResults) {
		installer := opts.DependencyInstaller
		if installer == nil {
			installer = runDependencyInstallAction
		}
		installResult, err := installer(installAction)
		if err != nil {
			return result, fmt.Errorf("install dependency %s: %w", installTool, err)
		}
		result.DependencyInstalled = true
		appendHelperTranscript(&result, "dependency_installed tool="+installTool)
		installedPath := strings.TrimSpace(installResult.InstalledBinary)
		if installedPath == "" {
			var err error
			installedPath, err = lookPath(installTool)
			if err != nil {
				return result, fmt.Errorf("dependency install did not make %s available: %w", installTool, err)
			}
		}
		result.ToolResults = appendOrReplaceToolResult(result.ToolResults, ToolResult{Name: installTool, Found: true, Path: installedPath})
		helperArgv = replaceHelperArgvTool(helperArgv, installTool, installedPath)
	}
	var cleanup func() error
	if helperStartConfigured {
		helperStarter := opts.HelperStarter
		if helperStarter == nil {
			helperStarter = startHelperCommand
		}
		cleanup, err = helperStarter(helperArgv)
		if err != nil {
			return result, fmt.Errorf("start helper %s: %w", helperTool, err)
		}
		result.HelperStarted = true
		appendHelperTranscript(&result, "helper_started tool="+helperTool)
		if err := waitForGateway(result.SelectedGatewayURL, httpProbe, probeTimeout); err != nil {
			cleanupHelper(&result, cleanup, helperTool)
			return result, fmt.Errorf("helper gateway not reachable after starting %s: %w", helperTool, err)
		}
		appendHelperTranscript(&result, "helper_gateway_reachable selected_path="+result.SelectedPath)
	}
	appendHelperTranscript(&result, "host_serve_invoked")
	if err := commandRunner(rdevCommand, result.HostServeArgs); err != nil {
		cleanupHelper(&result, cleanup, helperTool)
		return result, err
	}
	result.Executed = true
	appendHelperTranscript(&result, "host_serve_completed")
	cleanupHelper(&result, cleanup, helperTool)
	return result, nil
}

func appendHelperTranscript(result *RunResult, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	result.HelperTranscript = append(result.HelperTranscript, line)
}

func cleanupHelper(result *RunResult, cleanup func() error, helperTool string) {
	if cleanup == nil || result.HelperCleanupAttempted {
		return
	}
	result.HelperCleanupAttempted = true
	appendHelperTranscript(result, "helper_cleanup_attempted tool="+helperTool)
	if err := cleanup(); err != nil {
		appendHelperTranscript(result, "helper_cleanup_failed tool="+helperTool)
		return
	}
	result.HelperCleanupSucceeded = true
	appendHelperTranscript(result, "helper_cleanup_succeeded tool="+helperTool)
}

func WriteAcceptanceEvidence(dir string, result RunResult, generatedAt time.Time) (EvidenceReport, error) {
	if strings.TrimSpace(dir) == "" {
		return EvidenceReport{}, errors.New("evidence directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return EvidenceReport{}, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return EvidenceReport{}, err
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	report := EvidenceReport{
		SchemaVersion:    EvidenceSchemaVersion,
		GeneratedAt:      generatedAt.UTC(),
		Directory:        abs,
		RunnerResult:     filepath.Join(abs, "runner-result.json"),
		HelperTranscript: filepath.Join(abs, "helper-transcript.txt"),
		GatewayStatus:    filepath.Join(abs, "gateway-status.json"),
		HostStatus:       filepath.Join(abs, "host-status.json"),
		ConnectionStatus: filepath.Join(abs, "connection-status.json"),
		Audit:            filepath.Join(abs, "audit.jsonl"),
		SelectedPath:     result.SelectedPath,
		Connected:        result.Executed && len(result.ManualActionRequired) == 0,
	}
	if err := writeJSONFile(report.RunnerResult, result); err != nil {
		return EvidenceReport{}, err
	}
	if err := writeTextFile(report.HelperTranscript, HelperTranscriptTextForEvidence(result)); err != nil {
		return EvidenceReport{}, err
	}
	if err := writeJSONFile(report.GatewayStatus, gatewayStatusEvidence(result)); err != nil {
		return EvidenceReport{}, err
	}
	if err := writeJSONFile(report.HostStatus, hostStatusEvidence(result)); err != nil {
		return EvidenceReport{}, err
	}
	if err := writeJSONFile(report.ConnectionStatus, connectionStatusEvidence(result)); err != nil {
		return EvidenceReport{}, err
	}
	if err := writeTextFile(report.Audit, auditEvidenceJSONL(result, generatedAt.UTC())); err != nil {
		return EvidenceReport{}, err
	}
	if err := writeJSONFile(filepath.Join(abs, "evidence-report.json"), report); err != nil {
		return EvidenceReport{}, err
	}
	return report, nil
}

func HelperTranscriptTextForEvidence(result RunResult) string {
	lines := append([]string(nil), result.HelperTranscript...)
	if len(lines) == 0 {
		lines = []string{"no_helper_transcript selected_path=" + result.SelectedPath}
	}
	return strings.Join(lines, "\n") + "\n"
}

func gatewayStatusEvidence(result RunResult) map[string]any {
	reachable := selectedPathProbeOK(result) || helperTranscriptHas(result, "helper_gateway_reachable")
	return map[string]any{
		"schema_version":  "rdev.connection-entry.gateway-status.v1",
		"ok":              result.SelectedPath != "" && reachable,
		"selected_path":   result.SelectedPath,
		"gateway_present": result.SelectedGatewayURL != "",
		"reachable":       reachable,
	}
}

func hostStatusEvidence(result RunResult) map[string]any {
	status := "not-executed"
	if result.Executed {
		status = "completed"
	} else if len(result.ManualActionRequired) > 0 {
		status = "manual-action-required"
	}
	return map[string]any{
		"schema_version":     "rdev.connection-entry.host-status.v1",
		"ok":                 result.Executed,
		"host_status":        status,
		"selected_path":      result.SelectedPath,
		"host_serve_invoked": helperTranscriptHas(result, "host_serve_invoked"),
		"host_serve_done":    result.Executed,
	}
}

func connectionStatusEvidence(result RunResult) map[string]any {
	connected := result.Executed && len(result.ManualActionRequired) == 0
	return map[string]any{
		"schema_version": "rdev.connection-entry.connection-status.v1",
		"ok":             connected,
		"connected":      connected,
		"selected_path":  result.SelectedPath,
		"transport":      result.SelectedTransport,
		"helper_started": result.HelperStarted,
		"host_executed":  result.Executed,
	}
}

func auditEvidenceJSONL(result RunResult, generatedAt time.Time) string {
	events := []map[string]any{
		{"schema_version": "rdev.connection-entry.runner-audit-event.v1", "generated_at": generatedAt, "event": "selected_path", "selected_path": result.SelectedPath},
		{"schema_version": "rdev.connection-entry.runner-audit-event.v1", "generated_at": generatedAt, "event": "helper_start", "configured": result.HelperStartConfigured, "started": result.HelperStarted, "tool": result.HelperStartTool},
		{"schema_version": "rdev.connection-entry.runner-audit-event.v1", "generated_at": generatedAt, "event": "host_serve", "executed": result.Executed},
		{"schema_version": "rdev.connection-entry.runner-audit-event.v1", "generated_at": generatedAt, "event": "cleanup", "attempted": result.HelperCleanupAttempted, "succeeded": result.HelperCleanupSucceeded},
	}
	var builder strings.Builder
	for _, event := range events {
		content, err := json.Marshal(event)
		if err != nil {
			continue
		}
		builder.Write(content)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func selectedPathProbeOK(result RunResult) bool {
	for _, probe := range result.ProbeResults {
		if probe.PathID == result.SelectedPath && probe.OK {
			return true
		}
	}
	return false
}

func helperTranscriptHas(result RunResult, marker string) bool {
	for _, line := range result.HelperTranscript {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func writeJSONFile(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeTextFile(path, string(content))
}

func writeTextFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func writePackage(pkg *Package, rdevCommand string) error {
	outDir, err := filepath.Abs(pkg.OutDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	pkg.OutDir = outDir
	manifestPath := filepath.Join(outDir, "connection-entry-runner.json")
	manifestContent, err := json.MarshalIndent(pkg.Manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestContent = append(manifestContent, '\n')
	if err := os.WriteFile(manifestPath, manifestContent, 0o600); err != nil {
		return err
	}
	pkg.ManifestPath = manifestPath
	pkg.Plan.ManifestPath = manifestPath
	launcherPath := filepath.Join(outDir, launcherName(pkg.Manifest.TargetOS))
	launcherContent := renderLauncher(pkg.Manifest.TargetOS, rdevCommand, manifestPath)
	if err := os.WriteFile(launcherPath, []byte(launcherContent), 0o700); err != nil {
		return err
	}
	pkg.LauncherPath = launcherPath
	pkg.Plan.LauncherPath = launcherPath
	sum := sha256.Sum256([]byte(launcherContent))
	pkg.LauncherSHA256 = hex.EncodeToString(sum[:])
	planPath := filepath.Join(outDir, "connection-entry-runner-plan.json")
	pkg.PlanPath = planPath
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(planPath, content, 0o600)
}

func connectionPaths(invite agentinvite.Invite) []ConnectionPath {
	gatewayURL := strings.TrimSpace(invite.GatewayURL)
	manifestURL := strings.TrimSpace(invite.ManifestURL)
	root := strings.TrimSpace(invite.ManifestRootPublicKey)
	return []ConnectionPath{
		{
			ID:                "native-direct-gateway",
			Priority:          1,
			Kind:              "native",
			Status:            "implemented",
			BestFor:           "target can reach the gateway URL directly over outbound HTTP(S)/WSS",
			Probe:             []string{"GET gateway /healthz", "GET signed join manifest", "verify manifest root"},
			UsesHostServe:     true,
			TransportOverride: "auto",
			ExecuteWhen:       []string{"gateway health check succeeds", "signed join manifest is reachable"},
			Evidence:          []string{"gateway health probe", "manifest verification", "rdev host serve transport fallback output"},
		},
		{
			ID:                "native-lan-gateway",
			Priority:          2,
			Kind:              "native",
			Status:            lanStatus(gatewayURL),
			BestFor:           "agent and target share a LAN, VPN, or routed private subnet",
			Probe:             []string{"resolve gateway host", "probe gateway port", "GET gateway /healthz"},
			UsesHostServe:     true,
			TransportOverride: "auto",
			ExecuteWhen:       []string{"gateway host is private or local and reachable from the target"},
			Evidence:          []string{"selected LAN/private gateway URL", "port probe", "health probe"},
		},
		{
			ID:                "proxy-aware-https",
			Priority:          3,
			Kind:              "native",
			Status:            "implemented-via-standard-http-proxy-environment",
			BestFor:           "corporate network where HTTPS egress requires HTTP_PROXY, HTTPS_PROXY, or NO_PROXY",
			Probe:             []string{"inspect proxy environment variables", "GET signed join manifest through default HTTP client"},
			UsesHostServe:     true,
			TransportOverride: "auto",
			ExecuteWhen:       []string{"proxy environment variables are present and manifest fetch succeeds"},
			Evidence:          []string{"proxy variable names only", "manifest fetch result"},
		},
		{
			ID:                             "existing-ssh-tunnel",
			Priority:                       4,
			Kind:                           "ssh",
			Status:                         "auto-executable-when-already-configured",
			BestFor:                        "operator already has SSH config or ssh-agent route that can reach the gateway side",
			Probe:                          []string{"ssh executable present", "SSH endpoint is configured outside public artifacts"},
			RequiredTools:                  []string{"ssh"},
			GatewayEnvVars:                 []string{"RDEV_SSH_GATEWAY_URL"},
			DependencyInstallActionEnvVars: []string{"RDEV_SSH_INSTALL_ACTION_JSON"},
			HelperStartArgvEnvVars:         []string{"RDEV_SSH_TUNNEL_START_ARGV_JSON"},
			UsesHostServe:                  true,
			ExecuteWhen:                    []string{"SSH route and local forward are already configured by operator policy"},
			ApprovalRequired:               []string{"ask before creating new SSH keys, editing SSH config, or selecting among ambiguous SSH identities"},
			Evidence:                       []string{"ssh tool detection", "local forward endpoint", "host serve through forwarded gateway"},
		},
		{
			ID:                             "existing-frp-or-chisel-relay",
			Priority:                       5,
			Kind:                           "relay",
			Status:                         "auto-executable-when-already-configured",
			BestFor:                        "NAT, firewall, or CGNAT where a preconfigured open-source relay is available",
			Probe:                          []string{"frpc or chisel executable present", "relay config path discovered from local environment or runner policy"},
			RequiredTools:                  []string{"frpc", "chisel"},
			GatewayEnvVars:                 []string{"RDEV_RELAY_GATEWAY_URL"},
			DependencyInstallActionEnvVars: []string{"RDEV_RELAY_INSTALL_ACTION_JSON"},
			HelperStartArgvEnvVars:         []string{"RDEV_RELAY_START_ARGV_JSON"},
			UsesHostServe:                  true,
			ExecuteWhen:                    []string{"relay config exists and credential choice is unambiguous"},
			ApprovalRequired:               []string{"ask before downloading relay binaries, editing relay config, creating relay accounts, opening firewall ports, or using paid relay services"},
			Evidence:                       []string{"relay tool detection", "redacted relay config identity", "selected forwarded gateway"},
		},
		{
			ID:                             "existing-headscale-tailscale-mesh",
			Priority:                       6,
			Kind:                           "mesh",
			Status:                         "auto-executable-when-already-configured",
			BestFor:                        "owned or managed hosts already enrolled in Tailscale/headscale-compatible mesh",
			Probe:                          []string{"tailscale executable present", "mesh status reports a usable route"},
			RequiredTools:                  []string{"tailscale"},
			GatewayEnvVars:                 []string{"RDEV_MESH_GATEWAY_URL"},
			DependencyInstallActionEnvVars: []string{"RDEV_MESH_INSTALL_ACTION_JSON"},
			HelperStartArgvEnvVars:         []string{"RDEV_MESH_START_ARGV_JSON"},
			UsesHostServe:                  true,
			ExecuteWhen:                    []string{"mesh route to gateway exists and no new enrollment is needed"},
			ApprovalRequired:               []string{"ask before mesh enrollment, auth-key use, ACL changes, DNS changes, or persistent service changes"},
			Evidence:                       []string{"mesh tool detection", "redacted mesh route status", "selected mesh gateway"},
		},
		{
			ID:                             "existing-wireguard-vpn",
			Priority:                       7,
			Kind:                           "vpn",
			Status:                         "auto-executable-when-already-configured",
			BestFor:                        "owned hosts with an existing WireGuard tunnel profile that reaches the gateway",
			Probe:                          []string{"wg or wg-quick executable present", "configured tunnel is already active"},
			RequiredTools:                  []string{"wg", "wg-quick"},
			GatewayEnvVars:                 []string{"RDEV_VPN_GATEWAY_URL"},
			DependencyInstallActionEnvVars: []string{"RDEV_VPN_INSTALL_ACTION_JSON"},
			HelperStartArgvEnvVars:         []string{"RDEV_VPN_START_ARGV_JSON"},
			UsesHostServe:                  true,
			ExecuteWhen:                    []string{"WireGuard route is active and gateway health check succeeds"},
			ApprovalRequired:               []string{"ask before creating keys, importing profiles, starting persistent VPN tunnels, changing DNS, or editing firewall routes"},
			Evidence:                       []string{"WireGuard tool detection", "active route proof", "selected VPN gateway"},
		},
		{
			ID:                "manifest-only-native-fallback",
			Priority:          8,
			Kind:              "native",
			Status:            "implemented",
			BestFor:           "gateway health endpoint is unavailable but signed join manifest can still be fetched",
			Probe:             []string{"GET signed join manifest", "verify manifest root"},
			UsesHostServe:     true,
			TransportOverride: "auto",
			ExecuteWhen:       []string{"manifest URL is reachable and root is pinned"},
			Evidence:          []string{"manifest URL", "manifest root verification"},
		},
		{
			ID:            "runner-metadata",
			Priority:      99,
			Kind:          "metadata",
			Status:        "not-executable",
			BestFor:       "carrying agent-only values so target-side humans never assemble flags",
			Probe:         []string{"manifest_url=" + manifestURL, "manifest_root_public_key_present=" + fmt.Sprint(root != ""), "gateway_url_present=" + fmt.Sprint(gatewayURL != "")},
			UsesHostServe: false,
			ExecuteWhen:   []string{"never selected as a connection path"},
			Evidence:      []string{"runner manifest checksum"},
		},
	}
}

func runtimeProbes(targetOS string) []RuntimeProbe {
	common := []RuntimeProbe{
		{Name: "os-arch", Intent: "select the correct package and launcher", Commands: []string{"runtime.GOOS/runtime.GOARCH or platform shell equivalent"}, NoSecrets: true, CanExecute: true},
		{Name: "gateway-health", Intent: "prove direct gateway reachability", Commands: []string{"GET <gateway>/healthz", "GET <manifest_url>"}, NoSecrets: true, CanExecute: true},
		{Name: "proxy-env", Intent: "reuse standard proxy configuration without printing values", Commands: []string{"inspect HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, NO_PROXY variable names"}, NoSecrets: true, CanExecute: true},
		{Name: "helper-tools", Intent: "detect existing connectivity helpers", Commands: []string{"which ssh frpc chisel tailscale wg wg-quick", "Get-Command ssh, frpc, chisel, tailscale, wg"}, NoSecrets: true, CanExecute: true},
	}
	if targetOS == "windows" {
		common = append(common, RuntimeProbe{Name: "windows-network", Intent: "collect target-side reachability without changing firewall state", Commands: []string{"Test-NetConnection gateway-host -Port gateway-port", "Get-NetRoute"}, NoSecrets: true, CanExecute: true})
	} else {
		common = append(common, RuntimeProbe{Name: "unix-network", Intent: "collect target-side reachability without changing firewall state", Commands: []string{"nc -vz gateway-host gateway-port", "ip route get gateway-host", "route -n get gateway-host"}, NoSecrets: true, CanExecute: true})
	}
	return common
}

func helperPolicy() HelperPolicy {
	return HelperPolicy{
		SchemaVersion: "rdev.connection-entry.helper-policy.v1",
		AutoExecuteAllowed: []string{
			"standard HTTP(S)/WSS outbound requests through the OS/default proxy configuration",
			"already-active mesh or VPN routes",
			"already-configured non-privileged SSH local forwards",
			"already-configured non-privileged frp or Chisel relay clients",
			"operator-approved dependency install actions scoped to user, workspace, attended-visible, or managed-approved policy",
		},
		ApprovalRequired: []string{
			"installing or downloading frp, Chisel, headscale, Tailscale, WireGuard, SSH, or any connectivity helper",
			"creating, rotating, or importing SSH keys, WireGuard keys, mesh auth keys, relay tokens, or cloud credentials",
			"starting persistent services, changing firewall rules, changing DNS, adding routes, or mutating security policy",
			"using paid hosted relay, cloud resources, or third-party accounts",
			"running privileged commands or elevation prompts",
		},
		PreferredOpenSourceFirst: []string{
			"frp",
			"Chisel",
			"headscale-compatible mesh",
			"WireGuard",
			"OpenSSH",
		},
		Disallowed: []string{
			"hidden installation",
			"UAC, sudo, Gatekeeper, TCC, security product, or firewall bypass",
			"ExecutionPolicy Bypass, encoded shell commands, or shell command-string wrappers",
			"printing private keys, relay credentials, mesh tokens, private server addresses, or secrets into public artifacts",
			"treating connectivity helper success as job authorization",
		},
	}
}

func runnerChecks(manifest Manifest) []Check {
	return []Check{
		{Name: "runner_schema", Passed: manifest.SchemaVersion == ManifestSchemaVersion, Detail: manifest.SchemaVersion},
		{Name: "manifest_url", Passed: strings.TrimSpace(manifest.ManifestURL) != "", Detail: manifest.ManifestURL},
		{Name: "manifest_root_public_key", Passed: strings.TrimSpace(manifest.ManifestRootPublicKey) != "", Detail: manifest.ManifestRootPublicKey},
		{Name: "gateway_url", Passed: strings.TrimSpace(manifest.GatewayURL) != "", Detail: manifest.GatewayURL},
		{Name: "connection_paths", Passed: len(manifest.ConnectionPaths) >= 7, Detail: fmt.Sprint(len(manifest.ConnectionPaths))},
		{Name: "no_manual_assembly", Passed: manifest.NoManualAssembly, Detail: "runner carries ticket/root/gateway/transport metadata"},
		{Name: "helper_policy_requires_approval_for_mutation", Passed: len(manifest.HelperPolicy.ApprovalRequired) > 0, Detail: manifest.HelperPolicy.SchemaVersion},
	}
}

func connectivityTools() []ConnectivityTool {
	return []ConnectivityTool{
		{
			ID:        "ssh",
			ToolNames: []string{"ssh"},
			Status:    "use-existing-config-only",
			BestFor:   "existing authorized tunnel route",
			AutoUseWhen: []string{
				"SSH config, endpoint, local forward, and key choice are already configured and unambiguous",
			},
			ApprovalRequired: []string{"new keys", "config edits", "ambiguous identities", "privileged ports"},
			Notes:            []string{"SSH is a tunnel only; rdev policy still gates all jobs."},
		},
		{
			ID:        "frp",
			ToolNames: []string{"frpc"},
			Status:    "use-existing-config-only",
			BestFor:   "reverse proxy/NAT traversal through an operator-controlled relay",
			AutoUseWhen: []string{
				"frpc is installed and an approved config is present",
			},
			ApprovalRequired: []string{"download/install", "relay credential creation", "public port changes", "paid relay"},
			Notes:            []string{"Prefer operator-hosted relay endpoints; keep tokens out of public artifacts."},
		},
		{
			ID:        "chisel",
			ToolNames: []string{"chisel"},
			Status:    "use-existing-config-only",
			BestFor:   "HTTP(S)-friendly TCP/UDP tunneling",
			AutoUseWhen: []string{
				"chisel is installed and an approved client command/config is present",
			},
			ApprovalRequired: []string{"download/install", "new relay server", "credential changes", "persistent service"},
			Notes:            []string{"Useful when WebSocket is blocked but HTTPS egress exists."},
		},
		{
			ID:        "headscale-tailscale",
			ToolNames: []string{"tailscale"},
			Status:    "use-existing-enrollment-only",
			BestFor:   "owned managed hosts already in mesh",
			AutoUseWhen: []string{
				"tailscale status shows an active route to the gateway",
			},
			ApprovalRequired: []string{"new enrollment", "auth key use", "ACL/DNS changes", "service changes"},
			Notes:            []string{"headscale/Tailscale identity assists routing only."},
		},
		{
			ID:        "wireguard",
			ToolNames: []string{"wg", "wg-quick"},
			Status:    "use-active-tunnel-only",
			BestFor:   "operator-owned VPN route to gateway",
			AutoUseWhen: []string{
				"an existing active tunnel already routes to the gateway",
			},
			ApprovalRequired: []string{"key creation", "profile import", "route/DNS/firewall mutation", "persistent tunnel start"},
			Notes:            []string{"Never print WireGuard keys or config bodies into evidence."},
		},
	}
}

func requiredToolNames(manifest Manifest) []string {
	seen := map[string]bool{}
	for _, path := range manifest.ConnectionPaths {
		for _, tool := range path.RequiredTools {
			seen[tool] = true
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func selectPath(manifest Manifest, tools []ToolResult, httpProbe func(string, time.Duration) error, timeout time.Duration) (*ConnectionPath, []ProbeResult) {
	toolFound := map[string]bool{}
	for _, tool := range tools {
		toolFound[tool.Name] = tool.Found
	}
	paths := append([]ConnectionPath(nil), manifest.ConnectionPaths...)
	sort.SliceStable(paths, func(i, j int) bool {
		return paths[i].Priority < paths[j].Priority
	})
	var probes []ProbeResult
	for _, path := range paths {
		if !path.UsesHostServe {
			continue
		}
		ok := false
		detail := ""
		switch path.ID {
		case "native-direct-gateway", "native-lan-gateway":
			err := httpProbe(manifest.GatewayURL, timeout)
			ok = err == nil
			if err != nil {
				detail = err.Error()
			} else {
				detail = "gateway health reachable"
			}
		case "proxy-aware-https":
			if proxyEnvironmentPresent() {
				err := httpProbe(manifest.GatewayURL, timeout)
				ok = err == nil
				if err != nil {
					detail = err.Error()
				} else {
					detail = "proxy environment present and gateway reachable"
				}
			} else {
				detail = "no proxy environment variable names detected"
			}
		case "manifest-only-native-fallback":
			err := httpProbe(manifest.ManifestURL, timeout)
			ok = err == nil
			if err != nil {
				detail = err.Error()
			} else {
				detail = "manifest URL reachable"
			}
		default:
			helperGateway := firstEnv(path.GatewayEnvVars)
			installConfigured := firstEnv(path.DependencyInstallActionEnvVars) != ""
			helperStartConfigured := firstEnv(path.HelperStartArgvEnvVars) != ""
			requiredToolReady := requiredToolsFound(path.RequiredTools, toolFound)
			ok = (requiredToolReady || installConfigured) && helperGateway != ""
			if ok {
				path.GatewayOverride = helperGateway
				if requiredToolReady {
					detail = "required helper tooling and configured gateway override detected"
				} else {
					detail = "approved dependency install action and configured gateway override detected"
				}
				if err := httpProbe(helperGateway, timeout); err != nil {
					if helperStartConfigured {
						detail = "helper start argv configured; gateway will be probed after helper starts"
					} else {
						ok = false
						detail = "configured helper gateway is not reachable: " + err.Error()
					}
				}
			} else if helperGateway == "" && len(path.GatewayEnvVars) > 0 {
				detail = "required helper gateway env var not configured: " + strings.Join(path.GatewayEnvVars, ",")
			} else {
				detail = "required helper tooling not detected"
			}
		}
		probes = append(probes, ProbeResult{PathID: path.ID, OK: ok, Detail: detail})
		if ok {
			selected := path
			return &selected, probes
		}
	}
	return nil, probes
}

func hostServeArgs(manifest Manifest, gatewayURL, transport string, extra []string) []string {
	manifestURL := manifestURLForGateway(manifest.ManifestURL, gatewayURL)
	args := []string{
		"host", "serve",
		"--mode", hostModeArg(manifest.Mode),
		"--gateway", gatewayURL,
		"--manifest-url", manifestURL,
		"--manifest-root-public-key", manifest.ManifestRootPublicKey,
		"--transport", firstNonEmpty(transport, "auto"),
		"--once=false",
	}
	if manifest.HostName != "" {
		args = append(args, "--name", manifest.HostName)
	}
	return append(args, extra...)
}

func manifestURLForGateway(manifestURL, gatewayURL string) string {
	manifestParsed, err := url.Parse(manifestURL)
	if err != nil {
		return manifestURL
	}
	gatewayParsed, err := url.Parse(gatewayURL)
	if err != nil || gatewayParsed.Scheme == "" || gatewayParsed.Host == "" {
		return manifestURL
	}
	manifestParsed.Scheme = gatewayParsed.Scheme
	manifestParsed.Host = gatewayParsed.Host
	return manifestParsed.String()
}

func hostModeArg(mode model.HostMode) string {
	switch mode {
	case model.HostModeManaged:
		return "managed"
	case model.HostModeBreakGlass:
		return "break-glass"
	default:
		return "temporary"
	}
}

func probeGatewayHTTP(rawURL string, timeout time.Duration) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported gateway scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("missing gateway host")
	}
	probeURL := rawURL
	if !looksLikeManifestURL(parsed.Path) {
		parsed.Path = "/healthz"
		parsed.RawQuery = ""
		parsed.Fragment = ""
		probeURL = parsed.String()
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, probeURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return fmt.Errorf("probe returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func looksLikeManifestURL(path string) bool {
	return strings.HasSuffix(path, "/manifest")
}

func runCommand(command string, args []string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func dependencyInstallAction(path ConnectionPath, mode model.HostMode) (DependencyInstallAction, string, bool, error) {
	raw, envName := firstEnvWithName(path.DependencyInstallActionEnvVars)
	if raw == "" {
		return DependencyInstallAction{}, "", false, nil
	}
	var action DependencyInstallAction
	if err := json.Unmarshal([]byte(raw), &action); err != nil {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s must be a JSON dependency install action: %w", envName, err)
	}
	if action.SchemaVersion != "rdev.connection-entry.dependency-install-action.v1" {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s must use schema rdev.connection-entry.dependency-install-action.v1", envName)
	}
	action.Tool = executableBaseName(action.Tool)
	if action.Tool == "" {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s must include a non-empty tool", envName)
	}
	if !helperToolAllowed(action.Tool, path.RequiredTools) {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s installs %q, but %s only allows: %s", envName, action.Tool, path.ID, strings.Join(path.RequiredTools, ", "))
	}
	if len(action.Argv) == 0 {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s must include a non-empty argv array", envName)
	}
	for i, arg := range action.Argv {
		if strings.TrimSpace(arg) == "" {
			return DependencyInstallAction{}, "", true, fmt.Errorf("%s argv item %d must not be empty", envName, i)
		}
	}
	if err := rejectShellCommandStrings(action.Argv, envName); err != nil {
		return DependencyInstallAction{}, "", true, err
	}
	scope := strings.TrimSpace(action.Scope)
	if scope == "" {
		scope = "user"
		action.Scope = scope
	}
	switch scope {
	case "user", "workspace", "attended-visible":
	case "managed-approved":
		if mode != model.HostModeManaged {
			return DependencyInstallAction{}, "", true, fmt.Errorf("%s scope managed-approved requires managed host mode", envName)
		}
	default:
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s has unsupported install scope %q", envName, scope)
	}
	if action.RequiresElevation {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s requires elevation; run a reviewed managed service or package manager plan instead", envName)
	}
	if strings.TrimSpace(action.ExpectedSHA256) != "" && !isHexSHA256(action.ExpectedSHA256) {
		return DependencyInstallAction{}, "", true, fmt.Errorf("%s expected_sha256 must be a hex SHA-256 value", envName)
	}
	if err := validateStandardDependencyInstallArgv(action, envName); err != nil {
		return DependencyInstallAction{}, "", true, err
	}
	return action, action.Tool, true, nil
}

func validateStandardDependencyInstallArgv(action DependencyInstallAction, envName string) error {
	if len(action.Argv) == 0 {
		return fmt.Errorf("%s must include a non-empty argv array", envName)
	}
	if executableBaseName(action.Argv[0]) != "rdev" {
		return fmt.Errorf("%s must execute rdev deps install, got %q", envName, action.Argv[0])
	}
	if len(action.Argv) < 3 || action.Argv[1] != "deps" || action.Argv[2] != "install" {
		return fmt.Errorf("%s must execute rdev deps install", envName)
	}
	flags, positionals, err := parseDependencyInstallFlags(action.Argv[3:])
	if err != nil {
		return fmt.Errorf("%s %w", envName, err)
	}
	if len(positionals) > 1 {
		return fmt.Errorf("%s must not pass more than one positional tool to rdev deps install", envName)
	}
	tool := firstNonEmpty(flags["tool"], firstString(positionals))
	if executableBaseName(tool) != executableBaseName(action.Tool) {
		return fmt.Errorf("%s argv --tool must match action tool %q", envName, action.Tool)
	}
	scope := firstNonEmpty(flags["scope"], "user")
	if scope != action.Scope {
		return fmt.Errorf("%s argv --scope must match action scope %q", envName, action.Scope)
	}
	if strings.TrimSpace(flags["url"]) == "" {
		return fmt.Errorf("%s argv must include --url for reviewed helper download", envName)
	}
	if flags["expected-sha256"] != action.ExpectedSHA256 {
		return fmt.Errorf("%s argv --expected-sha256 must match action expected_sha256", envName)
	}
	if flags["execute"] != "true" {
		return fmt.Errorf("%s argv must include --execute so rdev performs the reviewed install, not a plan-only command", envName)
	}
	return nil
}

func parseDependencyInstallFlags(args []string) (map[string]string, []string, error) {
	flags := map[string]string{}
	var positionals []string
	valueFlags := map[string]bool{
		"tool":            true,
		"scope":           true,
		"version":         true,
		"platform":        true,
		"url":             true,
		"expected-sha256": true,
		"install-dir":     true,
	}
	boolFlags := map[string]bool{"execute": true}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		nameValue := strings.TrimPrefix(arg, "--")
		name, value, hasInlineValue := strings.Cut(nameValue, "=")
		if boolFlags[name] {
			if hasInlineValue && value != "true" {
				return nil, nil, fmt.Errorf("--%s must be a boolean execute flag", name)
			}
			flags[name] = "true"
			continue
		}
		if !valueFlags[name] {
			return nil, nil, fmt.Errorf("uses unsupported rdev deps install flag --%s", name)
		}
		if !hasInlineValue {
			i++
			if i >= len(args) {
				return nil, nil, fmt.Errorf("--%s requires a value", name)
			}
			value = strings.TrimSpace(args[i])
		}
		if value == "" {
			return nil, nil, fmt.Errorf("--%s requires a non-empty value", name)
		}
		flags[name] = value
	}
	return flags, positionals, nil
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func runDependencyInstallAction(action DependencyInstallAction) (DependencyInstallResult, error) {
	if len(action.Argv) == 0 {
		return DependencyInstallResult{}, errors.New("dependency install argv is empty")
	}
	cmd := exec.Command(action.Argv[0], action.Argv[1:]...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return DependencyInstallResult{}, err
	}
	result := dependencyInstallResultFromJSON(stdout.Bytes())
	if result.InstalledBinary != "" {
		return result, nil
	}
	return dependencyInstallResultFromEnv(action.Tool), nil
}

func helperStartArgv(path ConnectionPath) ([]string, string, bool, error) {
	raw, envName := firstEnvWithName(path.HelperStartArgvEnvVars)
	if raw == "" {
		return nil, "", false, nil
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return nil, "", true, fmt.Errorf("%s must be a JSON argv array: %w", envName, err)
	}
	if len(argv) == 0 {
		return nil, "", true, fmt.Errorf("%s must contain at least one argv item", envName)
	}
	for i, arg := range argv {
		if strings.TrimSpace(arg) == "" {
			return nil, "", true, fmt.Errorf("%s item %d must not be empty", envName, i)
		}
	}
	tool := executableBaseName(argv[0])
	if !helperToolAllowed(tool, path.RequiredTools) {
		return nil, "", true, fmt.Errorf("%s starts %q, but %s only allows: %s", envName, tool, path.ID, strings.Join(path.RequiredTools, ", "))
	}
	return argv, tool, true, nil
}

func dependencyInstallResultFromEnv(tool string) DependencyInstallResult {
	tool = strings.ToUpper(strings.ReplaceAll(executableBaseName(tool), "-", "_"))
	value := strings.TrimSpace(os.Getenv("RDEV_" + tool + "_BINARY"))
	if value == "" {
		return DependencyInstallResult{}
	}
	return DependencyInstallResult{InstalledBinary: value}
}

func dependencyInstallResultFromJSON(content []byte) DependencyInstallResult {
	var payload struct {
		InstalledBinary  string `json:"installed_binary"`
		DependencyReport struct {
			InstalledBinary string `json:"installed_binary"`
		} `json:"dependency_report"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return DependencyInstallResult{}
	}
	if strings.TrimSpace(payload.InstalledBinary) != "" {
		return DependencyInstallResult{InstalledBinary: strings.TrimSpace(payload.InstalledBinary)}
	}
	if strings.TrimSpace(payload.DependencyReport.InstalledBinary) != "" {
		return DependencyInstallResult{InstalledBinary: strings.TrimSpace(payload.DependencyReport.InstalledBinary)}
	}
	return DependencyInstallResult{}
}

func replaceHelperArgvTool(argv []string, tool, installedPath string) []string {
	if len(argv) == 0 || strings.TrimSpace(installedPath) == "" {
		return argv
	}
	if executableBaseName(argv[0]) != executableBaseName(tool) {
		return argv
	}
	replaced := append([]string(nil), argv...)
	replaced[0] = installedPath
	return replaced
}

func rejectShellCommandStrings(argv []string, label string) error {
	tool := executableBaseName(argv[0])
	for _, arg := range argv[1:] {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if strings.EqualFold(arg, "bypass") {
			return fmt.Errorf("%s must not use ExecutionPolicy Bypass", label)
		}
		if lower == "-encodedcommand" || lower == "-enc" {
			return fmt.Errorf("%s must not use encoded shell commands", label)
		}
		if !isShellTool(tool) {
			continue
		}
		if lower == "-c" || lower == "/c" || lower == "-command" {
			return fmt.Errorf("%s must use argv execution, not shell command-string execution", label)
		}
	}
	return nil
}

func isShellTool(tool string) bool {
	switch strings.TrimSuffix(strings.ToLower(strings.TrimSpace(tool)), ".exe") {
	case "sh", "bash", "zsh", "dash", "fish", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func startHelperCommand(argv []string) (func() error, error) {
	if len(argv) == 0 {
		return nil, errors.New("helper argv is empty")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	return func() error {
		select {
		case err := <-done:
			return err
		default:
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			return errors.New("helper process did not exit after cleanup")
		}
	}, nil
}

func waitForGateway(gatewayURL string, httpProbe func(string, time.Duration) error, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	probeTimeout := timeout
	if probeTimeout > time.Second {
		probeTimeout = time.Second
	}
	var lastErr error
	for {
		if err := httpProbe(gatewayURL, probeTimeout); err != nil {
			lastErr = err
		} else {
			return nil
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func renderLauncher(targetOS, rdevCommand, manifestPath string) string {
	if targetOS == "windows" {
		return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ManifestPath = Join-Path $PSScriptRoot 'connection-entry-runner.json'
Write-Host "Starting Remote Dev Skillkit Connection Entry..."
& %s connection-entry run --runner-manifest "$ManifestPath"
`, psCommand(rdevCommand))
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
MANIFEST="$SCRIPT_DIR/connection-entry-runner.json"
echo "Starting Remote Dev Skillkit Connection Entry..."
exec %s connection-entry run --runner-manifest "$MANIFEST"
`, shellToken(rdevCommand))
}

func launcherName(targetOS string) string {
	if targetOS == "windows" {
		return "Start-ConnectionEntry.ps1"
	}
	return "start-connection-entry.sh"
}

func requiredToolsFound(required []string, found map[string]bool) bool {
	if len(required) == 0 {
		return true
	}
	for _, tool := range required {
		if found[tool] {
			return true
		}
	}
	return false
}

func requiredToolsFoundInResults(required []string, results []ToolResult) bool {
	found := map[string]bool{}
	for _, result := range results {
		found[result.Name] = result.Found
	}
	return requiredToolsFound(required, found)
}

func appendOrReplaceToolResult(results []ToolResult, item ToolResult) []ToolResult {
	updated := append([]ToolResult(nil), results...)
	for i, existing := range updated {
		if existing.Name == item.Name {
			updated[i] = item
			return updated
		}
	}
	return append(updated, item)
}

func proxyEnvironmentPresent() bool {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

func firstEnv(names []string) string {
	value, _ := firstEnvWithName(names)
	return value
}

func firstEnvWithName(names []string) (string, string) {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, name
		}
	}
	return "", ""
}

func executableBaseName(value string) string {
	normalized := strings.ReplaceAll(value, "\\", "/")
	parts := strings.Split(normalized, "/")
	base := strings.TrimSpace(parts[len(parts)-1])
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base
}

func helperToolAllowed(tool string, allowed []string) bool {
	tool = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(tool)), ".exe")
	for _, value := range allowed {
		if tool == strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".exe") {
			return true
		}
	}
	return false
}

func isHexSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func lanStatus(gatewayURL string) string {
	parsed, err := url.Parse(gatewayURL)
	if err != nil {
		return "candidate"
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())) {
		return "implemented-when-target-can-route-to-gateway"
	}
	return "candidate-when-agent-provides-lan-reachable-gateway"
}

func normalizeOS(value string) string {
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

func normalizeArch(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "x64", "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shellToken(value string) string {
	if strings.TrimSpace(value) == "rdev" {
		return "rdev"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func psCommand(value string) string {
	if strings.TrimSpace(value) == "rdev" {
		return "rdev"
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
