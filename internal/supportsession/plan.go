package supportsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/hostcap"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
)

const PlanSchemaVersion = "rdev.support-session-plan.v1"
const PrepareSchemaVersion = "rdev.support-session-prepare.v1"
const HandoffSchemaVersion = "rdev.support-session-handoff.v1"
const ConnectSchemaVersion = "rdev.support-session-connect.v1"
const CreatedSchemaVersion = "rdev.support-session-created.v1"
const StartedSchemaVersion = "rdev.support-session-started.v1"
const StatusSchemaVersion = "rdev.support-session-status.v1"
const StatusFileSchemaVersion = "rdev.support-session-status-file.v1"
const ConnectionAttemptPolicySchemaVersion = "rdev.connection-attempt-policy.v1"
const UserHandoffSchemaVersion = "rdev.support-session-user-handoff.v1"
const ConnectionRecoverySchemaVersion = "rdev.support-session-connection-recovery.v1"
const ConnectedNextStepsSchemaVersion = "rdev.support-session-connected-next-steps.v1"
const ContinuityPolicySchemaVersion = "rdev.support-session-continuity-policy.v1"
const ConnectionSupervisionSchemaVersion = "rdev.support-session-connection-supervision.v1"
const GatewayCandidatePreflightSchemaVersion = "rdev.support-session-gateway-candidate-preflight.v1"
const ConnectivityHelperPreflightSchemaVersion = "rdev.support-session-connectivity-helper-preflight.v1"
const AgentConnectionRunbookSchemaVersion = "rdev.support-session-agent-runbook.v1"
const FreshAgentFailurePreventionSchemaVersion = "rdev.support-session-fresh-agent-failure-prevention.v1"

const (
	targetHTTPConnectTimeoutSeconds = 2
	targetHTTPMaxTimeSeconds        = 10
	targetHTTPRetries               = 1
	targetHTTPRetryDelaySeconds     = 1
)

type Options struct {
	RepoRoot    string
	WorkDir     string
	GatewayURL  string
	Addr        string
	Target      string
	Reason      string
	TTLSeconds  int
	AutoApprove bool
	Locale      string
}

type PrepareOptions struct {
	RepoRoot    string
	WorkDir     string
	Addr        string
	GatewayURL  string
	Target      string
	BuildAssets bool
}

type HandoffOptions struct {
	RepoRoot    string
	WorkDir     string
	Addr        string
	GatewayURL  string
	Target      string
	Reason      string
	TTLSeconds  int
	AutoApprove bool
	Locale      string
	RdevCommand string
}

type GatewayURLCandidate struct {
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Source      string `json:"source"`
	Recommended bool   `json:"recommended"`
	Reason      string `json:"reason"`
}

type GatewayEnvCandidate struct {
	EnvVar string
	URL    string
	Kind   string
	Scope  string
	Reason string
}

type connectivityHelperDefinition struct {
	ID               string
	Kind             string
	GatewayEnv       string
	StartArgvEnv     string
	InstallActionEnv string
	AllowedTools     []string
	ApprovalRequired []string
}

func BuildHandoff(opts HandoffOptions) map[string]any {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	gatewayURL, gatewayCandidates := ResolveGatewayURL(addr, opts.GatewayURL)
	target := normalizeTarget(opts.Target)
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	reason := strings.TrimSpace(opts.Reason)
	if reason == "" {
		reason = "visible temporary remote support"
	}
	ttl := opts.TTLSeconds
	if ttl == 0 {
		ttl = 7200
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	selectedPath := "start-foreground-gateway"
	agentNextStep := "run cli_start_now_command in a visible terminal, then send the returned user_handoff.message plus user_handoff.copy_paste"
	mcpNextTool := ""
	resolvedCreateGatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if resolvedCreateGatewayURL == "" {
		resolvedCreateGatewayURL, _ = ConfiguredGatewayURLCandidate()
	}
	if resolvedCreateGatewayURL != "" {
		gatewayURL = resolvedCreateGatewayURL
		selectedPath = "create-with-reachable-gateway"
		agentNextStep = "call rdev.support_session.create with mcp_next_arguments, then send the returned user_handoff.message plus user_handoff.copy_paste"
		mcpNextTool = "rdev.support_session.create"
	}
	createArgs := map[string]any{
		"gateway_url":  resolvedCreateGatewayURL,
		"target":       target,
		"reason":       reason,
		"ttl_seconds":  ttl,
		"auto_approve": opts.AutoApprove,
		"locale":       locale,
		"rdev_command": rdevCommand,
	}
	startCommand := []string{
		rdevCommand, "support-session", "start",
		"--addr", addr,
		"--target", target,
		"--reason", reason,
		"--ttl-seconds", strconv.Itoa(ttl),
		"--locale", locale,
		"--rdev-command", rdevCommand,
	}
	if workDir != "" {
		startCommand = append(startCommand, "--work-dir", workDir)
	}
	if gatewayURL != "" {
		startCommand = append(startCommand, "--gateway-url", gatewayURL)
	}
	if opts.AutoApprove {
		startCommand = append(startCommand, "--auto-approve")
	} else {
		startCommand = append(startCommand, "--auto-approve=false")
	}
	connectStartCommand := []string{
		rdevCommand, "support-session", "connect",
		"--start",
		"--addr", addr,
		"--target", target,
		"--reason", reason,
		"--ttl-seconds", strconv.Itoa(ttl),
		"--locale", locale,
		"--rdev-command", rdevCommand,
	}
	if workDir != "" {
		connectStartCommand = append(connectStartCommand, "--work-dir", workDir)
	}
	if gatewayURL != "" {
		connectStartCommand = append(connectStartCommand, "--gateway-url", gatewayURL)
	}
	if opts.AutoApprove {
		connectStartCommand = append(connectStartCommand, "--auto-approve")
	} else {
		connectStartCommand = append(connectStartCommand, "--auto-approve=false")
	}
	return map[string]any{
		"schema_version":           HandoffSchemaVersion,
		"ok":                       true,
		"intent":                   "single-agent-decision-for-one-command-visible-support-session",
		"selected_path":            selectedPath,
		"agent_next_step":          agentNextStep,
		"mcp_next_tool":            mcpNextTool,
		"mcp_next_arguments":       createArgs,
		"cli_start_now_command":    connectStartCommand,
		"foreground_start_command": startCommand,
		"prepare_command": []string{
			rdevCommand, "support-session", "prepare",
			"--build-assets",
			"--repo-root", repoRoot,
			"--addr", addr,
			"--gateway-url", gatewayURL,
			"--target", target,
		},
		"status_watch_rule": "after sending the returned user_handoff to the human, call rdev.support_session.status with wait=true; when connected=true, proactively report the connection is established",
		"recovery_rule":     "if create/start/status fails or times out, read connection_recovery or rerun rdev.support_session.prepare; do not write custom recovery scripts",
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:        selectedPath,
			Status:       "not-created",
			Target:       target,
			Locale:       locale,
			GatewayURL:   gatewayURL,
			Candidates:   gatewayCandidates,
			AutoApprove:  opts.AutoApprove,
			RdevCommand:  rdevCommand,
			NeedStartNow: selectedPath != "create-with-reachable-gateway",
		}),
		"gateway_url":            gatewayURL,
		"gateway_url_candidates": gatewayCandidates,
		"target":                 target,
		"locale":                 locale,
		"auto_approve": map[string]any{
			"enabled": opts.AutoApprove,
			"scope":   "attended-temporary first host only for this standard visible session",
		},
		"human_surface_rule": "humans receive only user_handoff.message plus user_handoff.copy_paste from the next tool output",
		"agent_rule":         "use this handoff result as the first routing decision; do not choose support-session plan or write shell/PowerShell/bootstrap/relay glue unless this contract explicitly asks for a standard rdev command",
		"forbidden": []string{
			"manual ticket/root/gateway/transport assembly for target humans",
			"Agent-authored PowerShell or shell bootstrap/recovery scripts",
			"ExecutionPolicy Bypass",
			"hidden install",
			"UAC or sudo bypass",
			"service, firewall, DNS, route, credential, paid relay, or cloud changes without explicit approval",
		},
	}
}

func BuildConnectFromHandoff(handoff map[string]any) map[string]any {
	selectedPath, _ := handoff["selected_path"].(string)
	payload := map[string]any{
		"schema_version": ConnectSchemaVersion,
		"ok":             true,
		"intent":         "single-call-agent-entry-for-one-command-visible-support-session",
		"selected_path":  selectedPath,
		"handoff":        handoff,
		"agent_rule":     "Agents should call rdev.support_session.connect first when a human asks to connect a computer; follow this payload instead of choosing handoff/create/start/status manually.",
		"forbidden": []string{
			"manual ticket/root/gateway/transport assembly for target humans",
			"Agent-authored PowerShell or shell bootstrap/recovery scripts",
			"ExecutionPolicy Bypass",
			"hidden install",
		},
	}
	if selectedPath == "create-with-reachable-gateway" {
		payload["ready_to_send_to_human"] = false
		payload["mcp_next_tool"] = handoff["mcp_next_tool"]
		payload["mcp_next_arguments"] = handoff["mcp_next_arguments"]
		payload["agent_connection_runbook"] = handoff["agent_connection_runbook"]
		payload["agent_next_step"] = "call mcp_next_tool with mcp_next_arguments; then send only the returned user_handoff.message plus user_handoff.copy_paste and wait for connected=true"
		return payload
	}
	payload["ready_to_send_to_human"] = false
	payload["cli_start_now_command"] = handoff["cli_start_now_command"]
	payload["foreground_start_command"] = handoff["foreground_start_command"]
	payload["prepare_command"] = handoff["prepare_command"]
	payload["agent_connection_runbook"] = handoff["agent_connection_runbook"]
	payload["agent_next_step"] = "run cli_start_now_command in a visible terminal; it starts the gateway, builds verified helper assets, prints the target command, writes ready_file.path and status_file.path, and waits for the target; then send only the started payload's user_handoff.message plus user_handoff.copy_paste and wait for connected=true"
	payload["human_surface_rule"] = "do not send this connect payload to the target human; run the returned cli_start_now_command first and then send the started payload's top-level user_handoff"
	return payload
}

func BuildConnectFromCreated(created map[string]any) map[string]any {
	return map[string]any{
		"schema_version":          ConnectSchemaVersion,
		"ok":                      true,
		"intent":                  "single-call-agent-entry-for-one-command-visible-support-session",
		"selected_path":           "created-with-reachable-gateway",
		"ready_to_send_to_human":  true,
		"created_session":         created,
		"user_handoff":            created["user_handoff"],
		"target_command":          created["target_command"],
		"join_url":                created["join_url"],
		"watch_connection_status": created["watch_connection_status"],
		"watch_connection_status_configured_gateway": created["watch_connection_status_configured_gateway"],
		"connection_supervision":                     created["connection_supervision"],
		"gateway_candidate_preflight":                created["gateway_candidate_preflight"],
		"connectivity_helper_preflight":              created["connectivity_helper_preflight"],
		"agent_connection_runbook":                   created["agent_connection_runbook"],
		"mcp_follow_up":                              created["mcp_follow_up"],
		"agent_next_step":                            "send user_handoff.message plus user_handoff.copy_paste to the target human, wait with rdev.support_session.status, then proactively report connected_next_steps.user_report when connected=true",
		"human_surface_rule":                         "humans receive only user_handoff.message plus user_handoff.copy_paste",
		"agent_rule":                                 "Agents should call rdev.support_session.connect first when a human asks to connect a computer; do not manually choose handoff/create/start/status when this payload is available.",
		"forbidden": []string{
			"manual ticket/root/gateway/transport assembly for target humans",
			"Agent-authored PowerShell or shell bootstrap/recovery scripts",
			"ExecutionPolicy Bypass",
			"hidden install",
		},
	}
}

type StatusOptions struct {
	TicketCode string
	Hosts      []model.Host
	Locale     string
}

type CreatedOptions struct {
	GatewayURL               string
	GatewayURLCandidates     []GatewayURLCandidate
	JoinURL                  string
	ManifestURL              string
	ManifestRootPublicKey    string
	Ticket                   model.Ticket
	Target                   string
	Locale                   string
	RdevCommand              string
	AutoApprove              bool
	TargetBootstrapReadiness any
}

type StartedOptions struct {
	Addr                      string
	GatewayURL                string
	WorkDir                   string
	ReadyFile                 string
	StatusFile                string
	Created                   map[string]any
	AssetReport               any
	ConnectionReadiness       any
	ConnectivityStrategy      any
	GatewayCandidatePreflight any
	StandardRecoveryActions   []string
}

