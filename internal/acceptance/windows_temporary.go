package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const WindowsTemporaryPlanSchemaVersion = "rdev.acceptance.windows-temporary-plan.v1"

type WindowsTemporaryOptions struct {
	OutDir                         string
	GatewayURL                     string
	TicketCode                     string
	DownloadURL                    string
	ExpectedSHA256                 string
	BootstrapScriptPath            string
	BootstrapScriptURL             string
	BootstrapScriptExpectedSHA256  string
	ManifestURL                    string
	ManifestRootPublicKey          string
	ReleaseManifestURL             string
	ReleaseBundleURL               string
	ReleaseBundleRequiredArtifacts string
	ReleaseRootPublicKey           string
	VerifierDownloadURL            string
	VerifierExpectedSHA256         string
	TrustPin                       string
	HostName                       string
	Force                          bool
	Now                            time.Time
}

type WindowsTemporaryPlan struct {
	SchemaVersion                  string                     `json:"schema_version"`
	GeneratedAt                    time.Time                  `json:"generated_at"`
	Platform                       string                     `json:"platform"`
	OutDir                         string                     `json:"out_dir"`
	BootstrapScriptPath            string                     `json:"bootstrap_script_path,omitempty"`
	BootstrapScriptURL             string                     `json:"bootstrap_script_url,omitempty"`
	BootstrapScriptSHA256          string                     `json:"bootstrap_script_sha256,omitempty"`
	LauncherPath                   string                     `json:"launcher_path"`
	GatewayURL                     string                     `json:"gateway_url"`
	TicketCode                     string                     `json:"ticket_code"`
	ManifestURL                    string                     `json:"manifest_url,omitempty"`
	ManifestRootPublicKey          string                     `json:"manifest_root_public_key,omitempty"`
	ReleaseManifestURL             string                     `json:"release_manifest_url,omitempty"`
	ReleaseBundleURL               string                     `json:"release_bundle_url,omitempty"`
	ReleaseBundleRequiredArtifacts string                     `json:"release_bundle_required_artifacts,omitempty"`
	ReleaseRootPublicKey           string                     `json:"release_root_public_key,omitempty"`
	VerifierDownloadURL            string                     `json:"verifier_download_url,omitempty"`
	VerifierExpectedSHA256         string                     `json:"verifier_expected_sha256,omitempty"`
	TrustPin                       string                     `json:"trust_pin,omitempty"`
	HostName                       string                     `json:"host_name,omitempty"`
	HostDownloadURL                string                     `json:"host_download_url"`
	HostExpectedSHA256             string                     `json:"host_expected_sha256"`
	Checks                         []Check                    `json:"checks"`
	Commands                       []WindowsAcceptanceCommand `json:"commands"`
	NoPersistenceChecks            []WindowsAcceptanceCommand `json:"no_persistence_checks"`
	ApprovalProbes                 []WindowsApprovalProbe     `json:"approval_probes"`
	RecommendedActions             []string                   `json:"recommended_actions"`
	RequiredEvidence               []string                   `json:"required_evidence"`
}

type WindowsAcceptanceCommand struct {
	Name    string   `json:"name"`
	Purpose string   `json:"purpose"`
	Shell   string   `json:"shell"`
	Argv    []string `json:"argv,omitempty"`
	Manual  bool     `json:"manual"`
}

type WindowsApprovalProbe struct {
	Operation        string `json:"operation"`
	ExpectedArtifact string `json:"expected_artifact"`
	Purpose          string `json:"purpose"`
}

