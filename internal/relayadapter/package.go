package relayadapter

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

const PackageSchemaVersion = "rdev.relay-adapter-package.v1"
const VerificationSchemaVersion = "rdev.relay-adapter-package-verification.v1"
const AcceptanceEvidencePlanSchemaVersion = "rdev.relay-adapter-acceptance-evidence-plan.v1"

type Options struct {
	OutDir      string
	Name        string
	AdapterKind string
	GeneratedAt time.Time
	Force       bool
}

type Package struct {
	SchemaVersion     string        `json:"schema_version"`
	Name              string        `json:"name"`
	GeneratedAt       time.Time     `json:"generated_at"`
	AdapterKind       string        `json:"adapter_kind"`
	ExternalMutation  bool          `json:"external_mutation"`
	ProductionClaim   string        `json:"production_claim"`
	RunnerEnv         RunnerEnv     `json:"runner_env"`
	Helper            Helper        `json:"helper"`
	ConnectionPathID  string        `json:"connection_path_id"`
	StartArgvTemplate []string      `json:"start_argv_template"`
	InstallAction     InstallAction `json:"install_action_template"`
	EvidencePlanPath  string        `json:"evidence_plan_path"`
	EvidenceRequired  []string      `json:"evidence_required"`
	ApprovalRequired  []string      `json:"approval_required"`
	AgentRules        []string      `json:"agent_rules"`
	Files             []PackageFile `json:"files"`
	Checks            []Check       `json:"checks"`
}

type AcceptanceEvidencePlan struct {
	SchemaVersion     string             `json:"schema_version"`
	GeneratedAt       time.Time          `json:"generated_at"`
	AdapterKind       string             `json:"adapter_kind"`
	ConnectionPathID  string             `json:"connection_path_id"`
	PackagePath       string             `json:"package_path"`
	ExternalMutation  bool               `json:"external_mutation"`
	EvidenceFiles     []EvidencePlanFile `json:"evidence_files"`
	DryRunCommand     []string           `json:"dry_run_command"`
	RunCommand        []string           `json:"run_command"`
	PackageCommand    []string           `json:"package_command"`
	VerifyCommand     []string           `json:"verify_command"`
	AgentRules        []string           `json:"agent_rules"`
	ApprovalRequired  []string           `json:"approval_required"`
	UnsupportedClaims []string           `json:"unsupported_claims"`
}

type EvidencePlanFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Flag        string `json:"flag"`
	Description string `json:"description"`
}

type RunnerEnv struct {
	GatewayURLVar     string `json:"gateway_url_var"`
	StartArgvVar      string `json:"start_argv_var"`
	InstallActionVar  string `json:"install_action_var"`
	ConnectionPathID  string `json:"connection_path_id"`
	RunnerCommandHint string `json:"runner_command_hint"`
}

type Helper struct {
	Tool              string   `json:"tool"`
	Aliases           []string `json:"aliases,omitempty"`
	Scope             string   `json:"scope"`
	SupportedPlatform []string `json:"supported_platforms"`
	VerifyCommand     string   `json:"verify_command"`
	RuntimeStatus     string   `json:"runtime_status"`
}

type InstallAction struct {
	SchemaVersion     string   `json:"schema_version"`
	Tool              string   `json:"tool"`
	Argv              []string `json:"argv"`
	Scope             string   `json:"scope"`
	Reason            string   `json:"reason"`
	ExpectedSHA256    string   `json:"expected_sha256"`
	RequiresElevation bool     `json:"requires_elevation"`
}

type PackageFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Kind      string `json:"kind"`
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type Verification struct {
	SchemaVersion      string      `json:"schema_version"`
	PackagePath        string      `json:"package_path"`
	PackageDir         string      `json:"package_dir"`
	Name               string      `json:"name,omitempty"`
	AdapterKind        string      `json:"adapter_kind,omitempty"`
	Checks             []Check     `json:"checks"`
	Files              []FileCheck `json:"files"`
	RecommendedActions []string    `json:"recommended_actions,omitempty"`
}

type FileCheck struct {
	Path           string  `json:"path"`
	Kind           string  `json:"kind"`
	ExpectedSHA256 string  `json:"expected_sha256"`
	ActualSHA256   string  `json:"actual_sha256,omitempty"`
	ExpectedSize   int64   `json:"expected_size"`
	ActualSize     int64   `json:"actual_size,omitempty"`
	Checks         []Check `json:"checks"`
}