func BuildStarted(opts StartedOptions) map[string]any {
	addr := strings.TrimSpace(opts.Addr)
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	workDir := strings.TrimSpace(opts.WorkDir)
	readyFile := strings.TrimSpace(opts.ReadyFile)
	statusFile := strings.TrimSpace(opts.StatusFile)
	session := opts.Created
	payload := map[string]any{
		"schema_version": StartedSchemaVersion,
		"ok":             true,
		"intent":         "foreground-local-gateway-plus-one-command-support-session",
		"gateway": map[string]any{
			"addr":        addr,
			"gateway_url": gatewayURL,
			"work_dir":    workDir,
			"lifecycle":   "foreground-visible-process",
			"stop":        "interrupt this rdev support-session start process",
		},
		"ready_to_send_to_human":  true,
		"user_handoff":            session["user_handoff"],
		"target_command":          session["target_command"],
		"join_url":                session["join_url"],
		"watch_connection_status": session["watch_connection_status"],
		"watch_connection_status_configured_gateway": session["watch_connection_status_configured_gateway"],
		"connection_supervision":                     session["connection_supervision"],
		"foreground_feedback": map[string]any{
			"schema_version": "rdev.support-session-foreground-feedback.v1",
			"stream":         "stderr",
			"event_prefix":   "rdev support session event: ",
			"events":         []string{"waiting", "pending-approval", "connected"},
			"connected_rule": "when event=connected, immediately tell the user the connection has been established before creating jobs",
			"agent_rule":     "parse foreground feedback events from stderr when this command is kept open; read status_file.path or use the status watcher as fallback sources of truth",
		},
		"mcp_follow_up":         session["mcp_follow_up"],
		"session":               session,
		"asset_report":          opts.AssetReport,
		"connection_readiness":  opts.ConnectionReadiness,
		"connectivity_strategy": opts.ConnectivityStrategy,
		"gateway_candidate_preflight": firstNonNil(
			opts.GatewayCandidatePreflight,
			session["gateway_candidate_preflight"],
		),
		"connectivity_helper_preflight": session["connectivity_helper_preflight"],
		"agent_connection_runbook": firstNonNil(
			session["agent_connection_runbook"],
			agentConnectionRunbook(agentConnectionRunbookOptions{
				Phase:       "foreground-started",
				Status:      "waiting",
				GatewayURL:  gatewayURL,
				RdevCommand: "rdev",
			}),
		),
		"agent_flow": []string{
			"keep this process running while the target host connects",
			"give the target-side human only user_handoff.message plus user_handoff.copy_paste",
			"watch connection status with status_file.path, foreground_feedback, watch_connection_status, or rdev.support_session.status",
			"when connected=true, proactively report that the connection is established",
			"if connection_readiness.ready is false, follow standard_recovery_actions instead of writing ad hoc bootstrap or relay code",
		},
		"standard_recovery_actions": standardRecoveryActions(opts.StandardRecoveryActions),
		"human_surface_rule":        "humans receive only user_handoff.message plus user_handoff.copy_paste",
		"forbidden": []string{
			"background hidden gateway",
			"ExecutionPolicy Bypass",
			"manual ticket/root/gateway/transport assembly for target user",
			"ad hoc bootstrap script generated by the Agent",
		},
	}
	if readyFile != "" {
		payload["ready_file"] = map[string]any{
			"schema_version": "rdev.support-session-ready-file.v1",
			"path":           readyFile,
			"contains":       StartedSchemaVersion,
			"agent_rule":     "read this file after starting the foreground gateway when terminal stdout is hard to parse; send user_handoff.message plus user_handoff.copy_paste to the human",
		}
	}
	if statusFile != "" {
		payload["status_file"] = map[string]any{
			"schema_version":        StatusFileSchemaVersion,
			"path":                  statusFile,
			"contains":              "rdev.support-session-foreground-event.v1",
			"status_schema_version": StatusSchemaVersion,
			"agent_rule":            "read this file after starting the foreground gateway when terminal output is unavailable; when event=connected or status.connected=true, immediately report connected_next_steps.user_report",
		}
	}
	return payload
}

func Prepare(ctx context.Context, opts PrepareOptions) (map[string]any, error) {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	gatewayURL, gatewayCandidates := ResolveGatewayURL(addr, opts.GatewayURL)
	repoRootInput := strings.TrimSpace(opts.RepoRoot)
	if repoRootInput == "" {
		repoRootInput = "."
	}
	repoRoot, err := filepath.Abs(repoRootInput)
	if err != nil {
		repoRoot = repoRootInput
	}
	if found := findRepoRoot(repoRoot); found != "" {
		repoRoot = found
	} else if wd, err := os.Getwd(); err == nil {
		if found := findRepoRoot(wd); found != "" {
			repoRoot = found
		}
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(repoRoot, "work", "rdev-support-session")
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	binDir := filepath.Join(workDir, "bin")
	goPath, _ := exec.LookPath("go")
	gitPath, _ := exec.LookPath("git")
	rdevPath, _ := exec.LookPath("rdev")
	currentExecutable, _ := os.Executable()
	repoValid := pathExists(filepath.Join(repoRoot, "go.mod")) && pathExists(filepath.Join(repoRoot, "cmd", "rdev", "main.go"))
	assets := supportSessionAssetSpecs(binDir)
	missingInputs := []string{}
	if strings.TrimSpace(goPath) == "" {
		missingInputs = append(missingInputs, "go binary is required to build missing helper assets from source")
	}
	if !repoValid {
		missingInputs = append(missingInputs, "valid remote-dev-skillkit checkout with go.mod and cmd/rdev/main.go")
	}
	assetReports := make([]map[string]any, 0, len(assets))
	allAssetsReady := true
	for _, asset := range assets {
		assetReady := false
		report := map[string]any{
			"id":           asset.ID,
			"goos":         asset.GOOS,
			"goarch":       asset.GOARCH,
			"path":         asset.Path,
			"asset_url":    gatewayURL + "/assets/" + asset.Name,
			"sha256_url":   gatewayURL + "/assets/" + asset.Name + ".sha256",
			"build_status": "not-requested",
		}
		if fileExists(asset.Path) {
			sum, err := fileSHA256Hex(asset.Path)
			if err != nil {
				report["present"] = false
				report["error"] = err.Error()
			} else {
				report["present"] = true
				report["sha256"] = sum
				assetReady = true
			}
			if !assetReady {
				allAssetsReady = false
			}
			assetReports = append(assetReports, report)
			continue
		}
		report["present"] = false
		if opts.BuildAssets && repoValid && strings.TrimSpace(goPath) != "" {
			if err := os.MkdirAll(filepath.Dir(asset.Path), 0o700); err != nil {
				report["build_status"] = "failed"
				report["error"] = err.Error()
				assetReports = append(assetReports, report)
				continue
			}
			cmd := exec.CommandContext(ctx, goPath, "build", "-o", asset.Path, "./cmd/rdev")
			cmd.Dir = repoRoot
			cmd.Env = append(os.Environ(), "GOOS="+asset.GOOS, "GOARCH="+asset.GOARCH, "CGO_ENABLED=0")
			output, err := cmd.CombinedOutput()
			if err != nil {
				report["build_status"] = "failed"
				report["error"] = err.Error()
				if len(output) > 0 {
					report["build_output_tail"] = tailString(string(output), 800)
				}
				assetReports = append(assetReports, report)
				continue
			}
			sum, err := fileSHA256Hex(asset.Path)
			if err != nil {
				report["build_status"] = "failed"
				report["error"] = err.Error()
				assetReports = append(assetReports, report)
				continue
			}
			report["present"] = true
			report["build_status"] = "built"
			report["sha256"] = sum
			assetReady = true
		}
		if !assetReady {
			allAssetsReady = false
		}
		assetReports = append(assetReports, report)
	}
	localRdevUsable := strings.TrimSpace(rdevPath) != ""
	if !localRdevUsable && strings.TrimSpace(currentExecutable) != "" && strings.Contains(filepath.Base(currentExecutable), "rdev") {
		localRdevUsable = true
		rdevPath = currentExecutable
	}
	target := normalizeTarget(opts.Target)
	helperPreflight := connectivityHelperPreflight()
	connectionReadiness := map[string]any{
		"ready":                         localRdevUsable || allAssetsReady,
		"local_rdev_usable":             localRdevUsable,
		"target_bootstrap_self_repair":  allAssetsReady,
		"gateway_url":                   gatewayURL,
		"gateway_url_candidates":        gatewayCandidates,
		"gateway_candidate_preflight":   gatewayCandidatePreflight(gatewayURL, target, gatewayCandidates),
		"connectivity_helper_preflight": helperPreflight,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:       "prepare",
			Status:      "not-created",
			Target:      target,
			GatewayURL:  gatewayURL,
			Candidates:  gatewayCandidates,
			RdevCommand: "rdev",
		}),
		"target":                        target,
		"human_gets_one_command":        true,
		"auto_approval_contract":        "attended-temporary first host only when created by support-session start/create",
		"requires_human_decision_first": []string{"company or owner authorization when the target is not clearly operator-owned"},
	}
	recoveryActions := []string{
		"prefer rdev support-session connect --start when no gateway is running; it creates the ticket, prints one target command, prepares helper assets, and watches through support-session status",
		"if local_rdev_usable is false, run go install ./cmd/rdev from a valid checkout or use go run ./cmd/rdev bootstrap agent-plan --repo-root . as a temporary planner",
		"if target_bootstrap_self_repair is false, rerun rdev support-session prepare --build-assets from a valid checkout before giving commands to targets that may not have rdev installed",
		"do not write custom PowerShell, relay, approval polling, ticket substitution, or bootstrap glue",
	}
	return map[string]any{
		"schema_version":         PrepareSchemaVersion,
		"ok":                     true,
		"repo_root":              repoRoot,
		"repo_root_valid":        repoValid,
		"work_dir":               workDir,
		"bin_dir":                binDir,
		"addr":                   addr,
		"gateway_url":            gatewayURL,
		"gateway_url_candidates": gatewayCandidates,
		"detected": map[string]any{
			"os":                 runtime.GOOS,
			"arch":               runtime.GOARCH,
			"go_path":            goPath,
			"git_path":           gitPath,
			"rdev_path":          rdevPath,
			"current_executable": currentExecutable,
			"host_capabilities":  hostcap.Detect(ctx),
		},
		"asset_report": map[string]any{
			"schema_version": "rdev.support-session-assets.v1",
			"build_assets":   opts.BuildAssets,
			"all_ready":      allAssetsReady,
			"assets":         assetReports,
		},
		"connectivity_strategy":         connectivityStrategy(gatewayURL, target, gatewayCandidates),
		"gateway_candidate_preflight":   gatewayCandidatePreflight(gatewayURL, target, gatewayCandidates),
		"connectivity_helper_preflight": helperPreflight,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:       "prepare",
			Status:      "not-created",
			Target:      target,
			GatewayURL:  gatewayURL,
			Candidates:  gatewayCandidates,
			RdevCommand: "rdev",
		}),
		"connection_readiness":     connectionReadiness,
		"missing_inputs":           missingInputs,
		"standard_recovery":        recoveryActions,
		"target_handoff_policy":    "give the target-side human only the generated target_command or join_url",
		"forbidden":                []string{"ExecutionPolicy Bypass", "hidden install", "manual ticket/root/gateway/transport assembly", "ad hoc bootstrap code"},
		"recommended_next_step":    recommendedSupportSessionNextStep(localRdevUsable, allAssetsReady),
		"command_to_connect_start": []string{"rdev", "support-session", "connect", "--start", "--addr", addr, "--gateway-url", gatewayURL, "--target", target},
		"command_to_start":         []string{"rdev", "support-session", "start", "--addr", addr, "--gateway-url", gatewayURL, "--target", target},
		"command_to_prepare_all":   []string{"rdev", "support-session", "prepare", "--build-assets", "--addr", addr, "--gateway-url", gatewayURL, "--target", target},
	}, nil
}

func connectivityStrategy(gatewayURL, target string, gatewayCandidates []GatewayURLCandidate) map[string]any {
	return map[string]any{
		"schema_version":         "rdev.support-session-connectivity-strategy.v1",
		"intent":                 "adaptive-connectivity-with-automatic-native-fallback-and-configured-helper-escalation",
		"gateway_url":            gatewayURL,
		"gateway_url_candidates": gatewayCandidates,
		"target":                 target,
		"selection_order": []string{
			"same-machine-local-mcp",
			"native-direct-gateway",
			"native-lan-gateway",
			"proxy-aware-https",
			"wss-then-long-poll-then-poll",
			"existing-ssh-tunnel",
			"existing-frp-or-chisel-relay",
			"existing-headscale-tailscale-mesh",
			"existing-wireguard-vpn",
			"operator-provided-hosted-gateway",
		},
		"automatic_downgrade": []string{
			"if WSS is blocked, rdev host serve --transport auto falls back to HTTPS long-poll",
			"if long-poll is blocked or unstable, rdev host serve --transport auto falls back to short polling",
			"after registration, rdev host serve --transport auto can switch to another signed join-manifest gateway candidate if the current gateway fails before processing jobs",
			"if a direct gateway health check fails, try LAN/private gateway candidates and configured proxy variables before helper paths",
			"if a configured helper path fails, report manual_action_required instead of guessing credentials or mutating network policy",
		},
		"automatic_upgrade": []string{
			"prefer LAN/private gateway when probes show both sides are routed locally",
			"prefer WSS/mTLS-capable gateway when available for lower latency",
			"for owned recurring machines, use a reviewed managed Connection Entry only after explicit persistence approval",
			"after reconnect evidence is available, update runtime memory with the best known stable path for this host",
		},
		"read_only_probes": []string{
			"detect OS and architecture",
			"detect rdev availability",
			"detect proxy environment variable names without logging secret values",
			"probe gateway /healthz and signed manifest reachability",
			"detect ssh, frpc, chisel, tailscale, headscale, and wg/wireguard tools",
			"inspect route/LAN hints only within the operator-approved scope",
		},
		"auto_use_when_configured": []string{
			"HTTP(S) proxy environment variables",
			"existing SSH tunnel start argv in RDEV_SSH_TUNNEL_START_ARGV_JSON",
			"existing relay start argv in RDEV_RELAY_START_ARGV_JSON",
			"existing mesh gateway URL in RDEV_MESH_GATEWAY_URL",
			"existing VPN gateway URL in RDEV_VPN_GATEWAY_URL",
		},
		"approval_required_for": []string{
			"installing tunnel, mesh, VPN, driver, service, or firewall components",
			"creating or editing SSH keys/config",
			"opening ports or changing router/NAT/firewall/DNS/routes",
			"using paid hosted relay or cloud resources",
			"turning a temporary third-party session into managed persistence",
		},
		"agent_rule": "use Connection Entry runner metadata for relay/mesh/VPN/SSH execution; never ask the target-side human to assemble low-level flags",
	}
}

