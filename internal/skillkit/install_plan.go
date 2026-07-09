package skillkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	InstallPlanSchemaVersion             = "rdev.skillkit-install-plan.v1"
	InstallPlanVerificationSchemaVersion = "rdev.skillkit-install-plan-verification.v1"
)

type InstallPlanOptions struct {
	BundleDir   string
	OutDir      string
	Frameworks  []string
	RdevCommand string
	GeneratedAt time.Time
}

type InstallPlan struct {
	SchemaVersion         string                        `json:"schema_version"`
	GeneratedAt           time.Time                     `json:"generated_at"`
	BundleDir             string                        `json:"bundle_dir"`
	OutDir                string                        `json:"out_dir"`
	ExternalMutation      bool                          `json:"external_mutation"`
	AdaptiveConfiguration AdaptiveConfigurationContract `json:"adaptive_configuration"`
	BundleVerification    VerificationReport            `json:"bundle_verification"`
	Frameworks            []FrameworkInstallPlan        `json:"frameworks"`
	Files                 []FileEntry                   `json:"files"`
	RecommendedNextSteps  []string                      `json:"recommended_next_steps"`
}

type FrameworkInstallPlan struct {
	Framework        string   `json:"framework"`
	DisplayName      string   `json:"display_name"`
	SkillTargetEnv   string   `json:"skill_target_env"`
	SkillTargetHint  string   `json:"skill_target_hint"`
	FrameworkDoc     string   `json:"framework_doc"`
	ShellScript      string   `json:"shell_script"`
	PowerShellScript string   `json:"powershell_script"`
	MCPCommand       string   `json:"mcp_command"`
	RequiredSkills   []string `json:"required_skills"`
	ReviewNotes      []string `json:"review_notes"`
}

type InstallPlanVerification struct {
	SchemaVersion      string              `json:"schema_version"`
	GeneratedAt        time.Time           `json:"generated_at"`
	PlanPath           string              `json:"plan_path"`
	PlanSchema         string              `json:"plan_schema,omitempty"`
	BundleVerification VerificationReport  `json:"bundle_verification"`
	Checks             []VerificationCheck `json:"checks"`
	FilesVerified      int                 `json:"files_verified"`
	FrameworksVerified int                 `json:"frameworks_verified"`
	RecommendedActions []string            `json:"recommended_actions,omitempty"`
}