func (p Package) OK() bool {
	if len(p.Checks) == 0 || len(p.Files) == 0 {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (v Verification) OK() bool {
	if len(v.Checks) == 0 || len(v.Files) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	for _, file := range v.Files {
		for _, check := range file.Checks {
			if !check.Passed {
				return false
			}
		}
	}
	return true
}

func Build(opts Options) (Package, error) {
	if strings.TrimSpace(opts.OutDir) == "" {
		return Package{}, fmt.Errorf("out is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return Package{}, err
	}
	if err := prepareOut(outDir, opts.Force); err != nil {
		return Package{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	kind := normalizeKind(opts.AdapterKind)
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "rdev-" + kind + "-connectivity-adapter"
	}
	descriptor := adapterDescriptor(kind)
	pkg := Package{
		SchemaVersion:    PackageSchemaVersion,
		Name:             name,
		GeneratedAt:      now.UTC(),
		AdapterKind:      kind,
		ExternalMutation: false,
		ProductionClaim:  "connectivity-adapter-package-surface-only",
		RunnerEnv: RunnerEnv{
			GatewayURLVar:     descriptor.GatewayURLVar,
			StartArgvVar:      descriptor.StartArgvVar,
			InstallActionVar:  descriptor.InstallActionVar,
			ConnectionPathID:  descriptor.ConnectionPathID,
			RunnerCommandHint: "rdev connection-entry run --runner-manifest <connection-entry-runner.json> --dry-run",
		},
		Helper:            descriptor.Helper,
		ConnectionPathID:  descriptor.ConnectionPathID,
		StartArgvTemplate: descriptor.StartArgvTemplate,
		InstallAction:     descriptor.InstallAction,
		EvidencePlanPath:  "acceptance-evidence-plan.json",
		EvidenceRequired: []string{
			"connectivity adapter package verification",
			"helper binary detection or reviewed install report",
			"redacted helper start argv",
			"selected gateway override environment variable",
			"Connection Entry runner result with selected_path=" + descriptor.ConnectionPathID,
			"host registration and transport fallback evidence from rdev host serve",
		},
		ApprovalRequired: []string{
			"creating connectivity accounts, keys, profiles, auth keys, or credentials",
			"editing relay, SSH, mesh, or VPN configuration",
			"opening firewall, NAT, DNS, or routing paths",
			"installing persistent services, drivers, or privileged network components",
			"using paid relay, mesh, VPN, cloud, or third-party resources",
		},
		AgentRules: []string{
			"Use this package to set the declared RDEV_* gateway, helper argv, and install-action variables; do not write ad hoc SSH, relay, mesh, VPN, PowerShell, shell, or polling scripts.",
			"Scaffold restrictive-network evidence with rdev acceptance scaffold-evidence --relay-adapter-package <relay-adapter-package-dir> before collecting real helper or connectivity evidence.",
			"Ask one short question only when the endpoint, credential, identity, privilege, enrollment, route, or persistence decision is unclear.",
			"Keep real endpoints, credentials, keys, private IPs, local paths, and organization identifiers outside this package.",
			"Run the Connection Entry runner in dry-run mode before execution so rdev selects the path and reports evidence.",
		},
	}
	pkg.Checks = packageChecks(pkg)
	files := []struct {
		path    string
		kind    string
		content []byte
	}{
		{"RELAY_ADAPTER.md", "documentation", []byte(renderReadme(pkg))},
		{"runner.env.example", "env-template", []byte(renderEnvTemplate(pkg))},
	}
	evidencePlan := acceptanceEvidencePlan(pkg, now)
	evidencePlanContent, err := json.MarshalIndent(evidencePlan, "", "  ")
	if err != nil {
		return Package{}, err
	}
	files = append(files, struct {
		path    string
		kind    string
		content []byte
	}{"acceptance-evidence-plan.json", "acceptance-evidence-plan", append(evidencePlanContent, '\n')})
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(outDir, file.path), file.content, 0o644); err != nil {
			return Package{}, err
		}
	}
	for _, file := range files {
		entry, err := packageFile(outDir, file.path, file.kind)
		if err != nil {
			return Package{}, err
		}
		pkg.Files = append(pkg.Files, entry)
	}
	sort.Slice(pkg.Files, func(i, j int) bool { return pkg.Files[i].Path < pkg.Files[j].Path })
	manifest, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return Package{}, err
	}
	manifest = append(manifest, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "relay-adapter.json"), manifest, 0o644); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

func Verify(path string) (Verification, error) {
	manifestPath, dir, err := resolveManifest(path)
	if err != nil {
		return Verification{}, err
	}
	v := Verification{
		SchemaVersion: VerificationSchemaVersion,
		PackagePath:   "relay-adapter.json",
		PackageDir:    ".",
	}
	add := func(name string, passed bool, detail string) {
		v.Checks = append(v.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	content, err := os.ReadFile(manifestPath)
	add("manifest_exists", err == nil, "relay-adapter.json")
	if err != nil {
		v.RecommendedActions = failureActions()
		return v, nil
	}
	var pkg Package
	err = json.Unmarshal(content, &pkg)
	add("manifest_json_valid", err == nil, errorDetail(err))
	if err != nil {
		v.RecommendedActions = failureActions()
		return v, nil
	}
	v.Name = pkg.Name
	v.AdapterKind = pkg.AdapterKind
	add("schema_version", pkg.SchemaVersion == PackageSchemaVersion, pkg.SchemaVersion)
	add("adapter_kind_supported", supportedKind(pkg.AdapterKind), pkg.AdapterKind)
	add("external_mutation_false", !pkg.ExternalMutation, fmt.Sprintf("%t", pkg.ExternalMutation))
	add("production_claim_is_scoped", pkg.ProductionClaim == "connectivity-adapter-package-surface-only", pkg.ProductionClaim)
	add("runner_env_declared", runnerEnvDeclaredForKind(pkg.AdapterKind, pkg.RunnerEnv), fmt.Sprintf("%#v", pkg.RunnerEnv))
	add("start_argv_template_safe", safeStartArgv(pkg.AdapterKind, pkg.StartArgvTemplate), strings.Join(pkg.StartArgvTemplate, " "))
	add("install_action_safe", safeInstallAction(pkg.AdapterKind, pkg.InstallAction), strings.Join(pkg.InstallAction.Argv, " "))
	add("acceptance_evidence_plan_declared", pkg.EvidencePlanPath == "acceptance-evidence-plan.json", pkg.EvidencePlanPath)
	add("agent_rules_present", len(pkg.AgentRules) >= 3, fmt.Sprintf("%d", len(pkg.AgentRules)))
	add("no_private_surface", noPrivateSurface(content), "manifest")
	for _, check := range packageChecks(pkg) {
		v.Checks = append(v.Checks, check)
	}
	v.Files = verifyFiles(dir, pkg.Files)
	v.Checks = append(v.Checks, verifyAcceptanceEvidencePlan(dir, pkg)...)
	if unlisted := unlistedFiles(dir, pkg.Files); len(unlisted) > 0 {
		add("no_unlisted_files", false, strings.Join(unlisted, ","))
	} else {
		add("no_unlisted_files", true, "")
	}
	if !v.OK() {
		v.RecommendedActions = failureActions()
	}
	return v, nil
}

func normalizeKind(value string) string {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".exe")
	switch value {
	case "", "chisel":
		return "chisel"
	case "frp", "frpc":
		return "frpc"
	case "ssh", "openssh", "ssh-tunnel":
		return "ssh-tunnel"
	case "headscale", "tailscale", "headscale-tailscale", "tailscale-compatible":
		return "headscale-tailscale"
	case "wireguard", "wg", "wg-quick":
		return "wireguard"
	default:
		return value
	}
}

func supportedKind(kind string) bool {
	switch normalizeKind(kind) {
	case "chisel", "frpc", "ssh-tunnel", "headscale-tailscale", "wireguard":
		return true
	default:
		return false
	}
}

type descriptor struct {
	ConnectionPathID  string
	GatewayURLVar     string
	StartArgvVar      string
	InstallActionVar  string
	Helper            Helper
	StartArgvTemplate []string
	InstallAction     InstallAction
}

func adapterDescriptor(kind string) descriptor {
	switch normalizeKind(kind) {
	case "frpc":
		return descriptor{
			ConnectionPathID:  "existing-frp-or-chisel-relay",
			GatewayURLVar:     "RDEV_RELAY_GATEWAY_URL",
			StartArgvVar:      "RDEV_RELAY_START_ARGV_JSON",
			InstallActionVar:  "RDEV_RELAY_INSTALL_ACTION_JSON",
			Helper:            helper("frpc", []string{"frp"}, "rdev deps install --tool frpc --url ${RDEV_FRPC_DOWNLOAD_URL} --expected-sha256 ${RDEV_FRPC_SHA256}", "configured-helper-executable"),
			StartArgvTemplate: []string{"frpc", "-c", "${RDEV_FRPC_CONFIG}"},
			InstallAction:     installAction("frpc", "FRPC", true),
		}
	case "ssh-tunnel":
		return descriptor{
			ConnectionPathID:  "existing-ssh-tunnel",
			GatewayURLVar:     "RDEV_SSH_GATEWAY_URL",
			StartArgvVar:      "RDEV_SSH_TUNNEL_START_ARGV_JSON",
			InstallActionVar:  "RDEV_SSH_INSTALL_ACTION_JSON",
			Helper:            helper("ssh", []string{"openssh"}, "ssh -V", "use-existing-config-only"),
			StartArgvTemplate: []string{"ssh", "-N", "-L", "${RDEV_SSH_LOCAL_FORWARD}", "${RDEV_SSH_TARGET_ALIAS}"},
			InstallAction:     installAction("ssh", "SSH", false),
		}
	case "headscale-tailscale":
		return descriptor{
			ConnectionPathID:  "existing-headscale-tailscale-mesh",
			GatewayURLVar:     "RDEV_MESH_GATEWAY_URL",
			StartArgvVar:      "RDEV_MESH_START_ARGV_JSON",
			InstallActionVar:  "RDEV_MESH_INSTALL_ACTION_JSON",
			Helper:            helper("tailscale", []string{"headscale-compatible"}, "rdev deps install --tool tailscale --url ${RDEV_MESH_DOWNLOAD_URL} --expected-sha256 ${RDEV_MESH_SHA256}", "configured-helper-executable-existing-enrollment-only"),
			StartArgvTemplate: []string{"tailscale", "status", "--json"},
			InstallAction:     installAction("tailscale", "MESH", true),
		}
	case "wireguard":
		return descriptor{
			ConnectionPathID:  "existing-wireguard-vpn",
			GatewayURLVar:     "RDEV_VPN_GATEWAY_URL",
			StartArgvVar:      "RDEV_VPN_START_ARGV_JSON",
			InstallActionVar:  "RDEV_VPN_INSTALL_ACTION_JSON",
			Helper:            helper("wg", []string{"wg-quick", "wireguard"}, "rdev deps install --tool wg --url ${RDEV_VPN_DOWNLOAD_URL} --expected-sha256 ${RDEV_VPN_SHA256}", "configured-helper-executable-active-tunnel-only"),
			StartArgvTemplate: []string{"wg", "show"},
			InstallAction:     installAction("wg", "VPN", true),
		}
	default:
		return descriptor{
			ConnectionPathID:  "existing-frp-or-chisel-relay",
			GatewayURLVar:     "RDEV_RELAY_GATEWAY_URL",
			StartArgvVar:      "RDEV_RELAY_START_ARGV_JSON",
			InstallActionVar:  "RDEV_RELAY_INSTALL_ACTION_JSON",
			Helper:            helper("chisel", nil, "rdev deps install --tool chisel --url ${RDEV_CHISEL_DOWNLOAD_URL} --expected-sha256 ${RDEV_CHISEL_SHA256}", "configured-helper-executable"),
			StartArgvTemplate: []string{"chisel", "client", "${RDEV_CHISEL_SERVER}", "R:${RDEV_RELAY_REMOTE_PORT}:127.0.0.1:${RDEV_GATEWAY_LOCAL_PORT}"},
			InstallAction:     installAction("chisel", "CHISEL", true),
		}
	}
}

func helper(tool string, aliases []string, verifyCommand, runtimeStatus string) Helper {
	return Helper{
		Tool:              tool,
		Aliases:           aliases,
		Scope:             "user-or-workspace",
		SupportedPlatform: []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"},
		VerifyCommand:     verifyCommand,
		RuntimeStatus:     runtimeStatus,
	}
}

func installAction(tool, prefix string, downloadable bool) InstallAction {
	tool = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(tool)), ".exe")
	argv := []string{"manual-review-required"}
	expectedSHA := "not-applicable-existing-helper"
	reason := "Use an existing reviewed helper installation or operator-approved install plan."
	if downloadable {
		argv = []string{"rdev", "deps", "install", "--tool", tool, "--scope", "user", "--url", "${RDEV_" + prefix + "_DOWNLOAD_URL}", "--expected-sha256", "${RDEV_" + prefix + "_SHA256}", "--execute"}
		expectedSHA = "${RDEV_" + prefix + "_SHA256}"
		reason = "Install the reviewed open-source relay helper when the target does not already have it."
	}
	return InstallAction{
		SchemaVersion:     "rdev.connection-entry.dependency-install-action.v1",
		Tool:              tool,
		Argv:              argv,
		Scope:             "user",
		Reason:            reason,
		ExpectedSHA256:    expectedSHA,
		RequiresElevation: false,
	}
}