func gatewayCandidatePreflight(gatewayURL, target string, gatewayCandidates []GatewayURLCandidate) map[string]any {
	candidates := make([]map[string]any, 0, len(gatewayCandidates))
	for i, candidate := range gatewayCandidates {
		candidates = append(candidates, gatewayCandidatePreflightReport(i+1, candidate))
	}
	return map[string]any{
		"schema_version":  GatewayCandidatePreflightSchemaVersion,
		"intent":          "agent-readable-gateway-candidate-decision-table",
		"gateway_url":     strings.TrimRight(strings.TrimSpace(gatewayURL), "/"),
		"target":          normalizeTarget(target),
		"preflight_mode":  "local-classification-no-network-scan",
		"candidate_count": len(candidates),
		"candidates":      candidates,
		"standard_sequence": []string{
			"call rdev.support_session.connect or run rdev support-session connect first",
			"send only returned user_handoff.message plus user_handoff.copy_paste",
			"wait with returned connection_supervision or foreground_feedback",
			"if all candidates fail, rerun rdev.support_session.prepare or rdev support-session prepare --build-assets before asking the human",
		},
		"ask_human_only_for": []string{
			"authorization or company policy",
			"persistent managed-host approval",
			"privileged firewall, DNS, route, service, driver, or credential changes",
			"real hosted/relay/mesh/VPN/SSH endpoint credentials when none are configured",
		},
		"agent_rule":         "use this candidate table before asking humans or writing probes; the target command owns ordered URL fallback and status/recovery owns waiting",
		"human_surface_rule": "do not expose this table to target users; humans receive only user_handoff.message plus user_handoff.copy_paste",
		"forbidden": []string{
			"Agent-authored PowerShell or shell network probes",
			"custom relay, mesh, VPN, SSH, or polling scripts",
			"manual ticket/root/gateway/transport/checksum assembly",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
		},
	}
}

func gatewayCandidatePreflightReport(order int, candidate GatewayURLCandidate) map[string]any {
	kind := strings.TrimSpace(candidate.Kind)
	scope := strings.TrimSpace(candidate.Scope)
	sameMachineOnly := kind == "loopback" || scope == "same-machine-only" || isLoopbackHost(candidate.Host)
	stableFallback := isStableGatewayCandidateKind(kind)
	status := "candidate-unverified"
	nextAction := "use the returned target command and status watcher; if it times out, follow connection_recovery"
	switch {
	case sameMachineOnly:
		status = "same-machine-only"
		nextAction = "use only for same-machine demos; for a remote target, prefer a LAN/private or configured hosted/relay/mesh/VPN/SSH candidate"
	case kind == "lan-private":
		status = "lan-candidate-unverified"
		nextAction = "try this as the first opportunistic LAN path; if status wait times out, configure a stable RDEV_*_GATEWAY_URL and create a fresh Connection Entry"
	case stableFallback:
		status = "configured-stable-fallback-unverified"
		nextAction = "use this configured fallback in the generated target command, then wait with connection_supervision"
	case kind == "explicit":
		status = "operator-provided-unverified"
		nextAction = "use when the operator or Agent runtime supplied this gateway; verify by standard status feedback, not custom scripts"
	}
	report := map[string]any{
		"order":                           order,
		"url":                             strings.TrimRight(strings.TrimSpace(candidate.URL), "/"),
		"kind":                            kind,
		"scope":                           scope,
		"source":                          candidate.Source,
		"recommended":                     candidate.Recommended,
		"reason":                          candidate.Reason,
		"host":                            candidate.Host,
		"port":                            candidate.Port,
		"health_url":                      strings.TrimRight(strings.TrimSpace(candidate.URL), "/") + "/healthz",
		"status":                          status,
		"probeable_from_agent":            !sameMachineOnly,
		"same_machine_only":               sameMachineOnly,
		"stable_fallback":                 stableFallback,
		"requires_operator_configuration": false,
		"standard_next_action":            nextAction,
	}
	if stableFallback {
		report["durability"] = "configured-fallback-for-lan-changes"
	} else if sameMachineOnly {
		report["durability"] = "same-machine-only"
	} else {
		report["durability"] = "opportunistic"
	}
	return report
}

func isStableGatewayCandidateKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "hosted", "relay", "mesh", "vpn", "ssh":
		return true
	default:
		return false
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

type agentConnectionRunbookOptions struct {
	Phase        string
	Status       string
	TicketCode   string
	Target       string
	Locale       string
	GatewayURL   string
	Candidates   []GatewayURLCandidate
	AutoApprove  bool
	RdevCommand  string
	NeedStartNow bool
	TimedOut     bool
}

func agentConnectionRunbook(opts agentConnectionRunbookOptions) map[string]any {
	phase := strings.TrimSpace(opts.Phase)
	if phase == "" {
		phase = "support-session"
	}
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = "waiting"
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	ticketCode := strings.TrimSpace(opts.TicketCode)
	sequence := []string{
		"start with rdev.support_session.connect or rdev support-session connect",
		"if ready_to_send_to_human=false, run cli_start_now_command exactly in a visible foreground terminal",
		"send only user_handoff.message plus user_handoff.copy_paste to the target-side human",
		"keep the target-side command/page and the local foreground gateway open while waiting",
		"watch with foreground_feedback, connection_supervision.mcp_watch_call, or rdev.support_session.status wait=true",
		"when connected=true, immediately report connected_next_steps.user_report before creating jobs",
		"inspect capabilities, create the smallest scoped job, then review evidence before declaring work complete",
	}
	if opts.NeedStartNow {
		sequence = append([]string{"run cli_start_now_command before sending anything to the target-side human"}, sequence...)
	}
	watch := map[string]any{
		"foreground_feedback_event": "connected",
		"mcp_tool":                  "rdev.support_session.status",
		"mcp_arguments": map[string]any{
			"ticket_code": ticketCode,
			"locale":      opts.Locale,
			"wait":        true,
		},
		"cli_command": []string{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait", "--locale", opts.Locale},
		"rule":        "use returned watcher fields when present; do not write a polling loop",
	}
	if ticketCode == "" {
		watch["mcp_arguments"] = map[string]any{"wait": true}
		watch["cli_command"] = []string{rdevCommand, "support-session", "status", "--ticket-code", "<ticket-code>", "--wait"}
	}
	return map[string]any{
		"schema_version": AgentConnectionRunbookSchemaVersion,
		"intent":         "fresh-agent-one-command-connect-operate-recover-runbook",
		"phase":          phase,
		"status":         status,
		"timed_out":      opts.TimedOut,
		"ticket_code":    ticketCode,
		"target":         normalizeTarget(opts.Target),
		"locale":         opts.Locale,
		"gateway_url":    strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/"),
		"standard_entry_tool": map[string]any{
			"mcp_tool":    "rdev.support_session.connect",
			"cli_command": []string{rdevCommand, "support-session", "connect"},
			"rule":        "for a human request like connect this computer, call this first; it chooses create-via-reachable-gateway or foreground start and returns the only human handoff to send",
		},
		"fallback_entry_tool": map[string]any{
			"mcp_tool":    "rdev.support_session.prepare",
			"cli_command": []string{rdevCommand, "support-session", "prepare", "--build-assets"},
			"rule":        "use only to repair missing local helper assets or inspect gateway candidates before creating a fresh support-session entry",
		},
		"fresh_agent_failure_prevention": freshAgentFailurePrevention(),
		"auto_approve": map[string]any{
			"enabled": opts.AutoApprove,
			"scope":   "standard attended-temporary first host only",
		},
		"gateway_candidate_summary": gatewayCandidateRunbookSummary(opts.Candidates),
		"sequence":                  sequence,
		"watch":                     watch,
		"on_connected": []string{
			"tell the user the connection has been established",
			"call rdev.hosts.capabilities for the connected host",
			"ask for task intent only if it is still unclear",
			"create the smallest scoped job allowed by capabilities and policy",
		},
		"on_timeout_or_failure": []string{
			"read connection_recovery and gateway_candidate_preflight from the returned payload",
			"rerun rdev.support_session.prepare or rdev support-session prepare --build-assets",
			"create a fresh Connection Entry with configured hosted/relay/mesh/VPN/SSH fallback when LAN-only paths are insufficient",
			"ask one short question only for authorization, persistence approval, privileged network changes, paid/cloud resources, credentials, or unclear ownership",
		},
		"human_surface_rule": "target-side humans receive only user_handoff.message plus user_handoff.copy_paste",
		"agent_rule":         "follow this runbook before choosing lower-level support-session tools; never make humans assemble low-level connection values",
		"low_level_entry_rule": map[string]any{
			"do_not_start_with": []string{
				"rdev.invites.create",
				"rdev invite create",
				"rdev.connection_entry.plan",
				"rdev connection-entry plan",
			},
			"reason": "low-level invite and package materialization surfaces are for reviewed packaging or advanced workflows; fresh Agents should use support_session.connect so helper assets, auto-approval, foreground feedback, and status watching are generated together",
			"allowed_when": []string{
				"an operator explicitly asks for package materialization",
				"managed owned-host service planning has been explicitly approved",
				"support_session.connect or support_session.prepare returns a standard recovery instruction that names Connection Entry planning",
			},
		},
		"forbidden": []string{
			"manual ticket/root/gateway/transport/checksum assembly",
			"Agent-authored PowerShell or shell bootstrap, relay, approval, or polling scripts",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
			"UAC or sudo bypass",
			"firewall, DNS, route, service, driver, credential, paid relay, or cloud changes without explicit approval",
		},
	}
}

func freshAgentFailurePrevention() map[string]any {
	return map[string]any{
		"schema_version": FreshAgentFailurePreventionSchemaVersion,
		"purpose":        "keep fresh Agents on the standardized connect/start/watch/recover path instead of recreating fragile gateway, invite, bootstrap, and approval glue",
		"known_failure_pattern": []string{
			"manual rdev gateway serve plus rdev invite create can omit verified helper assets and produce target bootstraps that fail with rdev is required",
			"background or ad hoc gateway process management can disappear before the target joins and leaves the Agent without a ready/status file",
			"hand-written Windows PowerShell or shell bootstrap code causes model-dependent behavior and can weaken the security contract",
			"asking humans to choose ticket, root key, gateway URL, transport, release root, checksum, or platform command creates avoidable multi-turn setup failures",
		},
		"required_standard_path": []string{
			"start ordinary connect-this-computer requests with rdev.support_session.connect or rdev support-session connect",
			"when ready_to_send_to_human=false, run cli_start_now_command exactly in a visible foreground terminal",
			"send only user_handoff.message plus user_handoff.copy_paste to the human",
			"watch foreground_feedback, status_file.path, connection_supervision, or rdev.support_session.status wait=true",
			"report connected_next_steps.user_report immediately when connected=true",
		},
		"standard_recovery": []string{
			"if rdev is missing, recover from the checkout with go install ./cmd/rdev or go run ./cmd/rdev bootstrap agent-plan --repo-root .",
			"if helper assets are missing, run rdev support-session connect --start or rdev support-session prepare --build-assets from a valid checkout",
			"if a LAN-only path times out or will not survive network changes, configure a standard hosted/relay/mesh/VPN/SSH gateway candidate and create a fresh Connection Entry",
			"ask one short question only for authorization, persistence approval, privileged network changes, paid/cloud resources, credentials, or unclear ownership",
		},
		"forbidden_agent_generated_workarounds": []string{
			"manual ticket/root/gateway/transport/checksum substitution for humans",
			"PowerShell or shell bootstrap/download scripts written by the Agent",
			"nohup/background gateway lifecycle glue written by the Agent",
			"custom relay, mesh, SSH, VPN, or polling scripts outside standard rdev tools",
			"ExecutionPolicy Bypass",
			"hidden install or persistence",
			"manual approval polling loops",
		},
		"agent_rule": "treat this as a regression guard: if tempted to write setup code, stop and use the standard rdev support-session connect/start/prepare/status contracts instead",
	}
}

func gatewayCandidateRunbookSummary(candidates []GatewayURLCandidate) map[string]any {
	kinds := []string{}
	hasStable := false
	hasLAN := false
	hasSameMachineOnly := len(candidates) == 0
	for _, candidate := range candidates {
		kind := strings.TrimSpace(candidate.Kind)
		if kind == "" {
			kind = "unknown"
		}
		kinds = append(kinds, kind)
		if kind == "lan-private" {
			hasLAN = true
			hasSameMachineOnly = false
		}
		if isStableGatewayCandidateKind(kind) {
			hasStable = true
			hasSameMachineOnly = false
		}
		if kind != "loopback" && candidate.Scope != "same-machine-only" {
			hasSameMachineOnly = false
		}
	}
	return map[string]any{
		"candidate_count":                 len(candidates),
		"candidate_kinds":                 dedupeStrings(kinds),
		"has_lan_candidate":               hasLAN,
		"has_stable_configured_fallback":  hasStable,
		"has_only_same_machine_candidate": hasSameMachineOnly,
		"rule":                            "if stable fallback is false, do not promise durable connectivity beyond the current direct/LAN path",
	}
}

func standardRecoveryActions(actions []string) []string {
	if len(actions) > 0 {
		return actions
	}
	return []string{
		"run rdev support-session prepare to inspect local rdev, Go, Git, repository, helper assets, gateway URL, and target command readiness",
		"if rdev is missing, build it from the checkout with go install ./cmd/rdev or go run ./cmd/rdev bootstrap agent-plan --repo-root .",
		"if helper assets are missing, rerun rdev support-session connect --start from a valid checkout so target bootstraps can download verified helpers",
		"ask one concise question only when authorization, persistence approval, privileged network changes, or a real gateway/relay credential is required",
	}
}

func ResolveGatewayURL(addr, explicitGatewayURL string) (string, []GatewayURLCandidate) {
	candidates := GatewayURLCandidatesFromIPs(addr, explicitGatewayURL, localInterfaceIPs())
	for _, candidate := range candidates {
		if candidate.Recommended {
			return candidate.URL, candidates
		}
	}
	if len(candidates) > 0 {
		return candidates[0].URL, candidates
	}
	return strings.TrimRight(strings.TrimSpace(explicitGatewayURL), "/"), candidates
}

func GatewayURLCandidatesFromIPs(addr, explicitGatewayURL string, ips []net.IP) []GatewayURLCandidate {
	return gatewayURLCandidatesFromIPsAndEnv(addr, explicitGatewayURL, ips, gatewayEnvCandidatesFromEnv())
}

func ConfiguredGatewayURLCandidate() (string, []GatewayURLCandidate) {
	candidates := appendConfiguredGatewayCandidates(nil, gatewayEnvCandidatesFromEnv())
	for _, candidate := range candidates {
		if candidate.Recommended {
			return candidate.URL, candidates
		}
	}
	for _, candidate := range candidates {
		return candidate.URL, candidates
	}
	return "", candidates
}

func gatewayURLCandidatesFromIPsAndEnv(addr, explicitGatewayURL string, ips []net.IP, envCandidates []GatewayEnvCandidate) []GatewayURLCandidate {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	host, port := splitListenAddress(addr)
	if port == "" {
		port = "8787"
	}
	explicit := strings.TrimRight(strings.TrimSpace(explicitGatewayURL), "/")
	candidates := make([]GatewayURLCandidate, 0, 2+len(ips))
	if explicit != "" {
		candidates = append(candidates, candidateFromURL(explicit, "explicit", "operator-provided", "argument:gateway_url", true, "explicit gateway_url supplied by the Agent/operator"))
	}
	if isWildcardHost(host) {
		for _, ip := range sortedPrivateIPs(ips) {
			url := "http://" + net.JoinHostPort(ip.String(), port)
			candidates = append(candidates, GatewayURLCandidate{
				URL:         url,
				Kind:        "lan-private",
				Scope:       "LAN/VPN/routed-private-network",
				Host:        ip.String(),
				Port:        port,
				Source:      "local-interface",
				Recommended: explicit == "" && !hasRecommendedGatewayCandidate(candidates),
				Reason:      "listen address is wildcard; this private interface address is more useful for target machines than 0.0.0.0",
			})
		}
		candidates = appendConfiguredGatewayCandidates(candidates, envCandidates)
		loopbackURL := "http://" + net.JoinHostPort("127.0.0.1", port)
		candidates = append(candidates, GatewayURLCandidate{
			URL:         loopbackURL,
			Kind:        "loopback",
			Scope:       "same-machine-only",
			Host:        "127.0.0.1",
			Port:        port,
			Source:      "listen-address",
			Recommended: explicit == "" && !hasRecommendedGatewayCandidate(candidates),
			Reason:      "fallback for same-machine testing only; remote target machines cannot use this URL",
		})
		return dedupeGatewayCandidates(candidates)
	}
	urlHost := host
	if urlHost == "" {
		urlHost = "127.0.0.1"
	}
	scope := "host-specific"
	kind := "host"
	reason := "listen address names a concrete host"
	if isLoopbackHost(urlHost) {
		scope = "same-machine-only"
		kind = "loopback"
		reason = "loopback is only suitable when the target is this same machine"
	}
	if kind == "loopback" {
		candidates = appendConfiguredGatewayCandidates(candidates, envCandidates)
	}
	candidates = append(candidates, GatewayURLCandidate{
		URL:         "http://" + net.JoinHostPort(urlHost, port),
		Kind:        kind,
		Scope:       scope,
		Host:        urlHost,
		Port:        port,
		Source:      "listen-address",
		Recommended: explicit == "",
		Reason:      reason,
	})
	return appendConfiguredGatewayCandidates(candidates, envCandidates)
}

func splitListenAddress(addr string) (string, string) {
	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		return strings.Trim(host, "[]"), port
	}
	if strings.HasPrefix(addr, ":") {
		return "", strings.TrimPrefix(addr, ":")
	}
	if strings.Count(addr, ":") == 0 {
		return addr, ""
	}
	return strings.Trim(addr, "[]"), ""
}