func (v InstallPlanVerification) OK() bool {
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

func PlanInstall(opts InstallPlanOptions) (InstallPlan, error) {
	if strings.TrimSpace(opts.BundleDir) == "" {
		return InstallPlan{}, fmt.Errorf("bundle directory is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return InstallPlan{}, fmt.Errorf("out is required")
	}
	if strings.TrimSpace(opts.RdevCommand) == "" {
		opts.RdevCommand = RecommendedRdevCommand()
	}
	if opts.GeneratedAt.IsZero() {
		opts.GeneratedAt = time.Now()
	}
	bundleDir, err := filepath.Abs(opts.BundleDir)
	if err != nil {
		return InstallPlan{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return InstallPlan{}, err
	}
	if err := prepareOutputDir(outDir); err != nil {
		return InstallPlan{}, err
	}
	verification, err := Verify(VerifyOptions{BundleDir: bundleDir, GeneratedAt: opts.GeneratedAt})
	if err != nil {
		return InstallPlan{}, err
	}
	if !verification.OK() {
		return InstallPlan{}, fmt.Errorf("skillkit bundle verification failed")
	}
	manifest, err := readManifest(bundleDir)
	if err != nil {
		return InstallPlan{}, err
	}
	frameworks, err := normalizeFrameworks(opts.Frameworks)
	if err != nil {
		return InstallPlan{}, err
	}
	requiredSkills := skillNames(manifest)
	var files []FileEntry
	var plans []FrameworkInstallPlan
	for _, framework := range frameworks {
		spec := frameworkSpec(framework)
		if spec.Name == "" {
			return InstallPlan{}, fmt.Errorf("unsupported framework %q", framework)
		}
		shellScript := "install-" + spec.Name + ".sh"
		powerShellScript := "install-" + spec.Name + ".ps1"
		shellEntry, err := writeInstallPlanFile(outDir, shellScript, "install-script", []byte(shellInstallScript(bundleDir, opts.RdevCommand, spec, requiredSkills)))
		if err != nil {
			return InstallPlan{}, err
		}
		files = append(files, shellEntry)
		powerShellEntry, err := writeInstallPlanFile(outDir, powerShellScript, "install-script", []byte(powerShellInstallScript(bundleDir, opts.RdevCommand, spec, requiredSkills)))
		if err != nil {
			return InstallPlan{}, err
		}
		files = append(files, powerShellEntry)
		plans = append(plans, FrameworkInstallPlan{
			Framework:        spec.Name,
			DisplayName:      spec.DisplayName,
			SkillTargetEnv:   spec.TargetEnv,
			SkillTargetHint:  spec.ShellTargetHint,
			FrameworkDoc:     spec.DocPath,
			ShellScript:      shellScript,
			PowerShellScript: powerShellScript,
			MCPCommand:       opts.RdevCommand + " mcp serve",
			RequiredSkills:   append([]string(nil), requiredSkills...),
			ReviewNotes: []string{
				"Review the script before running it.",
				"The script verifies the bundle before copying skills.",
				"Before asking an agent to use the installed skills, probe rdev doctor, rdev mcp tools, OS/shell, service manager, gateway, workspace, adapters, framework path, and permissions.",
				"If gateway, ticket, root key, release URL, checksum, framework path, workspace, adapter, or authorization policy is unclear, ask instead of inventing a value.",
				"Set " + spec.TargetEnv + " to override the target skill directory.",
				"Set RDEV_SKILLKIT_FORCE=1 only after reviewing any existing skill directory conflict.",
			},
		})
	}
	commandsEntry, err := writeInstallPlanFile(outDir, "INSTALL_COMMANDS.md", "install-guide", []byte(installCommandsDoc(bundleDir, opts.RdevCommand, plans)))
	if err != nil {
		return InstallPlan{}, err
	}
	files = append(files, commandsEntry)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	plan := InstallPlan{
		SchemaVersion:         InstallPlanSchemaVersion,
		GeneratedAt:           opts.GeneratedAt.UTC(),
		BundleDir:             bundleDir,
		OutDir:                outDir,
		ExternalMutation:      false,
		AdaptiveConfiguration: defaultAdaptiveConfigurationContract(),
		BundleVerification:    verification,
		Frameworks:            plans,
		Files:                 files,
		RecommendedNextSteps: []string{
			"Run rdev skillkit verify-install-plan --plan " + filepath.Join(outDir, "install-plan.json") + " before running any generated script.",
			"Run only the script for the agent framework you are installing.",
			"Probe the environment with rdev doctor and rdev mcp tools before asking an agent to operate the installed skills.",
			"Ask a short follow-up question whenever gateway, ticket, release, framework path, workspace, adapter, or authorization policy cannot be discovered safely.",
			"Configure the agent MCP client to execute: " + opts.RdevCommand + " mcp serve.",
		},
	}
	content, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return InstallPlan{}, err
	}
	content = append(content, '\n')
	if _, err := writeInstallPlanFile(outDir, "install-plan.json", "install-plan", content); err != nil {
		return InstallPlan{}, err
	}
	return plan, nil
}

func VerifyInstallPlan(planPath string, generatedAt time.Time) (InstallPlanVerification, error) {
	if strings.TrimSpace(planPath) == "" {
		return InstallPlanVerification{}, fmt.Errorf("plan is required")
	}
	absPlanPath, err := filepath.Abs(planPath)
	if err != nil {
		return InstallPlanVerification{}, err
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	report := InstallPlanVerification{
		SchemaVersion: InstallPlanVerificationSchemaVersion,
		GeneratedAt:   generatedAt.UTC(),
		PlanPath:      absPlanPath,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, VerificationCheck{Name: name, Passed: passed, Detail: detail})
	}
	content, err := os.ReadFile(absPlanPath)
	add("plan_exists", err == nil, absPlanPath)
	if err != nil {
		report.RecommendedActions = failedInstallPlanActions()
		return report, nil
	}
	var plan InstallPlan
	err = json.Unmarshal(content, &plan)
	add("plan_json_valid", err == nil, errorString(err))
	if err != nil {
		report.RecommendedActions = failedInstallPlanActions()
		return report, nil
	}
	report.PlanSchema = plan.SchemaVersion
	add("plan_schema", plan.SchemaVersion == InstallPlanSchemaVersion, plan.SchemaVersion)
	add("external_mutation_false", !plan.ExternalMutation, fmt.Sprintf("%t", plan.ExternalMutation))
	add("adaptive_configuration_contract", adaptiveContractFailure(plan.AdaptiveConfiguration) == "", adaptiveContractFailure(plan.AdaptiveConfiguration))
	add("frameworks_present", len(plan.Frameworks) > 0, fmt.Sprintf("%d", len(plan.Frameworks)))
	add("files_present", len(plan.Files) > 0, fmt.Sprintf("%d", len(plan.Files)))
	bundleVerification, err := Verify(VerifyOptions{BundleDir: plan.BundleDir, GeneratedAt: generatedAt})
	report.BundleVerification = bundleVerification
	add("bundle_verifies", err == nil && bundleVerification.OK(), firstNonEmpty(errorString(err), bundleVerificationFailures(bundleVerification)))
	planDir := filepath.Dir(absPlanPath)
	seenFiles := map[string]bool{}
	var duplicateFiles []string
	var unsafeFiles []string
	for _, file := range plan.Files {
		if seenFiles[file.Path] {
			duplicateFiles = append(duplicateFiles, file.Path)
		}
		seenFiles[file.Path] = true
		if !safeBundlePath(file.Path) {
			unsafeFiles = append(unsafeFiles, file.Path)
		}
	}
	seenFiles["install-plan.json"] = true
	sort.Strings(duplicateFiles)
	sort.Strings(unsafeFiles)
	add("file_paths_unique", len(duplicateFiles) == 0, strings.Join(duplicateFiles, ","))
	add("file_paths_safe", len(unsafeFiles) == 0, strings.Join(unsafeFiles, ","))
	verified, fileFailures := verifyListedFiles(planDir, plan.Files)
	report.FilesVerified = verified
	add("listed_files_exist", !strings.Contains(fileFailures, "missing:"), fileFailures)
	add("listed_files_sha256_match", !strings.Contains(fileFailures, "sha256:"), fileFailures)
	add("listed_files_size_match", !strings.Contains(fileFailures, "size:"), fileFailures)
	unlisted, err := findUnlistedFiles(planDir, seenFiles)
	add("install_plan_has_no_unlisted_files", err == nil && len(unlisted) == 0, firstNonEmpty(errorString(err), strings.Join(unlisted, ",")))
	report.FrameworksVerified = len(plan.Frameworks)
	add("install_scripts_present", installScriptsPresent(planDir, plan.Frameworks) == "", installScriptsPresent(planDir, plan.Frameworks))
	add("install_scripts_no_forbidden_mutation", installScriptsForbiddenMutation(planDir, plan.Frameworks) == "", installScriptsForbiddenMutation(planDir, plan.Frameworks))
	add("install_scripts_verify_bundle_first", installScriptsRequireBundleVerify(planDir, plan.Frameworks) == "", installScriptsRequireBundleVerify(planDir, plan.Frameworks))
	add("install_scripts_use_standard_installer", installScriptsUseStandardInstaller(planDir, plan.Frameworks) == "", installScriptsUseStandardInstaller(planDir, plan.Frameworks))
	add("install_scripts_keep_adaptive_contract", installScriptsKeepAdaptiveContract(planDir, plan.Frameworks) == "", installScriptsKeepAdaptiveContract(planDir, plan.Frameworks))
	add("install_commands_keep_adaptive_contract", installCommandsKeepAdaptiveContract(planDir) == "", installCommandsKeepAdaptiveContract(planDir))
	if !report.OK() {
		report.RecommendedActions = failedInstallPlanActions()
	}
	return report, nil
}

type frameworkInstallSpec struct {
	Name              string
	DisplayName       string
	TargetEnv         string
	ShellTargetHint   string
	ShellDefault      string
	PowerShellDefault string
	DocPath           string
}

func frameworkSpec(name string) frameworkInstallSpec {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex":
		return frameworkInstallSpec{
			Name:              "codex",
			DisplayName:       "Codex",
			TargetEnv:         "RDEV_CODEX_SKILLS_DIR",
			ShellTargetHint:   "${CODEX_HOME:-$HOME/.codex}/skills",
			ShellDefault:      "${CODEX_HOME:-$HOME/.codex}/skills",
			PowerShellDefault: `$(if ($env:CODEX_HOME) { Join-Path $env:CODEX_HOME 'skills' } else { Join-Path $HOME '.codex/skills' })`,
			DocPath:           "frameworks/codex.md",
		}
	case "claude-code":
		return frameworkInstallSpec{
			Name:              "claude-code",
			DisplayName:       "Claude Code",
			TargetEnv:         "RDEV_CLAUDE_CODE_SKILLS_DIR",
			ShellTargetHint:   "${CLAUDE_CODE_HOME:-$HOME/.claude}/skills",
			ShellDefault:      "${CLAUDE_CODE_HOME:-$HOME/.claude}/skills",
			PowerShellDefault: `$(if ($env:CLAUDE_CODE_HOME) { Join-Path $env:CLAUDE_CODE_HOME 'skills' } else { Join-Path $HOME '.claude/skills' })`,
			DocPath:           "frameworks/claude-code.md",
		}
	case "hermes":
		return frameworkInstallSpec{
			Name:              "hermes",
			DisplayName:       "Hermes",
			TargetEnv:         "RDEV_HERMES_SKILLS_DIR",
			ShellTargetHint:   "${HERMES_HOME:-$HOME/.hermes}/skills",
			ShellDefault:      "${HERMES_HOME:-$HOME/.hermes}/skills",
			PowerShellDefault: `$(if ($env:HERMES_HOME) { Join-Path $env:HERMES_HOME 'skills' } else { Join-Path $HOME '.hermes/skills' })`,
			DocPath:           "frameworks/hermes.md",
		}
	case "openclaw":
		return frameworkInstallSpec{
			Name:              "openclaw",
			DisplayName:       "OpenClaw",
			TargetEnv:         "RDEV_OPENCLAW_SKILLS_DIR",
			ShellTargetHint:   "${OPENCLAW_HOME:-$HOME/.openclaw}/skills",
			ShellDefault:      "${OPENCLAW_HOME:-$HOME/.openclaw}/skills",
			PowerShellDefault: `$(if ($env:OPENCLAW_HOME) { Join-Path $env:OPENCLAW_HOME 'skills' } else { Join-Path $HOME '.openclaw/skills' })`,
			DocPath:           "frameworks/openclaw-opencode.md",
		}
	case "opencode":
		return frameworkInstallSpec{
			Name:              "opencode",
			DisplayName:       "OpenCode",
			TargetEnv:         "RDEV_OPENCODE_SKILLS_DIR",
			ShellTargetHint:   "${OPENCODE_HOME:-$HOME/.config/opencode}/skills",
			ShellDefault:      "${OPENCODE_HOME:-$HOME/.config/opencode}/skills",
			PowerShellDefault: `$(if ($env:OPENCODE_HOME) { Join-Path $env:OPENCODE_HOME 'skills' } else { Join-Path $env:APPDATA 'opencode/skills' })`,
			DocPath:           "frameworks/openclaw-opencode.md",
		}
	case "generic-mcp-agent":
		return frameworkInstallSpec{
			Name:              "generic-mcp-agent",
			DisplayName:       "Generic MCP Agent",
			TargetEnv:         "RDEV_GENERIC_AGENT_SKILLS_DIR",
			ShellTargetHint:   "$RDEV_GENERIC_AGENT_SKILLS_DIR",
			ShellDefault:      "",
			PowerShellDefault: "",
			DocPath:           "frameworks/generic-mcp-agent.md",
		}
	default:
		return frameworkInstallSpec{}
	}
}

func normalizeFrameworks(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"codex", "claude-code", "hermes", "openclaw", "opencode", "generic-mcp-agent"}, nil
	}
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.ToLower(strings.TrimSpace(part))
			switch normalized {
			case "":
				continue
			case "claude", "claude_code":
				normalized = "claude-code"
			case "openclaw-opencode":
				for _, split := range []string{"openclaw", "opencode"} {
					if !seen[split] {
						seen[split] = true
						result = append(result, split)
					}
				}
				continue
			case "generic", "mcp", "generic-mcp":
				normalized = "generic-mcp-agent"
			}
			if frameworkSpec(normalized).Name == "" {
				return nil, fmt.Errorf("unsupported framework %q", part)
			}
			if !seen[normalized] {
				seen[normalized] = true
				result = append(result, normalized)
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("at least one framework is required")
	}
	return result, nil
}