func packageChecks(pkg Package) []Check {
	return []Check{
		{Name: "schema_version", Passed: pkg.SchemaVersion == PackageSchemaVersion, Detail: pkg.SchemaVersion},
		{Name: "adapter_kind_supported", Passed: supportedKind(pkg.AdapterKind), Detail: pkg.AdapterKind},
		{Name: "external_mutation_false", Passed: !pkg.ExternalMutation, Detail: fmt.Sprintf("%t", pkg.ExternalMutation)},
		{Name: "production_claim_scoped", Passed: pkg.ProductionClaim == "connectivity-adapter-package-surface-only", Detail: pkg.ProductionClaim},
		{Name: "runner_env_declared", Passed: runnerEnvDeclaredForKind(pkg.AdapterKind, pkg.RunnerEnv), Detail: pkg.RunnerEnv.ConnectionPathID},
		{Name: "start_argv_template_safe", Passed: safeStartArgv(pkg.AdapterKind, pkg.StartArgvTemplate), Detail: strings.Join(pkg.StartArgvTemplate, " ")},
		{Name: "install_action_safe", Passed: safeInstallAction(pkg.AdapterKind, pkg.InstallAction), Detail: strings.Join(pkg.InstallAction.Argv, " ")},
		{Name: "acceptance_evidence_plan_declared", Passed: pkg.EvidencePlanPath == "acceptance-evidence-plan.json", Detail: pkg.EvidencePlanPath},
		{Name: "approval_boundaries_declared", Passed: len(pkg.ApprovalRequired) >= 3, Detail: fmt.Sprintf("%d", len(pkg.ApprovalRequired))},
		{Name: "evidence_required_declared", Passed: len(pkg.EvidenceRequired) >= 4, Detail: fmt.Sprintf("%d", len(pkg.EvidenceRequired))},
	}
}