func candidateFromURL(rawURL, kind, scope, source string, recommended bool, reason string) GatewayURLCandidate {
	host := ""
	port := ""
	withoutScheme := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	if parsedURL, err := neturl.Parse(rawURL); err == nil && parsedURL.Host != "" {
		host = strings.Trim(parsedURL.Hostname(), "[]")
		port = parsedURL.Port()
	} else if parsedHost, parsedPort, err := net.SplitHostPort(withoutScheme); err == nil {
		host = strings.Trim(parsedHost, "[]")
		port = parsedPort
	}
	return GatewayURLCandidate{
		URL:         rawURL,
		Kind:        kind,
		Scope:       scope,
		Host:        host,
		Port:        port,
		Source:      source,
		Recommended: recommended,
		Reason:      reason,
	}
}

func gatewayEnvCandidatesFromEnv() []GatewayEnvCandidate {
	definitions := []GatewayEnvCandidate{
		{EnvVar: "RDEV_HOSTED_GATEWAY_URL", Kind: "hosted", Scope: "operator-provided-hosted-gateway", Reason: "configured hosted gateway URL"},
		{EnvVar: "RDEV_RELAY_GATEWAY_URL", Kind: "relay", Scope: "configured-relay", Reason: "configured relay gateway URL"},
		{EnvVar: "RDEV_MESH_GATEWAY_URL", Kind: "mesh", Scope: "configured-mesh", Reason: "configured mesh gateway URL"},
		{EnvVar: "RDEV_VPN_GATEWAY_URL", Kind: "vpn", Scope: "configured-vpn", Reason: "configured VPN gateway URL"},
		{EnvVar: "RDEV_SSH_GATEWAY_URL", Kind: "ssh", Scope: "configured-ssh-tunnel", Reason: "configured SSH tunnel gateway URL"},
	}
	out := make([]GatewayEnvCandidate, 0, len(definitions))
	for _, definition := range definitions {
		raw := strings.TrimRight(strings.TrimSpace(os.Getenv(definition.EnvVar)), "/")
		if raw == "" {
			continue
		}
		definition.URL = raw
		out = append(out, definition)
	}
	return out
}

func connectivityHelperPreflight() map[string]any {
	definitions := connectivityHelperDefinitions()
	helpers := make([]map[string]any, 0, len(definitions))
	configured := []string{}
	readyCount := 0
	for _, definition := range definitions {
		gatewayURL := strings.TrimRight(strings.TrimSpace(os.Getenv(definition.GatewayEnv)), "/")
		startArgvRaw := strings.TrimSpace(os.Getenv(definition.StartArgvEnv))
		installActionRaw := strings.TrimSpace(os.Getenv(definition.InstallActionEnv))
		report := map[string]any{
			"id":                 definition.ID,
			"kind":               definition.Kind,
			"gateway_env":        definition.GatewayEnv,
			"start_argv_env":     definition.StartArgvEnv,
			"install_action_env": definition.InstallActionEnv,
			"gateway_configured": gatewayURL != "",
			"start_configured":   startArgvRaw != "",
			"install_configured": installActionRaw != "",
			"allowed_tools":      definition.AllowedTools,
			"approval_required":  definition.ApprovalRequired,
			"status":             "not-configured",
		}
		if gatewayURL != "" {
			report["gateway_url"] = gatewayURL
			configured = append(configured, definition.ID)
		}
		if startArgvRaw != "" {
			argv, tool, err := parseHelperStartArgv(startArgvRaw, definition.StartArgvEnv, definition.AllowedTools)
			if err != nil {
				report["status"] = "invalid-start-argv"
				report["error"] = err.Error()
			} else {
				report["start_tool"] = tool
				report["start_argc"] = len(argv)
				report["start_argv_preview"] = safeArgvPreview(argv)
				if gatewayURL != "" {
					report["status"] = "ready-to-use-after-approval-check"
					readyCount++
				} else {
					report["status"] = "start-argv-without-gateway"
				}
			}
		}
		if installActionRaw != "" {
			actionReport := parseHelperInstallActionReport(installActionRaw, definition.InstallActionEnv, definition.AllowedTools)
			report["install_action"] = actionReport
			if actionReport["valid"] == false && report["status"] == "not-configured" {
				report["status"] = "invalid-install-action"
			}
		}
		helpers = append(helpers, report)
	}
	return map[string]any{
		"schema_version":        ConnectivityHelperPreflightSchemaVersion,
		"intent":                "standard-read-only-helper-configuration-preflight-for-adaptive-connectivity",
		"helpers":               helpers,
		"configured_helper_ids": configured,
		"ready_helper_count":    readyCount,
		"auto_execute":          false,
		"standard_next_step":    "use rdev.connection_entry.plan plus rdev connection-entry run --dry-run when helper execution is needed; do not write custom SSH, relay, mesh, VPN, shell, or PowerShell startup code",
		"agent_rule":            "read this before asking network questions or writing tunnel commands; if a helper is configured, use standard Connection Entry runner metadata and approval boundaries",
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"encoded shell commands",
			"shell command-string wrappers",
			"printing credentials or private keys",
			"installing services, drivers, firewall, DNS, route, cloud, or paid resources without explicit approval",
		},
	}
}

func connectivityHelperDefinitions() []connectivityHelperDefinition {
	return []connectivityHelperDefinition{
		{
			ID:               "existing-ssh-tunnel",
			Kind:             "ssh",
			GatewayEnv:       "RDEV_SSH_GATEWAY_URL",
			StartArgvEnv:     "RDEV_SSH_TUNNEL_START_ARGV_JSON",
			InstallActionEnv: "RDEV_SSH_INSTALL_ACTION_JSON",
			AllowedTools:     []string{"ssh"},
			ApprovalRequired: []string{"new keys", "config edits", "ambiguous identities", "privileged ports"},
		},
		{
			ID:               "existing-frp-or-chisel-relay",
			Kind:             "relay",
			GatewayEnv:       "RDEV_RELAY_GATEWAY_URL",
			StartArgvEnv:     "RDEV_RELAY_START_ARGV_JSON",
			InstallActionEnv: "RDEV_RELAY_INSTALL_ACTION_JSON",
			AllowedTools:     []string{"frpc", "chisel"},
			ApprovalRequired: []string{"download/install", "relay credential creation", "public port changes", "paid relay", "persistent service"},
		},
		{
			ID:               "existing-headscale-tailscale-mesh",
			Kind:             "mesh",
			GatewayEnv:       "RDEV_MESH_GATEWAY_URL",
			StartArgvEnv:     "RDEV_MESH_START_ARGV_JSON",
			InstallActionEnv: "RDEV_MESH_INSTALL_ACTION_JSON",
			AllowedTools:     []string{"tailscale"},
			ApprovalRequired: []string{"new enrollment", "auth key use", "ACL changes", "DNS changes", "service changes"},
		},
		{
			ID:               "existing-wireguard-vpn",
			Kind:             "vpn",
			GatewayEnv:       "RDEV_VPN_GATEWAY_URL",
			StartArgvEnv:     "RDEV_VPN_START_ARGV_JSON",
			InstallActionEnv: "RDEV_VPN_INSTALL_ACTION_JSON",
			AllowedTools:     []string{"wg", "wg-quick"},
			ApprovalRequired: []string{"key creation", "profile import", "route/DNS/firewall mutation", "persistent tunnel start"},
		},
	}
}