func readManifest(bundleDir string) (Manifest, error) {
	content, err := os.ReadFile(filepath.Join(bundleDir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func skillNames(manifest Manifest) []string {
	var names []string
	for _, skill := range manifest.Skills {
		names = append(names, skill.Name)
	}
	sort.Strings(names)
	return names
}

func writeInstallPlanFile(root, bundlePath, kind string, content []byte) (FileEntry, error) {
	clean := filepath.Clean(filepath.FromSlash(bundlePath))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return FileEntry{}, fmt.Errorf("invalid install plan path %q", bundlePath)
	}
	path := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return FileEntry{}, err
	}
	mode := os.FileMode(0o600)
	if kind == "install-script" {
		mode = 0o700
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return FileEntry{}, err
	}
	sum := sha256.Sum256(content)
	return FileEntry{
		Path:      filepath.ToSlash(clean),
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: len(content),
		Kind:      kind,
	}, nil
}

func shellInstallScript(bundleDir, rdevCommand string, spec frameworkInstallSpec, skills []string) string {
	targetLine := `TARGET_DIR="${` + spec.TargetEnv + `:-` + spec.ShellDefault + `}"`
	if spec.ShellDefault == "" {
		targetLine = `TARGET_DIR="${` + spec.TargetEnv + `:-}"`
	}
	forceFlag := ``
	ifLine := `if [ "${RDEV_SKILLKIT_FORCE:-0}" = "1" ]; then FORCE_FLAG="--force"; fi`
	return strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`BUNDLE_DIR="${RDEV_SKILLKIT_BUNDLE:-` + shellQuote(bundleDir) + `}"`,
		`RDEV_BIN="${RDEV_BIN:-` + shellQuote(rdevCommand) + `}"`,
		targetLine,
		`if [ -z "$TARGET_DIR" ]; then echo "Set ` + spec.TargetEnv + ` to your agent skills directory." >&2; exit 1; fi`,
		`"$RDEV_BIN" skillkit verify --bundle "$BUNDLE_DIR" >/dev/null`,
		`printf '%s\n' "Adaptive Configuration Contract: before using these skills, probe: rdev doctor; rdev mcp tools; OS/shell; service manager; gateway; network reachability; proxy/DNS; NAT/firewall/CGNAT; SSH config; tunnel/mesh tools; connection modes; workspace; adapters; framework install path; permissions."`,
		`printf '%s\n' "For personal-computer installs, local MCP stdio with rdev mcp serve is enough; hosted gateway URLs are optional until a remote-host workflow needs one."`,
		`printf '%s\n' "If direct reachability is blocked, prefer existing or open-source/free tunnel/mesh options such as frp, Chisel, headscale, or WireGuard before paid relays."`,
		`printf '%s\n' "If gateway URL, ticket code, root key, release URL, checksum, framework install path, workspace root, adapter choice, tunnel/mesh authorization, or authorization policy is unclear, ask instead of inventing a value."`,
		`printf '%s\n' "Examples such as https://api.example.com/v1, /Users/example, /home/example, and C:\Users\Alice are placeholders."`,
		`FORCE_FLAG=` + forceFlag,
		ifLine,
		`"$RDEV_BIN" skillkit install --bundle "$BUNDLE_DIR" --framework ` + shellQuote(spec.Name) + ` --target "$TARGET_DIR" --execute $FORCE_FLAG`,
		`printf '%s\n' "Installed Remote Dev Skillkit skills for ` + spec.DisplayName + ` into $TARGET_DIR"`,
		`printf '%s\n' "Install manifest: $TARGET_DIR/.remote-dev-skillkit/install.json"`,
		`printf '%s\n' "Configure MCP stdio command: $RDEV_BIN mcp serve"`,
		"",
	}, "\n")
}