func acceptanceEvidencePlan(pkg Package, generatedAt time.Time) AcceptanceEvidencePlan {
	files := []EvidencePlanFile{
		{Name: "runner-result", Path: "runner-result.json", Kind: "json", Required: true, Flag: "--runner-result", Description: "Raw rdev.connection-entry.runner-result.v1 generated by rdev connection-entry run --evidence-dir."},
		{Name: "helper-transcript", Path: "helper-transcript.txt", Kind: "transcript", Required: true, Flag: "--helper-transcript", Description: "Standard helper transcript generated by rdev connection-entry run --evidence-dir, plus any extra redacted supervisor notes from the real run."},
		{Name: "gateway-status", Path: "gateway-status.json", Kind: "json", Required: true, Flag: "--gateway-status", Description: "Runner-generated gateway status or health probe evidence for the selected helper path."},
		{Name: "host-status", Path: "host-status.json", Kind: "json", Required: true, Flag: "--host-status", Description: "Runner-generated host registration or host serve status evidence."},
		{Name: "connection-status", Path: "connection-status.json", Kind: "json", Required: true, Flag: "--connection-status", Description: "Runner-generated connection status with connected=true for the selected standard path."},
		{Name: "audit", Path: "audit.jsonl", Kind: "transcript", Required: true, Flag: "--audit", Description: "Runner-generated redacted audit JSONL covering helper start, host registration, status, and cleanup."},
	}
	packageCommand := []string{
		"rdev", "acceptance", "package-relay-adapter",
		"--relay-package", "<relay-adapter-package-dir>",
		"--out", "<relay-adapter-evidence-out>",
		"--evidence-dir", ".",
	}
	return AcceptanceEvidencePlan{
		SchemaVersion:    AcceptanceEvidencePlanSchemaVersion,
		GeneratedAt:      generatedAt.UTC(),
		AdapterKind:      pkg.AdapterKind,
		ConnectionPathID: pkg.ConnectionPathID,
		PackagePath:      "relay-adapter.json",
		ExternalMutation: false,
		EvidenceFiles:    files,
		DryRunCommand:    []string{"rdev", "connection-entry", "run", "--runner-manifest", "connection-entry-runner.json", "--dry-run", "--evidence-dir", "."},
		RunCommand:       []string{"rdev", "connection-entry", "run", "--runner-manifest", "connection-entry-runner.json", "--evidence-dir", "."},
		PackageCommand:   packageCommand,
		VerifyCommand:    []string{"rdev", "acceptance", "verify-relay-adapter-package", "--package", "<relay-adapter-evidence-out>/package.json"},
		AgentRules: []string{
			"Start with rdev acceptance scaffold-evidence --relay-adapter-package <relay-adapter-package-dir> --out <relay-adapter-evidence-input>; do not hand-pick acceptance-evidence-plan.json unless an operator is reviewing an override.",
			"Use rdev connection-entry run --evidence-dir . to create runner-result.json, helper-transcript.txt, gateway-status.json, host-status.json, connection-status.json, audit.jsonl, and evidence-report.json; do not hand-write runner evidence.",
			"Use rdev acceptance package-relay-adapter --evidence-dir . from this plan when packaging real restrictive-network evidence.",
			"Redact helper endpoints, credentials, private IPs, local paths, usernames, and hostnames before sharing evidence outside the operator account.",
			"If endpoint, credential, identity, route, privilege, or persistence is unclear, ask one short question instead of guessing.",
		},
		ApprovalRequired: append([]string(nil), pkg.ApprovalRequired...),
		UnsupportedClaims: []string{
			"This package and plan do not prove a deployed relay, SSH tunnel, mesh, or VPN path by themselves.",
			"Production connectivity claims require a verified rdev.acceptance-package.relay-adapter.v1 evidence bundle from a real target environment.",
		},
	}
}