func parseHelperStartArgv(raw, envName string, allowedTools []string) ([]string, string, error) {
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return nil, "", fmt.Errorf("%s must be a JSON argv array: %w", envName, err)
	}
	if len(argv) == 0 {
		return nil, "", fmt.Errorf("%s must contain at least one argv item", envName)
	}
	for i, arg := range argv {
		if strings.TrimSpace(arg) == "" {
			return nil, "", fmt.Errorf("%s item %d must not be empty", envName, i)
		}
	}
	if err := rejectUnsafeArgv(argv, envName); err != nil {
		return nil, "", err
	}
	tool := executableBaseName(argv[0])
	if !helperToolAllowed(tool, allowedTools) {
		return nil, "", fmt.Errorf("%s starts %q, but only allows: %s", envName, tool, strings.Join(allowedTools, ", "))
	}
	return argv, tool, nil
}

func parseHelperInstallActionReport(raw, envName string, allowedTools []string) map[string]any {
	var action struct {
		SchemaVersion     string   `json:"schema_version"`
		Tool              string   `json:"tool"`
		Argv              []string `json:"argv"`
		Scope             string   `json:"scope"`
		Reason            string   `json:"reason"`
		ExpectedSHA256    string   `json:"expected_sha256"`
		RequiresElevation bool     `json:"requires_elevation"`
	}
	report := map[string]any{"valid": false, "env": envName}
	if err := json.Unmarshal([]byte(raw), &action); err != nil {
		report["error"] = fmt.Sprintf("%s must be a JSON dependency install action: %v", envName, err)
		return report
	}
	tool := executableBaseName(action.Tool)
	report["tool"] = tool
	report["scope"] = action.Scope
	report["reason"] = action.Reason
	report["has_expected_sha256"] = strings.TrimSpace(action.ExpectedSHA256) != ""
	report["requires_elevation"] = action.RequiresElevation
	if tool == "" {
		report["error"] = envName + " must include a non-empty tool"
		return report
	}
	if !helperToolAllowed(tool, allowedTools) {
		report["error"] = fmt.Sprintf("%s installs %q, but only allows: %s", envName, tool, strings.Join(allowedTools, ", "))
		return report
	}
	if len(action.Argv) == 0 {
		report["error"] = envName + " must include a non-empty argv array"
		return report
	}
	if err := rejectUnsafeArgv(action.Argv, envName); err != nil {
		report["error"] = err.Error()
		return report
	}
	if action.RequiresElevation {
		report["error"] = envName + " requires elevation; use a reviewed managed package/service plan instead"
		return report
	}
	scope := strings.TrimSpace(action.Scope)
	if scope == "" {
		scope = "user"
	}
	switch scope {
	case "user", "workspace", "attended-visible", "managed-approved":
	default:
		report["error"] = fmt.Sprintf("%s has unsupported install scope %q", envName, scope)
		return report
	}
	if strings.TrimSpace(action.ExpectedSHA256) != "" && !isHexSHA256(action.ExpectedSHA256) {
		report["error"] = envName + " expected_sha256 must be a hex SHA-256 value"
		return report
	}
	report["valid"] = true
	report["argc"] = len(action.Argv)
	report["argv_preview"] = safeArgvPreview(action.Argv)
	return report
}

func safeArgvPreview(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, 0, len(argv))
	for i, arg := range argv {
		if i == 0 {
			out = append(out, executableBaseName(arg))
			continue
		}
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") || strings.Contains(lower, "password") {
			out = append(out, "<redacted>")
			continue
		}
		out = append(out, arg)
	}
	if len(out) > 8 {
		return append(out[:8], "...")
	}
	return out
}

func executableBaseName(value string) string {
	normalized := strings.ReplaceAll(value, "\\", "/")
	parts := strings.Split(normalized, "/")
	base := strings.TrimSpace(parts[len(parts)-1])
	return strings.TrimSuffix(strings.ToLower(base), ".exe")
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

func rejectUnsafeArgv(argv []string, label string) error {
	if len(argv) == 0 {
		return nil
	}
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
	switch tool {
	case "sh", "bash", "zsh", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func isHexSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func appendConfiguredGatewayCandidates(candidates []GatewayURLCandidate, envCandidates []GatewayEnvCandidate) []GatewayURLCandidate {
	for _, envCandidate := range envCandidates {
		url := strings.TrimRight(strings.TrimSpace(envCandidate.URL), "/")
		if url == "" {
			continue
		}
		recommended := !hasRecommendedGatewayCandidate(candidates)
		candidates = append(candidates, candidateFromURL(
			url,
			envCandidate.Kind,
			envCandidate.Scope,
			"env:"+envCandidate.EnvVar,
			recommended,
			envCandidate.Reason+" from "+envCandidate.EnvVar,
		))
	}
	return dedupeGatewayCandidates(candidates)
}

func localInterfaceIPs() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			out = append(out, normalizeIP(ip))
		}
	}
	return out
}

func sortedPrivateIPs(ips []net.IP) []net.IP {
	values := make([]net.IP, 0, len(ips))
	seen := map[string]bool{}
	for _, ip := range ips {
		ip = normalizeIP(ip)
		if ip == nil || !ip.IsPrivate() {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		values = append(values, ip)
	}
	sort.Slice(values, func(i, j int) bool {
		left4 := values[i].To4() != nil
		right4 := values[j].To4() != nil
		if left4 != right4 {
			return left4
		}
		return values[i].String() < values[j].String()
	})
	return values
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func isWildcardHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func hasRecommendedGatewayCandidate(candidates []GatewayURLCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Recommended {
			return true
		}
	}
	return false
}

func dedupeGatewayCandidates(candidates []GatewayURLCandidate) []GatewayURLCandidate {
	out := make([]GatewayURLCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.URL) == "" || seen[candidate.URL] {
			continue
		}
		seen[candidate.URL] = true
		out = append(out, candidate)
	}
	return out
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func BuildCreated(opts CreatedOptions) map[string]any {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	joinURL := strings.TrimSpace(opts.JoinURL)
	if joinURL == "" && gatewayURL != "" && opts.Ticket.Code != "" {
		joinURL = gatewayURL + "/join/" + opts.Ticket.Code
	}
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	if manifestURL == "" && gatewayURL != "" && opts.Ticket.Code != "" {
		manifestURL = gatewayURL + "/v1/tickets/" + opts.Ticket.Code + "/manifest"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	target := normalizeTarget(opts.Target)
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	gatewayCandidates := normalizeCreatedGatewayCandidates(gatewayURL, opts.GatewayURLCandidates)
	joinURLs := joinURLsForGatewayCandidates(gatewayCandidates, opts.Ticket.Code)
	if len(joinURLs) == 0 && joinURL != "" {
		joinURLs = []string{joinURL}
	}
	attemptPolicy := connectionAttemptPolicy(gatewayCandidates, joinURLs)
	continuityPolicy := connectionContinuityPolicy(gatewayCandidates)
	helperPreflight := connectivityHelperPreflight()
	runtimeGatewayCandidates := runtimeGatewayCandidates(gatewayCandidates)
	windowsCommand := windowsBootstrapCommand(joinURLs, runtimeGatewayCandidates)
	macLinuxCommand := macLinuxBootstrapCommand(joinURLs, runtimeGatewayCandidates)
	bootstrapRequirements := bootstrapRequirements(target)
	targetCommands := map[string]string{
		"windows":     windowsCommand,
		"macos_linux": macLinuxCommand,
		"join_url":    joinURL,
	}
	recommended := joinURL
	recommendedSurface := "join_url"
	switch target {
	case "windows":
		recommended = windowsCommand
		recommendedSurface = "windows"
	case "macos", "linux":
		recommended = macLinuxCommand
		recommendedSurface = "macos_linux"
	}
	statusCommand := []string{
		rdevCommand, "support-session", "status",
		"--gateway-url", gatewayURL,
		"--ticket-code", opts.Ticket.Code,
		"--wait",
		"--locale", locale,
	}
	configuredGatewayStatusCommand := []string{
		rdevCommand, "support-session", "status",
		"--ticket-code", opts.Ticket.Code,
		"--wait",
		"--locale", locale,
	}
	return map[string]any{
		"schema_version":                CreatedSchemaVersion,
		"ok":                            true,
		"session_mode":                  string(model.HostModeAttendedTemporary),
		"intent":                        "agent-created-one-command-visible-support-session",
		"gateway_url":                   gatewayURL,
		"gateway_url_candidates":        gatewayCandidates,
		"ticket_code":                   opts.Ticket.Code,
		"ticket":                        opts.Ticket,
		"join_url":                      joinURL,
		"manifest_url":                  manifestURL,
		"manifest_root_public_key":      opts.ManifestRootPublicKey,
		"target":                        target,
		"locale":                        locale,
		"auto_approve":                  opts.AutoApprove,
		"recommended_surface":           recommendedSurface,
		"target_command":                recommended,
		"target_commands":               targetCommands,
		"user_handoff":                  userHandoff(locale, target, recommendedSurface, recommended, joinURL, targetCommands),
		"connection_attempt_policy":     attemptPolicy,
		"connection_continuity_policy":  continuityPolicy,
		"connection_supervision":        connectionSupervision(opts.Ticket.Code, locale, rdevCommand, attemptPolicy, continuityPolicy),
		"gateway_candidate_preflight":   gatewayCandidatePreflight(gatewayURL, target, gatewayCandidates),
		"connectivity_helper_preflight": helperPreflight,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:       "created",
			Status:      "waiting",
			TicketCode:  opts.Ticket.Code,
			Target:      target,
			Locale:      locale,
			GatewayURL:  gatewayURL,
			Candidates:  gatewayCandidates,
			AutoApprove: opts.AutoApprove,
			RdevCommand: rdevCommand,
		}),
		"target_bootstrap_requirements": bootstrapRequirements,
		"target_bootstrap_readiness":    opts.TargetBootstrapReadiness,
		"watch_connection_status":       statusCommand,
		"watch_connection_status_configured_gateway": map[string]any{
			"command": configuredGatewayStatusCommand,
			"requires_configured_gateway_env": []string{
				"RDEV_HOSTED_GATEWAY_URL",
				"RDEV_RELAY_GATEWAY_URL",
				"RDEV_MESH_GATEWAY_URL",
				"RDEV_VPN_GATEWAY_URL",
				"RDEV_SSH_GATEWAY_URL",
			},
			"agent_rule": "use this shorter watcher when one RDEV_*_GATEWAY_URL is configured; otherwise use watch_connection_status",
		},
		"mcp_follow_up": []map[string]any{
			{
				"tool": "rdev.support_session.status",
				"arguments": map[string]any{
					"ticket_code": opts.Ticket.Code,
					"locale":      locale,
					"wait":        true,
				},
			},
		},
		"human_message": localizedCreatedMessage(locale),
		"agent_flow": []string{
			"give the target-side human only target_command or join_url",
			"target_command already tries ordered gateway URL candidates with bounded per-candidate timeouts and retry policy; do not write your own fallback script",
			"read connection_continuity_policy to decide whether this session survives LAN changes or needs a configured hosted/relay/mesh/VPN/SSH path",
			"if the gateway was not started by rdev support-session start, verify target_bootstrap_requirements before sending a Windows/macOS/Linux command",
			"watch connection status with watch_connection_status or rdev.support_session.status",
			"when connected=true, proactively report that the connection is established",
			"do not ask the human to assemble ticket, gateway, manifest root, transport, or helper flags",
		},
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"hidden install",
			"manual ticket/root/gateway/transport assembly for target user",
			"ad hoc bootstrap script generated by the Agent",
		},
	}
}

func userHandoff(locale, target, surface, copyPaste, joinURL string, targetCommands map[string]string) map[string]any {
	return map[string]any{
		"schema_version":      UserHandoffSchemaVersion,
		"locale":              locale,
		"target":              target,
		"copy_paste_kind":     surface,
		"copy_paste":          copyPaste,
		"join_url":            joinURL,
		"message":             localizedUserHandoffMessage(locale, surface),
		"windows_command":     targetCommands["windows"],
		"macos_linux_command": targetCommands["macos_linux"],
		"auto_target_rule":    autoTargetHandoffRule(target),
		"agent_next_step":     "send message plus copy_paste to the user, then call rdev.support_session.status with wait=true",
		"agent_rule":          "do not rewrite copy_paste, do not ask the user to assemble ticket/root/gateway/transport, and do not add custom polling",
	}
}

func autoTargetHandoffRule(target string) string {
	if target != "auto" {
		return "target platform is selected; send copy_paste verbatim"
	}
	return "target platform is unknown; send the join_url copy_paste first because the join page selects OS-specific visible commands, and use windows_command or macos_linux_command only if the human asks for a terminal command or cannot open the page"
}

func normalizeCreatedGatewayCandidates(gatewayURL string, candidates []GatewayURLCandidate) []GatewayURLCandidate {
	values := make([]GatewayURLCandidate, 0, len(candidates)+1)
	for _, candidate := range candidates {
		candidate.URL = strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if candidate.URL == "" {
			continue
		}
		values = append(values, candidate)
	}
	if gatewayURL != "" && len(values) == 0 {
		values = append(values, candidateFromURL(gatewayURL, "explicit", "operator-provided", "created:generation", true, "created support session gateway URL"))
	}
	if !hasRecommendedGatewayCandidate(values) && len(values) > 0 {
		values[0].Recommended = true
	}
	return dedupeGatewayCandidates(values)
}