func powerShellInstallScript(bundleDir, rdevCommand string, spec frameworkInstallSpec, skills []string) string {
	targetBlock := `$TargetDir = if ($env:` + spec.TargetEnv + `) { $env:` + spec.TargetEnv + ` } else { ` + spec.PowerShellDefault + ` }`
	if spec.PowerShellDefault == "" {
		targetBlock = `$TargetDir = $env:` + spec.TargetEnv
	}
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		`$BundleDir = if ($env:RDEV_SKILLKIT_BUNDLE) { $env:RDEV_SKILLKIT_BUNDLE } else { ` + powerShellQuote(bundleDir) + ` }`,
		`$RdevBin = if ($env:RDEV_BIN) { $env:RDEV_BIN } else { ` + powerShellQuote(rdevCommand) + ` }`,
		targetBlock,
		`if ([string]::IsNullOrWhiteSpace($TargetDir)) { throw "Set ` + spec.TargetEnv + ` to your agent skills directory." }`,
		`& $RdevBin skillkit verify --bundle $BundleDir | Out-Null`,
		`Write-Output "Adaptive Configuration Contract: before using these skills, probe: rdev doctor; rdev mcp tools; OS/shell; service manager; gateway; network reachability; proxy/DNS; NAT/firewall/CGNAT; SSH config; tunnel/mesh tools; connection modes; workspace; adapters; framework install path; permissions."`,
		`Write-Output "For personal-computer installs, local MCP stdio with rdev mcp serve is enough; hosted gateway URLs are optional until a remote-host workflow needs one."`,
		`Write-Output "If direct reachability is blocked, prefer existing or open-source/free tunnel/mesh options such as frp, Chisel, headscale, or WireGuard before paid relays."`,
		`Write-Output "If gateway URL, ticket code, root key, release URL, checksum, framework install path, workspace root, adapter choice, tunnel/mesh authorization, or authorization policy is unclear, ask instead of inventing a value."`,
		`Write-Output "Examples such as https://api.example.com/v1, /Users/example, /home/example, and C:\Users\Alice are placeholders."`,
		`$InstallArgs = @('skillkit', 'install', '--bundle', $BundleDir, '--framework', ` + powerShellQuote(spec.Name) + `, '--target', $TargetDir, '--execute')`,
		`if ($env:RDEV_SKILLKIT_FORCE -eq '1') { $InstallArgs += '--force' }`,
		`& $RdevBin @InstallArgs`,
		`Write-Output "Installed Remote Dev Skillkit skills for ` + spec.DisplayName + ` into $TargetDir"`,
		`Write-Output "Install manifest: $(Join-Path $TargetDir '.remote-dev-skillkit/install.json')"`,
		`Write-Output "Configure MCP stdio command: $RdevBin mcp serve"`,
		"",
	}, "\n")
}