func verifyAcceptanceEvidencePlan(dir string, pkg Package) []Check {
	var checks []Check
	add := func(name string, passed bool, detail string) {
		checks = append(checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	content, err := os.ReadFile(filepath.Join(dir, "acceptance-evidence-plan.json"))
	add("acceptance_evidence_plan_exists", err == nil, "acceptance-evidence-plan.json")
	if err != nil {
		return checks
	}
	var plan AcceptanceEvidencePlan
	err = json.Unmarshal(content, &plan)
	add("acceptance_evidence_plan_json_valid", err == nil, errorDetail(err))
	if err != nil {
		return checks
	}
	add("acceptance_evidence_plan_schema", plan.SchemaVersion == AcceptanceEvidencePlanSchemaVersion, plan.SchemaVersion)
	add("acceptance_evidence_plan_adapter_match", plan.AdapterKind == pkg.AdapterKind && plan.ConnectionPathID == pkg.ConnectionPathID, plan.AdapterKind+"/"+plan.ConnectionPathID)
	add("acceptance_evidence_plan_package_path", plan.PackagePath == "relay-adapter.json", plan.PackagePath)
	add("acceptance_evidence_plan_external_mutation_false", !plan.ExternalMutation, fmt.Sprintf("%t", plan.ExternalMutation))
	add("acceptance_evidence_plan_files_declared", len(plan.EvidenceFiles) >= 6, fmt.Sprintf("%d", len(plan.EvidenceFiles)))
	add("acceptance_evidence_plan_uses_runner_evidence_dir", stringSliceContains(plan.DryRunCommand, "--evidence-dir") && stringSliceContains(plan.RunCommand, "--evidence-dir"), strings.Join(plan.RunCommand, " "))
	add("acceptance_evidence_plan_package_command", stringSliceContains(plan.PackageCommand, "package-relay-adapter") && stringSliceContains(plan.PackageCommand, "--relay-package"), strings.Join(plan.PackageCommand, " "))
	add("acceptance_evidence_plan_verify_command", stringSliceContains(plan.VerifyCommand, "verify-relay-adapter-package"), strings.Join(plan.VerifyCommand, " "))
	add("acceptance_evidence_plan_agent_rules", len(plan.AgentRules) >= 3, fmt.Sprintf("%d", len(plan.AgentRules)))
	add("acceptance_evidence_plan_scaffold_rule", stringSliceContainsSubstring(plan.AgentRules, "scaffold-evidence --relay-adapter-package"), strings.Join(plan.AgentRules, " | "))
	return checks
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSliceContainsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func runnerEnvDeclaredForKind(kind string, env RunnerEnv) bool {
	descriptor := adapterDescriptor(kind)
	return env.GatewayURLVar == descriptor.GatewayURLVar &&
		env.StartArgvVar == descriptor.StartArgvVar &&
		env.InstallActionVar == descriptor.InstallActionVar &&
		env.ConnectionPathID == descriptor.ConnectionPathID
}

func safeStartArgv(kind string, argv []string) bool {
	kind = normalizeKind(kind)
	if len(argv) == 0 || normalizeKind(argv[0]) != kind {
		return false
	}
	joined := strings.ToLower(strings.Join(argv, "\n"))
	for _, forbidden := range []string{"executionpolicy", "bypass", "encodedcommand", "powershell", "pwsh", "cmd.exe", "cmd /c", "bash -c", "sh -c", "curl |", "wget |"} {
		if strings.Contains(joined, forbidden) {
			return false
		}
	}
	return noPrivateSurface([]byte(joined))
}

func safeInstallAction(kind string, action InstallAction) bool {
	kind = normalizeKind(kind)
	if action.SchemaVersion != "rdev.connection-entry.dependency-install-action.v1" ||
		normalizeKind(action.Tool) != kind ||
		action.RequiresElevation {
		return false
	}
	joined := strings.ToLower(strings.Join(action.Argv, "\n"))
	if strings.Contains(joined, "executionpolicy") || strings.Contains(joined, "bypass") || strings.Contains(joined, "encodedcommand") {
		return false
	}
	if strings.Join(action.Argv, " ") == "manual-review-required" {
		return kind == "ssh-tunnel" || kind == "headscale-tailscale" || kind == "wireguard"
	}
	return strings.Contains(joined, "rdev") && strings.Contains(joined, "deps") && strings.Contains(joined, "install")
}

func prepareOut(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 && !force {
			return fmt.Errorf("output directory must be empty: %s", dir)
		}
		if force {
			for _, entry := range entries {
				if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func packageFile(root, path, kind string) (PackageFile, error) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return PackageFile{}, err
	}
	sum := sha256.Sum256(content)
	return PackageFile{
		Path:      path,
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(content)),
		Kind:      kind,
	}, nil
}

func renderReadme(pkg Package) string {
	return fmt.Sprintf(`# Remote Dev Skillkit Relay Adapter Package

Schema: %s
Name: %s
Adapter: %s

This package gives Agents a standard connectivity helper surface for restrictive
networks. It contains no real relay, SSH, mesh, VPN, credential, private IP,
local path, organization identifier, or server address.

Before collecting real restrictive-network evidence, scaffold from the package
directory:

    rdev acceptance scaffold-evidence --relay-adapter-package <relay-adapter-package-dir> --out <relay-adapter-evidence-input>

The scaffold reads acceptance-evidence-plan.json for Agents, writes the
standard runner/status/helper/audit checklist, and keeps package/verify
commands inside rdev instead of model-authored relay scripts. Read
acceptance-evidence-plan.json directly only for a reviewed operator override.

Runner environment:

- %s: target-usable gateway URL reached through the relay.
- %s: JSON argv for the reviewed helper process.
- %s: JSON install action consumed by rdev when the helper is missing.

Verify before use:

%s

Run a Connection Entry dry-run before execution:

%s

Privileged firewall, NAT, DNS, route, service, driver, paid relay, and relay
credential changes require explicit operator approval.
`, PackageSchemaVersion, pkg.Name, pkg.AdapterKind, pkg.RunnerEnv.GatewayURLVar, pkg.RunnerEnv.StartArgvVar, pkg.RunnerEnv.InstallActionVar, pkg.Helper.VerifyCommand, pkg.RunnerEnv.RunnerCommandHint)
}

func renderEnvTemplate(pkg Package) string {
	startArgv, _ := json.Marshal(pkg.StartArgvTemplate)
	install, _ := json.Marshal(pkg.InstallAction)
	installValue := string(install)
	installNote := "Optional reviewed helper install action. Requires SHA-256 before execution."
	if len(pkg.InstallAction.Argv) == 1 && pkg.InstallAction.Argv[0] == "manual-review-required" {
		installValue = ""
		installNote = "Optional reviewed helper install action. Leave empty unless the operator provides a real reviewed JSON action; enrollment, key, route, profile, or service changes require approval."
	}
	return fmt.Sprintf(`# Reviewed gateway URL reachable by the target host through this helper path.
%s=

# Reviewed helper argv. Keep credentials, keys, and real endpoints in external
# secret/config files.
%s=%s

# %s
%s=%s
`, pkg.RunnerEnv.GatewayURLVar, pkg.RunnerEnv.StartArgvVar, startArgv, installNote, pkg.RunnerEnv.InstallActionVar, installValue)
}

func resolveManifest(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("package is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(abs, "relay-adapter.json"), abs, nil
	}
	return abs, filepath.Dir(abs), nil
}

func verifyFiles(root string, files []PackageFile) []FileCheck {
	seen := map[string]bool{}
	var out []FileCheck
	for _, file := range files {
		result := FileCheck{
			Path:           file.Path,
			Kind:           file.Kind,
			ExpectedSHA256: file.SHA256,
			ExpectedSize:   file.SizeBytes,
		}
		add := func(name string, passed bool, detail string) {
			result.Checks = append(result.Checks, Check{Name: name, Passed: passed, Detail: detail})
		}
		safe := safePath(file.Path)
		add("file_path_safe", safe, file.Path)
		add("file_path_unique", !seen[file.Path], file.Path)
		seen[file.Path] = true
		add("expected_sha256_format", strings.HasPrefix(file.SHA256, "sha256:") && len(strings.TrimPrefix(file.SHA256, "sha256:")) == 64, file.SHA256)
		if safe {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
			add("file_exists", err == nil, errorDetail(err))
			if err == nil {
				sum := sha256.Sum256(content)
				result.ActualSHA256 = "sha256:" + hex.EncodeToString(sum[:])
				result.ActualSize = int64(len(content))
				add("file_sha256_matches", result.ActualSHA256 == file.SHA256, file.SHA256)
				add("file_size_matches", result.ActualSize == file.SizeBytes, fmt.Sprintf("%d", file.SizeBytes))
				add("file_has_no_private_surface", noPrivateSurface(content), file.Path)
			}
		}
		out = append(out, result)
	}
	return out
}

func unlistedFiles(root string, files []PackageFile) []string {
	listed := map[string]bool{}
	for _, file := range files {
		listed[file.Path] = true
	}
	listed["relay-adapter.json"] = true
	var unlisted []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !listed[key] {
			unlisted = append(unlisted, key)
		}
		return nil
	})
	sort.Strings(unlisted)
	return unlisted
}

func safePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func noPrivateSurface(content []byte) bool {
	lower := strings.ToLower(string(content))
	for _, marker := range []string{
		"begin private key",
		"api_key",
		"apikey",
		"password=",
		"secret=",
		"token=",
		"sk-",
		"192.168.",
		"10.0.",
		"10.1.",
		"172.16.",
		"172.17.",
		"eitan",
		"/users/",
		"c:\\users\\",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func failureActions() []string {
	return []string{
		"Regenerate the relay adapter package in a clean output directory.",
		"Keep real relay endpoints, credentials, private IPs, organization IDs, and local paths outside public package files.",
		"Use rdev connection-entry run --dry-run after exporting reviewed RDEV_RELAY_* environment variables.",
	}
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