func joinURLsForGatewayCandidates(candidates []GatewayURLCandidate, ticketCode string) []string {
	if strings.TrimSpace(ticketCode) == "" {
		return nil
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		base := strings.TrimRight(strings.TrimSpace(candidate.URL), "/")
		if base == "" {
			continue
		}
		values = append(values, base+"/join/"+ticketCode)
	}
	return dedupeStrings(values)
}

func connectionAttemptPolicy(candidates []GatewayURLCandidate, joinURLs []string) map[string]any {
	candidateReports := make([]map[string]any, 0, len(joinURLs))
	for i, joinURL := range joinURLs {
		report := map[string]any{
			"order":    i + 1,
			"join_url": joinURL,
		}
		if i < len(candidates) {
			report["gateway_url"] = candidates[i].URL
			report["kind"] = candidates[i].Kind
			report["scope"] = candidates[i].Scope
			report["recommended"] = candidates[i].Recommended
		}
		candidateReports = append(candidateReports, report)
	}
	return map[string]any{
		"schema_version":               ConnectionAttemptPolicySchemaVersion,
		"candidate_order":              candidateReports,
		"windows_download_timeout_sec": targetHTTPMaxTimeSeconds,
		"curl_connect_timeout_sec":     targetHTTPConnectTimeoutSeconds,
		"curl_max_time_sec":            targetHTTPMaxTimeSeconds,
		"retries_per_candidate":        targetHTTPRetries,
		"retry_delay_sec":              targetHTTPRetryDelaySeconds,
		"target_handoff":               "single visible command; target-side command owns URL fallback, timeout, retry, download, verification, and host startup",
		"agent_rule":                   "do not rewrite, wrap, or expand target_command; watch the returned status tool/command instead",
	}
}

func connectionContinuityPolicy(candidates []GatewayURLCandidate) map[string]any {
	kinds := make([]string, 0, len(candidates))
	stableKinds := []string{}
	hasLAN := false
	hasLoopbackOnly := len(candidates) == 0
	for _, candidate := range candidates {
		kind := strings.TrimSpace(candidate.Kind)
		if kind == "" {
			kind = "unknown"
		}
		kinds = append(kinds, kind)
		switch kind {
		case "lan-private":
			hasLAN = true
			hasLoopbackOnly = false
		case "hosted", "relay", "mesh", "vpn", "ssh":
			stableKinds = append(stableKinds, kind)
			hasLoopbackOnly = false
		case "explicit", "host":
			if candidate.Scope != "same-machine-only" {
				hasLoopbackOnly = false
			}
		case "loopback":
		default:
			hasLoopbackOnly = false
		}
	}
	stableAfterLANChange := len(stableKinds) > 0
	assessment := "lan-or-explicit-path"
	if stableAfterLANChange {
		assessment = "stable-fallback-configured"
	} else if hasLAN {
		assessment = "lan-dependent"
	} else if hasLoopbackOnly {
		assessment = "same-machine-only"
	}
	return map[string]any{
		"schema_version":                 ContinuityPolicySchemaVersion,
		"intent":                         "agent-readable-continuity-and-reconnect-guidance",
		"connection_model":               "target-initiated-outbound-connection-entry-with-ordered-candidate-fallback",
		"candidate_kinds":                dedupeStrings(kinds),
		"stable_fallback_kinds":          dedupeStrings(stableKinds),
		"has_lan_candidate":              hasLAN,
		"has_stable_configured_fallback": stableAfterLANChange,
		"stable_after_lan_change":        stableAfterLANChange,
		"assessment":                     assessment,
		"target_command_behavior":        "the returned target command tries candidate URLs in order with bounded timeouts before failing",
		"agent_watch_behavior":           "after handing over user_handoff.copy_paste, use rdev.support_session.status wait=true or the returned watcher command and proactively report connected=true",
		"automatic_downgrade":            []string{"target command tries the next Connection Entry URL when direct or LAN bootstrap fails", "host transport auto may downgrade WSS to HTTPS long-poll to short polling", "after registration, host transport auto may switch to another signed join-manifest gateway candidate when the current gateway fails before jobs are processed", "status wait timeout returns connection_recovery instead of requiring custom polling"},
		"automatic_upgrade":              []string{"when an RDEV_HOSTED_GATEWAY_URL, RDEV_RELAY_GATEWAY_URL, RDEV_MESH_GATEWAY_URL, RDEV_VPN_GATEWAY_URL, or RDEV_SSH_GATEWAY_URL becomes configured, create a fresh Connection Entry so future target commands include that stable path", "for operator-owned recurring machines, move from attended-temporary to a reviewed managed Connection Entry only after explicit persistence approval", "persist the best verified stable path in scoped runtime memory after evidence is reviewed"},
		"if_lan_or_loopback_fails":       []string{"run rdev.support_session.prepare or rdev support-session prepare --build-assets to refresh gateway_url_candidates", "prefer configured hosted/relay/mesh/VPN/SSH gateway URLs before asking the human for network details", "ask only when privileged network changes, credentials, paid/cloud resources, or managed persistence are required"},
		"requires_operator_approval_for": []string{"opening ports, router/NAT/firewall/DNS/route changes", "installing tunnel, mesh, VPN, service, driver, or persistent helper components", "creating or editing SSH credentials/config", "using paid hosted relay or cloud resources", "turning a temporary third-party session into managed persistence"},
		"forbidden":                      []string{"Agent-authored polling loops", "custom PowerShell or shell relay/bootstrap scripts", "asking humans to assemble ticket/root/gateway/transport/checksum values", "ExecutionPolicy Bypass", "hidden install or persistence"},
		"agent_rule":                     "treat LAN as an opportunistic first path, not the reliability plan; when stable_after_lan_change is false, prefer a configured hosted/relay/mesh/VPN/SSH gateway for durable work before claiming robust connectivity",
	}
}

func connectionSupervision(ticketCode, locale, rdevCommand string, attemptPolicy, continuityPolicy map[string]any) map[string]any {
	ticketCode = strings.TrimSpace(ticketCode)
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = "auto"
	}
	rdevCommand = strings.TrimSpace(rdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	stableAfterLANChange, _ := continuityPolicy["stable_after_lan_change"].(bool)
	assessment, _ := continuityPolicy["assessment"].(string)
	candidateOrder, _ := attemptPolicy["candidate_order"]
	upgradeRecommended := !stableAfterLANChange
	upgradeReason := "stable hosted/relay/mesh/VPN/SSH fallback already configured"
	if upgradeRecommended {
		upgradeReason = "current Connection Entry has no configured stable fallback beyond direct/LAN/explicit candidates"
	}
	return map[string]any{
		"schema_version": ConnectionSupervisionSchemaVersion,
		"intent":         "agent-side-watch-report-and-standard-upgrade-contract",
		"ticket_code":    ticketCode,
		"locale":         locale,
		"mcp_watch_call": map[string]any{
			"tool": "rdev.support_session.status",
			"arguments": map[string]any{
				"ticket_code": ticketCode,
				"locale":      locale,
				"wait":        true,
			},
		},
		"cli_watch_command":     []string{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait", "--locale", locale},
		"connected_report_rule": "when status.connected=true, immediately send connected_next_steps.user_report to the user before creating jobs",
		"pending_approval_rule": "if status=pending-approval, approve only the expected host or wait for scoped attended-temporary auto-approval; do not ask the target human for ticket/root/gateway/transport",
		"timeout_recovery_tools": []map[string]any{
			{"tool": "rdev.support_session.status", "arguments": map[string]any{"ticket_code": ticketCode, "locale": locale, "wait": true}},
			{"tool": "rdev.support_session.prepare", "arguments": map[string]any{"target": "auto", "build_assets": true}},
		},
		"timeout_recovery_commands": [][]string{
			{rdevCommand, "support-session", "status", "--ticket-code", ticketCode, "--wait", "--locale", locale},
			{rdevCommand, "support-session", "prepare", "--target", "auto", "--build-assets"},
		},
		"candidate_order":                candidateOrder,
		"continuity_assessment":          assessment,
		"stable_after_lan_change":        stableAfterLANChange,
		"upgrade_recommended":            upgradeRecommended,
		"upgrade_reason":                 upgradeReason,
		"standard_upgrade_paths":         []string{"configure RDEV_HOSTED_GATEWAY_URL, RDEV_RELAY_GATEWAY_URL, RDEV_MESH_GATEWAY_URL, RDEV_VPN_GATEWAY_URL, or RDEV_SSH_GATEWAY_URL, then create a fresh Connection Entry", "for operator-owned recurring machines, materialize a reviewed managed Connection Entry package after explicit persistence approval", "use rdev.connection_entry.plan plus rdev connection-entry run --dry-run for package/runner-based path selection"},
		"automatic_downgrade_boundaries": []string{"target command owns ordered URL fallback and bounded timeouts", "host transport auto may downgrade WSS to HTTPS long-poll to short polling", "host runtime may reuse signed join-manifest gateway candidates after registration if the current gateway fails before processing jobs", "status timeout returns connection_recovery for standard recovery"},
		"requires_operator_approval_for": continuityPolicy["requires_operator_approval_for"],
		"agent_rule":                     "after sending user_handoff, use this supervision contract to wait, report connected=true, and choose standard upgrade/recovery tools; do not write polling, relay, bootstrap, or network mutation scripts",
		"human_surface_rule":             "humans receive only user_handoff.message plus user_handoff.copy_paste; supervision fields are for the Agent runtime",
		"forbidden":                      []string{"custom polling loops", "Agent-authored PowerShell or shell relay/bootstrap scripts", "manual ticket/root/gateway/transport assembly", "ExecutionPolicy Bypass", "hidden install or persistence"},
	}
}

func bootstrapRequirements(target string) map[string]any {
	requirements := map[string]any{
		"schema_version": "rdev.support-session-target-bootstrap-requirements.v1",
		"target":         target,
		"agent_rule":     "support-session connect --start prepares these helper assets automatically; if using an existing gateway, verify the relevant /assets endpoints before sending a platform command",
		"standard_fix":   []string{"rdev support-session prepare --build-assets", "rdev support-session connect --start"},
		"forbidden":      []string{"telling the target user to install rdev manually as the first recovery step", "writing an ad hoc bootstrap downloader", "using ExecutionPolicy Bypass"},
	}
	switch target {
	case "windows":
		requirements["required_assets"] = []string{"rdev-windows-amd64.exe", "rdev-windows-amd64.exe.sha256"}
		requirements["verification"] = []string{"GET /assets/rdev-windows-amd64.exe.sha256 must return 200", "GET /assets/rdev-windows-amd64.exe must return 200"}
	case "macos", "linux":
		requirements["required_assets"] = []string{"matching rdev-<os>-<arch> helper", "matching .sha256"}
		requirements["verification"] = []string{"GET /assets/<helper>.sha256 must return 200", "GET /assets/<helper> must return 200"}
	default:
		requirements["required_assets"] = []string{"matching Windows/macOS/Linux rdev helper", "matching .sha256"}
		requirements["verification"] = []string{"send join_url first, or verify platform assets before sending a platform terminal command"}
	}
	return requirements
}

func windowsBootstrapCommand(joinURLs []string, runtimeCandidates []GatewayURLCandidate) string {
	urls := quotedPowerShellArrayValues(joinURLs, "/bootstrap.ps1", runtimeCandidates)
	return "powershell -NoProfile -Command \"$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; $urls=@(" + strings.Join(urls, ",") + "); foreach ($u in $urls) { try { irm $u -UseBasicParsing -TimeoutSec " + strconv.Itoa(targetHTTPMaxTimeSeconds) + " | iex; exit 0 } catch { Write-Host ('rdev Connection Entry failed: ' + $u) } }; throw 'No reachable rdev Connection Entry URL'\""
}

func macLinuxBootstrapCommand(joinURLs []string, runtimeCandidates []GatewayURLCandidate) string {
	urls := quotedShellArrayValues(joinURLs, "/bootstrap.sh", runtimeCandidates)
	return "sh -c 'set -eu; t=\"${TMPDIR:-/tmp}/rdev-bootstrap-$$.sh\"; for u in " + strings.Join(urls, " ") + "; do if curl --connect-timeout " + strconv.Itoa(targetHTTPConnectTimeoutSeconds) + " --max-time " + strconv.Itoa(targetHTTPMaxTimeSeconds) + " --retry " + strconv.Itoa(targetHTTPRetries) + " --retry-delay " + strconv.Itoa(targetHTTPRetryDelaySeconds) + " -fsSL \"$u\" -o \"$t\" && sh \"$t\"; then rm -f \"$t\"; exit 0; fi; rm -f \"$t\"; done; echo \"No reachable rdev Connection Entry URL\" >&2; exit 1'"
}

func quotedPowerShellArrayValues(values []string, suffix string, runtimeCandidates []GatewayURLCandidate) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		bootstrapURL := bootstrapURLWithRuntimeCandidates(value+suffix, runtimeCandidates)
		out = append(out, "'"+strings.ReplaceAll(bootstrapURL, "'", "''")+"'")
	}
	return out
}