func installCommandsDoc(bundleDir, rdevCommand string, plans []FrameworkInstallPlan) string {
	lines := []string{
		"# Remote Dev Skillkit Install Commands",
		"",
		"This directory was generated by `rdev skillkit plan-install`.",
		"It does not install anything until you run one of the generated scripts.",
		"",
		"Verify the plan first:",
		"",
		"```bash",
		rdevCommand + " skillkit verify-install-plan --plan install-plan.json",
		"```",
		"",
		"Bundle:",
		"",
		"```text",
		bundleDir,
		"```",
		"",
		"## Adaptive Configuration Contract",
		"",
		"Before using the installed skills, probe the runtime with `rdev doctor`, `rdev mcp tools`, OS/shell checks, service-manager checks, gateway configuration checks, network reachability checks, proxy/DNS checks, NAT/firewall/CGNAT checks, SSH configuration checks, tunnel/mesh tooling checks, connection modes checks, workspace checks, adapter detection, framework install path checks, and permission checks.",
		"",
		"For personal-computer installs, local MCP stdio with `rdev mcp serve` is enough. Hosted gateway URLs are optional until a remote-host workflow needs local dev, LAN, hosted, SSH-tunnel, or relay/mesh/VPN connectivity.",
		"",
		"If direct reachability is blocked, prefer existing or open-source/free tunnel/mesh options such as frp, Chisel, headscale, or WireGuard before paid relays.",
		"",
		"If gateway URL, ticket code, root key, release URL, checksum, framework install path, workspace root, adapter choice, tunnel/mesh authorization, or authorization policy is unclear, ask a short follow-up question instead of inventing a value.",
		"",
		"Examples such as `https://api.example.com/v1`, `/Users/example`, `/home/example`, and `C:\\Users\\Alice` are placeholders.",
		"",
		"## Framework Scripts",
		"",
	}
	for _, plan := range plans {
		lines = append(lines,
			"### "+plan.DisplayName,
			"",
			"Target hint: `"+plan.SkillTargetHint+"`",
			"",
			"Unix/macOS:",
			"",
			"```bash",
			"./"+plan.ShellScript,
			"```",
			"",
			"Windows PowerShell:",
			"",
			"```powershell",
			".\\"+plan.PowerShellScript,
			"```",
			"",
		)
	}
	lines = append(lines,
		"## Safety",
		"",
		"- Generated scripts run `rdev skillkit verify` before installing.",
		"- Generated scripts call `rdev skillkit install --execute`, so copied skills, reference files, and `.remote-dev-skillkit/install.json` come from the same standard installer.",
		"- Existing skill directories are not overwritten unless `RDEV_SKILLKIT_FORCE=1` is set.",
		"- MCP configuration is not silently modified; configure the runtime to run `"+rdevCommand+" mcp serve`.",
		"",
	)
	return strings.Join(lines, "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellQuotedValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, shellQuote(value))
	}
	return result
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powerShellQuotedValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, powerShellQuote(value))
	}
	return result
}