func RunWindowsTemporaryPlan(opts WindowsTemporaryOptions) (WindowsTemporaryPlan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return WindowsTemporaryPlan{}, fmt.Errorf("out directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return WindowsTemporaryPlan{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	resolved, err := resolveWindowsTemporaryOptions(outDir, opts)
	if err != nil {
		return WindowsTemporaryPlan{}, err
	}
	launcher := windowsTemporaryLauncher(resolved)
	if err := writeAcceptanceFile(resolved.LauncherPath, []byte(launcher), opts.Force); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	plan := WindowsTemporaryPlan{
		SchemaVersion:                  WindowsTemporaryPlanSchemaVersion,
		GeneratedAt:                    now.UTC(),
		Platform:                       "windows",
		OutDir:                         outDir,
		BootstrapScriptPath:            resolved.BootstrapScriptPath,
		BootstrapScriptURL:             resolved.BootstrapScriptURL,
		BootstrapScriptSHA256:          resolved.BootstrapScriptExpectedSHA256,
		LauncherPath:                   resolved.LauncherPath,
		GatewayURL:                     resolved.GatewayURL,
		TicketCode:                     resolved.TicketCode,
		ManifestURL:                    resolved.ManifestURL,
		ManifestRootPublicKey:          resolved.ManifestRootPublicKey,
		ReleaseManifestURL:             resolved.ReleaseManifestURL,
		ReleaseBundleURL:               resolved.ReleaseBundleURL,
		ReleaseBundleRequiredArtifacts: resolved.ReleaseBundleRequiredArtifacts,
		ReleaseRootPublicKey:           resolved.ReleaseRootPublicKey,
		VerifierDownloadURL:            resolved.VerifierDownloadURL,
		VerifierExpectedSHA256:         resolved.VerifierExpectedSHA256,
		TrustPin:                       resolved.TrustPin,
		HostName:                       resolved.HostName,
		HostDownloadURL:                resolved.DownloadURL,
		HostExpectedSHA256:             resolved.ExpectedSHA256,
		Commands:                       windowsTemporaryCommands(resolved),
		NoPersistenceChecks:            windowsNoPersistenceChecks(),
		ApprovalProbes:                 windowsApprovalProbes(),
		RecommendedActions: []string{
			"Review the generated PowerShell launcher and bootstrap script before using them on a Windows host.",
			"Run the launcher in a clean Windows 10/11 VM or target support host as a visible foreground session.",
			"Approve only scoped temporary capabilities from the gateway host registry.",
			"Run bounded diagnostic and repair jobs, then collect evidence and audit exports.",
			"Revoke the temporary host and run every no-persistence check before publishing the transcript.",
		},
		RequiredEvidence: []string{
			"PowerShell transcript for bootstrap and foreground host startup.",
			"SHA-256 verification output for the bootstrap script, verifier, and rdev-host binary.",
			"Signed release manifest or release bundle verification output from rdev-verify.",
			"Host registration, host approval, job, approval-required, revoke, and cancellation audit events.",
			"No-persistence inspection output for services, scheduled tasks, Run keys, startup folders, and firewall rules.",
		},
	}
	plan.Checks = windowsTemporaryChecks(plan, resolved)
	if err := writeWindowsTemporaryPlan(filepath.Join(outDir, "windows-temporary-plan.json"), plan); err != nil {
		return WindowsTemporaryPlan{}, err
	}
	return plan, nil
}

type windowsTemporaryResolvedOptions struct {
	OutDir                         string
	LauncherPath                   string
	GatewayURL                     string
	TicketCode                     string
	DownloadURL                    string
	ExpectedSHA256                 string
	BootstrapScriptPath            string
	BootstrapScriptURL             string
	BootstrapScriptExpectedSHA256  string
	ManifestURL                    string
	ManifestRootPublicKey          string
	ReleaseManifestURL             string
	ReleaseBundleURL               string
	ReleaseBundleRequiredArtifacts string
	ReleaseRootPublicKey           string
	VerifierDownloadURL            string
	VerifierExpectedSHA256         string
	TrustPin                       string
	HostName                       string
}

func resolveWindowsTemporaryOptions(outDir string, opts WindowsTemporaryOptions) (windowsTemporaryResolvedOptions, error) {
	bootstrapScriptPath := strings.TrimSpace(opts.BootstrapScriptPath)
	if bootstrapScriptPath == "" {
		bootstrapScriptPath = filepath.Join("scripts", "bootstrap", "windows-temporary.ps1")
	}
	if bootstrapScriptPath != "" && !filepath.IsAbs(bootstrapScriptPath) {
		abs, err := filepath.Abs(bootstrapScriptPath)
		if err != nil {
			return windowsTemporaryResolvedOptions{}, err
		}
		bootstrapScriptPath = abs
	}
	bootstrapHash := strings.TrimSpace(opts.BootstrapScriptExpectedSHA256)
	if bootstrapHash == "" && strings.TrimSpace(bootstrapScriptPath) != "" {
		if hash, err := fileSHA256(bootstrapScriptPath); err == nil {
			bootstrapHash = hash
		}
	}
	releaseBundleURL := strings.TrimSpace(opts.ReleaseBundleURL)
	releaseBundleRequiredArtifacts := strings.TrimSpace(opts.ReleaseBundleRequiredArtifacts)
	if releaseBundleURL == "" {
		releaseBundleRequiredArtifacts = ""
	} else if releaseBundleRequiredArtifacts == "" {
		releaseBundleRequiredArtifacts = "rdev-host.exe,rdev-verify.exe"
	}
	return windowsTemporaryResolvedOptions{
		OutDir:                         outDir,
		LauncherPath:                   filepath.Join(outDir, "run-windows-temporary.ps1"),
		GatewayURL:                     strings.TrimSpace(opts.GatewayURL),
		TicketCode:                     strings.TrimSpace(opts.TicketCode),
		DownloadURL:                    strings.TrimSpace(opts.DownloadURL),
		ExpectedSHA256:                 strings.ToLower(strings.TrimSpace(opts.ExpectedSHA256)),
		BootstrapScriptPath:            bootstrapScriptPath,
		BootstrapScriptURL:             strings.TrimSpace(opts.BootstrapScriptURL),
		BootstrapScriptExpectedSHA256:  strings.ToLower(bootstrapHash),
		ManifestURL:                    strings.TrimSpace(opts.ManifestURL),
		ManifestRootPublicKey:          strings.TrimSpace(opts.ManifestRootPublicKey),
		ReleaseManifestURL:             strings.TrimSpace(opts.ReleaseManifestURL),
		ReleaseBundleURL:               releaseBundleURL,
		ReleaseBundleRequiredArtifacts: releaseBundleRequiredArtifacts,
		ReleaseRootPublicKey:           strings.TrimSpace(opts.ReleaseRootPublicKey),
		VerifierDownloadURL:            strings.TrimSpace(opts.VerifierDownloadURL),
		VerifierExpectedSHA256:         strings.ToLower(strings.TrimSpace(opts.VerifierExpectedSHA256)),
		TrustPin:                       strings.TrimSpace(opts.TrustPin),
		HostName:                       strings.TrimSpace(opts.HostName),
	}, nil
}

func windowsTemporaryChecks(plan WindowsTemporaryPlan, opts windowsTemporaryResolvedOptions) []Check {
	_, scriptErr := os.Stat(opts.BootstrapScriptPath)
	return []Check{
		{Name: "launcher_written", Passed: pathExists(plan.LauncherPath), Detail: plan.LauncherPath},
		{Name: "bootstrap_script_available", Passed: scriptErr == nil || opts.BootstrapScriptURL != "", Detail: firstNonEmptyString(opts.BootstrapScriptURL, opts.BootstrapScriptPath)},
		{Name: "bootstrap_script_hash_available", Passed: opts.BootstrapScriptExpectedSHA256 != "", Detail: opts.BootstrapScriptExpectedSHA256},
		{Name: "gateway_url", Passed: opts.GatewayURL != "", Detail: opts.GatewayURL},
		{Name: "ticket_code", Passed: opts.TicketCode != "", Detail: opts.TicketCode},
		{Name: "host_download_url", Passed: opts.DownloadURL != "", Detail: opts.DownloadURL},
		{Name: "host_sha256", Passed: isHexSHA256(opts.ExpectedSHA256), Detail: opts.ExpectedSHA256},
		{Name: "release_manifest_or_bundle_url", Passed: opts.ReleaseManifestURL != "" || opts.ReleaseBundleURL != "", Detail: firstNonEmptyString(opts.ReleaseBundleURL, opts.ReleaseManifestURL)},
		{Name: "release_bundle_required_artifacts", Passed: opts.ReleaseBundleURL == "" || opts.ReleaseBundleRequiredArtifacts != "", Detail: opts.ReleaseBundleRequiredArtifacts},
		{Name: "release_root_public_key", Passed: opts.ReleaseRootPublicKey != ""},
		{Name: "verifier_download_url", Passed: opts.VerifierDownloadURL != "", Detail: opts.VerifierDownloadURL},
		{Name: "verifier_sha256", Passed: isHexSHA256(opts.VerifierExpectedSHA256), Detail: opts.VerifierExpectedSHA256},
		{Name: "no_persistence_checks_present", Passed: len(plan.NoPersistenceChecks) >= 5},
		{Name: "approval_probes_present", Passed: len(plan.ApprovalProbes) >= 4},
	}
}

func windowsTemporaryCommands(opts windowsTemporaryResolvedOptions) []WindowsAcceptanceCommand {
	launcherCommand := "powershell.exe -NoProfile -File " + powershellQuote(opts.LauncherPath)
	return []WindowsAcceptanceCommand{
		{
			Name:    "review_launcher",
			Purpose: "Inspect the generated launcher before sending or running it.",
			Shell:   "Get-Content -LiteralPath " + powershellQuote(opts.LauncherPath),
			Argv:    []string{"powershell.exe", "-NoProfile", "-Command", "Get-Content -LiteralPath " + powershellQuote(opts.LauncherPath)},
			Manual:  true,
		},
		{
			Name:    "run_foreground_temporary_host",
			Purpose: "Run the attended temporary host bootstrap as a visible foreground session.",
			Shell:   launcherCommand,
			Argv:    []string{"powershell.exe", "-NoProfile", "-File", opts.LauncherPath},
			Manual:  true,
		},
		{
			Name:    "start_transcript",
			Purpose: "Capture a local transcript before running the launcher on the Windows host.",
			Shell:   "Start-Transcript -Path (Join-Path $env:TEMP 'rdev-windows-temporary-transcript.txt')",
			Manual:  true,
		},
		{
			Name:    "stop_transcript",
			Purpose: "Stop the transcript after the host exits or is revoked.",
			Shell:   "Stop-Transcript",
			Manual:  true,
		},
	}
}

func windowsNoPersistenceChecks() []WindowsAcceptanceCommand {
	return []WindowsAcceptanceCommand{
		{
			Name:    "services",
			Purpose: "Confirm temporary mode did not install a Windows Service.",
			Shell:   "Get-Service | Where-Object { $_.Name -match 'rdev|remote-dev' -or $_.DisplayName -match 'rdev|Remote Dev' } | Select-Object Name, Status, StartType, DisplayName",
			Manual:  true,
		},
		{
			Name:    "scheduled_tasks",
			Purpose: "Confirm temporary mode did not create scheduled tasks.",
			Shell:   "Get-ScheduledTask | Where-Object { $_.TaskName -match 'rdev|remote-dev' -or $_.TaskPath -match 'rdev|remote-dev' } | Select-Object TaskPath, TaskName, State",
			Manual:  true,
		},
		{
			Name:    "hkcu_run_key",
			Purpose: "Confirm temporary mode did not add current-user Run-key autorun entries.",
			Shell:   "Get-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' | Select-Object *rdev*, *RemoteDev*",
			Manual:  true,
		},
		{
			Name:    "hklm_run_key",
			Purpose: "Confirm temporary mode did not add machine Run-key autorun entries.",
			Shell:   "Get-ItemProperty -Path 'HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' | Select-Object *rdev*, *RemoteDev*",
			Manual:  true,
		},
		{
			Name:    "startup_folders",
			Purpose: "Confirm temporary mode did not add startup-folder shortcuts or scripts.",
			Shell:   "Get-ChildItem \"$env:APPDATA\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\", \"$env:ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp\" -ErrorAction SilentlyContinue | Where-Object { $_.Name -match 'rdev|remote-dev' }",
			Manual:  true,
		},
		{
			Name:    "firewall_rules",
			Purpose: "Confirm temporary mode did not add firewall rules.",
			Shell:   "Get-NetFirewallRule -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -match 'rdev|Remote Dev' -or $_.Name -match 'rdev|remote-dev' } | Select-Object DisplayName, Enabled, Direction, Action",
			Manual:  true,
		},
	}
}

func windowsApprovalProbes() []WindowsApprovalProbe {
	return []WindowsApprovalProbe{
		{Operation: "package.install", ExpectedArtifact: "rdev.approval-required.v1", Purpose: "Package installation pauses before side effects."},
		{Operation: "elevation.request", ExpectedArtifact: "rdev.approval-required.v1", Purpose: "Privilege elevation pauses before side effects."},
		{Operation: "service.manage", ExpectedArtifact: "rdev.approval-required.v1", Purpose: "Service mutation is not allowed in temporary mode without approval."},
		{Operation: "gui.control", ExpectedArtifact: "rdev.approval-required.v1", Purpose: "GUI control requires explicit approval and local visibility."},
		{Operation: "credential.change", ExpectedArtifact: "rdev.approval-required.v1", Purpose: "Credential access or mutation requires approval."},
	}
}

func windowsTemporaryLauncher(opts windowsTemporaryResolvedOptions) string {
	var builder strings.Builder
	builder.WriteString("# Generated by rdev acceptance windows-temporary.\n")
	builder.WriteString("# Inspect this file before running it on a Windows host.\n")
	builder.WriteString("$ErrorActionPreference = 'Stop'\n")
	builder.WriteString("$bootstrap = ")
	if opts.BootstrapScriptURL != "" {
		builder.WriteString("Join-Path $env:TEMP 'rdev-windows-temporary.ps1'\n")
		builder.WriteString("Invoke-WebRequest -Uri ")
		builder.WriteString(powershellQuote(opts.BootstrapScriptURL))
		builder.WriteString(" -OutFile $bootstrap -UseBasicParsing\n")
		if opts.BootstrapScriptExpectedSHA256 != "" {
			builder.WriteString("$actualBootstrap = (Get-FileHash -Algorithm SHA256 -Path $bootstrap).Hash.ToLowerInvariant()\n")
			builder.WriteString("$expectedBootstrap = ")
			builder.WriteString(powershellQuote(opts.BootstrapScriptExpectedSHA256))
			builder.WriteString("\n")
			builder.WriteString("if ($actualBootstrap -ne $expectedBootstrap) { throw \"bootstrap SHA256 mismatch expected=$expectedBootstrap actual=$actualBootstrap\" }\n")
		}
	} else {
		builder.WriteString(powershellQuote(opts.BootstrapScriptPath))
		builder.WriteString("\n")
	}
	builder.WriteString("& $bootstrap")
	appendPowerShellArg(&builder, "GatewayUrl", opts.GatewayURL)
	appendPowerShellArg(&builder, "TicketCode", opts.TicketCode)
	appendPowerShellArg(&builder, "DownloadUrl", opts.DownloadURL)
	appendPowerShellArg(&builder, "ExpectedSha256", opts.ExpectedSHA256)
	appendPowerShellArg(&builder, "ManifestUrl", opts.ManifestURL)
	appendPowerShellArg(&builder, "ManifestRootPublicKey", opts.ManifestRootPublicKey)
	appendPowerShellArg(&builder, "ReleaseManifestUrl", opts.ReleaseManifestURL)
	appendPowerShellArg(&builder, "ReleaseBundleUrl", opts.ReleaseBundleURL)
	appendPowerShellArg(&builder, "ReleaseBundleRequiredArtifacts", opts.ReleaseBundleRequiredArtifacts)
	appendPowerShellArg(&builder, "ReleaseRootPublicKey", opts.ReleaseRootPublicKey)
	appendPowerShellArg(&builder, "VerifierDownloadUrl", opts.VerifierDownloadURL)
	appendPowerShellArg(&builder, "VerifierExpectedSha256", opts.VerifierExpectedSHA256)
	appendPowerShellArg(&builder, "TrustPin", opts.TrustPin)
	appendPowerShellArg(&builder, "HostName", opts.HostName)
	builder.WriteString("\n")
	return builder.String()
}

func appendPowerShellArg(builder *strings.Builder, name string, value string) {
	if value == "" {
		return
	}
	builder.WriteString(" `\n  -")
	builder.WriteString(name)
	builder.WriteByte(' ')
	builder.WriteString(powershellQuote(value))
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func writeWindowsTemporaryPlan(path string, plan WindowsTemporaryPlan) error {
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o600)
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func isHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