func quotedShellArrayValues(values []string, suffix string, runtimeCandidates []GatewayURLCandidate) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		out = append(out, shellQuote(bootstrapURLWithRuntimeCandidates(value+suffix, runtimeCandidates)))
	}
	return out
}

func runtimeGatewayCandidates(candidates []GatewayURLCandidate) []GatewayURLCandidate {
	stable := make([]GatewayURLCandidate, 0, len(candidates))
	rest := make([]GatewayURLCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.Kind {
		case "hosted", "relay", "mesh", "vpn", "ssh":
			stable = append(stable, candidate)
		default:
			rest = append(rest, candidate)
		}
	}
	return append(stable, rest...)
}

func bootstrapURLWithRuntimeCandidates(rawURL string, candidates []GatewayURLCandidate) string {
	if len(candidates) == 0 {
		return rawURL
	}
	values := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.URL) == "" {
			continue
		}
		values = append(values, map[string]any{
			"url":         strings.TrimRight(strings.TrimSpace(candidate.URL), "/"),
			"kind":        candidate.Kind,
			"scope":       candidate.Scope,
			"recommended": candidate.Recommended,
			"reason":      candidate.Reason,
		})
	}
	if len(values) == 0 {
		return rawURL
	}
	content, err := json.Marshal(values)
	if err != nil {
		return rawURL
	}
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + "gateway_url_candidates=" + neturl.QueryEscape(string(content))
}

func BuildPlan(ctx context.Context, opts Options) map[string]any {
	repoRootInput := strings.TrimSpace(opts.RepoRoot)
	if repoRootInput == "" {
		repoRootInput = "."
	}
	repoRoot, _ := filepath.Abs(repoRootInput)
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir = filepath.Join(repoRoot, "work", "rdev-support-session")
	}
	workDir, _ = filepath.Abs(workDir)
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "0.0.0.0:8787"
	}
	gatewayURL, gatewayCandidates := ResolveGatewayURL(addr, opts.GatewayURL)
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		target = "auto"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	ttl := opts.TTLSeconds
	if ttl == 0 {
		ttl = 7200
	}
	rdevPath := filepath.Join(workDir, "bin", exeName("rdev", runtime.GOOS))
	windowsRdevPath := filepath.Join(workDir, "bin", "rdev-windows-amd64.exe")
	linuxRdevPath := filepath.Join(workDir, "bin", "rdev-linux-amd64")
	linuxArmRdevPath := filepath.Join(workDir, "bin", "rdev-linux-arm64")
	darwinArmRdevPath := filepath.Join(workDir, "bin", "rdev-darwin-arm64")
	darwinAmdRdevPath := filepath.Join(workDir, "bin", "rdev-darwin-amd64")
	createInviteCommand := []string{
		rdevPath, "invite", "create",
		"--gateway", gatewayURL,
		"--mode", string(model.HostModeAttendedTemporary),
		"--ttl-seconds", strconv.Itoa(ttl),
		"--reason", opts.Reason,
		"--transport", "auto",
	}
	if opts.AutoApprove {
		createInviteCommand = append(createInviteCommand, "--auto-approve")
	}
	helperPreflight := connectivityHelperPreflight()
	inviteBody, _ := json.Marshal(map[string]any{
		"mode":         string(model.HostModeAttendedTemporary),
		"ttl_seconds":  ttl,
		"reason":       opts.Reason,
		"auto_approve": opts.AutoApprove,
		"metadata": map[string]string{
			"connection_entry":  "standard-visible",
			"approval_contract": "target-consent-scoped-ticket",
		},
	})
	return map[string]any{
		"schema_version":                PlanSchemaVersion,
		"ok":                            true,
		"intent":                        "one-command-visible-attended-temporary-connection-entry",
		"repo_root":                     repoRoot,
		"work_dir":                      workDir,
		"target":                        target,
		"locale":                        locale,
		"gateway_url":                   gatewayURL,
		"gateway_url_candidates":        gatewayCandidates,
		"connectivity_helper_preflight": helperPreflight,
		"auto_approve": map[string]any{
			"enabled":        opts.AutoApprove,
			"scope":          "attended-temporary tickets created by this standard plan only",
			"capabilities":   policyCapabilitiesToStrings(policy.TemporaryDefaults()),
			"security_model": "target consent plus signed manifest plus scoped ticket capabilities",
		},
		"commands": map[string]any{
			"prepare_dirs":           []string{"mkdir", "-p", filepath.Join(workDir, "bin"), filepath.Join(workDir, ".rdev", "keys"), filepath.Join(workDir, ".rdev", "gateway"), filepath.Join(workDir, ".rdev", "audit")},
			"build_local_rdev":       []string{"go", "build", "-o", rdevPath, "./cmd/rdev"},
			"build_windows_rdev":     []string{"env", "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", windowsRdevPath, "./cmd/rdev"},
			"build_linux_rdev":       []string{"env", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", linuxRdevPath, "./cmd/rdev"},
			"build_linux_arm64_rdev": []string{"env", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", linuxArmRdevPath, "./cmd/rdev"},
			"build_macos_arm64_rdev": []string{"env", "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-o", darwinArmRdevPath, "./cmd/rdev"},
			"build_macos_amd64_rdev": []string{"env", "GOOS=darwin", "GOARCH=amd64", "CGO_ENABLED=0", "go", "build", "-o", darwinAmdRdevPath, "./cmd/rdev"},
			"start_gateway": []string{
				rdevPath, "gateway", "serve", "--dev",
				"--addr", addr,
				"--audit-log", filepath.Join(workDir, ".rdev", "audit", "events.jsonl"),
				"--state", filepath.Join(workDir, ".rdev", "gateway", "state.json"),
				"--signing-key", filepath.Join(workDir, ".rdev", "keys", "gateway-signing-key.json"),
				"--manifest-signing-key", filepath.Join(workDir, ".rdev", "keys", "manifest-root-key.json"),
				"--rdev-windows-amd64", windowsRdevPath,
				"--rdev-linux-amd64", linuxRdevPath,
				"--rdev-linux-arm64", linuxArmRdevPath,
				"--rdev-darwin-arm64", darwinArmRdevPath,
				"--rdev-darwin-amd64", darwinAmdRdevPath,
			},
			"create_invite_http": []string{
				"curl", "-fsS", "-X", "POST", gatewayURL + "/v1/tickets",
				"-H", "Content-Type: application/json",
				"-d", string(inviteBody),
			},
			"create_invite_cli": createInviteCommand,
			"watch_connection_status": []string{
				rdevPath, "support-session", "status",
				"--gateway-url", gatewayURL,
				"--ticket-code", "<ticket-code>",
				"--wait",
				"--locale", locale,
			},
		},
		"target_user_instructions": LocalizedTargetInstructions(gatewayURL, locale),
		"agent_flow": []string{
			"run prepare_dirs and build commands from repo_root",
			"start gateway with the exact start_gateway argv in a managed terminal/session",
			"create the invite through HTTP or CLI",
			"give target user only the localized join URL or one-line visible script",
			"watch connection status with rdev.support_session.status or rdev support-session status --wait",
			"when connected=true, proactively tell the user the connection is established before creating jobs",
			"do not write ad hoc relay/nohup/bootstrap code",
			"after host connects, it is active when auto_approve is enabled; otherwise call rdev.hosts.approve",
		},
		"forbidden": []string{
			"ExecutionPolicy Bypass",
			"hidden install",
			"unverified binary download",
			"manual ticket/root/gateway/transport assembly for target user",
		},
		"detected_host_capabilities": hostcap.Detect(ctx),
	}
}

func normalizeTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "windows", "win":
		return "windows"
	case "macos", "mac", "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return "auto"
	}
}

func localizedCreatedMessage(locale string) string {
	switch locale {
	case "zh-CN", "zh":
		return "把 target_command 发给目标电脑上的用户执行；然后持续监听状态，连接建立后主动告诉用户。"
	case "ja":
		return "target_command を対象コンピューターのユーザーに渡して実行してもらい、接続状態を監視して、接続後に報告してください。"
	case "ko":
		return "대상 컴퓨터의 사용자에게 target_command를 실행하게 한 뒤 상태를 감시하고 연결되면 알려 주세요."
	case "es":
		return "Entrega target_command a la persona en el equipo de destino, vigila el estado y avisa cuando la conexion quede establecida."
	case "fr":
		return "Donne target_command a la personne sur l'ordinateur cible, surveille l'etat, puis annonce quand la connexion est etablie."
	case "de":
		return "Gib target_command an die Person am Zielcomputer, uberwache den Status und melde, sobald die Verbindung steht."
	case "pt-BR":
		return "Entregue target_command para a pessoa no computador de destino, acompanhe o status e avise quando a conexao estiver estabelecida."
	default:
		return "Give target_command to the target-side human, watch status, and proactively report when the connection is established."
	}
}

func localizedUserHandoffMessage(locale, surface string) string {
	copyHint := "Run this on the target computer and keep the window open while I connect."
	if surface == "join_url" {
		copyHint = "Open this on the target computer and keep the page or terminal open while I connect."
	}
	switch locale {
	case "zh-CN", "zh":
		if surface == "join_url" {
			return "请在目标电脑上打开下面这个连接。打开后保持页面或终端窗口不要关闭，我会自动等待连接并在连上后告诉你。"
		}
		return "请在目标电脑上运行下面这条命令。运行后保持窗口不要关闭，我会自动等待连接并在连上后告诉你。"
	case "ja":
		if surface == "join_url" {
			return "対象コンピューターで次のリンクを開いてください。接続中はページまたはターミナルを開いたままにしてください。接続後に報告します。"
		}
		return "対象コンピューターで次のコマンドを実行してください。接続中はウィンドウを開いたままにしてください。接続後に報告します。"
	case "ko":
		if surface == "join_url" {
			return "대상 컴퓨터에서 아래 링크를 열어 주세요. 연결되는 동안 페이지나 터미널을 닫지 마세요. 연결되면 알려 드립니다."
		}
		return "대상 컴퓨터에서 아래 명령을 실행해 주세요. 연결되는 동안 창을 닫지 마세요. 연결되면 알려 드립니다."
	case "es":
		if surface == "join_url" {
			return "Abre este enlace en el equipo de destino y deja la pagina o terminal abierta mientras conecto. Avisare cuando este conectado."
		}
		return "Ejecuta este comando en el equipo de destino y deja la ventana abierta mientras conecto. Avisare cuando este conectado."
	case "fr":
		if surface == "join_url" {
			return "Ouvre ce lien sur l'ordinateur cible et garde la page ou le terminal ouvert pendant la connexion. Je te dirai quand c'est connecte."
		}
		return "Execute cette commande sur l'ordinateur cible et garde la fenetre ouverte pendant la connexion. Je te dirai quand c'est connecte."
	case "de":
		if surface == "join_url" {
			return "Offne diesen Link auf dem Zielcomputer und lasse die Seite oder das Terminal wahrend der Verbindung offen. Ich melde mich, sobald es verbunden ist."
		}
		return "Fuhre diesen Befehl auf dem Zielcomputer aus und lasse das Fenster wahrend der Verbindung offen. Ich melde mich, sobald es verbunden ist."
	case "pt-BR":
		if surface == "join_url" {
			return "Abra este link no computador de destino e mantenha a pagina ou o terminal aberto enquanto eu conecto. Avisarei quando estiver conectado."
		}
		return "Execute este comando no computador de destino e mantenha a janela aberta enquanto eu conecto. Avisarei quando estiver conectado."
	default:
		return copyHint
	}
}

func BuildStatus(opts StatusOptions) map[string]any {
	ticketCode := strings.TrimSpace(opts.TicketCode)
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	hosts := append([]model.Host(nil), opts.Hosts...)
	active := hostsByStatus(hosts, model.HostStatusActive)
	pending := hostsByStatus(hosts, model.HostStatusPending)
	revoked := hostsByStatus(hosts, model.HostStatusRevoked)
	connected := len(active) > 0
	waiting := !connected && len(pending) == 0
	status := "waiting"
	if connected {
		status = "connected"
	} else if len(pending) > 0 {
		status = "pending-approval"
	} else if len(revoked) > 0 {
		status = "revoked"
	}
	return map[string]any{
		"schema_version": StatusSchemaVersion,
		"ok":             connected || len(pending) > 0 || waiting,
		"ticket_code":    ticketCode,
		"status":         status,
		"connected":      connected,
		"waiting":        waiting,
		"feedback":       localizedStatusFeedback(status, locale),
		"next_action":    localizedStatusNextAction(status, locale),
		"connected_next_steps": BuildConnectedNextSteps(ConnectedNextStepsOptions{
			Status: status,
			Hosts:  active,
			Locale: locale,
		}),
		"connection_recovery": BuildConnectionRecovery(ConnectionRecoveryOptions{
			Status:     status,
			TicketCode: ticketCode,
			Locale:     locale,
			TimedOut:   false,
		}),
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:      "status",
			Status:     status,
			TicketCode: ticketCode,
			Locale:     locale,
			TimedOut:   false,
		}),
		"active_hosts":  active,
		"pending_hosts": pending,
		"revoked_hosts": revoked,
		"host_count": map[string]int{
			"active":  len(active),
			"pending": len(pending),
			"revoked": len(revoked),
			"total":   len(hosts),
		},
	}
}