func bundleVerificationFailures(report VerificationReport) string {
	var failures []string
	for _, check := range report.Checks {
		if !check.Passed {
			failures = append(failures, check.Name+":"+check.Detail)
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func installScriptsPresent(planDir string, plans []FrameworkInstallPlan) string {
	var missing []string
	for _, plan := range plans {
		for _, script := range []string{plan.ShellScript, plan.PowerShellScript} {
			if _, err := os.Stat(filepath.Join(planDir, filepath.FromSlash(script))); err != nil {
				missing = append(missing, script)
			}
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, ",")
}

func installScriptsForbiddenMutation(planDir string, plans []FrameworkInstallPlan) string {
	forbidden := []string{
		"curl ", "wget ", "irm ", "iwr ", "invoke-webrequest",
		"git push", "gh release", "rm -rf", "set-executionpolicy",
		"new-service", "register-scheduledtask", "launchctl", "systemctl",
	}
	var failures []string
	for _, plan := range plans {
		for _, script := range []string{plan.ShellScript, plan.PowerShellScript} {
			content, err := os.ReadFile(filepath.Join(planDir, filepath.FromSlash(script)))
			if err != nil {
				continue
			}
			lower := strings.ToLower(string(content))
			for _, needle := range forbidden {
				if strings.Contains(lower, needle) {
					failures = append(failures, script+":"+strings.TrimSpace(needle))
				}
			}
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func installScriptsRequireBundleVerify(planDir string, plans []FrameworkInstallPlan) string {
	var failures []string
	for _, plan := range plans {
		for _, script := range []string{plan.ShellScript, plan.PowerShellScript} {
			content, err := os.ReadFile(filepath.Join(planDir, filepath.FromSlash(script)))
			if err != nil {
				continue
			}
			if !strings.Contains(string(content), "skillkit verify --bundle") {
				failures = append(failures, script)
			}
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func installScriptsUseStandardInstaller(planDir string, plans []FrameworkInstallPlan) string {
	var failures []string
	for _, plan := range plans {
		shellContent, shellErr := os.ReadFile(filepath.Join(planDir, filepath.FromSlash(plan.ShellScript)))
		if shellErr == nil {
			text := string(shellContent)
			if !strings.Contains(text, "skillkit install --bundle") ||
				!strings.Contains(text, "--execute") ||
				!strings.Contains(text, ".remote-dev-skillkit/install.json") {
				failures = append(failures, plan.ShellScript)
			}
		}
		powerShellContent, powerShellErr := os.ReadFile(filepath.Join(planDir, filepath.FromSlash(plan.PowerShellScript)))
		if powerShellErr == nil {
			text := string(powerShellContent)
			if !strings.Contains(text, "'skillkit', 'install'") ||
				!strings.Contains(text, "'--execute'") ||
				!strings.Contains(text, ".remote-dev-skillkit/install.json") {
				failures = append(failures, plan.PowerShellScript)
			}
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func installScriptsKeepAdaptiveContract(planDir string, plans []FrameworkInstallPlan) string {
	var failures []string
	for _, plan := range plans {
		for _, script := range []string{plan.ShellScript, plan.PowerShellScript} {
			content, err := os.ReadFile(filepath.Join(planDir, filepath.FromSlash(script)))
			if err != nil {
				continue
			}
			if !textKeepsAdaptiveContract(string(content)) {
				failures = append(failures, script)
			}
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func installCommandsKeepAdaptiveContract(planDir string) string {
	content, err := os.ReadFile(filepath.Join(planDir, "INSTALL_COMMANDS.md"))
	if err != nil {
		return errorString(err)
	}
	if !textKeepsAdaptiveContract(string(content)) {
		return "INSTALL_COMMANDS.md"
	}
	return ""
}

func failedInstallPlanActions() []string {
	return []string{
		"Regenerate the install plan with rdev skillkit plan-install.",
		"Do not run generated install scripts until verify-install-plan returns ok=true.",
		"If a script was edited intentionally, regenerate the plan so checksums match.",
	}
}