type ConnectedNextStepsOptions struct {
	Status string
	Hosts  []model.Host
	Locale string
}

func BuildConnectedNextSteps(opts ConnectedNextStepsOptions) map[string]any {
	connected := opts.Status == "connected" && len(opts.Hosts) > 0
	hostID := ""
	hostName := ""
	capabilities := []string{}
	if connected {
		host := opts.Hosts[0]
		hostID = host.ID
		hostName = host.Name
		capabilities = append([]string(nil), host.Capabilities...)
	}
	report := "Connection established. I can see the target host and will inspect capabilities before starting scoped work."
	switch opts.Locale {
	case "zh-CN", "zh":
		report = "连接已经建立。我已经看到目标主机，会先检查能力范围，再创建最小权限任务。"
	}
	actions := []string{}
	if connected {
		actions = []string{
			"send user_report to the user before creating jobs",
			"inspect host capabilities with rdev.hosts.capabilities",
			"create the smallest scoped job only after the user's task intent is clear",
			"export or review evidence before declaring the remote work complete",
		}
	} else {
		report = ""
	}
	var mcpNextCalls any
	var cliNextCommands any
	if connected {
		mcpNextCalls = connectedNextMCPCalls(hostID)
		cliNextCommands = connectedNextCLICommands(hostID)
	}
	return map[string]any{
		"schema_version":     ConnectedNextStepsSchemaVersion,
		"connected":          connected,
		"host_id":            hostID,
		"host_name":          hostName,
		"capabilities":       capabilities,
		"user_report":        report,
		"agent_next_actions": actions,
		"mcp_next_calls":     mcpNextCalls,
		"cli_next_commands":  cliNextCommands,
		"forbidden": []string{
			"creating a broad shell job before inspecting capabilities",
			"claiming work is complete before reviewing job evidence",
			"asking the user for ticket/root/gateway/transport after connected=true",
		},
	}
}

func connectedNextMCPCalls(hostID string) []map[string]any {
	if strings.TrimSpace(hostID) == "" {
		return nil
	}
	return []map[string]any{
		{
			"tool": "rdev.hosts.capabilities",
			"arguments": map[string]any{
				"host_id": hostID,
			},
		},
	}
}

func connectedNextCLICommands(hostID string) [][]string {
	if strings.TrimSpace(hostID) == "" {
		return nil
	}
	return [][]string{{"rdev", "hosts", "capabilities", "--host-id", hostID}}
}

type ConnectionRecoveryOptions struct {
	Status     string
	TicketCode string
	Locale     string
	TimedOut   bool
}

func BuildConnectionRecovery(opts ConnectionRecoveryOptions) map[string]any {
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = "waiting"
	}
	locale := strings.TrimSpace(opts.Locale)
	if locale == "" {
		locale = "auto"
	}
	ticketCode := strings.TrimSpace(opts.TicketCode)
	agentActions := []string{}
	humanChecks := []string{}
	switch status {
	case "connected":
		agentActions = []string{
			"tell the user the connection is established",
			"inspect host capabilities before creating the smallest scoped job",
			"review audit and evidence after each job",
		}
	case "pending-approval":
		agentActions = []string{
			"approve only the expected host when policy requires approval",
			"wait briefly for auto-approval synchronization when this is a standard attended-temporary session",
			"do not ask the target-side human for ticket, root, gateway, or transport values",
		}
	case "revoked":
		agentActions = []string{
			"create a new Connection Entry with rdev.support_session.create or rdev support-session connect --start",
			"send only the new user_handoff message plus copy_paste value to the human",
			"do not reuse old ticket, manifest root, gateway, or transport metadata",
		}
	default:
		agentActions = []string{
			"keep using rdev.support_session.status with wait=true or rdev support-session status --wait",
			"if the original user_handoff is still valid, resend user_handoff.message plus user_handoff.copy_paste verbatim",
			"if the target cannot reach the join URL or every gateway candidate times out, run rdev.support_session.prepare or rdev support-session prepare --build-assets, then create a fresh Connection Entry",
			"use the returned gateway_url_candidates, connection_attempt_policy, runner_plan, or standard_recovery fields instead of writing ad hoc network scripts",
		}
		humanChecks = []string{
			"keep the visible target-side command window open",
			"open the Connection Entry join URL on the target machine if the terminal command failed",
			"copy the visible error output only when the standard command reports a failure",
		}
	}
	if status != "waiting" && status != "" {
		humanChecks = append(humanChecks, "do not copy raw ticket, root key, gateway, transport, release, or checksum values")
	}
	reason := "standard status-specific recovery"
	if opts.TimedOut {
		reason = "standard wait timeout recovery"
		if status == "waiting" {
			agentActions = append([]string{
				"treat this as a connection-entry failure to reach or run, not as permission to improvise scripts",
			}, agentActions...)
		}
	}
	return map[string]any{
		"schema_version": ConnectionRecoverySchemaVersion,
		"status":         status,
		"ticket_code":    ticketCode,
		"timed_out":      opts.TimedOut,
		"reason":         reason,
		"agent_connection_runbook": agentConnectionRunbook(agentConnectionRunbookOptions{
			Phase:      "recovery",
			Status:     status,
			TicketCode: ticketCode,
			Locale:     locale,
			TimedOut:   opts.TimedOut,
		}),
		"agent_rule": localizedConnectionRecoveryAgentRule(locale),
		"agent_next_actions": append(agentActions, []string{
			"ask one short question only for authorization, persistence approval, privileged network changes, paid/cloud relay use, credentials, or unclear ownership",
		}...),
		"human_checks": humanChecks,
		"standard_tools": []string{
			"rdev.support_session.status",
			"rdev support-session status --wait",
			"rdev.support_session.prepare",
			"rdev support-session prepare --build-assets",
			"rdev.support_session.create",
			"rdev support-session connect --start",
			"rdev support-session start",
			"rdev.connection_entry.plan",
			"rdev connection-entry run --dry-run",
		},
		"forbidden": []string{
			"Agent-authored PowerShell or shell relay scripts",
			"manual ticket/root/gateway/transport assembly for target humans",
			"ExecutionPolicy Bypass",
			"hidden install",
			"UAC or sudo bypass",
			"firewall, DNS, route, service, driver, credential, paid relay, or cloud changes without explicit approval",
		},
	}
}

func LocalizedTargetInstructions(gatewayURL, locale string) map[string]any {
	windows := "powershell -NoProfile -Command \"irm '" + gatewayURL + "/join/<ticket-code>/bootstrap.ps1' | iex\""
	macLinux := "curl -fsSL " + gatewayURL + "/join/<ticket-code>/bootstrap.sh | sh"
	labels := map[string]string{
		"auto":  "Open this visible support command on the target computer. Keep the terminal open while the Agent works.",
		"en":    "Open this visible support command on the target computer. Keep the terminal open while the Agent works.",
		"zh-CN": "在目标电脑上运行这条可见的支持命令。Agent 工作期间请保持终端窗口打开。",
		"ja":    "対象コンピューターでこの表示されるサポートコマンドを実行し、Agent の作業中はターミナルを開いたままにしてください。",
		"ko":    "대상 컴퓨터에서 이 표시되는 지원 명령을 실행하고 Agent가 작업하는 동안 터미널을 열어 두세요.",
		"es":    "Ejecuta este comando visible de soporte en el equipo de destino y deja la terminal abierta mientras trabaja el Agent.",
		"fr":    "Executez cette commande d'assistance visible sur l'ordinateur cible et gardez le terminal ouvert pendant que l'Agent travaille.",
		"de":    "Fuhre diesen sichtbaren Support-Befehl auf dem Zielcomputer aus und lasse das Terminal offen, wahrend der Agent arbeitet.",
		"pt-BR": "Execute este comando visivel de suporte no computador de destino e mantenha o terminal aberto enquanto o Agent trabalha.",
	}
	message, ok := labels[locale]
	if !ok {
		message = labels["en"]
	}
	return map[string]any{
		"message":             message,
		"windows":             windows,
		"macos_linux":         macLinux,
		"join_url_template":   gatewayURL + "/join/<ticket-code>",
		"human_receives_only": []string{"localized join URL", "visible one-line script", "or signed package when published"},
	}
}

func hostsByStatus(hosts []model.Host, status model.HostStatus) []model.Host {
	values := make([]model.Host, 0, len(hosts))
	for _, host := range hosts {
		if host.Status == status {
			values = append(values, host)
		}
	}
	return values
}

func localizedStatusFeedback(status, locale string) string {
	switch locale {
	case "zh-CN", "zh":
		switch status {
		case "connected":
			return "连接已经建立，目标主机已在线并可用于受控任务。"
		case "pending-approval":
			return "目标主机已经出现，正在等待审批或自动批准完成。"
		case "revoked":
			return "连接票据或主机已经撤销。"
		default:
			return "还没有检测到目标主机连接，请确认目标机器上的可见命令仍在运行。"
		}
	default:
		switch status {
		case "connected":
			return "Connection established. The target host is online and ready for scoped work."
		case "pending-approval":
			return "The target host has appeared and is waiting for approval or auto-approval to complete."
		case "revoked":
			return "The connection ticket or host has been revoked."
		default:
			return "No target host is connected yet. Keep the visible command running on the target machine."
		}
	}
}

func localizedStatusNextAction(status, locale string) string {
	switch locale {
	case "zh-CN", "zh":
		switch status {
		case "connected":
			return "向用户汇报连接已建立，然后检查主机能力并创建最小权限任务。"
		case "pending-approval":
			return "如果不是标准自动批准会话，请审批预期主机；否则继续等待短暂同步。"
		case "revoked":
			return "创建新的 Connection Entry。"
		default:
			return "继续等待，或检查 gateway 地址、网络可达性和目标命令输出。"
		}
	default:
		switch status {
		case "connected":
			return "Tell the user the connection is established, then inspect capabilities and create the smallest scoped job."
		case "pending-approval":
			return "Approve the expected host if this is not a standard auto-approved session; otherwise wait briefly."
		case "revoked":
			return "Create a new Connection Entry."
		default:
			return "Keep waiting, or check gateway reachability, network path, and target command output."
		}
	}
}

func localizedConnectionRecoveryAgentRule(locale string) string {
	switch locale {
	case "zh-CN", "zh":
		return "使用返回的 Connection Entry、user_handoff、status、prepare、runner 字段恢复连接；不要编写临时 PowerShell、shell、relay、审批轮询或 bootstrap 代码，也不要让人类拼 ticket/root/gateway/transport。"
	default:
		return "Recover connection setup through returned Connection Entry, user_handoff, status, prepare, and runner fields; do not write ad hoc PowerShell, shell, relay, approval-polling, or bootstrap code, and do not ask humans to assemble ticket/root/gateway/transport values."
	}
}

func exeName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func policyCapabilitiesToStrings(caps []policy.Capability) []string {
	values := make([]string, 0, len(caps))
	for _, cap := range caps {
		values = append(values, string(cap))
	}
	return values
}

type supportSessionAssetSpec struct {
	ID     string
	Name   string
	GOOS   string
	GOARCH string
	Path   string
}

func supportSessionAssetSpecs(binDir string) []supportSessionAssetSpec {
	return []supportSessionAssetSpec{
		{ID: "windows-amd64", Name: "rdev-windows-amd64.exe", GOOS: "windows", GOARCH: "amd64", Path: filepath.Join(binDir, "rdev-windows-amd64.exe")},
		{ID: "darwin-arm64", Name: "rdev-darwin-arm64", GOOS: "darwin", GOARCH: "arm64", Path: filepath.Join(binDir, "rdev-darwin-arm64")},
		{ID: "darwin-amd64", Name: "rdev-darwin-amd64", GOOS: "darwin", GOARCH: "amd64", Path: filepath.Join(binDir, "rdev-darwin-amd64")},
		{ID: "linux-amd64", Name: "rdev-linux-amd64", GOOS: "linux", GOARCH: "amd64", Path: filepath.Join(binDir, "rdev-linux-amd64")},
		{ID: "linux-arm64", Name: "rdev-linux-arm64", GOOS: "linux", GOARCH: "arm64", Path: filepath.Join(binDir, "rdev-linux-arm64")},
	}
}

func recommendedSupportSessionNextStep(localRdevUsable, allAssetsReady bool) string {
	switch {
	case localRdevUsable && allAssetsReady:
		return "run rdev support-session connect --start and give the generated target_command to the target-side human"
	case allAssetsReady:
		return "use the current rdev executable or checkout command to run support-session connect --start; target-side self-repair assets are ready"
	default:
		return "prepare helper assets from a valid checkout before relying on one-command targets without preinstalled rdev"
	}
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func findRepoRoot(start string) string {
	if strings.TrimSpace(start) == "" {
		return ""
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if pathExists(filepath.Join(dir, "go.mod")) && pathExists(filepath.Join(dir, "cmd", "rdev", "main.go")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func fileSHA256Hex(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func tailString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
