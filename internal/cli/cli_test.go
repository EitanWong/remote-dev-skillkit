package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/connectionrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostidentity"
	"github.com/EitanWong/remote-dev-skillkit/internal/hosttrust"
	"github.com/EitanWong/remote-dev-skillkit/internal/httpapi"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/operatorauth"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/protectedstore"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

func TestVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "rdev") {
		t.Fatalf("expected version output to mention rdev, got %q", stdout.String())
	}
}

func TestMCPToolsOutputsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"mcp", "tools"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(payload.Tools) == 0 {
		t.Fatal("expected at least one tool")
	}
}

func TestBootstrapAgentPlanGuidesRdevRecoveryAndRemoteDefaults(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))

	if err := app.Run(context.Background(), []string{"bootstrap", "agent-plan", "--repo-root", repoRoot, "--framework", "codex", "--remote-requested"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Repo          string `json:"repo"`
		Framework     string `json:"framework"`
		RepoRootValid bool   `json:"repo_root_valid"`
		LocalMCP      struct {
			Mode       string   `json:"mode"`
			Command    string   `json:"command"`
			Args       []string `json:"args"`
			GatewayURL string   `json:"gateway_url"`
		} `json:"local_mcp"`
		RecoveryOrder []struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			Command []string `json:"command"`
		} `json:"rdev_recovery_order"`
		RemoteDefaults struct {
			Requested          bool     `json:"requested"`
			DefaultUnknownMode string   `json:"default_unknown_owner"`
			FirstQuestion      string   `json:"first_human_question"`
			SafeDefaults       []string `json:"safe_defaults"`
		} `json:"remote_host_defaults"`
		AskOnlyWhen      []string `json:"ask_only_when"`
		DoNotAskFor      []string `json:"do_not_ask_for"`
		ForbiddenActions []string `json:"forbidden_actions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid bootstrap plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.agent-bootstrap-plan.v1" || payload.Repo != "EitanWong/remote-dev-skillkit" || payload.Framework != "codex" {
		t.Fatalf("unexpected bootstrap plan identity: %#v", payload)
	}
	if !payload.RepoRootValid {
		t.Fatalf("repo root should be valid for checkout bootstrap plan: %s", stdout.String())
	}
	if payload.LocalMCP.Mode != "stdio" || !slices.Contains(payload.LocalMCP.Args, "serve") || payload.LocalMCP.GatewayURL != "optional-for-local-agent-install" {
		t.Fatalf("local agent install should default to MCP stdio without hosted gateway: %#v", payload.LocalMCP)
	}
	var recoveryIDs []string
	for _, action := range payload.RecoveryOrder {
		recoveryIDs = append(recoveryIDs, action.ID)
	}
	for _, expected := range []string{"use-existing-rdev", "build-from-checkout", "run-from-checkout-with-go", "clone-then-build", "signed-release-download"} {
		if !slices.Contains(recoveryIDs, expected) {
			t.Fatalf("missing recovery action %q in %#v", expected, recoveryIDs)
		}
	}
	if !payload.RemoteDefaults.Requested ||
		payload.RemoteDefaults.DefaultUnknownMode != string(model.HostModeAttendedTemporary) ||
		!strings.Contains(payload.RemoteDefaults.FirstQuestion, "company policy") ||
		!slices.Contains(payload.RemoteDefaults.SafeDefaults, "no hidden persistence") {
		t.Fatalf("remote defaults should collapse decisions into visible temporary support: %#v", payload.RemoteDefaults)
	}
	joinedAsk := strings.Join(payload.AskOnlyWhen, "\n")
	joinedDontAsk := strings.Join(payload.DoNotAskFor, "\n")
	joinedForbidden := strings.Join(payload.ForbiddenActions, "\n")
	if !strings.Contains(joinedAsk, "company or owner authorization") ||
		!strings.Contains(joinedDontAsk, "target OS before generating a Connection Entry") ||
		!strings.Contains(joinedDontAsk, "ticket code, manifest root, gateway URL") ||
		!strings.Contains(joinedForbidden, "ExecutionPolicy Bypass") {
		t.Fatalf("bootstrap plan should define ask boundaries and forbidden actions:\nask=%s\ndont=%s\nforbidden=%s", joinedAsk, joinedDontAsk, joinedForbidden)
	}
}

func TestAcceptanceFreshAgentSupportSession(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	out := filepath.Join(t.TempDir(), "fresh-agent")

	if err := app.Run(context.Background(), []string{
		"acceptance", "fresh-agent-support-session",
		"--out", out,
		"--gateway-url", "http://127.0.0.1:8787",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK     bool   `json:"ok"`
		Schema string `json:"schema"`
		Report string `json:"report"`
		Checks []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid acceptance JSON: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance.fresh-agent-support-session.v1" {
		t.Fatalf("unexpected fresh-agent acceptance summary: %#v", payload)
	}
	if _, err := os.Stat(payload.Report); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	var names []string
	for _, check := range payload.Checks {
		if !check.Passed {
			t.Fatalf("expected passing check: %#v", check)
		}
		names = append(names, check.Name)
	}
	for _, expected := range []string{
		"connect_without_gateway_returns_start_now_command",
		"handoff_without_gateway_prefers_connect_start",
		"handoff_with_gateway_selects_create_tool",
		"auto_approval_connects_first_attended_host",
		"connected_status_has_user_report",
		"waiting_recovery_forbids_custom_scripts",
	} {
		if !slices.Contains(names, expected) {
			t.Fatalf("missing check %q in %#v", expected, names)
		}
	}
	if !strings.Contains(payload.Note, "local contract gate only") {
		t.Fatalf("expected local-gate note, got %q", payload.Note)
	}
}

func TestSupportSessionConnectReturnsForegroundStartWithoutGateway(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{
		"support-session", "connect",
		"--target", "auto",
		"--reason", "company computer support",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion    string   `json:"schema_version"`
		SelectedPath     string   `json:"selected_path"`
		ReadyToSendHuman bool     `json:"ready_to_send_to_human"`
		StartNowCommand  []string `json:"cli_start_now_command"`
		StartCommand     []string `json:"foreground_start_command"`
		AgentNextStep    string   `json:"agent_next_step"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid connect JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-connect.v1" ||
		payload.SelectedPath != "start-foreground-gateway" ||
		payload.ReadyToSendHuman ||
		!slices.Contains(payload.StartNowCommand, "--start") ||
		!slices.Contains(payload.StartCommand, "start") ||
		!strings.Contains(payload.AgentNextStep, "cli_start_now_command") ||
		!strings.Contains(payload.AgentNextStep, "ready_file.path") ||
		!strings.Contains(payload.AgentNextStep, "status_file.path") {
		t.Fatalf("unexpected foreground connect payload: %#v", payload)
	}
}

func TestSupportSessionForegroundEventIsMachineReadable(t *testing.T) {
	var out bytes.Buffer
	statusFile := filepath.Join(t.TempDir(), "support-session-status.json")
	writeSupportSessionEvent(&out, statusFile, "connected", map[string]any{
		"schema_version": "rdev.support-session-status.v1",
		"connected":      true,
		"status":         "connected",
	})

	const prefix = "rdev support session event: "
	line := strings.TrimSpace(out.String())
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("expected event prefix, got %q", line)
	}
	var payload struct {
		SchemaVersion string         `json:"schema_version"`
		Event         string         `json:"event"`
		Status        map[string]any `json:"status"`
		AgentRule     string         `json:"agent_rule"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &payload); err != nil {
		t.Fatalf("invalid event JSON: %v\n%s", err, line)
	}
	if payload.SchemaVersion != "rdev.support-session-foreground-event.v1" ||
		payload.Event != "connected" ||
		payload.Status["connected"] != true ||
		!strings.Contains(payload.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("unexpected event payload: %#v", payload)
	}
	statusBytes, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	var statusPayload struct {
		SchemaVersion string         `json:"schema_version"`
		Event         string         `json:"event"`
		Status        map[string]any `json:"status"`
		AgentRule     string         `json:"agent_rule"`
	}
	if err := json.Unmarshal(statusBytes, &statusPayload); err != nil {
		t.Fatalf("invalid status file JSON: %v\n%s", err, string(statusBytes))
	}
	if statusPayload.SchemaVersion != "rdev.support-session-foreground-event.v1" ||
		statusPayload.Event != "connected" ||
		statusPayload.Status["connected"] != true ||
		!strings.Contains(statusPayload.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("unexpected status file payload: %#v", statusPayload)
	}
	info, err := os.Stat(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected status file permissions 0600, got %v", info.Mode().Perm())
	}
}

func TestSupportSessionForegroundWatcherWritesConnectedStatusFile(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicketWithMetadata(
		model.HostModeAttendedTemporary,
		600,
		cliPolicyCapabilitiesToStrings(policy.TemporaryDefaults()),
		"foreground status file watcher test",
		map[string]string{"auto_approve": "attended-temporary"},
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out bytes.Buffer
	statusFile := filepath.Join(t.TempDir(), "support-session-status.json")
	connectedReportFile := filepath.Join(t.TempDir(), "connected-report.txt")
	go watchForegroundSupportSession(ctx, &out, statusFile, connectedReportFile, gw, ticket.Code, "en")

	waitForStatusFileEvent(t, statusFile, "waiting")
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "fresh-agent-connected-host",
		OS:           "linux",
		Arch:         "amd64",
		Capabilities: ticket.Capabilities,
	}); err != nil {
		t.Fatal(err)
	}
	statusPayload := waitForStatusFileEvent(t, statusFile, "connected")
	status := statusPayload.Status
	connectedNext := status["connected_next_steps"].(map[string]any)
	if statusPayload.SchemaVersion != "rdev.support-session-foreground-event.v1" ||
		status["schema_version"] != "rdev.support-session-status.v1" ||
		status["connected"] != true ||
		!strings.Contains(connectedNext["user_report"].(string), "Connection established") ||
		!strings.Contains(statusPayload.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("expected connected status event in status file, got %#v", statusPayload)
	}
	if !strings.Contains(out.String(), `"event":"connected"`) {
		t.Fatalf("expected connected stderr event, got %s", out.String())
	}
	report, err := os.ReadFile(connectedReportFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "Connection established") {
		t.Fatalf("expected connected report text, got %q", string(report))
	}
}

type supportSessionStatusFileEvent struct {
	SchemaVersion string         `json:"schema_version"`
	Event         string         `json:"event"`
	Status        map[string]any `json:"status"`
	AgentRule     string         `json:"agent_rule"`
}

func waitForStatusFileEvent(t *testing.T, path, event string) supportSessionStatusFileEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			last = string(data)
			var payload supportSessionStatusFileEvent
			if err := json.Unmarshal(data, &payload); err == nil && payload.Event == event {
				return payload
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status file event %q at %s; last=%s", event, path, last)
	return supportSessionStatusFileEvent{}
}

func TestSupportSessionPlanStandardizesOneCommandConnection(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))
	workDir := filepath.Join(t.TempDir(), "support")

	if err := app.Run(context.Background(), []string{
		"support-session", "plan",
		"--repo-root", repoRoot,
		"--work-dir", workDir,
		"--gateway-url", "http://192.0.2.10:8787",
		"--target", "windows",
		"--reason", "company computer support",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string `json:"schema_version"`
		GatewayURL           string `json:"gateway_url"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Kind        string `json:"kind"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		AutoApprove struct {
			Enabled      bool     `json:"enabled"`
			Scope        string   `json:"scope"`
			Capabilities []string `json:"capabilities"`
		} `json:"auto_approve"`
		Commands map[string][]string `json:"commands"`
		Target   struct {
			Windows      string   `json:"windows"`
			HumanSurface []string `json:"human_receives_only"`
		} `json:"target_user_instructions"`
		Forbidden []string `json:"forbidden"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-plan.v1" || !payload.AutoApprove.Enabled {
		t.Fatalf("expected standard support session plan with auto approval: %#v", payload)
	}
	if payload.GatewayURL != "http://192.0.2.10:8787" ||
		len(payload.GatewayURLCandidates) == 0 ||
		payload.GatewayURLCandidates[0].URL != payload.GatewayURL ||
		!payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("plan should expose recommended gateway URL candidates: %#v", payload.GatewayURLCandidates)
	}
	if !strings.Contains(payload.AutoApprove.Scope, "attended-temporary") ||
		!slices.Contains(payload.AutoApprove.Capabilities, "shell.user") {
		t.Fatalf("auto approval should be scoped and minimal: %#v", payload.AutoApprove)
	}
	startGateway := strings.Join(payload.Commands["start_gateway"], "\x00")
	if !strings.Contains(startGateway, "--rdev-windows-amd64") ||
		!strings.Contains(startGateway, "--manifest-signing-key") ||
		!strings.Contains(startGateway, "--state") {
		t.Fatalf("gateway plan should carry assets and durable state: %#v", payload.Commands["start_gateway"])
	}
	createInviteHTTP := strings.Join(payload.Commands["create_invite_http"], "\n")
	if !strings.Contains(createInviteHTTP, `"auto_approve":true`) ||
		!strings.Contains(createInviteHTTP, `"mode":"attended-temporary"`) {
		t.Fatalf("invite command should create auto-approved attended temporary ticket: %s", createInviteHTTP)
	}
	if strings.Contains(payload.Target.Windows, "ExecutionPolicy Bypass") ||
		!strings.Contains(payload.Target.Windows, "bootstrap.ps1") ||
		!slices.Contains(payload.Target.HumanSurface, "visible one-line script") ||
		!slices.Contains(payload.Forbidden, "unverified binary download") {
		t.Fatalf("target instructions should be one visible safe command: %#v", payload.Target)
	}
}

func TestSupportSessionHandoffSelectsCreateWhenGatewayExists(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{
		"support-session", "handoff",
		"--gateway-url", "http://192.0.2.10:8787",
		"--target", "windows",
		"--reason", "company computer support",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion    string         `json:"schema_version"`
		SelectedPath     string         `json:"selected_path"`
		MCPNextTool      string         `json:"mcp_next_tool"`
		MCPNextArguments map[string]any `json:"mcp_next_arguments"`
		AgentNextStep    string         `json:"agent_next_step"`
		Forbidden        []string       `json:"forbidden"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-handoff.v1" ||
		payload.SelectedPath != "create-with-reachable-gateway" ||
		payload.MCPNextTool != "rdev.support_session.create" ||
		payload.MCPNextArguments["gateway_url"] != "http://192.0.2.10:8787" ||
		payload.MCPNextArguments["target"] != "windows" ||
		!strings.Contains(payload.AgentNextStep, "target_handoff_envelope.full_text") ||
		!slices.Contains(payload.Forbidden, "Agent-authored PowerShell or shell bootstrap/recovery scripts") {
		t.Fatalf("expected create handoff route, got %#v", payload)
	}
}

func TestSupportSessionHandoffSelectsForegroundStartWithoutGateway(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "handoff", "--target", "auto"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion          string   `json:"schema_version"`
		SelectedPath           string   `json:"selected_path"`
		MCPNextTool            string   `json:"mcp_next_tool"`
		ForegroundStartCommand []string `json:"foreground_start_command"`
		AgentRule              string   `json:"agent_rule"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	startCommand := strings.Join(payload.ForegroundStartCommand, "\x00")
	if payload.SchemaVersion != "rdev.support-session-handoff.v1" ||
		payload.SelectedPath != "start-foreground-gateway" ||
		payload.MCPNextTool != "" ||
		!strings.Contains(startCommand, "support-session\x00start") ||
		!strings.Contains(payload.AgentRule, "do not choose support-session plan") {
		t.Fatalf("expected foreground start handoff route, got %#v", payload)
	}
}

func TestSupportSessionHandoffUsesConfiguredGatewayWithoutExplicitURL(t *testing.T) {
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", "https://hosted.example.test/rdev")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "handoff", "--target", "auto"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion    string         `json:"schema_version"`
		SelectedPath     string         `json:"selected_path"`
		MCPNextTool      string         `json:"mcp_next_tool"`
		GatewayURL       string         `json:"gateway_url"`
		MCPNextArguments map[string]any `json:"mcp_next_arguments"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session handoff JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-handoff.v1" ||
		payload.SelectedPath != "create-with-reachable-gateway" ||
		payload.MCPNextTool != "rdev.support_session.create" ||
		payload.GatewayURL != "https://hosted.example.test/rdev" ||
		payload.MCPNextArguments["gateway_url"] != "https://hosted.example.test/rdev" {
		t.Fatalf("expected configured gateway handoff route, got %#v", payload)
	}
}

func TestSupportSessionPlanDefaultGatewayDoesNotUseWildcardURL(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	if err := app.Run(context.Background(), []string{"support-session", "plan", "--addr", "0.0.0.0:8787"}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		GatewayURL           string `json:"gateway_url"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		Commands map[string][]string `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid support session plan JSON: %v\n%s", err, stdout.String())
	}
	if payload.GatewayURL == "" ||
		strings.Contains(payload.GatewayURL, "0.0.0.0") ||
		strings.Contains(payload.GatewayURL, "[::]") {
		t.Fatalf("gateway URL should be target-usable, got %q", payload.GatewayURL)
	}
	createInvite := strings.Join(payload.Commands["create_invite_cli"], "\x00")
	watch := strings.Join(payload.Commands["watch_connection_status"], "\x00")
	if strings.Contains(createInvite, "0.0.0.0") || strings.Contains(watch, "0.0.0.0") {
		t.Fatalf("target-facing commands must not contain wildcard gateway URLs:\ncreate=%s\nwatch=%s", createInvite, watch)
	}
	if len(payload.GatewayURLCandidates) == 0 || !payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("expected a recommended gateway candidate, got %#v", payload.GatewayURLCandidates)
	}
}

func TestSupportSessionPrepareBuildsHelperAssetsForOneCommandTargets(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd))
	workDir := filepath.Join(t.TempDir(), "support")

	if err := app.Run(context.Background(), []string{
		"support-session", "prepare",
		"--repo-root", repoRoot,
		"--work-dir", workDir,
		"--gateway-url", "http://127.0.0.1:8787",
		"--target", "windows",
		"--build-assets",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion       string `json:"schema_version"`
		RepoRootValid       bool   `json:"repo_root_valid"`
		ConnectionReadiness struct {
			Ready                     bool `json:"ready"`
			TargetBootstrapSelfRepair bool `json:"target_bootstrap_self_repair"`
			HumanGetsOneCommand       bool `json:"human_gets_one_command"`
		} `json:"connection_readiness"`
		ConnectivityStrategy struct {
			SchemaVersion      string   `json:"schema_version"`
			SelectionOrder     []string `json:"selection_order"`
			AutomaticDowngrade []string `json:"automatic_downgrade"`
		} `json:"connectivity_strategy"`
		GatewayCandidatePreflight struct {
			SchemaVersion  string `json:"schema_version"`
			PreflightMode  string `json:"preflight_mode"`
			CandidateCount int    `json:"candidate_count"`
			AgentRule      string `json:"agent_rule"`
		} `json:"gateway_candidate_preflight"`
		AgentConnectionRunbook struct {
			SchemaVersion string   `json:"schema_version"`
			Phase         string   `json:"phase"`
			Sequence      []string `json:"sequence"`
		} `json:"agent_connection_runbook"`
		AssetReport struct {
			SchemaVersion string `json:"schema_version"`
			AllReady      bool   `json:"all_ready"`
			BuildAssets   bool   `json:"build_assets"`
			Assets        []struct {
				ID          string `json:"id"`
				Present     bool   `json:"present"`
				BuildStatus string `json:"build_status"`
				SHA256      string `json:"sha256"`
			} `json:"assets"`
		} `json:"asset_report"`
		StandardRecovery []string `json:"standard_recovery"`
		Forbidden        []string `json:"forbidden"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid prepare JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-prepare.v1" ||
		!payload.RepoRootValid ||
		!payload.ConnectionReadiness.Ready ||
		!payload.ConnectionReadiness.TargetBootstrapSelfRepair ||
		!payload.ConnectionReadiness.HumanGetsOneCommand ||
		payload.ConnectivityStrategy.SchemaVersion != "rdev.support-session-connectivity-strategy.v1" ||
		!slices.Contains(payload.ConnectivityStrategy.SelectionOrder, "native-lan-gateway") ||
		!slices.Contains(payload.ConnectivityStrategy.SelectionOrder, "existing-frp-or-chisel-relay") ||
		payload.GatewayCandidatePreflight.SchemaVersion != "rdev.support-session-gateway-candidate-preflight.v1" ||
		payload.GatewayCandidatePreflight.PreflightMode != "local-classification-no-network-scan" ||
		payload.GatewayCandidatePreflight.CandidateCount == 0 ||
		!strings.Contains(payload.GatewayCandidatePreflight.AgentRule, "target command owns ordered URL fallback") ||
		payload.AgentConnectionRunbook.SchemaVersion != "rdev.support-session-agent-runbook.v1" ||
		payload.AgentConnectionRunbook.Phase != "prepare" ||
		!slices.Contains(payload.AgentConnectionRunbook.Sequence, "send only target_handoff_envelope.full_text to the target-side human") ||
		!payload.AssetReport.AllReady ||
		!payload.AssetReport.BuildAssets ||
		len(payload.AssetReport.Assets) != 5 {
		t.Fatalf("unexpected prepare payload: %#v", payload)
	}
	for _, asset := range payload.AssetReport.Assets {
		if !asset.Present || asset.SHA256 == "" || (asset.BuildStatus != "built" && asset.BuildStatus != "not-requested") {
			t.Fatalf("expected present hashed asset, got %#v", asset)
		}
	}
	if !slices.Contains(payload.Forbidden, "ad hoc bootstrap code") ||
		!slices.Contains(payload.StandardRecovery, "do not write custom PowerShell, relay, approval polling, ticket substitution, or bootstrap glue") {
		t.Fatalf("expected standard guardrails, got recovery=%#v forbidden=%#v", payload.StandardRecovery, payload.Forbidden)
	}
}

func TestSupportSessionCreateReturnsReadyTargetCommandAndWatcher(t *testing.T) {
	t.Setenv("RDEV_RELAY_GATEWAY_URL", "https://relay.example.test/rdev")
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"support-session", "create",
		"--gateway-url", server.URL,
		"--target", "windows",
		"--reason", "company computer support",
		"--locale", "zh-CN",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string `json:"schema_version"`
		TicketCode           string `json:"ticket_code"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Kind        string `json:"kind"`
			Source      string `json:"source"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		TargetCommand         string            `json:"target_command"`
		TargetCommands        map[string]string `json:"target_commands"`
		WatchConnectionStatus []string          `json:"watch_connection_status"`
		UserHandoff           struct {
			SchemaVersion string `json:"schema_version"`
			CopyPasteKind string `json:"copy_paste_kind"`
			CopyPaste     string `json:"copy_paste"`
			Message       string `json:"message"`
			AgentNextStep string `json:"agent_next_step"`
		} `json:"user_handoff"`
		ConnectionAttemptPolicy struct {
			SchemaVersion             string `json:"schema_version"`
			WindowsDownloadTimeoutSec int    `json:"windows_download_timeout_sec"`
			CurlConnectTimeoutSec     int    `json:"curl_connect_timeout_sec"`
			CurlMaxTimeSec            int    `json:"curl_max_time_sec"`
			RetriesPerCandidate       int    `json:"retries_per_candidate"`
		} `json:"connection_attempt_policy"`
		ConnectionContinuityPolicy struct {
			SchemaVersion               string   `json:"schema_version"`
			StableAfterLANChange        bool     `json:"stable_after_lan_change"`
			HasStableConfiguredFallback bool     `json:"has_stable_configured_fallback"`
			StableFallbackKinds         []string `json:"stable_fallback_kinds"`
			Assessment                  string   `json:"assessment"`
			AgentRule                   string   `json:"agent_rule"`
		} `json:"connection_continuity_policy"`
		TargetBootstrapRequirements struct {
			SchemaVersion  string   `json:"schema_version"`
			RequiredAssets []string `json:"required_assets"`
			StandardFix    []string `json:"standard_fix"`
			Forbidden      []string `json:"forbidden"`
		} `json:"target_bootstrap_requirements"`
		TargetBootstrapReadiness struct {
			SchemaVersion string `json:"schema_version"`
			AllReady      bool   `json:"all_ready"`
			AgentRule     string `json:"agent_rule"`
		} `json:"target_bootstrap_readiness"`
		WatchConnectionStatusConfiguredGateway struct {
			Command   []string `json:"command"`
			AgentRule string   `json:"agent_rule"`
		} `json:"watch_connection_status_configured_gateway"`
		RecommendedSurface    string `json:"recommended_surface"`
		AutoApprove           bool   `json:"auto_approve"`
		ManifestRootPublicKey string `json:"manifest_root_public_key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid create JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-created.v1" ||
		payload.TicketCode == "" ||
		payload.RecommendedSurface != "windows" ||
		!payload.AutoApprove ||
		payload.ManifestRootPublicKey == "" {
		t.Fatalf("unexpected support-session create payload: %#v", payload)
	}
	if len(payload.GatewayURLCandidates) == 0 || !payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("expected created payload to carry gateway candidates, got %#v", payload.GatewayURLCandidates)
	}
	if len(payload.GatewayURLCandidates) < 2 ||
		payload.GatewayURLCandidates[1].URL != "https://relay.example.test/rdev" ||
		payload.GatewayURLCandidates[1].Kind != "relay" ||
		payload.GatewayURLCandidates[1].Source != "env:RDEV_RELAY_GATEWAY_URL" {
		t.Fatalf("expected configured relay fallback candidate, got %#v", payload.GatewayURLCandidates)
	}
	if !strings.Contains(payload.TargetCommand, payload.TicketCode) ||
		!strings.Contains(payload.TargetCommand, "bootstrap.ps1") ||
		!strings.Contains(payload.TargetCommand, "foreach ($u in $urls)") ||
		!strings.Contains(payload.TargetCommand, "https://relay.example.test/rdev/join/") ||
		!strings.Contains(payload.TargetCommand, "-TimeoutSec 10") ||
		strings.Contains(payload.TargetCommand, "<ticket-code>") ||
		strings.Contains(payload.TargetCommand, "ExecutionPolicy Bypass") {
		t.Fatalf("target command should be ready and safe: %s", payload.TargetCommand)
	}
	if !strings.Contains(payload.TargetCommands["macos_linux"], payload.TicketCode) ||
		!strings.Contains(payload.TargetCommands["macos_linux"], "for u in") ||
		!strings.Contains(payload.TargetCommands["macos_linux"], "--connect-timeout 2") ||
		!strings.Contains(payload.TargetCommands["macos_linux"], "--max-time 10") {
		t.Fatalf("expected cross-platform command candidates with real ticket: %#v", payload.TargetCommands)
	}
	if payload.ConnectionAttemptPolicy.SchemaVersion != "rdev.connection-attempt-policy.v1" ||
		payload.ConnectionAttemptPolicy.WindowsDownloadTimeoutSec != 10 ||
		payload.ConnectionAttemptPolicy.CurlConnectTimeoutSec != 2 ||
		payload.ConnectionAttemptPolicy.CurlMaxTimeSec != 10 ||
		payload.ConnectionAttemptPolicy.RetriesPerCandidate != 1 {
		t.Fatalf("expected bounded connection attempt policy, got %#v", payload.ConnectionAttemptPolicy)
	}
	if payload.ConnectionContinuityPolicy.SchemaVersion != "rdev.support-session-continuity-policy.v1" ||
		!payload.ConnectionContinuityPolicy.StableAfterLANChange ||
		!payload.ConnectionContinuityPolicy.HasStableConfiguredFallback ||
		!slices.Contains(payload.ConnectionContinuityPolicy.StableFallbackKinds, "relay") ||
		payload.ConnectionContinuityPolicy.Assessment != "stable-fallback-configured" ||
		!strings.Contains(payload.ConnectionContinuityPolicy.AgentRule, "opportunistic first path") {
		t.Fatalf("expected configured relay continuity policy, got %#v", payload.ConnectionContinuityPolicy)
	}
	if payload.TargetBootstrapRequirements.SchemaVersion != "rdev.support-session-target-bootstrap-requirements.v1" ||
		!slices.Contains(payload.TargetBootstrapRequirements.RequiredAssets, "rdev-windows-amd64.exe") ||
		!slices.Contains(payload.TargetBootstrapRequirements.StandardFix, "rdev support-session connect --start") ||
		!slices.Contains(payload.TargetBootstrapRequirements.Forbidden, "using ExecutionPolicy Bypass") {
		t.Fatalf("expected Windows bootstrap requirements and standard recovery, got %#v", payload.TargetBootstrapRequirements)
	}
	if payload.TargetBootstrapReadiness.SchemaVersion != "rdev.support-session-target-bootstrap-readiness.v1" ||
		payload.TargetBootstrapReadiness.AllReady ||
		!strings.Contains(payload.TargetBootstrapReadiness.AgentRule, "support-session connect --start") {
		t.Fatalf("expected create to report missing gateway helper assets, got %#v", payload.TargetBootstrapReadiness)
	}
	if payload.UserHandoff.SchemaVersion != "rdev.support-session-user-handoff.v1" ||
		payload.UserHandoff.CopyPasteKind != "windows" ||
		payload.UserHandoff.CopyPaste != payload.TargetCommand ||
		!strings.Contains(payload.UserHandoff.Message, "目标电脑") ||
		!strings.Contains(payload.UserHandoff.AgentNextStep, "wait=true") {
		t.Fatalf("expected ready user handoff, got %#v", payload.UserHandoff)
	}
	watch := strings.Join(payload.WatchConnectionStatus, "\x00")
	if !strings.Contains(watch, payload.TicketCode) ||
		strings.Contains(watch, "<ticket-code>") ||
		!strings.Contains(watch, "--wait") {
		t.Fatalf("watch command should be ready: %#v", payload.WatchConnectionStatus)
	}
	configuredWatch := strings.Join(payload.WatchConnectionStatusConfiguredGateway.Command, "\x00")
	if !strings.Contains(configuredWatch, payload.TicketCode) ||
		!strings.Contains(configuredWatch, "--wait") ||
		strings.Contains(configuredWatch, "--gateway-url") ||
		!strings.Contains(payload.WatchConnectionStatusConfiguredGateway.AgentRule, "RDEV_*_GATEWAY_URL") {
		t.Fatalf("configured gateway watcher should be ready and omit gateway URL: %#v", payload.WatchConnectionStatusConfiguredGateway)
	}
}

func TestSupportSessionCreateUsesConfiguredGatewayURL(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", server.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"support-session", "create",
		"--target", "auto",
		"--reason", "company computer support",
		"--locale", "en",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion        string `json:"schema_version"`
		GatewayURL           string `json:"gateway_url"`
		TicketCode           string `json:"ticket_code"`
		GatewayURLCandidates []struct {
			URL         string `json:"url"`
			Recommended bool   `json:"recommended"`
		} `json:"gateway_url_candidates"`
		TargetCommand string `json:"target_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid create JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-created.v1" ||
		payload.GatewayURL != server.URL ||
		payload.TicketCode == "" ||
		payload.TargetCommand != server.URL+"/join/"+payload.TicketCode {
		t.Fatalf("expected configured gateway create payload, got %#v", payload)
	}
	if len(payload.GatewayURLCandidates) == 0 ||
		payload.GatewayURLCandidates[0].URL != server.URL ||
		!payload.GatewayURLCandidates[0].Recommended {
		t.Fatalf("expected configured gateway candidate, got %#v", payload.GatewayURLCandidates)
	}
}

func TestSupportSessionStartServesGatewayAndPrintsReadySession(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	gatewayURL := "http://" + addr
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	workDir := filepath.Join(t.TempDir(), "support")
	readyFile := filepath.Join(workDir, "ready", "support-session-ready.json")
	statusFile := filepath.Join(workDir, "status", "support-session-status.json")
	handoffTextFile := filepath.Join(workDir, "handoff", "target-handoff.txt")
	connectedReportFile := filepath.Join(workDir, "status", "connected-report.txt")
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(ctx, []string{
			"support-session", "start",
			"--addr", addr,
			"--gateway-url", gatewayURL,
			"--work-dir", workDir,
			"--ready-file", readyFile,
			"--status-file", statusFile,
			"--handoff-text-file", handoffTextFile,
			"--connected-report-file", connectedReportFile,
			"--target", "windows",
			"--locale", "zh-CN",
		})
	}()
	waitForHTTP(t, gatewayURL+"/healthz")
	resp, err := http.Get(gatewayURL + "/assets/rdev-windows-amd64.exe.sha256")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("support-session start should serve Windows helper hash, got %s", resp.Status)
	}
	cancel()
	err = <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled foreground server, got %v", err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Gateway       struct {
			Addr       string `json:"addr"`
			GatewayURL string `json:"gateway_url"`
			Lifecycle  string `json:"lifecycle"`
		} `json:"gateway"`
		AssetReport struct {
			SchemaVersion string `json:"schema_version"`
			AllReady      bool   `json:"all_ready"`
		} `json:"asset_report"`
		ConnectionReadiness struct {
			Ready                     bool `json:"ready"`
			TargetBootstrapSelfRepair bool `json:"target_bootstrap_self_repair"`
		} `json:"connection_readiness"`
		ConnectivityStrategy struct {
			SchemaVersion  string   `json:"schema_version"`
			SelectionOrder []string `json:"selection_order"`
		} `json:"connectivity_strategy"`
		GatewayCandidatePreflight struct {
			SchemaVersion  string `json:"schema_version"`
			CandidateCount int    `json:"candidate_count"`
			AgentRule      string `json:"agent_rule"`
		} `json:"gateway_candidate_preflight"`
		AgentConnectionRunbook struct {
			SchemaVersion string `json:"schema_version"`
			Phase         string `json:"phase"`
			Watch         struct {
				MCPTool string `json:"mcp_tool"`
			} `json:"watch"`
		} `json:"agent_connection_runbook"`
		ReadyFile struct {
			SchemaVersion string `json:"schema_version"`
			Path          string `json:"path"`
			Contains      string `json:"contains"`
			AgentRule     string `json:"agent_rule"`
		} `json:"ready_file"`
		StatusFile struct {
			SchemaVersion       string `json:"schema_version"`
			Path                string `json:"path"`
			Contains            string `json:"contains"`
			StatusSchemaVersion string `json:"status_schema_version"`
			AgentRule           string `json:"agent_rule"`
		} `json:"status_file"`
		HandoffTextFile struct {
			SchemaVersion string `json:"schema_version"`
			Path          string `json:"path"`
			Contains      string `json:"contains"`
			AgentRule     string `json:"agent_rule"`
		} `json:"handoff_text_file"`
		ConnectedReportFile struct {
			SchemaVersion string `json:"schema_version"`
			Path          string `json:"path"`
			Contains      string `json:"contains"`
			AgentRule     string `json:"agent_rule"`
		} `json:"connected_report_file"`
		ReadyToSendHuman bool   `json:"ready_to_send_to_human"`
		TargetCommand    string `json:"target_command"`
		JoinURL          string `json:"join_url"`
		UserHandoff      struct {
			SchemaVersion string `json:"schema_version"`
			CopyPaste     string `json:"copy_paste"`
		} `json:"user_handoff"`
		WatchConnectionStatus []string `json:"watch_connection_status"`
		Session               struct {
			SchemaVersion         string   `json:"schema_version"`
			TicketCode            string   `json:"ticket_code"`
			TargetCommand         string   `json:"target_command"`
			WatchConnectionStatus []string `json:"watch_connection_status"`
			AutoApprove           bool     `json:"auto_approve"`
		} `json:"session"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid start JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-started.v1" ||
		payload.Gateway.Addr != addr ||
		payload.Gateway.GatewayURL != gatewayURL ||
		payload.Gateway.Lifecycle != "foreground-visible-process" {
		t.Fatalf("unexpected started payload: %#v", payload)
	}
	if payload.AssetReport.SchemaVersion != "rdev.support-session-assets.v1" ||
		!payload.AssetReport.AllReady ||
		!payload.ConnectionReadiness.Ready ||
		!payload.ConnectionReadiness.TargetBootstrapSelfRepair ||
		payload.ConnectivityStrategy.SchemaVersion != "rdev.support-session-connectivity-strategy.v1" ||
		!slices.Contains(payload.ConnectivityStrategy.SelectionOrder, "existing-wireguard-vpn") {
		t.Fatalf("expected support-session start to prepare helper assets, got %#v", payload)
	}
	if payload.GatewayCandidatePreflight.SchemaVersion != "rdev.support-session-gateway-candidate-preflight.v1" ||
		payload.GatewayCandidatePreflight.CandidateCount == 0 ||
		!strings.Contains(payload.GatewayCandidatePreflight.AgentRule, "candidate table") {
		t.Fatalf("expected started payload gateway preflight, got %#v", payload.GatewayCandidatePreflight)
	}
	if payload.AgentConnectionRunbook.SchemaVersion != "rdev.support-session-agent-runbook.v1" ||
		payload.AgentConnectionRunbook.Watch.MCPTool != "rdev.support_session.status" {
		t.Fatalf("expected started payload Agent runbook, got %#v", payload.AgentConnectionRunbook)
	}
	if payload.ReadyFile.SchemaVersion != "rdev.support-session-ready-file.v1" ||
		payload.ReadyFile.Path != readyFile ||
		payload.ReadyFile.Contains != "rdev.support-session-started.v1" ||
		!strings.Contains(payload.ReadyFile.AgentRule, "target_handoff_envelope.full_text") {
		t.Fatalf("expected ready-file metadata, got %#v", payload.ReadyFile)
	}
	if payload.StatusFile.SchemaVersion != "rdev.support-session-status-file.v1" ||
		payload.StatusFile.Path != statusFile ||
		payload.StatusFile.Contains != "rdev.support-session-foreground-event.v1" ||
		payload.StatusFile.StatusSchemaVersion != "rdev.support-session-status.v1" ||
		!strings.Contains(payload.StatusFile.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("expected status-file metadata, got %#v", payload.StatusFile)
	}
	if payload.HandoffTextFile.SchemaVersion != "rdev.support-session-handoff-text-file.v1" ||
		payload.HandoffTextFile.Path != handoffTextFile ||
		payload.HandoffTextFile.Contains != "target_handoff_envelope.full_text" ||
		!strings.Contains(payload.HandoffTextFile.AgentRule, "plain-text") {
		t.Fatalf("expected handoff text-file metadata, got %#v", payload.HandoffTextFile)
	}
	if payload.ConnectedReportFile.SchemaVersion != "rdev.support-session-connected-report-file.v1" ||
		payload.ConnectedReportFile.Path != connectedReportFile ||
		payload.ConnectedReportFile.Contains != "connected_next_steps.user_report" ||
		!strings.Contains(payload.ConnectedReportFile.AgentRule, "plain text") {
		t.Fatalf("expected connected report-file metadata, got %#v", payload.ConnectedReportFile)
	}
	if !payload.ReadyToSendHuman ||
		payload.UserHandoff.SchemaVersion != "rdev.support-session-user-handoff.v1" ||
		payload.UserHandoff.CopyPaste != payload.TargetCommand ||
		payload.TargetCommand != payload.Session.TargetCommand ||
		payload.JoinURL == "" {
		t.Fatalf("expected top-level started handoff fields, got %#v", payload)
	}
	if payload.Session.SchemaVersion != "rdev.support-session-created.v1" ||
		payload.Session.TicketCode == "" ||
		!payload.Session.AutoApprove ||
		!strings.Contains(payload.Session.TargetCommand, payload.Session.TicketCode) ||
		strings.Contains(payload.Session.TargetCommand, "<ticket-code>") ||
		strings.Contains(payload.Session.TargetCommand, "ExecutionPolicy Bypass") {
		t.Fatalf("expected ready embedded session, got %#v", payload.Session)
	}
	watch := strings.Join(payload.Session.WatchConnectionStatus, "\x00")
	if !strings.Contains(watch, payload.Session.TicketCode) || !strings.Contains(watch, "--wait") {
		t.Fatalf("expected ready watcher, got %#v", payload.Session.WatchConnectionStatus)
	}
	topLevelWatch := strings.Join(payload.WatchConnectionStatus, "\x00")
	if !strings.Contains(topLevelWatch, payload.Session.TicketCode) || !strings.Contains(topLevelWatch, "--wait") {
		t.Fatalf("expected top-level ready watcher, got %#v", payload.WatchConnectionStatus)
	}
	readyInfo, err := os.Stat(readyFile)
	if err != nil {
		t.Fatal(err)
	}
	if readyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected ready file permissions 0600, got %v", readyInfo.Mode().Perm())
	}
	statusInfo, err := os.Stat(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if statusInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected status file permissions 0600, got %v", statusInfo.Mode().Perm())
	}
	handoffBytes, err := os.ReadFile(handoffTextFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(handoffBytes), payload.Session.TicketCode) ||
		!strings.Contains(string(handoffBytes), payload.TargetCommand) {
		t.Fatalf("expected handoff text file to contain final target command, got %q", string(handoffBytes))
	}
	var readyPayload struct {
		SchemaVersion string `json:"schema_version"`
		ReadyFile     struct {
			Path string `json:"path"`
		} `json:"ready_file"`
		Session struct {
			TicketCode  string `json:"ticket_code"`
			UserHandoff struct {
				CopyPaste string `json:"copy_paste"`
			} `json:"user_handoff"`
		} `json:"session"`
	}
	readyBytes, err := os.ReadFile(readyFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readyBytes, &readyPayload); err != nil {
		t.Fatalf("invalid ready file JSON: %v\n%s", err, string(readyBytes))
	}
	if readyPayload.SchemaVersion != "rdev.support-session-started.v1" ||
		readyPayload.ReadyFile.Path != readyFile ||
		readyPayload.Session.TicketCode != payload.Session.TicketCode ||
		!strings.Contains(readyPayload.Session.UserHandoff.CopyPaste, payload.Session.TicketCode) {
		t.Fatalf("expected ready file to mirror started payload, got %#v", readyPayload)
	}
	var statusPayload struct {
		SchemaVersion string `json:"schema_version"`
		Event         string `json:"event"`
		Status        struct {
			SchemaVersion string `json:"schema_version"`
			TicketCode    string `json:"ticket_code"`
			Status        string `json:"status"`
		} `json:"status"`
		AgentRule string `json:"agent_rule"`
	}
	statusBytes, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(statusBytes, &statusPayload); err != nil {
		t.Fatalf("invalid status file JSON: %v\n%s", err, string(statusBytes))
	}
	if statusPayload.SchemaVersion != "rdev.support-session-foreground-event.v1" ||
		statusPayload.Event != "waiting" ||
		statusPayload.Status.SchemaVersion != "rdev.support-session-status.v1" ||
		statusPayload.Status.TicketCode != payload.Session.TicketCode ||
		!strings.Contains(statusPayload.AgentRule, "connected_next_steps.user_report") {
		t.Fatalf("expected status file to mirror foreground event, got %#v", statusPayload)
	}
}

func TestWriteJSONFile0600TightensExistingFilePermissions(t *testing.T) {
	readyFile := filepath.Join(t.TempDir(), "ready.json")
	if err := os.WriteFile(readyFile, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeJSONFile0600(readyFile, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(readyFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected existing ready file permissions to tighten to 0600, got %v", info.Mode().Perm())
	}
}

func TestGatewayAssetConfigUsesDirectoryWithExplicitOverrides(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	override := filepath.Join(t.TempDir(), "custom-rdev.exe")
	assets := gatewayAssetConfig(gatewayServeOptions{
		RdevAssetsDir:        dir,
		RdevWindowsAMD64Path: override,
	})
	if assets.RdevWindowsAMD64Path != override {
		t.Fatalf("explicit Windows helper should override assets dir: %#v", assets)
	}
	if assets.RdevDarwinARM64Path != filepath.Join(dir, "rdev-darwin-arm64") ||
		assets.RdevDarwinAMD64Path != filepath.Join(dir, "rdev-darwin-amd64") ||
		assets.RdevLinuxAMD64Path != filepath.Join(dir, "rdev-linux-amd64") ||
		assets.RdevLinuxARM64Path != filepath.Join(dir, "rdev-linux-arm64") {
		t.Fatalf("assets dir should populate platform helper paths: %#v", assets)
	}
}

func TestGatewayServeDevAutoBuildsRdevAssets(t *testing.T) {
	assetsDir, ready, err := prepareGatewayAutoBuildRdevAssets(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatalf("expected gateway helper assets to be ready in %s", assetsDir)
	}
	assets := gatewayAssetConfig(gatewayServeOptions{RdevAssetsDir: assetsDir})
	if assets.RdevWindowsAMD64Path == "" {
		t.Fatalf("expected auto-built Windows helper asset path, got %#v", assets)
	}
	info, err := os.Stat(assets.RdevWindowsAMD64Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() || info.Size() == 0 {
		t.Fatalf("expected non-empty Windows helper asset, got %#v", info)
	}
}

func waitForHTTP(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return
			}
			lastErr = fmt.Errorf("status %s", resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", endpoint, lastErr)
}

func TestSupportSessionStatusReportsConnectionFeedback(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "company computer support", map[string]string{
		"auto_approve": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-dev",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	if err := app.Run(context.Background(), []string{
		"support-session", "status",
		"--gateway-url", server.URL,
		"--ticket-code", ticket.Code,
		"--locale", "zh-CN",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Connected     bool   `json:"connected"`
		Status        string `json:"status"`
		Feedback      string `json:"feedback"`
		NextAction    string `json:"next_action"`
		ConnectedNext struct {
			SchemaVersion string `json:"schema_version"`
			Connected     bool   `json:"connected"`
			HostID        string `json:"host_id"`
			UserReport    string `json:"user_report"`
			MCPNextCalls  []struct {
				Tool      string         `json:"tool"`
				Arguments map[string]any `json:"arguments"`
			} `json:"mcp_next_calls"`
		} `json:"connected_next_steps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-status.v1" ||
		!payload.Connected ||
		payload.Status != "connected" ||
		!strings.Contains(payload.Feedback, "连接已经建立") ||
		!strings.Contains(payload.NextAction, "汇报连接已建立") {
		t.Fatalf("unexpected support-session status: %#v", payload)
	}
	if payload.ConnectedNext.SchemaVersion != "rdev.support-session-connected-next-steps.v1" ||
		!payload.ConnectedNext.Connected ||
		payload.ConnectedNext.HostID == "" ||
		!strings.Contains(payload.ConnectedNext.UserReport, "连接已经建立") ||
		len(payload.ConnectedNext.MCPNextCalls) != 1 ||
		payload.ConnectedNext.MCPNextCalls[0].Tool != "rdev.hosts.capabilities" {
		t.Fatalf("unexpected connected next-step contract: %#v", payload.ConnectedNext)
	}
}

func TestSupportSessionStatusUsesConfiguredGatewayURL(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("RDEV_HOSTED_GATEWAY_URL", server.URL)

	ticket, err := gw.CreateTicketWithMetadata(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "company computer support", map[string]string{
		"auto_approve": "attended-temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "win-dev",
		OS:           "windows",
		Arch:         "amd64",
		Capabilities: []string{"shell.user"},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"support-session", "status",
		"--ticket-code", ticket.Code,
		"--locale", "en",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Connected     bool   `json:"connected"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, stdout.String())
	}
	if payload.SchemaVersion != "rdev.support-session-status.v1" ||
		!payload.Connected ||
		payload.Status != "connected" {
		t.Fatalf("expected configured gateway status feedback, got %#v", payload)
	}
}

func TestSupportSessionStatusWaitTimeoutIncludesRecovery(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "visible support")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"support-session", "status",
		"--gateway-url", server.URL,
		"--ticket-code", ticket.Code,
		"--wait",
		"--timeout", "1ms",
		"--interval", "1ms",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK                 bool           `json:"ok"`
		TimedOut           bool           `json:"timed_out"`
		ConnectionRecovery map[string]any `json:"connection_recovery"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, stdout.String())
	}
	actions, _ := payload.ConnectionRecovery["agent_next_actions"].([]any)
	forbidden, _ := payload.ConnectionRecovery["forbidden"].([]any)
	if payload.OK ||
		!payload.TimedOut ||
		payload.ConnectionRecovery["schema_version"] != "rdev.support-session-connection-recovery.v1" ||
		payload.ConnectionRecovery["timed_out"] != true ||
		!strings.Contains(strings.Join(anyStrings(actions), "\n"), "connection-entry failure") ||
		!strings.Contains(strings.Join(anyStrings(forbidden), "\n"), "Agent-authored PowerShell") {
		t.Fatalf("expected wait timeout recovery contract, got %#v", payload)
	}
}

func TestInviteCreateUsesGatewayAndOutputsAgentPlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	handler := httpapi.NewServer(gw).Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--capabilities", "shell.user,codex.run,git.diff",
		"--transport", "wss",
	})
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		SchemaVersion         string `json:"schema_version"`
		GatewayURL            string `json:"gateway_url"`
		ManifestURL           string `json:"manifest_url"`
		ManifestRootPublicKey string `json:"manifest_root_public_key"`
		Transport             string `json:"transport"`
		TransportPlan         struct {
			SchemaVersion string `json:"schema_version"`
			Mode          string `json:"mode"`
			Candidates    []struct {
				Transport   string `json:"transport"`
				HostCommand string `json:"host_command"`
			} `json:"candidates"`
		} `json:"transport_plan"`
		ConnectionPlan struct {
			SchemaVersion       string `json:"schema_version"`
			NetworkScope        string `json:"network_scope"`
			GatewayReachability string `json:"gateway_reachability"`
			Implemented         []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"implemented"`
			AgentManaged []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"agent_managed"`
			DiscoveryPlan struct {
				SchemaVersion string   `json:"schema_version"`
				Allowed       []string `json:"allowed"`
			} `json:"discovery_plan"`
			SelectionOrder    []string `json:"selection_order"`
			EnvironmentProbes []string `json:"environment_probes"`
		} `json:"connection_plan"`
		AuthorityProfile struct {
			SchemaVersion  string `json:"schema_version"`
			Profile        string `json:"profile"`
			RemoteHostRole string `json:"remote_host_role"`
			Discovery      struct {
				Allowed bool   `json:"allowed"`
				Scope   string `json:"scope"`
			} `json:"discovery"`
			DownstreamControl struct {
				Allowed bool   `json:"allowed"`
				Scope   string `json:"scope"`
			} `json:"downstream_control"`
			RequiredCapabilities []string `json:"required_capabilities"`
			ControlPaths         []struct {
				ID string `json:"id"`
			} `json:"control_paths"`
		} `json:"authority_profile"`
		ConnectionEntry struct {
			SchemaVersion   string   `json:"schema_version"`
			HandoffName     string   `json:"handoff_name"`
			HandoffContract []string `json:"handoff_contract"`
			EntryURL        string   `json:"entry_url"`
			AutomationLevel string   `json:"automation_level"`
			PackageCatalog  struct {
				SchemaVersion string `json:"schema_version"`
				Candidates    []struct {
					ID                   string `json:"id"`
					PackageStatus        string `json:"package_status"`
					FallbackScriptStatus string `json:"fallback_script_status"`
				} `json:"candidates"`
			} `json:"package_catalog"`
			OneLineCommands map[string]string `json:"one_line_commands"`
			HumanSteps      []string          `json:"human_steps"`
		} `json:"connection_entry"`
		ConnectionEntryPlan struct {
			SchemaVersion         string   `json:"schema_version"`
			Mode                  string   `json:"mode"`
			PackagePlanSchema     string   `json:"package_plan_schema"`
			EntryModes            []string `json:"entry_modes"`
			TargetSelectionPolicy struct {
				SchemaVersion         string   `json:"schema_version"`
				DecisionOwner         string   `json:"decision_owner"`
				DefaultOwnedMode      string   `json:"default_owned_mode"`
				DefaultThirdPartyMode string   `json:"default_third_party_mode"`
				OwnedSignals          []string `json:"owned_signals"`
				ThirdPartySignals     []string `json:"third_party_signals"`
				AskWhen               []string `json:"ask_when"`
				AgentRules            []string `json:"agent_rules"`
			} `json:"target_selection_policy"`
			ModeSelection      []string `json:"mode_selection"`
			RequiredAgentFlow  []string `json:"required_agent_flow"`
			PackageFormats     []string `json:"package_formats"`
			RequiredContents   []string `json:"required_contents"`
			NetworkStrategy    []string `json:"network_strategy"`
			PrivilegeStrategy  []string `json:"privilege_strategy"`
			ImplementationGaps []string `json:"implementation_gaps"`
		} `json:"connection_entry_plan"`
		HostContextPlan struct {
			SchemaVersion         string   `json:"schema_version"`
			StorageLocation       string   `json:"storage_location"`
			ServerContextBudget   string   `json:"server_context_budget"`
			ProgressiveDisclosure []string `json:"progressive_disclosure"`
			HostLocalStores       []string `json:"host_local_stores"`
			GatewayIndexes        []string `json:"gateway_indexes"`
		} `json:"host_context_plan"`
		ProvisioningPlan struct {
			SchemaVersion       string   `json:"schema_version"`
			Mode                string   `json:"mode"`
			DiscoveryTargets    []string `json:"discovery_targets"`
			AutoInstallAllowed  []string `json:"auto_install_allowed"`
			ApprovalRequiredFor []string `json:"approval_required_for"`
		} `json:"agent_provisioning_plan"`
		CollaborationPlan struct {
			SchemaVersion     string   `json:"schema_version"`
			Mode              string   `json:"mode"`
			Protocols         []string `json:"protocols"`
			DiscoveryTargets  []string `json:"discovery_targets"`
			CollaborationUses []string `json:"collaboration_uses"`
			DelegationRules   []string `json:"delegation_rules"`
		} `json:"agent_collaboration_plan"`
		LocalizationPlan struct {
			SchemaVersion      string   `json:"schema_version"`
			Mode               string   `json:"mode"`
			SupportedLanguages []string `json:"supported_languages"`
			DetectionSources   []string `json:"detection_sources"`
			LocalizedSurfaces  []string `json:"localized_surfaces"`
			FallbackOrder      []string `json:"fallback_order"`
		} `json:"localization_plan"`
		ManagedDevPlan struct {
			SchemaVersion       string   `json:"schema_version"`
			Mode                string   `json:"mode"`
			HostModes           []string `json:"host_modes"`
			ServiceSurfaces     []string `json:"service_surfaces"`
			ReliabilityControls []string `json:"reliability_controls"`
			WorkspaceControls   []string `json:"workspace_controls"`
		} `json:"managed_development_plan"`
		HostCommand        string   `json:"host_command"`
		FallbackCommands   []string `json:"fallback_commands"`
		HumanNextActions   []string `json:"human_next_actions"`
		AgentNextActions   []string `json:"agent_next_actions"`
		ConnectivityChecks []string `json:"connectivity_checks"`
		Ticket             struct {
			Code         string   `json:"code"`
			Capabilities []string `json:"capabilities"`
		} `json:"ticket"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid invite JSON: %v\n%s", err, stdout.String())
	}
	legacyEntryField := "customer" + "_bootstrap"
	legacyPlanField := "connector" + "_package_plan"
	if strings.Contains(stdout.String(), legacyEntryField) || strings.Contains(stdout.String(), legacyPlanField) {
		t.Fatalf("invite JSON should use generic connection entry fields, got %s", stdout.String())
	}
	if payload.SchemaVersion != "rdev.agent-invite.v1" {
		t.Fatalf("unexpected schema: %#v", payload)
	}
	if payload.GatewayURL != server.URL {
		t.Fatalf("expected gateway URL %q, got %#v", server.URL, payload.GatewayURL)
	}
	if payload.Transport != "wss" || payload.TransportPlan.Mode != "wss" || len(payload.TransportPlan.Candidates) != 1 {
		t.Fatalf("explicit WSS invite should keep WSS-only plan: %#v", payload.TransportPlan)
	}
	if !strings.Contains(payload.ManifestURL, "/v1/tickets/") || !strings.Contains(payload.HostCommand, "host serve --manifest-url") || !strings.Contains(payload.HostCommand, "--transport wss") {
		t.Fatalf("invite should include manifest URL and WSS host command: %#v", payload)
	}
	if payload.ManifestRootPublicKey == "" || !strings.Contains(payload.HostCommand, "--manifest-root-public-key") {
		t.Fatalf("invite should carry the manifest root in host_command: %#v", payload)
	}
	if len(payload.TransportPlan.Candidates) == 0 || !strings.Contains(payload.TransportPlan.Candidates[0].HostCommand, "--manifest-root-public-key") {
		t.Fatalf("transport candidates should carry manifest root: %#v", payload.TransportPlan.Candidates)
	}
	if len(payload.HumanNextActions) == 0 || len(payload.AgentNextActions) == 0 || len(payload.ConnectivityChecks) == 0 {
		t.Fatalf("invite should split human and agent actions: %#v", payload)
	}
	if payload.ConnectionPlan.SchemaVersion != "rdev.connection-plan.v1" || len(payload.ConnectionPlan.Implemented) < 4 || len(payload.ConnectionPlan.AgentManaged) < 3 {
		t.Fatalf("invite should include implemented and agent-managed connection options: %#v", payload.ConnectionPlan)
	}
	if payload.ConnectionPlan.DiscoveryPlan.SchemaVersion != "rdev.discovery-plan.v1" || len(payload.ConnectionPlan.DiscoveryPlan.Allowed) == 0 {
		t.Fatalf("invite should include discovery plan: %#v", payload.ConnectionPlan)
	}
	if payload.AuthorityProfile.SchemaVersion != "rdev.agent-authority.v1" || payload.AuthorityProfile.Profile != "max-control" || !payload.AuthorityProfile.Discovery.Allowed || !payload.AuthorityProfile.DownstreamControl.Allowed {
		t.Fatalf("invite should include max-control authority profile: %#v", payload.AuthorityProfile)
	}
	if len(payload.AuthorityProfile.ControlPaths) < 3 || !slices.Contains(payload.AuthorityProfile.RequiredCapabilities, "downstream.control.scoped") {
		t.Fatalf("max-control profile should include downstream control paths and capability: %#v", payload.AuthorityProfile)
	}
	if payload.ConnectionEntry.SchemaVersion != "rdev.connection-entry.v1" ||
		payload.ConnectionEntry.HandoffName != "Connection Entry" ||
		payload.ConnectionEntry.EntryURL == "" ||
		len(payload.ConnectionEntry.OneLineCommands) < 2 ||
		len(payload.ConnectionEntry.HandoffContract) == 0 {
		t.Fatalf("invite should include connection entry: %#v", payload.ConnectionEntry)
	}
	if !slices.Contains(payload.ConnectionEntry.HandoffContract, "Target-side humans must not assemble ticket codes, gateway URLs, manifest roots, transports, release roots, or checksums by hand.") {
		t.Fatalf("connection entry should define the universal handoff contract: %#v", payload.ConnectionEntry.HandoffContract)
	}
	if payload.ConnectionEntry.PackageCatalog.SchemaVersion != model.ConnectionEntryPackageCatalogSchemaVersion ||
		len(payload.ConnectionEntry.PackageCatalog.Candidates) == 0 ||
		payload.ConnectionEntry.PackageCatalog.Candidates[0].PackageStatus != "planned-release-asset-required" ||
		payload.ConnectionEntry.PackageCatalog.Candidates[0].FallbackScriptStatus != "available" {
		t.Fatalf("connection entry should include package catalog candidates: %#v", payload.ConnectionEntry.PackageCatalog)
	}
	if !strings.Contains(payload.ConnectionEntry.OneLineCommands["macos_linux_sh"], "/join/") || !strings.Contains(payload.ConnectionEntry.OneLineCommands["windows_powershell"], "bootstrap.ps1") {
		t.Fatalf("connection entry should include one-link commands: %#v", payload.ConnectionEntry.OneLineCommands)
	}
	if payload.ConnectionEntryPlan.SchemaVersion != "rdev.connection-entry-plan.v1" ||
		payload.ConnectionEntryPlan.Mode != "universal-agent-selected-entry" ||
		payload.ConnectionEntryPlan.PackagePlanSchema != "rdev.connection-entry.package-plan.v1" {
		t.Fatalf("invite should include connection entry plan: %#v", payload.ConnectionEntryPlan)
	}
	if len(payload.ConnectionEntryPlan.EntryModes) < 2 ||
		payload.ConnectionEntryPlan.TargetSelectionPolicy.SchemaVersion != "rdev.target-selection-policy.v1" ||
		payload.ConnectionEntryPlan.TargetSelectionPolicy.DefaultOwnedMode != "managed" ||
		payload.ConnectionEntryPlan.TargetSelectionPolicy.DefaultThirdPartyMode != "attended-temporary" ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.OwnedSignals) == 0 ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.ThirdPartySignals) == 0 ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.AskWhen) == 0 ||
		len(payload.ConnectionEntryPlan.TargetSelectionPolicy.AgentRules) == 0 ||
		len(payload.ConnectionEntryPlan.ModeSelection) == 0 ||
		len(payload.ConnectionEntryPlan.RequiredAgentFlow) == 0 ||
		len(payload.ConnectionEntryPlan.PackageFormats) < 3 ||
		len(payload.ConnectionEntryPlan.RequiredContents) == 0 ||
		len(payload.ConnectionEntryPlan.NetworkStrategy) == 0 ||
		len(payload.ConnectionEntryPlan.PrivilegeStrategy) == 0 ||
		len(payload.ConnectionEntryPlan.ImplementationGaps) == 0 {
		t.Fatalf("connection entry plan should define mode, package, network, privilege, and gap details: %#v", payload.ConnectionEntryPlan)
	}
	if !slices.Contains(payload.ConnectionEntryPlan.RequiredAgentFlow, "materialize the invite with rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions") {
		t.Fatalf("connection entry plan should require invite materialization before target handoff: %#v", payload.ConnectionEntryPlan.RequiredAgentFlow)
	}
	if payload.HostContextPlan.SchemaVersion != "rdev.host-context-plan.v1" || payload.HostContextPlan.StorageLocation != "remote-host-first" || payload.HostContextPlan.ServerContextBudget != "index-and-on-demand-slices" {
		t.Fatalf("invite should include host context plan: %#v", payload.HostContextPlan)
	}
	if len(payload.HostContextPlan.ProgressiveDisclosure) == 0 || len(payload.HostContextPlan.HostLocalStores) == 0 || len(payload.HostContextPlan.GatewayIndexes) == 0 {
		t.Fatalf("host context plan should define progressive disclosure and indexes: %#v", payload.HostContextPlan)
	}
	if payload.ProvisioningPlan.SchemaVersion != "rdev.agent-provisioning-plan.v1" || payload.ProvisioningPlan.Mode != "adaptive-host-local" {
		t.Fatalf("invite should include agent provisioning plan: %#v", payload.ProvisioningPlan)
	}
	if len(payload.ProvisioningPlan.DiscoveryTargets) == 0 || len(payload.ProvisioningPlan.AutoInstallAllowed) == 0 || len(payload.ProvisioningPlan.ApprovalRequiredFor) == 0 {
		t.Fatalf("provisioning plan should define discovery, auto-install, and approval rules: %#v", payload.ProvisioningPlan)
	}
	if payload.CollaborationPlan.SchemaVersion != "rdev.agent-collaboration-plan.v1" || payload.CollaborationPlan.Mode != "host-local-peer-collaboration" {
		t.Fatalf("invite should include agent collaboration plan: %#v", payload.CollaborationPlan)
	}
	if !slices.Contains(payload.CollaborationPlan.Protocols, "a2a-agent-card") || len(payload.CollaborationPlan.DiscoveryTargets) == 0 || len(payload.CollaborationPlan.CollaborationUses) == 0 || len(payload.CollaborationPlan.DelegationRules) == 0 {
		t.Fatalf("collaboration plan should include A2A discovery and delegation rules: %#v", payload.CollaborationPlan)
	}
	if payload.LocalizationPlan.SchemaVersion != "rdev.localization-plan.v1" || payload.LocalizationPlan.Mode != "target-host-language-auto" {
		t.Fatalf("invite should include localization plan: %#v", payload.LocalizationPlan)
	}
	if !slices.Contains(payload.LocalizationPlan.SupportedLanguages, "zh-CN") || !slices.Contains(payload.LocalizationPlan.SupportedLanguages, "ar") || len(payload.LocalizationPlan.DetectionSources) == 0 || len(payload.LocalizationPlan.LocalizedSurfaces) == 0 || len(payload.LocalizationPlan.FallbackOrder) == 0 {
		t.Fatalf("localization plan should define languages, detection, surfaces, and fallback: %#v", payload.LocalizationPlan)
	}
	if payload.ManagedDevPlan.SchemaVersion != "rdev.managed-development-plan.v1" || payload.ManagedDevPlan.Mode != "owned-long-running-developer-workstation" {
		t.Fatalf("invite should include managed development plan: %#v", payload.ManagedDevPlan)
	}
	if !slices.Contains(payload.ManagedDevPlan.HostModes, "managed") || len(payload.ManagedDevPlan.ServiceSurfaces) == 0 || len(payload.ManagedDevPlan.ReliabilityControls) == 0 || len(payload.ManagedDevPlan.WorkspaceControls) == 0 {
		t.Fatalf("managed development plan should define modes, service surfaces, reliability, and workspace controls: %#v", payload.ManagedDevPlan)
	}
	privateHomePath := strings.Join([]string{"", "Users", "sample-user"}, "/")
	privateWorkspaceMarker := strings.Join([]string{"Documents", "SampleWorkspace"}, "/")
	if strings.Contains(stdout.String(), privateHomePath) || strings.Contains(stdout.String(), privateWorkspaceMarker) {
		t.Fatalf("invite leaked local private path: %s", stdout.String())
	}
}

func TestInviteCreateDefaultsToAutoTransportPlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Transport             string   `json:"transport"`
		ManifestRootPublicKey string   `json:"manifest_root_public_key"`
		HostCommand           string   `json:"host_command"`
		FallbackCommands      []string `json:"fallback_commands"`
		TransportPlan         struct {
			Mode       string `json:"mode"`
			Candidates []struct {
				Transport string `json:"transport"`
			} `json:"candidates"`
		} `json:"transport_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid invite JSON: %v\n%s", err, stdout.String())
	}
	if payload.Transport != "auto" || !strings.Contains(payload.HostCommand, "--transport auto") {
		t.Fatalf("expected auto host command, got %#v", payload)
	}
	if payload.ManifestRootPublicKey == "" || !strings.Contains(payload.HostCommand, "--manifest-root-public-key") {
		t.Fatalf("expected auto host command to include manifest root, got %#v", payload)
	}
	if payload.TransportPlan.Mode != "auto" || len(payload.TransportPlan.Candidates) != 3 {
		t.Fatalf("expected three transport candidates, got %#v", payload.TransportPlan)
	}
	if payload.TransportPlan.Candidates[0].Transport != "wss" || payload.TransportPlan.Candidates[1].Transport != "long-poll" || payload.TransportPlan.Candidates[2].Transport != "poll" {
		t.Fatalf("unexpected transport fallback order: %#v", payload.TransportPlan.Candidates)
	}
	if len(payload.FallbackCommands) != 2 || !strings.Contains(payload.FallbackCommands[0], "--transport long-poll") || !strings.Contains(payload.FallbackCommands[1], "--transport poll") || !strings.Contains(payload.FallbackCommands[0], "--manifest-root-public-key") {
		t.Fatalf("expected long-poll and poll fallback commands, got %#v", payload.FallbackCommands)
	}
}

func TestInviteCreateLANScopeMarksLANReachability(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--network-scope", "lan",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ConnectionPlan struct {
			NetworkScope        string   `json:"network_scope"`
			GatewayReachability string   `json:"gateway_reachability"`
			SelectionOrder      []string `json:"selection_order"`
		} `json:"connection_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid invite JSON: %v\n%s", err, stdout.String())
	}
	if payload.ConnectionPlan.NetworkScope != "lan" {
		t.Fatalf("expected lan network scope, got %#v", payload.ConnectionPlan)
	}
	if payload.ConnectionPlan.GatewayReachability != "local-machine" {
		t.Fatalf("expected local-machine reachability for httptest localhost gateway, got %#v", payload.ConnectionPlan)
	}
	if len(payload.ConnectionPlan.SelectionOrder) == 0 || !strings.Contains(payload.ConnectionPlan.SelectionOrder[0], "lan-gateway") {
		t.Fatalf("expected LAN path first in selection order, got %#v", payload.ConnectionPlan.SelectionOrder)
	}
}

func TestConnectionEntryPlanMaterializesGenericPackagePlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	inviteApp := NewApp(&inviteOut, &bytes.Buffer{})
	if err := inviteApp.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "repair target host",
		"--transport", "auto",
	}); err != nil {
		t.Fatal(err)
	}

	bootstrap := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(bootstrap, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "entry")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "plan",
		"--invite-json", inviteOut.String(),
		"--out", outDir,
		"--target-os", "windows",
		"--ownership", "third-party",
		"--windows-bootstrap-script", bootstrap,
		"--windows-host-download-url", "https://agent.example.com/rdev-host.exe",
		"--windows-host-sha256", strings.Repeat("a", 64),
		"--release-bundle-url", "https://agent.example.com/release-bundle.json",
		"--release-root-public-key", "release-root:" + strings.Repeat("b", 43),
		"--windows-verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--windows-verifier-sha256", strings.Repeat("c", 64),
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK               bool `json:"ok"`
		EntryPackagePlan struct {
			SchemaVersion       string   `json:"schema_version"`
			TargetOS            string   `json:"target_os"`
			SessionMode         string   `json:"session_mode"`
			PlatformPlanKind    string   `json:"platform_plan_kind"`
			LauncherPath        string   `json:"launcher_path"`
			AgentOnlyParameters []string `json:"agent_only_parameters"`
		} `json:"entry_package_plan"`
		Plan struct {
			ConnectionEntryName    string   `json:"connection_entry_name"`
			EntryPackagePlanSchema string   `json:"entry_package_plan_schema"`
			ModeDecision           string   `json:"mode_decision"`
			HumanSurface           []string `json:"human_surface"`
			AgentMetadata          []string `json:"agent_metadata"`
			HandoffContract        []string `json:"handoff_contract"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid connection entry output: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected connection entry plan ok, got %s", stdout.String())
	}
	if payload.EntryPackagePlan.SchemaVersion != "rdev.connection-entry.package-plan.v1" ||
		payload.EntryPackagePlan.TargetOS != "windows" ||
		payload.EntryPackagePlan.SessionMode != string(model.HostModeAttendedTemporary) ||
		payload.EntryPackagePlan.PlatformPlanKind != "windows-temporary-acceptance-plan" ||
		!fileExistsForCLITest(payload.EntryPackagePlan.LauncherPath) {
		t.Fatalf("expected generic entry package plan wrapping Windows temporary plan, got %#v", payload.EntryPackagePlan)
	}
	if !slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "ticket_code") ||
		!slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "manifest_root_public_key") {
		t.Fatalf("expected raw connection parameters to be agent-only, got %#v", payload.EntryPackagePlan.AgentOnlyParameters)
	}
	if payload.Plan.ConnectionEntryName != "Connection Entry" ||
		payload.Plan.EntryPackagePlanSchema != "rdev.connection-entry.package-plan.v1" ||
		!strings.Contains(payload.Plan.ModeDecision, "attended-temporary") ||
		!slices.Contains(payload.Plan.HumanSurface, "connection_entry.entry_url") ||
		!slices.Contains(payload.Plan.AgentMetadata, "gateway URL") ||
		!slices.Contains(payload.Plan.HandoffContract, "Agents must use rdev.connection_entry.plan or rdev connection-entry plan before giving target-side instructions.") {
		t.Fatalf("expected universal mode decision and split human/agent surfaces, got %#v", payload.Plan)
	}
	if strings.Contains(stdout.String(), "customer_bootstrap") ||
		strings.Contains(stdout.String(), "connector_package_plan") {
		t.Fatalf("connection entry output should not use legacy customer/connector names: %s", stdout.String())
	}
}

func TestConnectionEntryRunWritesRunnerResultEvidence(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	inviteApp := NewApp(&inviteOut, &bytes.Buffer{})
	if err := inviteApp.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--reason", "runner evidence",
		"--transport", "auto",
	}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "entry")
	var planOut bytes.Buffer
	app := NewApp(&planOut, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "plan",
		"--invite-json", inviteOut.String(),
		"--out", outDir,
		"--target-os", "linux",
		"--ownership", "third-party",
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		RunnerPlan struct {
			ManifestPath string `json:"manifest_path"`
		} `json:"runner_plan"`
	}
	if err := json.Unmarshal(planOut.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid connection entry plan JSON: %v\n%s", err, planOut.String())
	}
	resultOut := filepath.Join(t.TempDir(), "evidence", "runner-result.json")
	helperTranscriptOut := filepath.Join(t.TempDir(), "evidence", "helper-transcript.txt")
	evidenceDir := filepath.Join(t.TempDir(), "standard-evidence")
	var runOut bytes.Buffer
	app = NewApp(&runOut, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "run",
		"--runner-manifest", planPayload.RunnerPlan.ManifestPath,
		"--dry-run",
		"--probe-timeout", "1s",
		"--result-out", resultOut,
		"--helper-transcript-out", helperTranscriptOut,
		"--evidence-dir", evidenceDir,
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(resultOut)
	if err != nil {
		t.Fatalf("expected runner result evidence: %v", err)
	}
	var result connectionrunner.RunResult
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("invalid runner result evidence: %v\n%s", err, string(content))
	}
	if result.SchemaVersion != "rdev.connection-entry.runner-result.v1" ||
		result.SelectedPath != "native-direct-gateway" ||
		len(result.HostServeArgs) == 0 ||
		result.Executed {
		t.Fatalf("unexpected runner evidence: %#v\ncli output: %s", result, runOut.String())
	}
	helperTranscript, err := os.ReadFile(helperTranscriptOut)
	if err != nil {
		t.Fatalf("expected helper transcript evidence: %v", err)
	}
	if !strings.Contains(string(helperTranscript), "selected_path native-direct-gateway") ||
		!strings.Contains(string(helperTranscript), "dry_run no_execution") ||
		!strings.Contains(runOut.String(), `"helper_transcript": "`+helperTranscriptOut+`"`) {
		t.Fatalf("unexpected helper transcript evidence:\n%s\ncli output: %s", string(helperTranscript), runOut.String())
	}
	for _, expected := range []string{"runner-result.json", "helper-transcript.txt", "gateway-status.json", "host-status.json", "connection-status.json", "audit.jsonl", "evidence-report.json"} {
		if _, err := os.Stat(filepath.Join(evidenceDir, expected)); err != nil {
			t.Fatalf("expected standard evidence file %s: %v", expected, err)
		}
	}
	if !strings.Contains(runOut.String(), `"schema_version": "rdev.connection-entry.runner-evidence.v1"`) {
		t.Fatalf("expected evidence report in output: %s", runOut.String())
	}
}

func TestConnectionEntryPlanMaterializesManagedLinuxPackagePlan(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var inviteOut bytes.Buffer
	inviteApp := NewApp(&inviteOut, &bytes.Buffer{})
	if err := inviteApp.Run(context.Background(), []string{
		"invite", "create",
		"--gateway", server.URL,
		"--mode", string(model.HostModeManaged),
		"--reason", "owned workstation",
		"--transport", "auto",
	}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "managed-entry")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"connection-entry", "plan",
		"--invite-json", inviteOut.String(),
		"--out", outDir,
		"--target-os", "linux",
		"--ownership", "owned",
		"--managed-binary", "/opt/rdev/rdev",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:" + strings.Repeat("b", 43),
		"--release-bundle-required-artifacts", "rdev,rdev-host,rdev-verify",
	}); err != nil {
		t.Fatal(err)
	}

	var payload struct {
		OK               bool `json:"ok"`
		EntryPackagePlan struct {
			SchemaVersion       string   `json:"schema_version"`
			TargetOS            string   `json:"target_os"`
			SessionMode         string   `json:"session_mode"`
			PackageMode         string   `json:"package_mode"`
			PlatformPlanKind    string   `json:"platform_plan_kind"`
			LauncherPath        string   `json:"launcher_path"`
			AgentOnlyParameters []string `json:"agent_only_parameters"`
		} `json:"entry_package_plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid managed connection entry output: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected managed connection entry plan ok, got %s", stdout.String())
	}
	if payload.EntryPackagePlan.SchemaVersion != "rdev.connection-entry.package-plan.v1" ||
		payload.EntryPackagePlan.TargetOS != "linux" ||
		payload.EntryPackagePlan.SessionMode != string(model.HostModeManaged) ||
		payload.EntryPackagePlan.PackageMode != "reviewed-managed-service-connection-entry" ||
		payload.EntryPackagePlan.PlatformPlanKind != "linux-managed-service-plan" ||
		!fileExistsForCLITest(payload.EntryPackagePlan.LauncherPath) {
		t.Fatalf("expected generic entry package plan wrapping Linux managed service plan, got %#v", payload.EntryPackagePlan)
	}
	if !slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "managed_binary_path") ||
		!slices.Contains(payload.EntryPackagePlan.AgentOnlyParameters, "release_bundle_path") {
		t.Fatalf("expected managed raw parameters to be agent-only, got %#v", payload.EntryPackagePlan.AgentOnlyParameters)
	}
	if !fileExistsForCLITest(filepath.Join(outDir, "managed-linux", "linux-managed-service-plan.json")) {
		t.Fatalf("expected Linux managed service plan in entry package")
	}
}

func TestInviteCreateRequiresGateway(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"invite", "create", "--reason", "repair"})
	if err == nil || !strings.Contains(err.Error(), "requires --gateway") {
		t.Fatalf("expected gateway requirement, got %v", err)
	}
}

func TestGatewayTicketsURLAcceptsAPIRoot(t *testing.T) {
	if got := gatewayTicketsURL("https://api.example.com/v1"); got != "https://api.example.com/v1/tickets" {
		t.Fatalf("unexpected API root tickets URL: %s", got)
	}
	if got := gatewayTicketsURL("https://api.example.com"); got != "https://api.example.com/v1/tickets" {
		t.Fatalf("unexpected gateway root tickets URL: %s", got)
	}
}

func TestOperatorAuthInitAndVerify(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "operators.json")
	tokenDir := filepath.Join(dir, "tokens")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{"operator-auth", "init", "--out", authPath, "--token-dir", tokenDir}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "rdev_") {
		t.Fatalf("init output should not print bearer tokens: %s", stdout.String())
	}
	for _, name := range []string{"admin", "operator", "issuer", "auditor"} {
		if _, err := os.Stat(filepath.Join(tokenDir, name+".token")); err != nil {
			t.Fatalf("expected %s token file: %v", name, err)
		}
	}

	var verifyOut bytes.Buffer
	verifyApp := NewApp(&verifyOut, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{"operator-auth", "verify", "--auth", authPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyOut.String(), `"ok": true`) {
		t.Fatalf("expected verify ok, got %s", verifyOut.String())
	}
}

func TestOperatorAuthVerifyHosted(t *testing.T) {
	publicKey, _, err := operatorauth.GenerateHostedKey()
	if err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(t.TempDir(), "hosted-auth.json")
	authFile := operatorauth.HostedFile{
		SchemaVersion: operatorauth.HostedSchemaVersion,
		Issuer:        "https://auth.example.com/",
		Audience:      "rdev-gateway",
		Keys: []operatorauth.HostedAuthKey{{
			KeyID:     "operator-key",
			PublicKey: operatorauth.EncodePublicKey(publicKey),
		}},
	}
	content, err := json.Marshal(authFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"operator-auth", "verify-hosted", "--auth", authPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), `"key_count": 1`) {
		t.Fatalf("unexpected hosted verify output: %s", stdout.String())
	}
}

func TestOperatorAuthVerifyOIDCJWKSWithToken(t *testing.T) {
	now := time.Now().UTC()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": "oidc-key",
				"use": "sig",
				"alg": "RS256",
				"n":   operatorauth.EncodeRSAJWKValue(privateKey.PublicKey.N),
				"e":   operatorauth.EncodeRSAJWKValue(big.NewInt(int64(privateKey.PublicKey.E))),
			}},
		})
	}))
	defer server.Close()
	root := t.TempDir()
	authPath := filepath.Join(root, "oidc-jwks-auth.json")
	authFile := operatorauth.OIDCJWKSFile{
		SchemaVersion:    operatorauth.OIDCJWKSSchemaVersion,
		Issuer:           "https://issuer.example.test/",
		Audience:         "rdev-gateway",
		JWKSURL:          server.URL,
		RolesClaim:       "rdev_roles",
		ClockSkewSeconds: 30,
	}
	content, err := json.Marshal(authFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := operatorauth.SignOIDCJWKSToken("oidc-key", privateKey, operatorauth.OIDCClaims{
		Issuer:    "https://issuer.example.test/",
		Subject:   "operator@example.test",
		Audiences: []string{"rdev-gateway"},
		ExpiresAt: now.Add(time.Hour).Unix(),
		Roles:     []string{operatorauth.RoleOperator},
	}, "rdev_roles")
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(root, "operator.jwt")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"operator-auth", "verify-oidc-jwks",
		"--auth", authPath,
		"--token-file", tokenPath,
		"--role", "operator",
	}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, `"ok": true`) ||
		!strings.Contains(output, `"schema_version": "rdev.oidc-jwks-operator-auth.v1"`) ||
		!strings.Contains(output, `"token_verified": true`) ||
		!strings.Contains(output, `"key_count": 1`) {
		t.Fatalf("unexpected OIDC JWKS verify output: %s", output)
	}
}

func TestOperatorAuthVerifySAMLConfig(t *testing.T) {
	now := time.Now().UTC()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rdev test idp"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	authPath := filepath.Join(root, "saml-auth.json")
	authFile := operatorauth.SAMLFile{
		SchemaVersion:        operatorauth.SAMLSchemaVersion,
		IDPIssuer:            "https://idp.example.test/saml",
		Audience:             "rdev-gateway",
		AssertionConsumerURL: "https://gateway.example.test/saml/acs",
		RoleAttribute:        "rdev_roles",
		CertificatePEM:       string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})),
	}
	content, err := json.Marshal(authFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"operator-auth", "verify-saml", "--auth", authPath}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, `"ok": true`) ||
		!strings.Contains(output, `"schema_version": "rdev.saml-operator-auth.v1"`) ||
		!strings.Contains(output, `"certificate_count": 1`) ||
		!strings.Contains(output, `"response_verified": false`) {
		t.Fatalf("unexpected SAML verify output: %s", output)
	}
}

func TestGatewayStorageVerifyRejectsUnknownProvider(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "unknown", "--path", filepath.Join(t.TempDir(), "state.json")})
	if err == nil || !strings.Contains(err.Error(), "unsupported gateway storage provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestGatewayStorageVerifyFileProvider(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	path := filepath.Join(t.TempDir(), "state.json")
	if err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "file", "--path", path}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "file:"+path) {
		t.Fatalf("unexpected storage verify output: %s", stdout.String())
	}
}

func TestGatewayStorageVerifyPostgresRejectsInlinePassword(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "postgres", "--path", "postgres://rdev:secret@example.invalid/rdev"})
	if err == nil || !strings.Contains(err.Error(), "must not contain inline passwords") {
		t.Fatalf("expected inline password rejection, got %v", err)
	}
}

func TestGatewayStorageVerifyRedisRejectsInlineCredentials(t *testing.T) {
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{"gateway", "storage", "verify", "--provider", "redis-stream", "--path", "redis://default:secret@example.invalid:6379/0"})
	if err == nil || !strings.Contains(err.Error(), "must not contain inline credentials") {
		t.Fatalf("expected inline credential rejection, got %v", err)
	}
}

func TestHostedProviderPackageAndVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "file",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageStdout.String(), `"schema": "rdev.hosted-provider-package.v1"`) ||
		!strings.Contains(packageStdout.String(), `"external_mutation": false`) ||
		!strings.Contains(packageStdout.String(), `"runtime_contract_schema": "rdev.hosted-provider-runtime-contract.v1"`) ||
		!strings.Contains(packageStdout.String(), `"runtime_evidence_plan_schema": "rdev.hosted-provider-runtime-evidence-plan.v1"`) {
		t.Fatalf("unexpected hosted provider package output: %s", packageStdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "runtime-evidence-plan.json")); err != nil {
		t.Fatalf("expected runtime evidence plan: %v", err)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.hosted-provider-package-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"runtime_evidence_plan_schema": "rdev.hosted-provider-runtime-evidence-plan.v1"`) {
		t.Fatalf("unexpected hosted provider verify output: %s", verifyStdout.String())
	}
}

func TestHostedProviderExternalRuntimeContractPackageAndVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "postgres",
		"--auth-provider", "oidc-jwks",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageStdout.String(), `"schema": "rdev.hosted-provider-package.v1"`) ||
		!strings.Contains(packageStdout.String(), `"storage_provider": "postgres"`) ||
		!strings.Contains(packageStdout.String(), `"auth_provider": "oidc-jwks"`) ||
		!strings.Contains(packageStdout.String(), `"runtime_status": "durable-runtime-evidence-required"`) {
		t.Fatalf("unexpected hosted provider package output: %s", packageStdout.String())
	}
	for _, expected := range []string{"hosted-provider.json", "runtime-contract.json", "runtime-evidence-plan.json", "HOSTED_PROVIDER_RUNTIME.md"} {
		if _, err := os.Stat(filepath.Join(out, expected)); err != nil {
			t.Fatalf("expected %s: %v", expected, err)
		}
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.hosted-provider-package-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"storage_provider": "postgres"`) {
		t.Fatalf("unexpected hosted provider verify output: %s", verifyStdout.String())
	}
}

func TestHostedProviderRedisHostedJWTUsesBuiltInGatewayRuntime(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "redis-stream",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	output := packageStdout.String()
	if !strings.Contains(output, `"storage_provider": "redis-stream"`) ||
		!strings.Contains(output, `"auth_provider": "hosted-ed25519-jwt"`) ||
		!strings.Contains(output, `"redis-stream"`) ||
		strings.Contains(output, "operator-reviewed-hosted-gateway-launcher") {
		t.Fatalf("unexpected redis hosted provider package output: %s", output)
	}
	var manifest struct {
		GatewayArgs []string `json:"gateway_args"`
	}
	content, err := os.ReadFile(filepath.Join(out, "hosted-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(manifest.GatewayArgs, " ")
	if !strings.Contains(joined, "gateway serve --storage-provider redis-stream") ||
		!strings.Contains(joined, "--hosted-operator-auth") {
		t.Fatalf("expected built-in redis gateway args, got %#v", manifest.GatewayArgs)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"storage_provider": "redis-stream"`) {
		t.Fatalf("unexpected verify output: %s", verifyStdout.String())
	}
}

func TestHostedProviderS3CompatibleHostedJWTUsesBuiltInGatewayRuntime(t *testing.T) {
	out := filepath.Join(t.TempDir(), "provider")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", out,
		"--storage-provider", "s3-compatible",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	output := packageStdout.String()
	if !strings.Contains(output, `"storage_provider": "s3-compatible"`) ||
		!strings.Contains(output, `"auth_provider": "hosted-ed25519-jwt"`) ||
		!strings.Contains(output, `"s3-compatible"`) ||
		strings.Contains(output, "operator-reviewed-hosted-gateway-launcher") {
		t.Fatalf("unexpected s3-compatible hosted provider package output: %s", output)
	}
	var manifest struct {
		GatewayArgs []string `json:"gateway_args"`
	}
	content, err := os.ReadFile(filepath.Join(out, "hosted-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(manifest.GatewayArgs, " ")
	if !strings.Contains(joined, "gateway serve --storage-provider s3-compatible") ||
		!strings.Contains(joined, "--hosted-operator-auth") {
		t.Fatalf("expected built-in s3-compatible gateway args, got %#v", manifest.GatewayArgs)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"hosted-provider", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"storage_provider": "s3-compatible"`) {
		t.Fatalf("unexpected verify output: %s", verifyStdout.String())
	}
}

func TestGatewayStorageVerifyS3CompatibleRejectsUnsafeLocation(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"gateway", "storage", "verify",
		"--provider", "s3-compatible",
		"--path", "s3://example-bucket/rdev?secret=inline",
	})
	if err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("expected unsafe S3-compatible location rejection, got %v output=%s", err, stdout.String())
	}
}

func TestRelayAdapterPackageAndVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "relay")
	var packageStdout bytes.Buffer
	app := NewApp(&packageStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", out,
		"--adapter", "chisel",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packageStdout.String(), `"schema": "rdev.relay-adapter-package.v1"`) ||
		!strings.Contains(packageStdout.String(), `"external_mutation": false`) ||
		!strings.Contains(packageStdout.String(), `"adapter_kind": "chisel"`) ||
		!strings.Contains(packageStdout.String(), `"acceptance_evidence_plan_schema": "rdev.relay-adapter-acceptance-evidence-plan.v1"`) {
		t.Fatalf("unexpected relay adapter package output: %s", packageStdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "acceptance-evidence-plan.json")); err != nil {
		t.Fatalf("expected acceptance evidence plan: %v", err)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"relay-adapter", "verify", "--package", out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.relay-adapter-package-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"adapter_kind": "chisel"`) ||
		!strings.Contains(verifyStdout.String(), `"acceptance_evidence_plan_schema": "rdev.relay-adapter-acceptance-evidence-plan.v1"`) {
		t.Fatalf("unexpected relay adapter verify output: %s", verifyStdout.String())
	}
}

func TestRelayAdapterPackageSupportsMeshSSHAndVPNKinds(t *testing.T) {
	for _, tc := range []struct {
		adapter    string
		kind       string
		helperTool string
		envVar     string
	}{
		{adapter: "ssh-tunnel", kind: "ssh-tunnel", helperTool: "ssh", envVar: "RDEV_SSH_GATEWAY_URL"},
		{adapter: "headscale-tailscale", kind: "headscale-tailscale", helperTool: "tailscale", envVar: "RDEV_MESH_GATEWAY_URL"},
		{adapter: "wireguard", kind: "wireguard", helperTool: "wg", envVar: "RDEV_VPN_GATEWAY_URL"},
	} {
		t.Run(tc.adapter, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "adapter")
			var packageStdout bytes.Buffer
			app := NewApp(&packageStdout, &bytes.Buffer{})
			if err := app.Run(context.Background(), []string{
				"relay-adapter", "package",
				"--out", out,
				"--adapter", tc.adapter,
			}); err != nil {
				t.Fatal(err)
			}
			output := packageStdout.String()
			for _, expected := range []string{
				`"schema": "rdev.relay-adapter-package.v1"`,
				`"adapter_kind": "` + tc.kind + `"`,
				`"helper_tool": "` + tc.helperTool + `"`,
				tc.envVar,
			} {
				if !strings.Contains(output, expected) {
					t.Fatalf("expected %q in output: %s", expected, output)
				}
			}

			var verifyStdout bytes.Buffer
			app = NewApp(&verifyStdout, &bytes.Buffer{})
			if err := app.Run(context.Background(), []string{"relay-adapter", "verify", "--package", out}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
				!strings.Contains(verifyStdout.String(), `"adapter_kind": "`+tc.kind+`"`) {
				t.Fatalf("unexpected verify output: %s", verifyStdout.String())
			}
		})
	}
}

func TestAcceptanceScaffoldEvidenceForHostedProviderAndRelayPlans(t *testing.T) {
	root := t.TempDir()
	providerOut := filepath.Join(root, "hosted-provider")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", providerOut,
		"--storage-provider", "postgres",
		"--auth-provider", "oidc-jwks",
	}); err != nil {
		t.Fatal(err)
	}

	hostedScaffold := filepath.Join(root, "hosted-scaffold")
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-evidence",
		"--plan", filepath.Join(providerOut, "runtime-evidence-plan.json"),
		"--out", hostedScaffold,
	}); err != nil {
		t.Fatal(err)
	}
	hostedOutput := stdout.String()
	for _, expected := range []string{
		`"schema": "rdev.acceptance-evidence-scaffold.v1"`,
		`"plan_schema": "rdev.hosted-provider-runtime-evidence-plan.v1"`,
		`"plan_kind": "hosted-provider-runtime"`,
		`"ready_for_packaging": false`,
		`"package-hosted-provider-runtime"`,
	} {
		if !strings.Contains(hostedOutput, expected) {
			t.Fatalf("expected %q in hosted scaffold output: %s", expected, hostedOutput)
		}
	}
	if _, err := os.Stat(filepath.Join(hostedScaffold, "gateway-startup.txt")); !os.IsNotExist(err) {
		t.Fatalf("default hosted scaffold must not create placeholders, err=%v", err)
	}
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"acceptance", "evidence-status",
		"--scaffold", hostedScaffold,
	})
	if err == nil {
		t.Fatalf("missing hosted evidence status should fail")
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.acceptance-evidence-status.v1"`) ||
		!strings.Contains(stdout.String(), `"ready_for_packaging": false`) ||
		!strings.Contains(stdout.String(), `"missing_count": 9`) {
		t.Fatalf("unexpected missing hosted status output: %s", stdout.String())
	}
	writeFile(t, filepath.Join(hostedScaffold, "gateway-startup.txt"), "gateway started\n")
	writeFile(t, filepath.Join(hostedScaffold, "storage-verification.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(hostedScaffold, "auth-verification.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(hostedScaffold, "backup-evidence.txt"), "backup complete\n")
	writeFile(t, filepath.Join(hostedScaffold, "restore-evidence.txt"), "restore complete\n")
	writeFile(t, filepath.Join(hostedScaffold, "retention-evidence.txt"), "retention reviewed\n")
	writeFile(t, filepath.Join(hostedScaffold, "role-mapping-evidence.json"), `{"probes":[{"authorized":true},{"authorized":false}]}`)
	writeFile(t, filepath.Join(hostedScaffold, "failure-mode-evidence.json"), `{"ok":true}`)
	writeFile(t, filepath.Join(hostedScaffold, "audit.jsonl"), `{"event":"hosted_acceptance"}`)
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "evidence-status",
		"--scaffold", hostedScaffold,
	}); err != nil {
		t.Fatalf("real hosted evidence status should pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ready_for_packaging": true`) ||
		!strings.Contains(stdout.String(), `"required_ready": 9`) {
		t.Fatalf("unexpected ready hosted status output: %s", stdout.String())
	}

	relayOut := filepath.Join(root, "relay-adapter")
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", relayOut,
		"--adapter", "wireguard",
	}); err != nil {
		t.Fatal(err)
	}
	relayScaffold := filepath.Join(root, "relay-scaffold")
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-evidence",
		"--plan", filepath.Join(relayOut, "acceptance-evidence-plan.json"),
		"--out", relayScaffold,
		"--create-placeholders",
	}); err != nil {
		t.Fatal(err)
	}
	relayOutput := stdout.String()
	for _, expected := range []string{
		`"schema": "rdev.acceptance-evidence-scaffold.v1"`,
		`"plan_schema": "rdev.relay-adapter-acceptance-evidence-plan.v1"`,
		`"plan_kind": "relay-adapter"`,
		`"create_placeholders": true`,
		`"package-relay-adapter"`,
	} {
		if !strings.Contains(relayOutput, expected) {
			t.Fatalf("expected %q in relay scaffold output: %s", expected, relayOutput)
		}
	}
	content, err := os.ReadFile(filepath.Join(relayScaffold, "runner-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"placeholder": true`) {
		t.Fatalf("expected placeholder runner result, got %s", string(content))
	}
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"acceptance", "evidence-status",
		"--scaffold", relayScaffold,
	})
	if err == nil {
		t.Fatalf("placeholder relay evidence status should fail")
	}
	if !strings.Contains(stdout.String(), `"placeholder_count": 6`) ||
		!strings.Contains(stdout.String(), `"ready_for_packaging": false`) {
		t.Fatalf("unexpected placeholder relay status output: %s", stdout.String())
	}
}

func TestMCPServeProcessesInitialize(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	_, _ = writer.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n")
	_ = writer.Close()

	if err := app.Run(context.Background(), []string{"mcp", "serve"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"protocolVersion":"2025-11-25"`) {
		t.Fatalf("expected initialize response, got %q", stdout.String())
	}
}

func TestHostInstallServiceWritesMacOSLaunchAgentPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "LaunchAgents", "com.example.rdev-host.plist")
	binaryPath := filepath.Join(dir, "bin", "rdev")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--identity-store", filepath.Join(dir, "identity.json"),
		"--trust-store", filepath.Join(dir, "trust.json"),
		"--nonce-store", filepath.Join(dir, "nonces.json"),
		"--approval-store", filepath.Join(dir, "approvals.json"),
		"--workspace-lock-store", filepath.Join(dir, "workspace-locks"),
		"--log-dir", filepath.Join(dir, "logs"),
		"--plist-out", plistPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := readFileForTest(t, plistPath)
	for _, expected := range []string{
		"<key>Label</key>",
		"<string>com.example.rdev-host</string>",
		"<string>host</string>",
		"<string>serve</string>",
		"<string>managed</string>",
		"<string>https://api.example.com/v1</string>",
		"<string>ABCD-1234</string>",
		"<string>--workspace-lock-store</string>",
		"<string>" + filepath.Join(dir, "workspace-locks") + "</string>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected plist to contain %q, got %s", expected, content)
		}
	}
	info, err := os.Stat(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected plist permissions 0600, got %#o", got)
	}
	if !strings.Contains(stdout.String(), `"launchctl bootstrap gui/$(id -u) `+plistPath+`"`) {
		t.Fatalf("expected launchctl next step in stdout, got %s", stdout.String())
	}
}

func TestHostInstallServiceDoesNotOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	if err := os.WriteFile(plistPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	})
	if err == nil {
		t.Fatal("expected existing plist to fail without --force")
	}
	if got := readFileForTest(t, plistPath); got != "existing" {
		t.Fatalf("expected existing plist to remain unchanged, got %q", got)
	}
}

func TestHostInstallServiceWritesLinuxSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "systemd", "user", "rdev-host.service")
	binaryPath := filepath.Join(dir, "bin", "rdev")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--identity-store", filepath.Join(dir, "identity.json"),
		"--trust-store", filepath.Join(dir, "trust.json"),
		"--nonce-store", filepath.Join(dir, "nonces.json"),
		"--approval-store", filepath.Join(dir, "approvals.json"),
		"--workspace-lock-store", filepath.Join(dir, "workspace-locks"),
		"--log-dir", filepath.Join(dir, "logs"),
		"--unit-out", unitPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := readFileForTest(t, unitPath)
	for _, expected := range []string{
		"[Unit]",
		"Description=Remote Dev Skillkit managed host",
		"[Service]",
		"ExecStart=" + binaryPath + " host serve --mode managed",
		"--gateway https://api.example.com/v1",
		"--ticket-code ABCD-1234",
		"--workspace-lock-store " + filepath.Join(dir, "workspace-locks"),
		"Restart=on-failure",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"StandardOutput=append:" + filepath.Join(dir, "logs", "rdev-host.out.log"),
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("expected unit to contain %q, got %s", expected, content)
		}
	}
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected unit permissions 0600, got %#o", got)
	}
	for _, expected := range []string{
		`"platform": "linux"`,
		`"unit_name": "rdev-host.service"`,
		`"systemctl --user enable --now rdev-host.service"`,
		`systemctl was not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected stdout to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostInstallServicePlansWindowsService(t *testing.T) {
	binaryPath := `C:\Program Files\rdev\rdev.exe`
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "windows",
		"--label", "RemoteDevSkillkitHost",
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--identity-store", `C:\ProgramData\rdev\identity.json`,
		"--trust-store", `C:\ProgramData\rdev\trust.json`,
		"--nonce-store", `C:\ProgramData\rdev\nonces.json`,
		"--approval-store", `C:\ProgramData\rdev\approvals.json`,
		"--workspace-lock-store", `C:\ProgramData\rdev\workspace-locks`,
		"--release-bundle", `C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key", "release-root:abc123",
		"--release-require-artifacts", "rdev-host.exe,rdev-verify.exe",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"platform": "windows"`,
		`"service_name": "RemoteDevSkillkitHost"`,
		`"sc.exe"`,
		`"create"`,
		`C:\\Program Files\\rdev\\rdev.exe`,
		`"--mode"`,
		`"managed"`,
		`"--release-bundle"`,
		`C:\\Program Files\\rdev\\release-bundle.json`,
		`"start_type": "demand"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows service output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostInstallServiceRejectsRelativeWindowsBinaryPath(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "windows",
		"--binary", `rdev.exe`,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
	})
	if err == nil || !strings.Contains(err.Error(), "binary path must be absolute") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestHostServiceStatusReadsMacOSLaunchAgentPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	statusApp := NewApp(&stdout, &bytes.Buffer{})
	if err := statusApp.Run(context.Background(), []string{
		"host", "service-status",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"exists": true`,
		`"label": "com.example.rdev-host"`,
		`"launchctl print gui/$(id -u)/com.example.rdev-host"`,
		`launchctl was not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected status output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceStatusReadsLinuxSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	statusApp := NewApp(&stdout, &bytes.Buffer{})
	if err := statusApp.Run(context.Background(), []string{
		"host", "service-status",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"exists": true`,
		`"unit_name": "rdev-host.service"`,
		`"systemctl --user status rdev-host.service"`,
		`systemctl was not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected status output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceStatusPlansWindowsCommands(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "service-status",
		"--platform", "windows",
		"--label", "RemoteDevSkillkitHost",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"platform": "windows"`,
		`"service_name": "RemoteDevSkillkitHost"`,
		`"query"`,
		`"qc"`,
		`status commands were not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows status output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlDryRunPlansLaunchctl(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	controlApp := NewApp(&stdout, &bytes.Buffer{})
	if err := controlApp.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "macos",
		"--action", "start",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
		"--domain", "gui/501",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"execute": false`,
		`"action": "start"`,
		`"launchctl"`,
		`"bootstrap"`,
		`"gui/501"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected service-control output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlDryRunPlansSystemd(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	controlApp := NewApp(&stdout, &bytes.Buffer{})
	if err := controlApp.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "linux",
		"--action", "start",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"execute": false`,
		`"action": "start"`,
		`"systemctl"`,
		`"daemon-reload"`,
		`"enable"`,
		`"--now"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected service-control output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlDryRunPlansWindowsService(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "windows",
		"--action", "start",
		"--label", "RemoteDevSkillkitHost",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"execute": false`,
		`"platform": "windows"`,
		`"action": "start"`,
		`"sc.exe"`,
		`"start"`,
		`dry-run only`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows service-control output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostServiceControlRejectsLabelMismatch(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.other.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.other.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "macos",
		"--action", "start",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	})
	if err == nil {
		t.Fatal("expected label mismatch to fail")
	}
	if !strings.Contains(err.Error(), "refusing service-control") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServiceControlRejectsSystemdUnitMismatch(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "other-rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "other-rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"host", "service-control",
		"--platform", "linux",
		"--action", "start",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	})
	if err == nil {
		t.Fatal("expected unit mismatch to fail")
	}
	if !strings.Contains(err.Error(), "refusing service-control") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostUninstallServiceRemovesMacOSLaunchAgentPlist(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.example.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	uninstallApp := NewApp(&stdout, &bytes.Buffer{})
	if err := uninstallApp.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("expected plist removal, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), `"removed": true`) {
		t.Fatalf("expected removed output, got %s", stdout.String())
	}
}

func TestHostUninstallServiceRemovesLinuxSystemdUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "rdev-host.service")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--unit-out", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	uninstallApp := NewApp(&stdout, &bytes.Buffer{})
	if err := uninstallApp.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "linux",
		"--label", "rdev-host.service",
		"--unit", unitPath,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit removal, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), `"removed": true`) {
		t.Fatalf("expected removed output, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `systemctl was not executed`) {
		t.Fatalf("expected no systemctl execution note, got %s", stdout.String())
	}
}

func TestHostUninstallServicePlansWindowsServiceRemoval(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "windows",
		"--label", "RemoteDevSkillkitHost",
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"platform": "windows"`,
		`"service_name": "RemoteDevSkillkitHost"`,
		`"stop"`,
		`"delete"`,
		`stop/delete commands were not executed`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected Windows uninstall output to contain %q, got %s", expected, stdout.String())
		}
	}
}

func TestHostUninstallServiceRejectsLabelMismatch(t *testing.T) {
	dir := t.TempDir()
	plistPath := filepath.Join(dir, "com.other.rdev-host.plist")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"host", "install-service",
		"--platform", "macos",
		"--label", "com.other.rdev-host",
		"--binary", filepath.Join(dir, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--plist-out", plistPath,
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"host", "uninstall-service",
		"--platform", "macos",
		"--label", "com.example.rdev-host",
		"--plist", plistPath,
	})
	if err == nil {
		t.Fatal("expected label mismatch to fail")
	}
	if !strings.Contains(err.Error(), "refusing to remove plist") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(plistPath); err != nil {
		t.Fatalf("plist should remain after mismatch: %v", err)
	}
}

func TestHostServeRejectsUnknownMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "hidden"})
	if err == nil {
		t.Fatal("expected unsupported mode to fail")
	}
	if !strings.Contains(err.Error(), "unsupported host mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServeWithoutTicketExplainsPlaceholder(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "provide --gateway and --ticket-code") {
		t.Fatalf("expected ticket-code guidance, got %q", stdout.String())
	}
}

func TestHostServeRegistersWithLocalGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--gateway", server.URL, "--ticket-code", ticket.Code, "--name", "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "registered-pending-approval" {
		t.Fatalf("unexpected status %q", payload.Status)
	}
	if payload.Host.Name != "test-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
	}
	if payload.Host.Status != "pending" {
		t.Fatalf("expected pending host, got %q", payload.Host.Status)
	}
}

func TestHostServeRegistersWithLocalMTLSGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gw).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--gateway-ca", material.CACert,
		"--gateway-client-cert", material.ClientCert,
		"--gateway-client-key", material.ClientKey,
		"--ticket-code", ticket.Code,
		"--name", "mtls-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name string `json:"name"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid host serve output: %v\n%s", err, stdout.String())
	}
	if payload.Status != "registered-pending-approval" || payload.Host.Name != "mtls-host" {
		t.Fatalf("unexpected registration payload: %s", stdout.String())
	}
	if len(gw.Hosts("")) != 1 {
		t.Fatalf("expected one registered host, got %d", len(gw.Hosts("")))
	}
}

func TestHostServeMTLSGatewayRejectsMissingClientCertificate(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gw).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--gateway-ca", material.CACert,
		"--ticket-code", ticket.Code,
		"--name", "missing-client-cert-host",
	})
	if err == nil {
		t.Fatalf("expected mTLS registration without client certificate to fail, got output %s", stdout.String())
	}
	if strings.Contains(err.Error(), "local dev gateways only") {
		t.Fatalf("https local gateway should pass the local dev URL gate: %v", err)
	}
	if len(gw.Hosts("")) != 0 {
		t.Fatalf("expected no registered hosts after mTLS failure, got %d", len(gw.Hosts("")))
	}
}

func TestHostServeRejectsNonLocalHTTPSGateway(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--gateway", "https://api.example.com/v1", "--ticket-code", "ABCD-1234"})
	if err == nil {
		t.Fatal("expected non-local gateway registration to fail")
	}
	if !strings.Contains(err.Error(), "requires --manifest-url with --manifest-root-public-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSignedManifestGatewayURLAllowsPrivateLANHTTPAndHTTPS(t *testing.T) {
	allowed := []string{
		"http://10.0.0.8:8787",
		"http://172.16.4.5:8787",
		"http://rdev-gateway.local:8787",
		"https://api.example.com/v1",
	}
	for _, value := range allowed {
		if !isSignedManifestGatewayURL(value, true) {
			t.Fatalf("expected signed manifest gateway URL to be allowed: %s", value)
		}
	}

	rejected := []string{
		"http://198.51.100.10:8787",
		"http://10.0.0.8",
		"ws://10.0.0.8:8787",
		"https://api.example.com/v1",
	}
	for _, value := range rejected {
		verified := true
		if strings.HasPrefix(value, "https://") {
			verified = false
		}
		if isSignedManifestGatewayURL(value, verified) {
			t.Fatalf("expected gateway URL to be rejected: %s verified=%v", value, verified)
		}
	}
}

func TestHostServeVerifiesReleaseBundleBeforeRegistration(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	root := signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host", "host-binary")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-verify", "verify-binary")
	bundlePath := createReleaseBundleForHostServeTest(t, dir, keyPath, "rdev-host,rdev-verify")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--name", "verified-host",
		"--release-bundle", bundlePath,
		"--release-root-public-key", root,
		"--release-require-artifacts", "rdev-host,rdev-verify",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name string `json:"name"`
		} `json:"host"`
		ReleaseGate struct {
			OK                bool     `json:"ok"`
			Schema            string   `json:"schema"`
			Bundle            string   `json:"bundle"`
			RootKeyID         string   `json:"root_key_id"`
			RequiredArtifacts []string `json:"required_artifacts"`
			ArtifactCount     int      `json:"artifact_count"`
		} `json:"release_gate"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid host serve output: %v\n%s", err, stdout.String())
	}
	if payload.Status != "registered-pending-approval" || payload.Host.Name != "verified-host" {
		t.Fatalf("unexpected registration payload: %s", stdout.String())
	}
	if !payload.ReleaseGate.OK || payload.ReleaseGate.Schema != "rdev.release-bundle-verification.v1" {
		t.Fatalf("expected release gate verification in output, got %s", stdout.String())
	}
	if payload.ReleaseGate.Bundle != bundlePath || payload.ReleaseGate.RootKeyID != "release-root" {
		t.Fatalf("unexpected release gate identity: %#v", payload.ReleaseGate)
	}
	if payload.ReleaseGate.ArtifactCount != 2 || strings.Join(payload.ReleaseGate.RequiredArtifacts, ",") != "rdev-host,rdev-verify" {
		t.Fatalf("unexpected release gate artifacts: %#v", payload.ReleaseGate)
	}
	if len(gw.Hosts("")) != 1 {
		t.Fatalf("expected exactly one registered host, got %d", len(gw.Hosts("")))
	}
}

func TestHostServeRejectsTamperedReleaseBundleBeforeRegistration(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	root := signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host", "host-binary")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-verify", "verify-binary")
	bundlePath := createReleaseBundleForHostServeTest(t, dir, keyPath, "rdev-host,rdev-verify")
	if err := os.WriteFile(filepath.Join(dir, "rdev-host"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--release-bundle", bundlePath,
		"--release-root-public-key", root,
		"--release-require-artifacts", "rdev-host,rdev-verify",
	})
	if err == nil {
		t.Fatalf("expected tampered release gate to fail, got output %s", stdout.String())
	}
	if !strings.Contains(err.Error(), "host release bundle verification failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gw.Hosts("")) != 0 {
		t.Fatalf("release gate failed after registering hosts: %#v", gw.Hosts(""))
	}
}

func TestHostServeRegistersWithIdentityStore(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")

	firstFingerprint := runHostServeWithIdentityStore(t, server.URL, ticket.Code, identityPath, "test-host-1")
	hosts := gw.Hosts("")
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].IdentityFingerprint != firstFingerprint {
		t.Fatalf("expected stored host fingerprint %q, got %q", firstFingerprint, hosts[0].IdentityFingerprint)
	}
	if hosts[0].IdentityKeyID != "host-test" {
		t.Fatalf("expected identity key id host-test, got %q", hosts[0].IdentityKeyID)
	}
	info, err := os.Stat(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected identity store 0600 permissions, got %#o", got)
	}

	secondTicket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint := runHostServeWithIdentityStore(t, server.URL, secondTicket.Code, identityPath, "test-host-2")
	if secondFingerprint != firstFingerprint {
		t.Fatalf("expected identity fingerprint reuse, got %s then %s", firstFingerprint, secondFingerprint)
	}
}

func TestHostServeRegistersWithEnrollmentCertificate(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "certified temporary host")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	err = signApp.Run(context.Background(), []string{
		"enrollment", "sign-certificate",
		"--out", certificatePath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
		"--ticket-code", ticket.Code,
		"--mode", "attended-temporary",
		"--name", "cert-host",
		"--os", runtime.GOOS,
		"--arch", runtime.GOARCH,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", strings.Join(capabilities, ","),
	})
	if err != nil {
		t.Fatal(err)
	}
	var signPayload struct {
		RootPublicKey string `json:"root_public_key"`
		Schema        string `json:"schema"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signPayload); err != nil {
		t.Fatalf("invalid sign output: %v\n%s", err, signStdout.String())
	}
	if signPayload.Schema != model.HostEnrollmentCertificateSchemaVersion {
		t.Fatalf("expected enrollment certificate schema, got %s", signStdout.String())
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-certificate",
		"--certificate", certificatePath,
		"--root-public-key", signPayload.RootPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected verify output ok=true, got %s", verifyStdout.String())
	}
	root, err := parseRootPublicKey(signPayload.RootPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	gw.WithEnrollmentRoot(root)
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", identityPath,
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--name", "cert-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status                string `json:"status"`
		EnrollmentCertificate struct {
			Schema      string `json:"schema"`
			IssuerKeyID string `json:"issuer_key_id"`
		} `json:"enrollment_certificate"`
		Host struct {
			Status              string `json:"status"`
			IdentityFingerprint string `json:"identity_fingerprint"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid host output: %v\n%s", err, stdout.String())
	}
	if payload.Status != "registered-pending-approval" || payload.Host.Status != "pending" {
		t.Fatalf("unexpected host registration output: %s", stdout.String())
	}
	if payload.EnrollmentCertificate.Schema != model.HostEnrollmentCertificateSchemaVersion || payload.EnrollmentCertificate.IssuerKeyID != "enrollment-root" {
		t.Fatalf("expected enrollment certificate summary, got %s", stdout.String())
	}
	if payload.Host.IdentityFingerprint != identity.Fingerprint() {
		t.Fatalf("expected host identity fingerprint %q, got %q", identity.Fingerprint(), payload.Host.IdentityFingerprint)
	}

	revocationsPath := filepath.Join(dir, "certs", "revocations.json")
	var revokeStdout bytes.Buffer
	revokeApp := NewApp(&revokeStdout, &bytes.Buffer{})
	err = revokeApp.Run(context.Background(), []string{
		"enrollment", "revoke-certificate",
		"--out", revocationsPath,
		"--key", keyPath,
		"--certificate", certificatePath,
		"--reason", "host retired",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(revokeStdout.String(), model.HostEnrollmentRevocationListSchemaVersion) {
		t.Fatalf("expected revocation list schema, got %s", revokeStdout.String())
	}
	var verifyRevocationsStdout bytes.Buffer
	verifyRevocationsApp := NewApp(&verifyRevocationsStdout, &bytes.Buffer{})
	err = verifyRevocationsApp.Run(context.Background(), []string{
		"enrollment", "verify-revocations",
		"--revocations", revocationsPath,
		"--root-public-key", signPayload.RootPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyRevocationsStdout.String(), `"ok": true`) {
		t.Fatalf("expected revocation verification ok=true, got %s", verifyRevocationsStdout.String())
	}
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-certificate",
		"--certificate", certificatePath,
		"--root-public-key", signPayload.RootPublicKey,
		"--revocations", revocationsPath,
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked certificate verification failure, got %v", err)
	}
}

func TestHostServeFetchesEnrollmentRevocationsBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, capabilities, "revoked temporary host", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, _, certificate, issuerPrivateKey := writeHostServeEnrollmentCertificateFixture(t, dir, ticket, "revoked-host")
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: fingerprint,
			Reason:                 "host retired",
			RevokedAt:              now,
		},
	}, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var registerCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/enrollment/revocations":
			_ = json.NewEncoder(w).Encode(map[string]any{"revocations": revocations})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/register":
			registerCalled.Store(true)
			http.Error(w, "registration should not be attempted", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--fetch-enrollment-revocations",
		"--enrollment-root-public-key", rootPublicKey,
		"--name", "revoked-host",
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected local revocation rejection, got %v\nstdout=%s", err, stdout.String())
	}
	if registerCalled.Load() {
		t.Fatalf("registration endpoint was called after local revocation rejection")
	}
}

func TestHostServeReportsFetchedEnrollmentRevocations(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	now := time.Now().UTC().Add(-time.Minute)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now.Add(time.Minute) })
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "certified temporary host")
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, root, _, issuerPrivateKey := writeHostServeEnrollmentCertificateFixture(t, dir, ticket, "cert-host")
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: "sha256:unrelated-enrollment-certificate",
			Reason:                 "other host retired",
			RevokedAt:              now,
		},
	}, root.SigningKeyID, issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw.WithEnrollmentRoot(root).WithEnrollmentRevocations(revocations)
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--fetch-enrollment-revocations",
		"--enrollment-root-public-key", rootPublicKey,
		"--name", "cert-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status                string `json:"status"`
		EnrollmentCertificate struct {
			Schema                  string `json:"schema"`
			RevocationsFetched      bool   `json:"revocations_fetched"`
			RevokedCertificateCount int    `json:"revoked_certificate_count"`
			RevocationRootKeyID     string `json:"revocation_root_key_id"`
		} `json:"enrollment_certificate"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid host output: %v\n%s", err, stdout.String())
	}
	if payload.Status != "registered-pending-approval" {
		t.Fatalf("expected registration success, got %s", stdout.String())
	}
	if !payload.EnrollmentCertificate.RevocationsFetched ||
		payload.EnrollmentCertificate.RevokedCertificateCount != 1 ||
		payload.EnrollmentCertificate.RevocationRootKeyID != root.SigningKeyID ||
		payload.EnrollmentCertificate.Schema != model.HostEnrollmentCertificateSchemaVersion {
		t.Fatalf("expected fetched revocation summary, got %s", stdout.String())
	}
}

func TestHostServeSendsOperatorTokenWhenFetchingEnrollmentRevocations(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, capabilities, "certified temporary host", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, root, _, issuerPrivateKey := writeHostServeEnrollmentCertificateFixture(t, dir, ticket, "token-host")
	now := time.Now().UTC().Add(-time.Minute)
	revocations, err := model.SignHostEnrollmentRevocationList(nil, root.SigningKeyID, issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(dir, "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte("operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/enrollment/revocations":
			seenAuthorization = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{"revocations": revocations})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts/register":
			var registration model.HostRegistration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				t.Fatalf("invalid registration body: %v", err)
			}
			host, err := model.NewHost(ticket, registration, time.Now())
			if err != nil {
				t.Fatalf("registration should verify: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"host": host})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--fetch-enrollment-revocations",
		"--enrollment-root-public-key", rootPublicKey,
		"--operator-token-file", tokenPath,
		"--name", "token-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
}

func TestHostServeRequiresExplicitEnrollmentRevocationFetch(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, capabilities, "certified temporary host", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, _, _, _ := writeHostServeEnrollmentCertificateFixture(t, dir, ticket, "cert-host")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", "http://127.0.0.1:8787",
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--enrollment-root-public-key", rootPublicKey,
		"--name", "cert-host",
	})
	if err == nil || !strings.Contains(err.Error(), "--fetch-enrollment-revocations") {
		t.Fatalf("expected explicit fetch flag requirement, got %v", err)
	}
}

func TestHostServeRenewsEnrollmentCertificateBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "renewing temporary host")
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, _, certificate, _ := writeHostServeEnrollmentCertificateFixtureWithRoot(t, dir, ticket, "renewing-host", root, issuerPrivateKey, 5*time.Minute)
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--renew-enrollment-certificate",
		"--enrollment-root-public-key", rootPublicKey,
		"--enrollment-renew-before", "10m",
		"--enrollment-renew-valid-minutes", "120",
		"--name", "renewing-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status                string `json:"status"`
		EnrollmentCertificate struct {
			Renewed                        bool   `json:"renewed"`
			PreviousCertificateFingerprint string `json:"previous_certificate_fingerprint"`
			CertificateFingerprint         string `json:"certificate_fingerprint"`
		} `json:"enrollment_certificate"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid host output: %v\n%s", err, stdout.String())
	}
	if payload.Status != "registered-pending-approval" || !payload.EnrollmentCertificate.Renewed {
		t.Fatalf("expected renewed registration output, got %s", stdout.String())
	}
	if payload.EnrollmentCertificate.PreviousCertificateFingerprint != previousFingerprint {
		t.Fatalf("expected previous fingerprint %q, got %s", previousFingerprint, stdout.String())
	}
	renewed, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	renewedFingerprint, err := model.HostEnrollmentCertificateFingerprint(renewed)
	if err != nil {
		t.Fatal(err)
	}
	if renewedFingerprint == previousFingerprint || renewedFingerprint != payload.EnrollmentCertificate.CertificateFingerprint {
		t.Fatalf("unexpected renewed fingerprint: before=%q after=%q output=%s", previousFingerprint, renewedFingerprint, stdout.String())
	}
	if !renewed.NotAfter.After(certificate.NotAfter) {
		t.Fatalf("expected renewed certificate validity to extend: before=%s after=%s", certificate.NotAfter, renewed.NotAfter)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(renewed, root, time.Now()); err != nil {
		t.Fatalf("renewed certificate should verify: %v", err)
	}
}

func TestHostServeSkipsEnrollmentRenewalWhenCertificateIsFresh(t *testing.T) {
	dir := t.TempDir()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "fresh temporary host")
	if err != nil {
		t.Fatal(err)
	}
	certificatePath, rootPublicKey, _, certificate, _ := writeHostServeEnrollmentCertificateFixtureWithRoot(t, dir, ticket, "fresh-host", root, issuerPrivateKey, 2*time.Hour)
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--ticket-code", ticket.Code,
		"--identity-store", filepath.Join(dir, "identity", "host.json"),
		"--identity-key-id", "host-test",
		"--enrollment-certificate", certificatePath,
		"--renew-enrollment-certificate",
		"--enrollment-root-public-key", rootPublicKey,
		"--enrollment-renew-before", "10m",
		"--name", "fresh-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		EnrollmentCertificate struct {
			Renewed bool `json:"renewed"`
		} `json:"enrollment_certificate"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid host output: %v\n%s", err, stdout.String())
	}
	if payload.EnrollmentCertificate.Renewed {
		t.Fatalf("did not expect fresh certificate renewal, got %s", stdout.String())
	}
	written, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	writtenFingerprint, err := model.HostEnrollmentCertificateFingerprint(written)
	if err != nil {
		t.Fatal(err)
	}
	if writtenFingerprint != previousFingerprint {
		t.Fatalf("expected certificate file to remain unchanged, before=%q after=%q", previousFingerprint, writtenFingerprint)
	}
}

func writeHostServeEnrollmentCertificateFixture(t *testing.T, dir string, ticket model.Ticket, name string) (string, string, model.TrustBundle, model.HostEnrollmentCertificate, ed25519.PrivateKey) {
	t.Helper()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	return writeHostServeEnrollmentCertificateFixtureWithRoot(t, dir, ticket, name, root, issuerPrivateKey, time.Hour)
}

func writeHostServeEnrollmentCertificateFixtureWithRoot(t *testing.T, dir string, ticket model.Ticket, name string, root model.TrustBundle, issuerPrivateKey ed25519.PrivateKey, ttl time.Duration) (string, string, model.TrustBundle, model.HostEnrollmentCertificate, ed25519.PrivateKey) {
	t.Helper()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	issuerPublicKey, err := root.Ed25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                name,
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		Capabilities:        ticket.Capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, root.SigningKeyID, issuerPrivateKey, time.Now().UTC().Add(-time.Minute), ttl)
	if err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	if err := writeEnrollmentCertificateFile(certificatePath, certificate, false); err != nil {
		t.Fatal(err)
	}
	return certificatePath, encodeRootPublicKey(root.SigningKeyID, issuerPublicKey), root, certificate, issuerPrivateKey
}

func TestEnrollmentFetchRevocationsWritesVerifiedList(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList([]model.HostEnrollmentCertificateRevocation{
		{
			CertificateFingerprint: "sha256:enrollment-fetch-revoked-test",
			Reason:                 "compromised",
			RevokedAt:              now,
		},
	}, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentRoot(model.NewTrustBundle("enrollment-root", publicKey)).
		WithEnrollmentRevocations(revocations)
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	outPath := filepath.Join(t.TempDir(), "revocations", "revocations.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "fetch-revocations",
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
		"--out", outPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok fetch output, got %s", stdout.String())
	}
	fetched, err := readEnrollmentRevocationListFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentRevocationListSignature(fetched, model.NewTrustBundle("enrollment-root", publicKey), time.Now()); err != nil {
		t.Fatalf("expected fetched revocations to verify: %v", err)
	}
	if len(fetched.RevokedCertificates) != 1 {
		t.Fatalf("expected one revoked certificate, got %d", len(fetched.RevokedCertificates))
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = app.Run(context.Background(), []string{
		"enrollment", "fetch-revocations",
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey("enrollment-root", wrongPublicKey),
		"--out", filepath.Join(t.TempDir(), "wrong-root.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected wrong root to reject fetched revocations, got %v", err)
	}
}

func TestEnrollmentFetchRevocationsSendsOperatorTokenFromFile(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	revocations, err := model.SignHostEnrollmentRevocationList(nil, "enrollment-root", privateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/enrollment/revocations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revocations": revocations})
	}))
	defer server.Close()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte("operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "revocations", "revocations.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "fetch-revocations",
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
		"--operator-token-file", tokenPath,
		"--out", outPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok fetch output, got %s", stdout.String())
	}
}

func TestEnrollmentInitRevocationsWritesEmptyVerifiedList(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	revocationsPath := filepath.Join(dir, "revocations", "revocations.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"enrollment", "init-revocations",
		"--out", revocationsPath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK                      bool   `json:"ok"`
		Schema                  string `json:"schema"`
		RootPublicKey           string `json:"root_public_key"`
		RevokedCertificateCount int    `json:"revoked_certificate_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid init output: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != model.HostEnrollmentRevocationListSchemaVersion || payload.RevokedCertificateCount != 0 {
		t.Fatalf("unexpected init output: %s", stdout.String())
	}
	revocations, err := readEnrollmentRevocationListFile(revocationsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(revocations.RevokedCertificates) != 0 {
		t.Fatalf("expected empty revocation list, got %d entries", len(revocations.RevokedCertificates))
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-revocations",
		"--revocations", revocationsPath,
		"--root-public-key", payload.RootPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"revoked_certificate_count": 0`) {
		t.Fatalf("expected empty revocation verification, got %s", verifyStdout.String())
	}
}

func TestEnrollmentLifecycleKeyCustodyWritesRecord(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "custody.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "lifecycle", "key-custody",
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
		"--custodian", "release-team",
		"--provider", "kms",
		"--rotation-days", "30",
		"--out", outPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(readFileForTest(t, outPath), `"schema_version": "rdev.enrollment-key-custody.v1"`) {
		t.Fatalf("unexpected key custody output: %s", stdout.String())
	}
}

func TestEnrollmentLifecycleFleetRenewalPlanRequiresRevocations(t *testing.T) {
	certificatesPath := filepath.Join(t.TempDir(), "certificates.json")
	if err := os.WriteFile(certificatesPath, []byte(`{"certificates":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "lifecycle", "fleet-renewal-plan",
		"--certificates", certificatesPath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", publicKey),
	})
	if err == nil || !strings.Contains(err.Error(), "revocations are required by policy") {
		t.Fatalf("expected missing revocations to fail, got %v", err)
	}
}

func TestEnrollmentLifecycleEmergencyDrillWritesEvidence(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "enrollment-root.json")
	revocationsPath := filepath.Join(dir, "revocations.json")
	initOut := bytes.Buffer{}
	initApp := NewApp(&initOut, &bytes.Buffer{})
	if err := initApp.Run(context.Background(), []string{
		"enrollment", "init-revocations",
		"--out", revocationsPath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
	}); err != nil {
		t.Fatal(err)
	}
	var initPayload struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(initOut.Bytes(), &initPayload); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "drill.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"enrollment", "lifecycle", "emergency-drill",
		"--name", "root-compromise-drill",
		"--scenario", "enrollment-root-compromise",
		"--operator-role", "admin",
		"--root-public-key", initPayload.RootPublicKey,
		"--revocations", revocationsPath,
		"--out", outPath,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(readFileForTest(t, outPath), `"schema_version": "rdev.enrollment-emergency-drill.v1"`) {
		t.Fatalf("unexpected emergency drill output: %s", stdout.String())
	}
	if strings.Contains(readFileForTest(t, outPath), dir) {
		t.Fatalf("drill evidence leaked local temp path: %s", readFileForTest(t, outPath))
	}
}

func TestEnrollmentIssueCertificateWritesVerifiedGatewayCertificate(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user", "git.diff"}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(root, issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "issue-certificate",
		"--gateway", server.URL,
		"--out", certificatePath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", issuerPublicKey),
		"--ticket-code", ticket.Code,
		"--name", "managed-mac",
		"--os", "darwin",
		"--arch", "arm64",
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", "shell.user",
		"--valid-minutes", "30",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK                     bool   `json:"ok"`
		Schema                 string `json:"schema"`
		CertificatePath        string `json:"certificate_path"`
		CertificateFingerprint string `json:"certificate_fingerprint"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid issue output: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != model.HostEnrollmentCertificateSchemaVersion || payload.CertificatePath != certificatePath || payload.CertificateFingerprint == "" {
		t.Fatalf("unexpected issue output: %s", stdout.String())
	}
	certificate, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.TicketCode != ticket.Code || certificate.HostName != "managed-mac" || certificate.Mode != model.HostModeManaged {
		t.Fatalf("unexpected issued certificate: %#v", certificate)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(certificate, root, now); err != nil {
		t.Fatalf("issued certificate should verify: %v", err)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-certificate",
		"--certificate", certificatePath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", issuerPublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected issued certificate verification, got %s", verifyStdout.String())
	}
}

func TestEnrollmentIssueCertificateRejectsWrongPinnedRoot(t *testing.T) {
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now }).
		WithEnrollmentIssuer(model.NewTrustBundle("enrollment-root", issuerPublicKey), issuerPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, []string{"shell.user"}, "managed enrollment")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "certs", "host-enrollment.json")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "issue-certificate",
		"--gateway", server.URL,
		"--out", outPath,
		"--root-public-key", encodeRootPublicKey("enrollment-root", wrongPublicKey),
		"--ticket-code", ticket.Code,
		"--name", "managed-mac",
		"--os", "darwin",
		"--arch", "arm64",
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match pinned root-public-key") {
		t.Fatalf("expected pinned root rejection, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no certificate to be written, stat err=%v", statErr)
	}
}

func TestEnrollmentIssueCertificateSendsOperatorTokenFromFile(t *testing.T) {
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user"}
	ticket, err := model.NewTicket(model.HostModeManaged, 600, capabilities, "managed enrollment", now)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, root.SigningKeyID, issuerPrivateKey, now, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/enrollment/certificates" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"certificate":             certificate,
			"certificate_fingerprint": fingerprint,
			"enrollment_root":         root,
		})
	}))
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte(" operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "certs", "host-enrollment.json")
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "issue-certificate",
		"--gateway", server.URL,
		"--out", outPath,
		"--root-public-key", encodeRootPublicKey(root.SigningKeyID, issuerPublicKey),
		"--ticket-code", ticket.Code,
		"--name", registration.Name,
		"--os", registration.OS,
		"--arch", registration.Arch,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--operator-token-file", tokenPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
	if _, err := readEnrollmentCertificateFile(outPath); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentRenewCertificateExtendsVerifiedCertificate(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "renew enrollment certificate")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	err = signApp.Run(context.Background(), []string{
		"enrollment", "sign-certificate",
		"--out", certificatePath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
		"--ticket-code", ticket.Code,
		"--mode", "managed",
		"--name", "renew-host",
		"--os", runtime.GOOS,
		"--arch", runtime.GOARCH,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", strings.Join(capabilities, ","),
		"--valid-minutes", "30",
	})
	if err != nil {
		t.Fatal(err)
	}
	var signPayload struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signPayload); err != nil {
		t.Fatalf("invalid sign output: %v\n%s", err, signStdout.String())
	}
	original, err := readEnrollmentCertificateFile(certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	originalFingerprint, err := model.HostEnrollmentCertificateFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}

	revocationsPath := filepath.Join(dir, "revocations", "revocations.json")
	var initStdout bytes.Buffer
	initApp := NewApp(&initStdout, &bytes.Buffer{})
	err = initApp.Run(context.Background(), []string{
		"enrollment", "init-revocations",
		"--out", revocationsPath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
	})
	if err != nil {
		t.Fatal(err)
	}

	renewedPath := filepath.Join(dir, "certs", "host-enrollment-renewed.json")
	var renewStdout bytes.Buffer
	renewApp := NewApp(&renewStdout, &bytes.Buffer{})
	err = renewApp.Run(context.Background(), []string{
		"enrollment", "renew-certificate",
		"--certificate", certificatePath,
		"--out", renewedPath,
		"--key", keyPath,
		"--revocations", revocationsPath,
		"--valid-minutes", "120",
	})
	if err != nil {
		t.Fatal(err)
	}
	var renewPayload struct {
		OK                             bool   `json:"ok"`
		Schema                         string `json:"schema"`
		PreviousCertificateFingerprint string `json:"previous_certificate_fingerprint"`
		CertificateFingerprint         string `json:"certificate_fingerprint"`
		RootPublicKey                  string `json:"root_public_key"`
	}
	if err := json.Unmarshal(renewStdout.Bytes(), &renewPayload); err != nil {
		t.Fatalf("invalid renew output: %v\n%s", err, renewStdout.String())
	}
	if !renewPayload.OK || renewPayload.Schema != model.HostEnrollmentCertificateSchemaVersion {
		t.Fatalf("unexpected renew output: %s", renewStdout.String())
	}
	if renewPayload.RootPublicKey != signPayload.RootPublicKey {
		t.Fatalf("expected same enrollment root, got sign=%q renew=%q", signPayload.RootPublicKey, renewPayload.RootPublicKey)
	}
	if renewPayload.PreviousCertificateFingerprint != originalFingerprint {
		t.Fatalf("expected previous fingerprint %q, got %q", originalFingerprint, renewPayload.PreviousCertificateFingerprint)
	}
	if renewPayload.CertificateFingerprint == originalFingerprint {
		t.Fatalf("expected renewed fingerprint to change, got %q", renewPayload.CertificateFingerprint)
	}
	renewed, err := readEnrollmentCertificateFile(renewedPath)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.TicketCode != original.TicketCode || renewed.Mode != original.Mode || renewed.HostName != original.HostName || renewed.SubjectIdentityFingerprint != original.SubjectIdentityFingerprint {
		t.Fatalf("renewal changed certificate scope: before=%#v after=%#v", original, renewed)
	}
	if renewed.OS != original.OS || renewed.Arch != original.Arch || !slices.Equal(renewed.Capabilities, original.Capabilities) {
		t.Fatalf("renewal changed platform or capabilities: before=%#v after=%#v", original, renewed)
	}
	if !renewed.NotAfter.After(original.NotAfter) {
		t.Fatalf("expected renewed certificate to extend validity: before=%s after=%s", original.NotAfter, renewed.NotAfter)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"enrollment", "verify-certificate",
		"--certificate", renewedPath,
		"--root-public-key", signPayload.RootPublicKey,
		"--revocations", revocationsPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected renewed certificate verification, got %s", verifyStdout.String())
	}
}

func TestEnrollmentRenewCertificateFromGatewayWritesVerifiedCertificate(t *testing.T) {
	now := time.Now().UTC()
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := model.NewTrustBundle("enrollment-root", issuerPublicKey)
	capabilities := []string{"shell.user"}
	ticket, err := model.NewTicket(model.HostModeManaged, 600, capabilities, "managed enrollment", now)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          ticket.Code,
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        capabilities,
		IdentityKeyID:       identity.KeyID,
		IdentityPublicKey:   identity.EncodedPublicKey(),
		IdentityFingerprint: identity.Fingerprint(),
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, root.SigningKeyID, issuerPrivateKey, now, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	previousFingerprint, err := model.HostEnrollmentCertificateFingerprint(certificate)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := model.RenewHostEnrollmentCertificate(certificate, root, issuerPrivateKey, now, 120*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	renewedFingerprint, err := model.HostEnrollmentCertificateFingerprint(renewed)
	if err != nil {
		t.Fatal(err)
	}
	seenAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/enrollment/certificates/renew" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"certificate":                      renewed,
			"certificate_fingerprint":          renewedFingerprint,
			"previous_certificate_fingerprint": previousFingerprint,
			"enrollment_root":                  root,
		})
	}))
	defer server.Close()
	certificatePath := filepath.Join(t.TempDir(), "certs", "host-enrollment.json")
	if err := writeEnrollmentCertificateFile(certificatePath, certificate, false); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "operator-token.txt")
	if err := os.WriteFile(tokenPath, []byte("operator-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "certs", "host-enrollment-renewed.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err = app.Run(context.Background(), []string{
		"enrollment", "renew-certificate",
		"--certificate", certificatePath,
		"--out", outPath,
		"--gateway", server.URL,
		"--root-public-key", encodeRootPublicKey(root.SigningKeyID, issuerPublicKey),
		"--operator-token-file", tokenPath,
		"--valid-minutes", "120",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer operator-secret" {
		t.Fatalf("expected bearer token header, got %q", seenAuthorization)
	}
	var payload struct {
		OK                             bool   `json:"ok"`
		Schema                         string `json:"schema"`
		PreviousCertificateFingerprint string `json:"previous_certificate_fingerprint"`
		CertificateFingerprint         string `json:"certificate_fingerprint"`
		RootPublicKey                  string `json:"root_public_key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid renew output: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != model.HostEnrollmentCertificateSchemaVersion || payload.RootPublicKey != encodeRootPublicKey(root.SigningKeyID, issuerPublicKey) {
		t.Fatalf("unexpected renew output: %s", stdout.String())
	}
	if payload.PreviousCertificateFingerprint != previousFingerprint || payload.CertificateFingerprint != renewedFingerprint {
		t.Fatalf("unexpected fingerprints in renew output: %s", stdout.String())
	}
	written, err := readEnrollmentCertificateFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.VerifyHostEnrollmentCertificateSignature(written, root, time.Now()); err != nil {
		t.Fatalf("written renewed certificate should verify: %v", err)
	}
}

func TestEnrollmentRenewCertificateRejectsRevokedCertificate(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity", "host.json")
	identity, _, err := hostidentity.LoadOrCreate(identityPath, "host-test")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeManaged, 600, capabilities, "revoked renewal")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "keys", "enrollment-root.json")
	certificatePath := filepath.Join(dir, "certs", "host-enrollment.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	err = signApp.Run(context.Background(), []string{
		"enrollment", "sign-certificate",
		"--out", certificatePath,
		"--key", keyPath,
		"--key-id", "enrollment-root",
		"--ticket-code", ticket.Code,
		"--mode", "managed",
		"--name", "revoked-renew-host",
		"--os", runtime.GOOS,
		"--arch", runtime.GOARCH,
		"--identity-key-id", identity.KeyID,
		"--identity-public-key", identity.EncodedPublicKey(),
		"--identity-fingerprint", identity.Fingerprint(),
		"--capabilities", strings.Join(capabilities, ","),
	})
	if err != nil {
		t.Fatal(err)
	}

	revocationsPath := filepath.Join(dir, "revocations", "revocations.json")
	var revokeStdout bytes.Buffer
	revokeApp := NewApp(&revokeStdout, &bytes.Buffer{})
	err = revokeApp.Run(context.Background(), []string{
		"enrollment", "revoke-certificate",
		"--out", revocationsPath,
		"--key", keyPath,
		"--certificate", certificatePath,
		"--reason", "renewal blocked",
	})
	if err != nil {
		t.Fatal(err)
	}
	var renewStdout bytes.Buffer
	renewApp := NewApp(&renewStdout, &bytes.Buffer{})
	err = renewApp.Run(context.Background(), []string{
		"enrollment", "renew-certificate",
		"--certificate", certificatePath,
		"--out", filepath.Join(dir, "certs", "host-enrollment-renewed.json"),
		"--key", keyPath,
		"--revocations", revocationsPath,
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked certificate renewal failure, got %v", err)
	}
}

func TestHostServeRegistersWithProtectedIdentityStore(t *testing.T) {
	backend := &cliMemoryKeychainBackend{items: map[string][]byte{}}
	restore := protectedstore.SetKeychainBackendForTest(backend)
	defer restore()

	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	identityRef := "keychain:remote-dev-skillkit/cli-managed-host"

	firstFingerprint := runHostServeWithIdentityStore(t, server.URL, ticket.Code, identityRef, "test-host-1")
	if len(backend.items) != 1 {
		t.Fatalf("expected one protected identity item, got %d", len(backend.items))
	}
	secondTicket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint := runHostServeWithIdentityStore(t, server.URL, secondTicket.Code, identityRef, "test-host-2")
	if secondFingerprint != firstFingerprint {
		t.Fatalf("expected protected identity fingerprint reuse, got %s then %s", firstFingerprint, secondFingerprint)
	}
}

func TestHostServeRegistersWithJoinManifest(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{"host", "serve", "--mode", "temporary", "--manifest-url", server.URL + "/v1/tickets/" + ticket.Code + "/manifest", "--name", "manifest-host"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Status string `json:"status"`
		Host   struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "registered-pending-approval" {
		t.Fatalf("unexpected status %q", payload.Status)
	}
	if payload.Host.Name != "manifest-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
	}
}

func TestHostServePreservesGatewayOverrideWithJoinManifest(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", server.URL,
		"--manifest-url", server.URL + "/v1/tickets/" + ticket.Code + "/manifest",
		"--name", "override-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Gateway string `json:"gateway"`
		Host    struct {
			Name string `json:"name"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Gateway != server.URL || payload.Host.Name != "override-host" {
		t.Fatalf("expected explicit gateway override to survive manifest verification, got %#v", payload)
	}
}

func TestHostServeSelectsReachableManifestGatewayCandidate(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	candidates := url.QueryEscape(`[
		{"url":"http://127.0.0.1:1","kind":"lan-private","scope":"unreachable-test","recommended":true},
		{"url":"` + server.URL + `","kind":"relay","scope":"configured-relay"}
	]`)
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--manifest-url", server.URL + "/v1/tickets/" + ticket.Code + "/manifest?gateway_url_candidates=" + candidates,
		"--name", "candidate-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Gateway                  string `json:"gateway"`
		ManifestGatewaySelection struct {
			SelectedGatewayURL string `json:"selected_gateway_url"`
			Source             string `json:"source"`
		} `json:"manifest_gateway_selection"`
		Host struct {
			Name string `json:"name"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Gateway != server.URL || payload.ManifestGatewaySelection.SelectedGatewayURL != server.URL {
		t.Fatalf("expected host serve to select reachable manifest candidate, got %#v", payload)
	}
	if payload.Host.Name != "candidate-host" || payload.ManifestGatewaySelection.Source != "signed-join-manifest-candidates" {
		t.Fatalf("unexpected host/selection payload: %#v", payload)
	}
}

func TestHostServeRegistersWithJoinManifestRoot(t *testing.T) {
	gatewayPublicKey, gatewayPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestPublicKey, manifestPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, "gateway-jobs", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err = app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--manifest-url", server.URL + "/v1/tickets/" + ticket.Code + "/manifest",
		"--manifest-root-public-key", encodeRootPublicKey("manifest-root", manifestPublicKey),
		"--name", "manifest-root-host",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Host struct {
			Name string `json:"name"`
		} `json:"host"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.Name != "manifest-root-host" {
		t.Fatalf("expected host name override, got %q", payload.Host.Name)
	}
}

func TestFetchJoinManifestRejectsWrongManifestRoot(t *testing.T) {
	gatewayPublicKey, gatewayPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifestPublicKey, manifestPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, "gateway-jobs", gatewayPublicKey, gatewayPrivateKey).
		WithManifestSigningKey("manifest-root", manifestPublicKey, manifestPrivateKey)
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilitiesToStrings(policy.TemporaryDefaults()), "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	_, err = fetchJoinManifest(
		context.Background(),
		nil,
		server.URL+"/v1/tickets/"+ticket.Code+"/manifest",
		"",
		encodeRootPublicKey("manifest-root", wrongPublicKey),
	)
	if !errors.Is(err, model.ErrJoinManifestSignature) {
		t.Fatalf("expected manifest signature failure, got %v", err)
	}
}

func TestFetchJoinManifestRejectsPinMismatch(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilitiesToStrings(policy.TemporaryDefaults()), "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()

	_, err = fetchJoinManifest(context.Background(), nil, server.URL+"/v1/tickets/"+ticket.Code+"/manifest", "sha256:0000", "")
	if err == nil {
		t.Fatal("expected trust pin mismatch")
	}
	if !strings.Contains(err.Error(), "trust pin mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostServePollsAndCompletesDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, nil, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"exit_code": 0`) {
		t.Fatalf("expected shell execution evidence, got %s", artifacts[0].Content)
	}
}

func TestHostServeWSSCompletesDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "wss-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL: server.URL,
		Transport:  "wss",
		MaxJobs:    1,
	}, nil, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
	if artifacts := gw.Artifacts(job.ID); len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
}

func TestHostServePollsAndCompletesDevJobWithLocalMTLSGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "mtls-job-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gw).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()
	client, err := gatewayHTTPClient(hostServeOptions{
		GatewayCACertPath:     material.CACert,
		GatewayClientCertPath: material.ClientCert,
		GatewayClientKeyPath:  material.ClientKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, client, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
}

func TestHostServeWSSCompletesDevJobWithLocalMTLSGateway(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "wss-mtls-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gw).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()
	client, err := gatewayHTTPClient(hostServeOptions{
		GatewayCACertPath:     material.CACert,
		GatewayClientCertPath: material.ClientCert,
		GatewayClientKeyPath:  material.ClientKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:            server.URL,
		Transport:             "wss",
		MaxJobs:               1,
		GatewayCACertPath:     material.CACert,
		GatewayClientCertPath: material.ClientCert,
		GatewayClientKeyPath:  material.ClientKey,
	}, client, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
}

func TestHostServeCapturesRuntimeFixtureArtifact(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:            server.URL,
		PollInterval:          1,
		MaxJobs:               1,
		CaptureRuntimeFixture: true,
	}, nil, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 2 {
		t.Fatalf("expected result artifact and runtime fixture, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"schema_version": "rdev.shell-result.v1"`) {
		t.Fatalf("expected primary shell result artifact, got %s", artifacts[0].Content)
	}
	if artifacts[1].Name != "adapter-runtime-fixture.json" {
		t.Fatalf("expected runtime fixture artifact name, got %q", artifacts[1].Name)
	}
	if !strings.Contains(artifacts[1].Content, `"schema_version": "rdev.adapter-runtime-fixture.v1"`) {
		t.Fatalf("expected runtime fixture content, got %s", artifacts[1].Content)
	}
}

func TestHostServeLongPollWaitsAndCompletesDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)

	go func() {
		processed, err := app.pollAndRunDevJobs(ctx, hostServeOptions{
			GatewayURL:      server.URL,
			Transport:       "long-poll",
			LongPollTimeout: time.Second,
			MaxJobs:         1,
		}, nil, host.ID, "")
		if err != nil {
			done <- err
			return
		}
		if processed != 1 {
			done <- errors.New("expected one processed long-poll job")
			return
		}
		done <- nil
	}()

	time.Sleep(50 * time.Millisecond)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", completed.Status)
	}
}

func TestHostServeAutoFallsBackToLongPoll(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	processed, err := app.pollAndRunDevJobs(ctx, hostServeOptions{
		GatewayURL:      server.URL,
		Transport:       "auto",
		LongPollTimeout: 100 * time.Millisecond,
		MaxJobs:         1,
	}, nil, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded after auto fallback, got %s", completed.Status)
	}
}

func TestHostServeAutoSwitchesSignedManifestGatewayCandidateAfterFailure(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trustPin, err := gw.TrustBundle().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	processed, err := app.pollAndRunDevJobs(ctx, hostServeOptions{
		GatewayURL:      "http://127.0.0.1:1",
		TrustPin:        trustPin,
		Transport:       "auto",
		LongPollTimeout: 100 * time.Millisecond,
		MaxJobs:         1,
		ManifestGatewayCandidates: []model.JoinManifestGatewayCandidate{
			{URL: "http://127.0.0.1:1", Kind: "lan-private", Scope: "stale-lan", Recommended: true},
			{URL: server.URL, Kind: "relay", Scope: "configured-relay"},
		},
	}, nil, host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 processed job after gateway candidate switch, got %d", processed)
	}
	completed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.JobStatusSucceeded {
		t.Fatalf("expected job succeeded after signed candidate switch, got %s", completed.Status)
	}
}

func TestHostServeCancelsRunningCodexJobWhenGatewayJobCanceled(t *testing.T) {
	requireGitForCLITest(t)
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	repo := initGitRepoForCLITest(t)
	fakeCodex := buildCLITestBinary(t, `package main

import "time"

func main() {
	time.Sleep(5 * time.Second)
}
`)
	job, err := gw.CreateJob(host.ID, "codex", "cancel running codex", map[string]any{
		"workspace_root":       repo,
		"capabilities":         []string{"codex.run", "git.diff"},
		"prompt":               "sleep until the gateway cancels this job",
		"codex_command":        fakeCodex,
		"max_duration_seconds": 30,
		"max_output_bytes":     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct {
		processed int
		err       error
	}, 1)
	go func() {
		processed, err := app.pollAndRunDevJobs(ctx, hostServeOptions{
			GatewayURL:   server.URL,
			PollInterval: 50 * time.Millisecond,
			MaxJobs:      1,
		}, nil, host.ID, "")
		done <- struct {
			processed int
			err       error
		}{processed: processed, err: err}
	}()
	waitForJobStatus(t, gw, job.ID, model.JobStatusRunning, 2*time.Second)
	if _, err := gw.CancelJob(job.ID, "operator cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.processed != 1 {
			t.Fatalf("expected one processed canceled job, got %d", result.processed)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	canceled, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != model.JobStatusCanceled {
		t.Fatalf("expected job to remain canceled, got %s", canceled.Status)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected one cancellation artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"canceled": true`) {
		t.Fatalf("expected canceled evidence artifact, got %s", artifacts[0].Content)
	}
}

func TestHostServeRejectsTrustPinMismatch(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	_, err = app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
		TrustPin:     "sha256:0000",
	}, nil, host.ID, "")
	if err == nil {
		t.Fatal("expected trust pin mismatch")
	}
	if !strings.Contains(err.Error(), "trust pin mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchHostTrustFallsBackToLegacyTrustEndpoint(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	legacy := gw.TrustBundle()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/trust":
			_ = json.NewEncoder(w).Encode(map[string]any{"trust": legacy})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if trust.Legacy == nil {
		t.Fatal("expected legacy trust fallback")
	}
	if trust.SignedBundle != nil {
		t.Fatal("did not expect signed trust bundle")
	}
	if trust.Legacy.SigningKeyID != legacy.SigningKeyID {
		t.Fatalf("expected legacy key %q, got %q", legacy.SigningKeyID, trust.Legacy.SigningKeyID)
	}
}

func TestFetchHostTrustPersistsSignedBundle(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	storePath := filepath.Join(t.TempDir(), "trust", "bundle.json")

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", storePath)
	if err != nil {
		t.Fatal(err)
	}
	if trust.SignedBundle == nil {
		t.Fatal("expected signed trust bundle")
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatal(err)
	}
}

func TestFetchHostTrustPersistsSignedBundleToProtectedStore(t *testing.T) {
	backend := &cliMemoryKeychainBackend{items: map[string][]byte{}}
	restore := protectedstore.SetKeychainBackendForTest(backend)
	defer restore()

	gw := gateway.NewMemoryGateway()
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	storeRef := "keychain:remote-dev-skillkit/cli-managed-trust"

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", storeRef)
	if err != nil {
		t.Fatal(err)
	}
	if trust.SignedBundle == nil {
		t.Fatal("expected signed trust bundle")
	}
	if len(backend.items) != 1 {
		t.Fatalf("expected one protected trust item, got %d", len(backend.items))
	}
	stored, ok, err := hosttrust.ProtectedStore{Ref: storeRef}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || stored.Sequence != gw.SignedTrustBundle().Sequence {
		t.Fatalf("expected stored protected sequence %d, got ok=%v bundle=%#v", gw.SignedTrustBundle().Sequence, ok, stored)
	}
}

func TestFetchHostTrustUsesStoredBundleWhenGatewayTrustBundleUnavailable(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	storePath := filepath.Join(t.TempDir(), "trust", "bundle.json")
	store := hosttrust.FileStore{Path: storePath}
	if err := store.Save(gw.SignedTrustBundle()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	trust, err := fetchHostTrust(context.Background(), nil, server.URL, "", storePath)
	if err != nil {
		t.Fatal(err)
	}
	if trust.SignedBundle == nil {
		t.Fatal("expected stored signed trust bundle")
	}
	if trust.SignedBundle.Sequence != gw.SignedTrustBundle().Sequence {
		t.Fatalf("expected stored sequence %d, got %d", gw.SignedTrustBundle().Sequence, trust.SignedBundle.Sequence)
	}
}

type cliMemoryKeychainBackend struct {
	items map[string][]byte
}

func (b *cliMemoryKeychainBackend) Load(service, account string) ([]byte, bool, error) {
	content, ok := b.items[service+"/"+account]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), content...), true, nil
}

func (b *cliMemoryKeychainBackend) Save(service, account string, content []byte) error {
	b.items[service+"/"+account] = append([]byte(nil), content...)
	return nil
}

func TestRefreshHostTrustUpdatePersistsGatewayUpdate(t *testing.T) {
	now := time.Now().Add(-time.Minute).UTC()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(func() time.Time { return now }, "gateway-dev", publicKey, privateKey)
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "managed-mac",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(t.TempDir(), "trust", "bundle.json")
	store := hosttrust.FileStore{Path: storePath}
	first := gw.SignedTrustBundle()
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	next, err := model.NewSignedTrustBundle(model.SignedTrustBundleSpec{
		BundleID:           first.BundleID,
		Sequence:           2,
		NotBefore:          now,
		NotAfter:           now.Add(time.Hour),
		PreviousBundleHash: firstHash,
		SigningKeyID:       "gateway-dev",
		Keys: []model.TrustKey{
			model.NewTrustKey("gateway-dev", publicKey, model.TrustKeyStatusActive, now),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Sign(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateSignedTrustBundle(next); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	current := hostTrust{SignedBundle: &first}

	updated, err := refreshHostTrustUpdate(context.Background(), nil, server.URL, host.ID, storePath, current)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SignedBundle == nil || updated.SignedBundle.Sequence != 2 {
		t.Fatalf("expected in-memory trust to update to sequence 2, got %#v", updated.SignedBundle)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Sequence != 2 {
		t.Fatalf("expected persisted sequence 2, ok=%v bundle=%#v", ok, loaded)
	}
}

func TestHostTrustRejectsReplayWithNonceStore(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	legacy := gw.TrustBundle()
	trust := hostTrust{
		Legacy:     &legacy,
		NonceStore: hostNonceStore(filepath.Join(t.TempDir(), "nonce", "store.json")),
	}
	now := time.Now()
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); err != nil {
		t.Fatalf("expected first execution to pass: %v", err)
	}
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); err == nil {
		t.Fatal("expected replay rejection")
	}
}

func TestHostTrustRejectsConsumedApprovalWithApprovalStore(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":     ".",
		"capabilities":       []string{"shell.user"},
		"argv":               []string{"go", "env", "GOOS"},
		"allow_commands":     []string{"go"},
		"approvals_required": []string{"git.push"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = gw.ApproveJob(job.ID, "git.push", "approved", "test approval")
	if err != nil {
		t.Fatal(err)
	}
	legacy := gw.TrustBundle()
	trust := hostTrust{
		Legacy:        &legacy,
		ApprovalStore: hostApprovalStore(filepath.Join(t.TempDir(), "approval", "store.json")),
	}
	now := time.Now()
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); err != nil {
		t.Fatalf("expected first approved execution to pass: %v", err)
	}
	if _, err := trust.RunDevJob(context.Background(), host.ID, "", job, now); !errors.Is(err, model.ErrApprovalTokenConsumed) {
		t.Fatalf("expected consumed approval token rejection, got %v", err)
	}
}

func TestGatewayServeDevReusesSigningKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-signing-key.json")

	key, created, err := signing.LoadOrCreate(path, signing.DefaultKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected initial key creation")
	}
	gw := gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, key.ID, key.PublicKey, key.PrivateKey)
	firstFingerprint, err := gw.TrustBundle().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	reused, created, err := signing.LoadOrCreate(path, signing.DefaultKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected key reuse")
	}
	gw = gateway.NewMemoryGatewayWithSigningKey(timeNowForTest, reused.ID, reused.PublicKey, reused.PrivateKey)
	secondFingerprint, err := gw.TrustBundle().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("expected stable fingerprint, got %s then %s", firstFingerprint, secondFingerprint)
	}
}

func TestTrustInitRotateRevokeAndVerify(t *testing.T) {
	dir := t.TempDir()
	rootKey := filepath.Join(dir, "trust-root.json")
	gatewayOne := filepath.Join(dir, "gateway-one.json")
	gatewayTwo := filepath.Join(dir, "gateway-two.json")
	firstPath := filepath.Join(dir, "trust-1.json")
	secondPath := filepath.Join(dir, "trust-2.json")
	thirdPath := filepath.Join(dir, "trust-3.json")

	var initStdout bytes.Buffer
	app := NewApp(&initStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "init",
		"--out", firstPath,
		"--root-key", rootKey,
		"--root-key-id", "trust-root",
		"--gateway-key", gatewayOne,
		"--gateway-key-id", "gateway-one",
		"--bundle-id", "managed-hosts",
		"--valid-hours", "24",
	}); err != nil {
		t.Fatal(err)
	}
	var initPayload struct {
		OK            bool   `json:"ok"`
		RootPublicKey string `json:"root_public_key"`
		Sequence      int    `json:"sequence"`
	}
	if err := json.Unmarshal(initStdout.Bytes(), &initPayload); err != nil {
		t.Fatalf("invalid trust init output: %v\n%s", err, initStdout.String())
	}
	if !initPayload.OK || initPayload.Sequence != 1 || initPayload.RootPublicKey == "" {
		t.Fatalf("unexpected trust init output: %s", initStdout.String())
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "verify",
		"--bundle", firstPath,
		"--root-public-key", initPayload.RootPublicKey,
	}); err != nil {
		t.Fatal(err)
	}
	assertTrustVerifyOK(t, verifyStdout.Bytes(), 1)

	var rotateStdout bytes.Buffer
	app = NewApp(&rotateStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "rotate",
		"--current", firstPath,
		"--out", secondPath,
		"--root-key", rootKey,
		"--gateway-key", gatewayTwo,
		"--gateway-key-id", "gateway-two",
		"--retire-key", "gateway-one",
		"--valid-hours", "24",
	}); err != nil {
		t.Fatal(err)
	}
	var rotatePayload struct {
		OK       bool `json:"ok"`
		Sequence int  `json:"sequence"`
	}
	if err := json.Unmarshal(rotateStdout.Bytes(), &rotatePayload); err != nil {
		t.Fatalf("invalid trust rotate output: %v\n%s", err, rotateStdout.String())
	}
	if !rotatePayload.OK || rotatePayload.Sequence != 2 {
		t.Fatalf("unexpected trust rotate output: %s", rotateStdout.String())
	}
	firstBundle := readTrustBundleForTest(t, firstPath)
	secondBundle := readTrustBundleForTest(t, secondPath)
	firstHash, err := firstBundle.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if secondBundle.Sequence != 2 || secondBundle.PreviousBundleHash != firstHash {
		t.Fatalf("unexpected rotated bundle: seq=%d previous=%q want %q", secondBundle.Sequence, secondBundle.PreviousBundleHash, firstHash)
	}
	if key, ok := secondBundle.Key("gateway-one"); !ok || key.Status != model.TrustKeyStatusRetired {
		t.Fatalf("expected gateway-one retired, got ok=%v key=%#v", ok, key)
	}
	if key, ok := secondBundle.Key("gateway-two"); !ok || key.Status != model.TrustKeyStatusActive {
		t.Fatalf("expected gateway-two active, got ok=%v key=%#v", ok, key)
	}

	var revokeStdout bytes.Buffer
	app = NewApp(&revokeStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "revoke",
		"--current", secondPath,
		"--out", thirdPath,
		"--root-key", rootKey,
		"--key-id", "gateway-two",
		"--reason", "test compromise",
		"--valid-hours", "24",
	}); err != nil {
		t.Fatal(err)
	}
	thirdBundle := readTrustBundleForTest(t, thirdPath)
	if thirdBundle.Sequence != 3 {
		t.Fatalf("expected sequence 3, got %d", thirdBundle.Sequence)
	}
	if key, ok := thirdBundle.Key("gateway-two"); !ok || key.Status != model.TrustKeyStatusRevoked || key.RevokedReason != "test compromise" || key.RevokedAt == nil {
		t.Fatalf("expected gateway-two revoked with reason, got ok=%v key=%#v", ok, key)
	}
	verifyStdout.Reset()
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "verify",
		"--bundle", thirdPath,
		"--root-public-key", initPayload.RootPublicKey,
	}); err != nil {
		t.Fatal(err)
	}
	assertTrustVerifyOK(t, verifyStdout.Bytes(), 3)
}

func TestTrustRevokeRefusesCurrentSigningKey(t *testing.T) {
	dir := t.TempDir()
	rootKey := filepath.Join(dir, "trust-root.json")
	gatewayKey := filepath.Join(dir, "gateway.json")
	bundlePath := filepath.Join(dir, "trust.json")
	outPath := filepath.Join(dir, "trust-revoked.json")

	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"trust", "init",
		"--out", bundlePath,
		"--root-key", rootKey,
		"--root-key-id", "trust-root",
		"--gateway-key", gatewayKey,
		"--gateway-key-id", "gateway",
	}); err != nil {
		t.Fatal(err)
	}
	err := app.Run(context.Background(), []string{
		"trust", "revoke",
		"--current", bundlePath,
		"--out", outPath,
		"--root-key", rootKey,
		"--key-id", "trust-root",
		"--reason", "root compromise",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot revoke current signing key") {
		t.Fatalf("expected current signing key revoke to fail, got %v", err)
	}
}

func TestHostServeReportsFailedDevJob(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "tool", "rdev-no-such-tool"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, nil, host.ID, "")
	if err == nil {
		t.Fatal("expected host runner failure")
	}
	if processed != 0 {
		t.Fatalf("expected 0 processed jobs, got %d", processed)
	}
	failed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failed.Status)
	}
	if failed.FailureReason == "" {
		t.Fatal("failure reason should be set")
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"exit_code":`) {
		t.Fatalf("expected failure execution evidence, got %s", artifacts[0].Content)
	}
}

func TestHostServeReportsHostDenialArtifact(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"capabilities": []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, nil, host.ID, "")
	if err == nil {
		t.Fatal("expected host runner denial")
	}
	if processed != 0 {
		t.Fatalf("expected 0 processed jobs, got %d", processed)
	}
	failed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failed.Status)
	}
	if !strings.Contains(failed.FailureReason, "Workspace root is required") {
		t.Fatalf("expected denial summary as failure reason, got %q", failed.FailureReason)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"schema_version": "rdev.host-denial.v1"`) {
		t.Fatalf("expected denial artifact, got %s", artifacts[0].Content)
	}
	if !strings.Contains(artifacts[0].Content, `"code": "workspace_required"`) {
		t.Fatalf("expected workspace_required denial artifact, got %s", artifacts[0].Content)
	}
}

func TestHostServeReportsWorkspaceLockedArtifact(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	lockStore := filepath.Join(t.TempDir(), "locks")
	if _, err := workspace.NewFileLockStore(lockStore).Acquire(workspace.LockOptions{
		RepoRoot:     repo,
		HostID:       host.ID,
		JobID:        "job_existing",
		OwnerAdapter: "codex",
		TTL:          time.Hour,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": repo,
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:         server.URL,
		PollInterval:       1,
		MaxJobs:            1,
		WorkspaceLockStore: lockStore,
	}, nil, host.ID, "")
	if err == nil {
		t.Fatal("expected workspace lock denial")
	}
	if processed != 0 {
		t.Fatalf("expected 0 processed jobs, got %d", processed)
	}
	failed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failed.Status)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"code": "workspace_locked"`) {
		t.Fatalf("expected workspace_locked denial artifact, got %s", artifacts[0].Content)
	}
}

func TestHostServeReportsApprovalRequiredArtifact(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root":     ".",
		"capabilities":       []string{"shell.user"},
		"argv":               []string{"go", "env", "GOOS"},
		"allow_commands":     []string{"go"},
		"approvals_required": []string{"git.push"},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	processed, err := app.pollAndRunDevJobs(context.Background(), hostServeOptions{
		GatewayURL:   server.URL,
		PollInterval: 1,
		MaxJobs:      1,
	}, nil, host.ID, "")
	if err == nil {
		t.Fatal("expected approval requirement")
	}
	if processed != 0 {
		t.Fatalf("expected 0 processed jobs, got %d", processed)
	}
	failed, err := gw.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.JobStatusFailed {
		t.Fatalf("expected failed job, got %s", failed.Status)
	}
	if !strings.Contains(failed.FailureReason, "requires approval") {
		t.Fatalf("expected approval summary as failure reason, got %q", failed.FailureReason)
	}
	artifacts := gw.Artifacts(job.ID)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 failure artifact, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Content, `"schema_version": "rdev.approval-required.v1"`) {
		t.Fatalf("expected approval-required artifact, got %s", artifacts[0].Content)
	}
	if !strings.Contains(artifacts[0].Content, `"git.push"`) {
		t.Fatalf("expected git.push approval requirement, got %s", artifacts[0].Content)
	}
}

func TestTicketCreateOutputsJoinURL(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"ticket", "create", "--ttl-seconds", "600", "--reason", "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "https://agent.example.com/join/") {
		t.Fatalf("expected join URL, got %q", stdout.String())
	}
}

func TestPolicyExplainOutputsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"policy", "explain", "--capability", "shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Allowed {
		t.Fatal("shell.user should be allowed in temporary mode")
	}
}

func TestPolicyExplainShellOutputsJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"policy", "explain-shell",
		"--policy-json", `{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Allowed {
		t.Fatalf("expected shell policy to be allowed, got %s", stdout.String())
	}
}

func TestDemoLocalOutputsClosedLoop(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"demo", "local"})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Host struct {
			Status string `json:"status"`
		} `json:"host"`
		Job struct {
			Status string `json:"status"`
		} `json:"job"`
		Audit []struct {
			Action string `json:"action"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host.Status != "active" {
		t.Fatalf("host should be active, got %q", payload.Host.Status)
	}
	if payload.Job.Status != "succeeded" {
		t.Fatalf("job should succeed, got %q", payload.Job.Status)
	}
	if len(payload.Audit) != 5 {
		t.Fatalf("expected 5 audit events, got %d", len(payload.Audit))
	}
}

func TestGatewayServeRequiresDevFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{"gateway", "serve"})
	if err == nil {
		t.Fatal("expected gateway serve without --dev to fail")
	}
	if !strings.Contains(err.Error(), "requires --dev") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayServeStateRequiresSigningKey(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	err := app.Run(context.Background(), []string{
		"gateway", "serve",
		"--dev",
		"--state", filepath.Join(t.TempDir(), "state.json"),
	})
	if err == nil {
		t.Fatal("expected gateway serve --state without --signing-key to fail")
	}
	if !strings.Contains(err.Error(), "persistent storage requires --signing-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayTLSConfigRequiresCompleteKeyPair(t *testing.T) {
	_, err := gatewayTLSConfig(gatewayServeOptions{TLSCertPath: filepath.Join(t.TempDir(), "cert.pem")})
	if err == nil || !strings.Contains(err.Error(), "both --tls-cert and --tls-key") {
		t.Fatalf("expected incomplete TLS keypair error, got %v", err)
	}
	_, err = gatewayTLSConfig(gatewayServeOptions{ClientCAPath: filepath.Join(t.TempDir(), "ca.pem")})
	if err == nil || !strings.Contains(err.Error(), "--client-ca requires --tls-cert and --tls-key") {
		t.Fatalf("expected client CA TLS requirement, got %v", err)
	}
}

func TestGatewayHTTPClientRequiresCompleteClientKeyPair(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	_, err := gatewayHTTPClient(hostServeOptions{
		GatewayCACertPath:     material.CACert,
		GatewayClientCertPath: material.ClientCert,
	})
	if err == nil || !strings.Contains(err.Error(), "both --gateway-client-cert and --gateway-client-key") {
		t.Fatalf("expected incomplete gateway client keypair error, got %v", err)
	}
	_, err = gatewayHTTPClient(hostServeOptions{
		GatewayCACertPath:    material.CACert,
		GatewayClientKeyPath: material.ClientKey,
	})
	if err == nil || !strings.Contains(err.Error(), "both --gateway-client-cert and --gateway-client-key") {
		t.Fatalf("expected incomplete gateway client keypair error, got %v", err)
	}
}

func TestGatewayHTTPClientRejectsInvalidCA(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := gatewayHTTPClient(hostServeOptions{GatewayCACertPath: caPath})
	if err == nil || !strings.Contains(err.Error(), "--gateway-ca does not contain a valid PEM certificate") {
		t.Fatalf("expected invalid gateway CA error, got %v", err)
	}
}

func TestGatewayTLSConfigLoadsServerTLS(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath: material.ServerCert,
		TLSKeyPath:  material.ServerKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config == nil {
		t.Fatal("expected TLS config")
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 minimum, got %d", config.MinVersion)
	}
	if len(config.Certificates) != 1 {
		t.Fatalf("expected one server certificate, got %d", len(config.Certificates))
	}
	if config.ClientAuth != tls.NoClientCert {
		t.Fatalf("expected no client cert requirement, got %v", config.ClientAuth)
	}
}

func TestGatewayTLSConfigRequiresClientCertificatesWhenClientCASet(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected client certificate enforcement, got %v", config.ClientAuth)
	}
	if config.ClientCAs == nil {
		t.Fatal("expected client CA pool")
	}
}

func TestGatewayDevMTLSHealthzRequiresClientCertificate(t *testing.T) {
	material := writeGatewayTLSMaterial(t)
	config, err := gatewayTLSConfig(gatewayServeOptions{
		TLSCertPath:  material.ServerCert,
		TLSKeyPath:   material.ServerKey,
		ClientCAPath: material.CACert,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(httpapi.NewServer(gateway.NewMemoryGateway()).Handler())
	server.TLS = config
	server.StartTLS()
	defer server.Close()

	caPEM, err := os.ReadFile(material.CACert)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("expected test CA PEM to parse")
	}
	noClientCert := server.Client()
	noClientCert.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	resp, err := noClientCert.Get(server.URL + "/healthz")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected TLS handshake to fail without a client certificate")
	}

	clientCert, err := tls.LoadX509KeyPair(material.ClientCert, material.ClientKey)
	if err != nil {
		t.Fatal(err)
	}
	withClientCert := server.Client()
	withClientCert.Transport = &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      roots,
		Certificates: []tls.Certificate{clientCert},
	}}
	resp, err = withClientCert.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with client certificate, got %d", resp.StatusCode)
	}
}

func TestAuditExportAndVerify(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")
	chainPath := filepath.Join(dir, "chain.json")
	store := audit.NewJSONLStore(jsonlPath)
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	for _, event := range []model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "ticket.create", TargetID: "tkt_1", Message: "created", At: now},
		{Sequence: 2, Actor: "host", Action: "host.register", TargetID: "hst_1", Message: "registered", At: now.Add(time.Second)},
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	var exportStdout bytes.Buffer
	exportApp := NewApp(&exportStdout, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{"audit", "export", "--input", jsonlPath, "--out", chainPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exportStdout.String(), `"ok": true`) {
		t.Fatalf("expected export ok, got %s", exportStdout.String())
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{"audit", "verify", "--input", chainPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"event_count": 2`) {
		t.Fatalf("expected verify event count, got %s", verifyStdout.String())
	}
}

func TestAuditVerifyRejectsTamperedChain(t *testing.T) {
	dir := t.TempDir()
	chainPath := filepath.Join(dir, "chain.json")
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	chain, err := audit.ExportChain([]model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "ticket.create", TargetID: "tkt_1", Message: "created", At: now},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	chain.Entries[0].Event.Message = "tampered"
	if err := audit.WriteChain(chainPath, chain); err != nil {
		t.Fatal(err)
	}
	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{"audit", "verify", "--input", chainPath}); err == nil {
		t.Fatal("expected tampered chain verification to fail")
	}
}

func TestEvidenceExportWritesBundle(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	job := model.Job{
		ID:        "job_1",
		HostID:    "hst_1",
		Adapter:   "shell",
		Intent:    "demo",
		Status:    model.JobStatusSucceeded,
		CreatedAt: now,
		Envelope: &model.JobEnvelope{
			SchemaVersion: "rdev.job.v1",
			JobID:         "job_1",
			HostID:        "hst_1",
			TicketID:      "tkt_1",
			OperatorID:    "operator",
			IssuedAt:      now,
			ExpiresAt:     now.Add(time.Hour),
			Nonce:         "nonce",
			Mode:          model.HostModeAttendedTemporary,
			Adapter:       "shell",
			Intent:        "demo",
			Capabilities:  []string{"shell.user"},
			Limits:        model.JobLimits{MaxDurationSeconds: 60, MaxOutputBytes: 1024, Network: "default-deny"},
			SigningAlg:    "ed25519",
			SigningKeyID:  "gateway-dev",
			Signature:     "signature",
		},
	}
	artifact := model.Artifact{
		ID:        "art_1",
		JobID:     "job_1",
		Kind:      "text",
		Name:      "result.json",
		Content:   `{"schema_version":"rdev.shell-result.v1"}`,
		CreatedAt: now,
	}
	jobPath := filepath.Join(dir, "job.json")
	artifactsPath := filepath.Join(dir, "artifacts.json")
	auditPath := filepath.Join(dir, "events.jsonl")
	out := filepath.Join(dir, "bundle")
	writeJSONForTest(t, jobPath, job)
	writeJSONForTest(t, artifactsPath, []model.Artifact{artifact})
	store := audit.NewJSONLStore(auditPath)
	for _, event := range []model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "job.create", TargetID: "job_1", Message: "created", At: now},
		{Sequence: 2, Actor: "host", Action: "job.complete", TargetID: "job_1", Message: "done", At: now},
	} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"evidence", "export",
		"--job-json", jobPath,
		"--artifacts-json", artifactsPath,
		"--audit-jsonl", auditPath,
		"--out", out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) {
		t.Fatalf("expected ok output, got %s", stdout.String())
	}
	for _, path := range []string{"manifest.json", "job.json", "envelope.json", "artifacts/art_1-result.json", "audit-chain.json", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected bundle file %s: %v", path, err)
		}
	}
}

func TestEvidenceExportFromGatewayJobIDWritesBundle(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	capabilities := capabilitiesToStrings(policy.TemporaryDefaults())
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, capabilities, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode:   ticket.Code,
		Name:         "test-host",
		OS:           "darwin",
		Arch:         "arm64",
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CompleteJobForHost(host.ID, job.ID, `{"schema_version":"rdev.shell-result.v1","exit_code":0}`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewServer(gw).Handler())
	defer server.Close()
	out := filepath.Join(t.TempDir(), "bundle")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err = app.Run(context.Background(), []string{
		"evidence", "export",
		"--gateway", server.URL,
		"--job-id", job.ID,
		"--out", out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"source": "gateway"`) {
		t.Fatalf("expected gateway source output, got %s", stdout.String())
	}
	for _, path := range []string{"manifest.json", "job.json", "envelope.json", "artifacts.json", "audit-slice.jsonl", "audit-chain.json", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected gateway evidence bundle file %s: %v", path, err)
		}
	}
	artifactFiles, err := os.ReadDir(filepath.Join(out, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifactFiles) != 1 {
		t.Fatalf("expected one artifact file, got %d", len(artifactFiles))
	}
	artifactContent := readFileForTest(t, filepath.Join(out, "artifacts", artifactFiles[0].Name()))
	if !strings.Contains(artifactContent, "rdev.shell-result.v1") {
		t.Fatalf("expected shell result artifact content, got %s", artifactContent)
	}
	if content := readFileForTest(t, filepath.Join(out, "audit-slice.jsonl")); !strings.Contains(content, "job.complete") {
		t.Fatalf("expected job.complete audit event, got %s", content)
	}
}

func TestSkillkitExportWritesInstallBundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "skillkit")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--gateway-url", "https://api.example.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.skillkit-bundle.v1"`) {
		t.Fatalf("expected skillkit schema output, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"adaptive_configuration_schema": "rdev.adaptive-configuration-contract.v1"`) ||
		!strings.Contains(stdout.String(), `"adaptive_configuration_required": true`) {
		t.Fatalf("expected adaptive configuration output, got %s", stdout.String())
	}
	for _, path := range []string{
		"manifest.json",
		"INSTALL.md",
		"mcp/tools.json",
		"skills/remote-vibe-coding/SKILL.md",
		"frameworks/codex.md",
		"frameworks/claude-code.md",
		"frameworks/hermes.md",
		"frameworks/openclaw-opencode.md",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected skillkit bundle file %s: %v", path, err)
		}
	}
}

func TestSkillkitVerifyChecksInstallBundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--gateway-url", "https://api.example.com/v1",
	}); err != nil {
		t.Fatal(err)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"skillkit", "verify",
		"--bundle", out,
	}); err != nil {
		t.Fatalf("expected verify to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) || !strings.Contains(verifyStdout.String(), `"schema": "rdev.skillkit-bundle-verification.v1"`) {
		t.Fatalf("expected skillkit verification output, got %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"adaptive_configuration_verified": true`) {
		t.Fatalf("expected adaptive configuration verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(out, "skills", "host-triage", "SKILL.md"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"skillkit", "verify",
		"--bundle", out,
	})
	if err == nil {
		t.Fatalf("expected tampered bundle verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) || !strings.Contains(tamperedStdout.String(), "listed_files_sha256_match") {
		t.Fatalf("expected structured tamper failure, got %s", tamperedStdout.String())
	}
}

func TestSkillkitPlanInstallAndVerifyInstallPlan(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", bundle,
		"--gateway-url", "https://api.example.com/v1",
	}); err != nil {
		t.Fatal(err)
	}

	planDir := filepath.Join(t.TempDir(), "install-plan")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"skillkit", "plan-install",
		"--bundle", bundle,
		"--out", planDir,
		"--frameworks", "codex,generic",
		"--rdev-command", "rdev-test",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planStdout.String(), `"schema": "rdev.skillkit-install-plan.v1"`) || !strings.Contains(planStdout.String(), `"external_mutation": false`) {
		t.Fatalf("expected structured install plan output, got %s", planStdout.String())
	}
	if !strings.Contains(planStdout.String(), `"adaptive_configuration_schema": "rdev.adaptive-configuration-contract.v1"`) {
		t.Fatalf("expected adaptive configuration plan output, got %s", planStdout.String())
	}
	for _, path := range []string{
		"install-plan.json",
		"INSTALL_COMMANDS.md",
		"install-codex.sh",
		"install-codex.ps1",
		"install-generic-mcp-agent.sh",
		"install-generic-mcp-agent.ps1",
	} {
		if _, err := os.Stat(filepath.Join(planDir, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected install plan file %s: %v", path, err)
		}
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"skillkit", "verify-install-plan",
		"--plan", filepath.Join(planDir, "install-plan.json"),
	}); err != nil {
		t.Fatalf("expected verify-install-plan to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) || !strings.Contains(verifyStdout.String(), `"schema": "rdev.skillkit-install-plan-verification.v1"`) {
		t.Fatalf("expected install plan verification output, got %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"adaptive_configuration_verified": true`) {
		t.Fatalf("expected adaptive configuration install-plan verification, got %s", verifyStdout.String())
	}
}

func TestSkillkitInstallDryRunAndExecute(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "skillkit")
	exportApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := exportApp.Run(context.Background(), []string{
		"skillkit", "export",
		"--source-root", filepath.Join("..", ".."),
		"--out", bundle,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "codex-skills")

	var dryRunStdout bytes.Buffer
	dryRunApp := NewApp(&dryRunStdout, &bytes.Buffer{})
	if err := dryRunApp.Run(context.Background(), []string{
		"skillkit", "install",
		"--bundle", bundle,
		"--framework", "codex",
		"--target", target,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dryRunStdout.String(), `"schema": "rdev.skillkit-install-report.v1"`) ||
		!strings.Contains(dryRunStdout.String(), `"execute": false`) ||
		!strings.Contains(dryRunStdout.String(), `"local_mutation": false`) {
		t.Fatalf("expected dry-run install report, got %s", dryRunStdout.String())
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not copy skills, stat err=%v", err)
	}

	var executeStdout bytes.Buffer
	executeApp := NewApp(&executeStdout, &bytes.Buffer{})
	if err := executeApp.Run(context.Background(), []string{
		"skillkit", "install",
		"--bundle", bundle,
		"--framework", "codex",
		"--target", target,
		"--execute",
	}); err != nil {
		t.Fatalf("expected execute install to pass: %v\n%s", err, executeStdout.String())
	}
	if !strings.Contains(executeStdout.String(), `"executed": true`) ||
		!strings.Contains(executeStdout.String(), `"external_mutation": false`) {
		t.Fatalf("expected executed install report, got %s", executeStdout.String())
	}
	if _, err := os.Stat(filepath.Join(target, "remote-vibe-coding", "SKILL.md")); err != nil {
		t.Fatalf("expected installed skill: %v", err)
	}
}

func TestAdapterVerifyResultAcceptsShellArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": 0,
  "timed_out": false,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-result",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	}); err != nil {
		t.Fatal(err)
	}
	var report struct {
		SchemaVersion string `json:"schema_version"`
		OK            bool   `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid conformance output: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion != "rdev.adapter-conformance-report.v1" || !report.OK {
		t.Fatalf("unexpected conformance output: %s", stdout.String())
	}
}

func TestAdapterVerifyResultRejectsMissingCommandEvidence(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-result",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	})
	if err == nil || !strings.Contains(err.Error(), "conformance failed") {
		t.Fatalf("expected conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) {
		t.Fatalf("expected structured failure report, got %s", stdout.String())
	}
}

func TestAdapterScaffoldCreatesVerifiableLifecycleManifest(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "claude-code-lifecycle.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "scaffold",
		"--adapter", "claude-code",
		"--out", artifactPath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Schema       string `json:"schema"`
		OK           bool   `json:"ok"`
		Adapter      string `json:"adapter"`
		Manifest     string `json:"manifest"`
		ResultSchema string `json:"result_schema"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid scaffold output: %v\n%s", err, stdout.String())
	}
	if payload.Schema != "rdev.adapter-scaffold.v1" || !payload.OK || payload.Adapter != "claude-code" || payload.Manifest != artifactPath || payload.ResultSchema != "rdev.claude-code-result.v1" {
		t.Fatalf("unexpected scaffold output: %s", stdout.String())
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"adapter", "verify-lifecycle",
		"--artifact", artifactPath,
		"--adapter", "claude-code",
	}); err != nil {
		t.Fatalf("generated lifecycle manifest should verify: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected generated lifecycle manifest verification to pass, got %s", verifyStdout.String())
	}
}

func TestAdapterScaffoldRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "adapter.json")
	if err := os.WriteFile(artifactPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "scaffold",
		"--adapter", "claude-code",
		"--out", artifactPath,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "{}\n" {
		t.Fatalf("scaffold should not overwrite without --force, got %s", string(content))
	}
}

func TestAdapterVerifyLifecycleAcceptsManifest(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "claude-code-lifecycle.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_approvals": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_jobs": false,
    "adapter_approves_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": true, "evidence_field": "canceled", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-lifecycle",
		"--artifact", artifactPath,
		"--adapter", "claude-code",
	}); err != nil {
		t.Fatal(err)
	}
	var report struct {
		SchemaVersion  string `json:"schema_version"`
		ArtifactSchema string `json:"artifact_schema"`
		OK             bool   `json:"ok"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid lifecycle conformance output: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion != "rdev.adapter-conformance-report.v1" || report.ArtifactSchema != "rdev.adapter-lifecycle.v1" || !report.OK {
		t.Fatalf("unexpected lifecycle conformance output: %s", stdout.String())
	}
}

func TestAdapterVerifyLifecycleRejectsMissingCancellation(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "claude-code-lifecycle.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_approvals": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": false},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_jobs": false,
    "adapter_approves_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": false, "evidence_field": "", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-lifecycle",
		"--artifact", artifactPath,
		"--adapter", "claude-code",
	})
	if err == nil || !strings.Contains(err.Error(), "lifecycle conformance failed") {
		t.Fatalf("expected lifecycle conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) || !strings.Contains(stdout.String(), "run_supports_cancellation") {
		t.Fatalf("expected structured lifecycle failure report, got %s", stdout.String())
	}
}

func TestAdapterVerifyCancellationAcceptsCanceledShellArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": false,
  "canceled": true,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-cancellation",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "cancellation_canceled_true:.") {
		t.Fatalf("expected cancellation conformance success, got %s", stdout.String())
	}
}

func TestAdapterVerifyCancellationRejectsTimeoutArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "shell-result.json")
	if err := os.WriteFile(artifactPath, []byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": true,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-cancellation",
		"--artifact", artifactPath,
		"--adapter", "shell",
		"--schema", "rdev.shell-result.v1",
	})
	if err == nil || !strings.Contains(err.Error(), "cancellation conformance failed") {
		t.Fatalf("expected cancellation conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) || !strings.Contains(stdout.String(), "cancellation_not_timed_out:.") {
		t.Fatalf("expected structured cancellation failure, got %s", stdout.String())
	}
}

func TestAdapterVerifyRuntimeAcceptsRuntimeFixture(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "adapter-runtime-fixture.json")
	if err := os.WriteFile(artifactPath, []byte(runtimeFixtureJSON("fake", true)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"adapter", "verify-runtime",
		"--artifact", artifactPath,
		"--adapter", "fake",
		"--require-result-artifact",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"ok": true`) || !strings.Contains(stdout.String(), "result_artifact_object") {
		t.Fatalf("expected runtime conformance success, got %s", stdout.String())
	}
}

func TestAdapterVerifyRuntimeRejectsMissingCleanup(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "adapter-runtime-fixture.json")
	if err := os.WriteFile(artifactPath, []byte(runtimeFixtureJSON("fake", false)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	err := app.Run(context.Background(), []string{
		"adapter", "verify-runtime",
		"--artifact", artifactPath,
		"--adapter", "fake",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime conformance failed") {
		t.Fatalf("expected runtime conformance failure, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"ok": false`) || !strings.Contains(stdout.String(), "cleanup_attempted") {
		t.Fatalf("expected structured runtime failure, got %s", stdout.String())
	}
}

func TestReleaseSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "release-root.json")
	manifestPath := filepath.Join(dir, "rdev-host.release.json")
	var signStdout bytes.Buffer
	var signStderr bytes.Buffer
	signApp := NewApp(&signStdout, &signStderr)

	err := signApp.Run(context.Background(), []string{
		"release", "sign",
		"--artifact", artifactPath,
		"--key", keyPath,
		"--key-id", "release-root",
		"--out", manifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	if signed.RootPublicKey == "" {
		t.Fatal("root public key should be returned")
	}
	var verifyStdout bytes.Buffer
	var verifyStderr bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &verifyStderr)
	err = verifyApp.Run(context.Background(), []string{
		"release", "verify",
		"--artifact", artifactPath,
		"--manifest", manifestPath,
		"--root-public-key", signed.RootPublicKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %q", verifyStdout.String())
	}
}

func runtimeFixtureJSON(adapter string, cleanup bool) string {
	cleanupAttempted := "false"
	cleanupOK := "false"
	cleanupPhase := ""
	if cleanup {
		cleanupAttempted = "true"
		cleanupOK = "true"
		cleanupPhase = `,
    {"phase": "cleanup", "ok": true, "evidence": ["cleanup"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}`
	}
	return fmt.Sprintf(`{
  "schema_version": "rdev.adapter-runtime-fixture.v1",
  "adapter": %q,
  "job_id": "job_123",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "canceled": false,
  "timed_out": false,
  "cleanup_attempted": %s,
  "cleanup_ok": %s,
  "result_artifact_schema": "rdev.fake-result.v1",
  "result_artifact": {"schema_version": "rdev.fake-result.v1", "adapter": "fake", "workspace_root": "/tmp/repo"},
  "phases": [
    {"phase": "detect", "ok": true, "evidence": ["version"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "plan", "ok": true, "evidence": ["commands"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "prepare", "ok": true, "evidence": ["workspace"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "run", "ok": true, "evidence": ["process"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "collect", "ok": true, "evidence": ["result"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}%s
  ]
}`, adapter, cleanupAttempted, cleanupOK, cleanupPhase)
}

func TestReleaseVerifyRejectsTamperedArtifact(t *testing.T) {
	dir := t.TempDir()
	artifactPath := filepath.Join(dir, "rdev-host.exe")
	if err := os.WriteFile(artifactPath, []byte("host-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "release-root.json")
	manifestPath := filepath.Join(dir, "rdev-host.release.json")
	var signStdout bytes.Buffer
	signApp := NewApp(&signStdout, &bytes.Buffer{})
	if err := signApp.Run(context.Background(), []string{
		"release", "sign",
		"--artifact", artifactPath,
		"--key", keyPath,
		"--out", manifestPath,
	}); err != nil {
		t.Fatal(err)
	}
	var signed struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(signStdout.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	verifyApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := verifyApp.Run(context.Background(), []string{
		"release", "verify",
		"--artifact", artifactPath,
		"--manifest", manifestPath,
		"--root-public-key", signed.RootPublicKey,
	})
	if err == nil {
		t.Fatal("expected tampered artifact to fail")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleaseCreateAndVerifyBundle(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	root := signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host.exe", "host-binary")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-verify.exe", "verify-binary")

	var createStdout bytes.Buffer
	createApp := NewApp(&createStdout, &bytes.Buffer{})
	if err := createApp.Run(context.Background(), []string{
		"release", "create-bundle",
		"--dir", dir,
		"--artifacts", "rdev-host.exe,rdev-verify.exe",
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(createStdout.String(), `"schema": "rdev.release-bundle.v1"`) {
		t.Fatalf("expected bundle output, got %s", createStdout.String())
	}
	bundlePath := filepath.Join(dir, "release-bundle.json")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("expected bundle file: %v", err)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"release", "verify-bundle",
		"--bundle", bundlePath,
		"--root-public-key", root,
	}); err != nil {
		t.Fatalf("expected bundle verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) || !strings.Contains(verifyStdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured bundle verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(dir, "rdev-host.exe"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"release", "verify-bundle",
		"--bundle", bundlePath,
		"--root-public-key", root,
	})
	if err == nil {
		t.Fatalf("expected tampered bundle verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) ||
		!strings.Contains(tamperedStdout.String(), "artifact_sha256_matches_index") ||
		!strings.Contains(tamperedStdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured tampered bundle failure, got %s", tamperedStdout.String())
	}
}

func TestReleaseCreateBundleRejectsOutOutsideReleaseDir(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "release-root.json")
	signReleaseArtifactWithCLIForTest(t, dir, keyPath, "rdev-host.exe", "host-binary")

	app := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"release", "create-bundle",
		"--dir", dir,
		"--artifacts", "rdev-host.exe",
		"--key", keyPath,
		"--out", filepath.Join(t.TempDir(), "release-bundle.json"),
	})
	if err == nil {
		t.Fatal("expected out path outside release dir to fail")
	}
	if !strings.Contains(err.Error(), "bundle output must be inside release directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleasePrepareCandidateStagesBundleAndSkillkit(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rdev := writeCLIArtifactForTest(t, artifactsDir, "rdev", "cli-binary")
	host := writeCLIArtifactForTest(t, artifactsDir, "rdev-host.exe", "host-binary")
	verifier := writeCLIArtifactForTest(t, artifactsDir, "rdev-verify.exe", "verify-binary")
	out := filepath.Join(dir, "candidate")
	keyPath := filepath.Join(dir, "release-root.json")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})

	if err := app.Run(context.Background(), []string{
		"release", "prepare-candidate",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--version", "v0.1.0",
		"--gateway-url", "https://api.example.com/v1",
		"--artifacts", strings.Join([]string{rdev, host, verifier}, ","),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatalf("expected release candidate preparation to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ok": true`) ||
		!strings.Contains(stdout.String(), `"schema": "rdev.release-candidate.v1"`) ||
		!strings.Contains(stdout.String(), `"sbom":`) ||
		!strings.Contains(stdout.String(), `"provenance":`) {
		t.Fatalf("expected release candidate output, got %s", stdout.String())
	}
	for _, path := range []string{
		"release-candidate.json",
		"release-bundle.json",
		"sbom.spdx.json",
		"provenance.json",
		"checksums.txt",
		"skillkit/manifest.json",
		"rdev-host.exe.rdev-release.json",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected release candidate file %s: %v", path, err)
		}
	}
}

func TestReleaseVerifyCandidateChecksPreparedCandidate(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rdev := writeCLIArtifactForTest(t, artifactsDir, "rdev", "cli-binary")
	host := writeCLIArtifactForTest(t, artifactsDir, "rdev-host.exe", "host-binary")
	verifier := writeCLIArtifactForTest(t, artifactsDir, "rdev-verify.exe", "verify-binary")
	out := filepath.Join(dir, "candidate")
	keyPath := filepath.Join(dir, "release-root.json")
	prepareApp := NewApp(&bytes.Buffer{}, &bytes.Buffer{})
	if err := prepareApp.Run(context.Background(), []string{
		"release", "prepare-candidate",
		"--source-root", filepath.Join("..", ".."),
		"--out", out,
		"--version", "v0.1.0",
		"--gateway-url", "https://api.example.com/v1",
		"--artifacts", strings.Join([]string{rdev, host, verifier}, ","),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatal(err)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"release", "verify-candidate",
		"--candidate", out,
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
	}); err != nil {
		t.Fatalf("expected release candidate verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) ||
		!strings.Contains(verifyStdout.String(), `"schema": "rdev.release-candidate-verification.v1"`) ||
		!strings.Contains(verifyStdout.String(), "bundle_verification") {
		t.Fatalf("expected structured candidate verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(out, "rdev-host.exe"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"release", "verify-candidate",
		"--candidate", filepath.Join(out, "release-candidate.json"),
		"--require-artifacts", "rdev-host.exe,rdev-verify.exe",
	})
	if err == nil {
		t.Fatalf("expected tampered release candidate verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) ||
		!strings.Contains(tamperedStdout.String(), "file_sha256_matches") ||
		!strings.Contains(tamperedStdout.String(), "signed_manifest_verifies_artifact") {
		t.Fatalf("expected structured tamper output, got %s", tamperedStdout.String())
	}
}

func TestUpdateCheckReadsLatestGitHubRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/EitanWong/remote-dev-skillkit/releases/latest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got == "" {
			t.Fatal("expected GitHub API version header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
  "tag_name": "v0.2.0",
  "name": "v0.2.0",
  "html_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/tag/v0.2.0",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-07-02T00:00:00Z",
  "assets": [
    {
      "name": "remote-dev-skillkit-v0.2.0-darwin-arm64.tar.gz",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-darwin-arm64.tar.gz",
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "size": 123,
      "content_type": "application/gzip"
    },
    {
      "name": "release-bundle.json",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/release-bundle.json",
      "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "size": 456,
      "content_type": "application/json"
    }
  ]
}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"update", "check",
		"--repo", "EitanWong/remote-dev-skillkit",
		"--api-base-url", server.URL,
		"--current-version", "v0.1.0",
	}); err != nil {
		t.Fatalf("expected update check to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"schema_version": "rdev.update-check.v1"`) ||
		!strings.Contains(stdout.String(), `"latest_version": "v0.2.0"`) ||
		!strings.Contains(stdout.String(), `"update_available": true`) {
		t.Fatalf("unexpected update check output: %s", stdout.String())
	}
}

func TestUpdatePlanSelectsPlatformArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
  "tag_name": "v0.2.0",
  "name": "v0.2.0",
  "html_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/tag/v0.2.0",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-07-02T00:00:00Z",
  "assets": [
    {
      "name": "remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz",
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "size": 123
    },
    {
      "name": "remote-dev-skillkit-v0.2.0-windows-amd64.zip",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/remote-dev-skillkit-v0.2.0-windows-amd64.zip",
      "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
      "size": 234
    },
    {
      "name": "release-bundle.json",
      "browser_download_url": "https://github.com/EitanWong/remote-dev-skillkit/releases/download/v0.2.0/release-bundle.json",
      "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "size": 456
    }
  ]
}`)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"update", "plan",
		"--repo", "EitanWong/remote-dev-skillkit",
		"--api-base-url", server.URL,
		"--current-version", "v0.1.0",
		"--platform", "linux/amd64",
	}); err != nil {
		t.Fatalf("expected update plan to pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"schema_version": "rdev.update-plan.v1"`) ||
		!strings.Contains(stdout.String(), `"platform": "linux/amd64"`) ||
		!strings.Contains(stdout.String(), "remote-dev-skillkit-v0.2.0-linux-amd64.tar.gz") ||
		!strings.Contains(stdout.String(), "rdev release verify-bundle") ||
		!strings.Contains(stdout.String(), `"plan_is_dry_run"`) {
		t.Fatalf("unexpected update plan output: %s", stdout.String())
	}
}

func TestDepsInstallPlanOnlyOutputsReport(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"deps", "install",
		"--tool", "chisel",
		"--scope", "user",
		"--platform", "linux/amd64",
		"--url", "https://example.com/chisel.tar.gz",
		"--expected-sha256", strings.Repeat("d", 64),
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.dependency-install-report.v1"`) ||
		!strings.Contains(stdout.String(), `"tool": "chisel"`) ||
		!strings.Contains(stdout.String(), `"execute": false`) ||
		!strings.Contains(stdout.String(), `"no_privileged_install"`) {
		t.Fatalf("unexpected deps install output: %s", stdout.String())
	}
}

func TestDepsInstallPlanOnlySupportsMeshAndVPNHelpers(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool string
		want string
	}{
		{name: "tailscale", tool: "tailscale", want: `"tool": "tailscale"`},
		{name: "wireguard alias", tool: "wireguard", want: `"tool": "wg"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			app := NewApp(&stdout, &bytes.Buffer{})
			if err := app.Run(context.Background(), []string{
				"deps", "install",
				"--tool", tc.tool,
				"--scope", "workspace",
				"--platform", "linux/amd64",
				"--url", "https://example.com/" + tc.tool + ".zip",
				"--expected-sha256", strings.Repeat("e", 64),
			}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.want) ||
				!strings.Contains(stdout.String(), `"ok": true`) ||
				!strings.Contains(stdout.String(), `"execute": false`) {
				t.Fatalf("unexpected deps install output: %s", stdout.String())
			}
		})
	}
}

func writeCLIArtifactForTest(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkspaceLockStatusAndUnlock(t *testing.T) {
	repo := t.TempDir()
	store := filepath.Join(t.TempDir(), "locks")
	var lockStdout bytes.Buffer
	app := NewApp(&lockStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"workspace", "lock",
		"--repo", repo,
		"--store", store,
		"--host-id", "hst_cli",
		"--job-id", "job_cli",
		"--adapter", "codex",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lockStdout.String(), `"owner_adapter": "codex"`) {
		t.Fatalf("expected lock output, got %s", lockStdout.String())
	}

	var statusStdout bytes.Buffer
	statusApp := NewApp(&statusStdout, &bytes.Buffer{})
	if err := statusApp.Run(context.Background(), []string{
		"workspace", "status",
		"--repo", repo,
		"--store", store,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusStdout.String(), `"exists": true`) {
		t.Fatalf("expected existing status, got %s", statusStdout.String())
	}

	var unlockStdout bytes.Buffer
	unlockApp := NewApp(&unlockStdout, &bytes.Buffer{})
	if err := unlockApp.Run(context.Background(), []string{
		"workspace", "unlock",
		"--repo", repo,
		"--store", store,
		"--job-id", "job_cli",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unlockStdout.String(), `"removed": true`) {
		t.Fatalf("expected removal output, got %s", unlockStdout.String())
	}
}

func TestWorkspacePrepareWorktreeCreatesGitWorktree(t *testing.T) {
	requireGitForCLITest(t)
	repo := initGitRepoForCLITest(t)
	store := filepath.Join(t.TempDir(), "locks")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"workspace", "prepare-worktree",
		"--repo", repo,
		"--store", store,
		"--host-id", "hst_cli",
		"--job-id", "job_cli",
		"--adapter", "codex",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Worktree struct {
			SchemaVersion string `json:"schema_version"`
			WorktreePath  string `json:"worktree_path"`
			Branch        string `json:"branch"`
			Lock          struct {
				JobID        string `json:"job_id"`
				OwnerAdapter string `json:"owner_adapter"`
			} `json:"lock"`
		} `json:"worktree"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if payload.Worktree.SchemaVersion != "rdev.git-worktree-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Worktree.SchemaVersion)
	}
	if payload.Worktree.Branch != "rdev/job_job_cli" {
		t.Fatalf("unexpected branch %q", payload.Worktree.Branch)
	}
	if payload.Worktree.Lock.JobID != "job_cli" || payload.Worktree.Lock.OwnerAdapter != "codex" {
		t.Fatalf("unexpected lock %#v", payload.Worktree.Lock)
	}
	if _, err := os.Stat(filepath.Join(payload.Worktree.WorktreePath, "README.md")); err != nil {
		t.Fatalf("expected checked out worktree: %v", err)
	}
}

func TestAcceptanceManagedMacGeneratesEvidence(t *testing.T) {
	requireGitForCLITest(t)
	fakeCodex := buildCLITestBinary(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# rdev acceptance fixture\n\nChanged by managed Mac acceptance.\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex acceptance run")
}
`)
	out := filepath.Join(t.TempDir(), "managed-mac-acceptance")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "managed-mac",
		"--out", out,
		"--codex-command", fakeCodex,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK               bool `json:"ok"`
		Report           string
		Evidence         string
		ApprovalEvidence string `json:"approval_evidence"`
		Worktree         string
		Checks           []struct {
			Name   string `json:"name"`
			Passed bool   `json:"passed"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected acceptance ok, got %s", stdout.String())
	}
	for _, path := range []string{
		payload.Report,
		filepath.Join(payload.Evidence, "manifest.json"),
		filepath.Join(payload.ApprovalEvidence, "manifest.json"),
		filepath.Join(payload.Worktree, "README.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	readme := readFileForTest(t, filepath.Join(payload.Worktree, "README.md"))
	if !strings.Contains(readme, "Changed by managed Mac acceptance") {
		t.Fatalf("expected fake codex change in worktree, got %s", readme)
	}
	if len(payload.Checks) == 0 {
		t.Fatal("expected checks")
	}
	for _, check := range payload.Checks {
		if !check.Passed {
			t.Fatalf("expected check %s to pass: %s", check.Name, stdout.String())
		}
	}
	report := readFileForTest(t, payload.Report)
	if !strings.Contains(report, `"schema_version": "rdev.acceptance.managed-mac.v1"`) {
		t.Fatalf("expected managed Mac acceptance report, got %s", report)
	}
	if !strings.Contains(report, `"schema_version": "rdev.evidence-bundle.v1"`) {
		t.Fatalf("expected embedded evidence manifests, got %s", report)
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify",
		"--report", payload.Report,
	}); err != nil {
		t.Fatalf("expected acceptance verification to pass: %v\n%s", err, verifyStdout.String())
	}
	var verifyPayload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(verifyStdout.Bytes(), &verifyPayload); err != nil {
		t.Fatalf("invalid verification json: %v\n%s", err, verifyStdout.String())
	}
	if !verifyPayload.OK {
		t.Fatalf("expected verification ok, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(filepath.Join(payload.Evidence, "artifacts.json"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"acceptance", "verify",
		"--report", payload.Report,
	})
	if err == nil {
		t.Fatalf("expected tampered acceptance verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) {
		t.Fatalf("expected structured failed verification, got %s", tamperedStdout.String())
	}
}

func TestAcceptanceManagedMacServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "managed-mac-service")
	repo := t.TempDir()
	binaryPath := filepath.Join(t.TempDir(), "rdev")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "managed-mac-service",
		"--out", out,
		"--binary", binaryPath,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--repo", repo,
		"--label", "com.example.rdev-acceptance",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Plist    string `json:"plist"`
		Commands []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected service acceptance plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.managed-mac-service-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	for _, path := range []string{payload.Plan, payload.Plist} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	commands := stdout.String()
	if !strings.Contains(commands, "rdev host service-control") || !strings.Contains(commands, "rdev acceptance verify") {
		t.Fatalf("expected service-control and verification commands, got %s", commands)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-managed-mac-service",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected managed mac service plan verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.managed-mac-service-plan.v1"`) {
		t.Fatalf("expected managed mac service verification schema, got %s", verifyStdout.String())
	}
}

func TestAcceptancePackageManagedMacService(t *testing.T) {
	requireGitForCLITest(t)
	fakeCodex := buildCLITestBinary(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if err := os.WriteFile("README.md", []byte("# rdev acceptance fixture\n\nChanged by managed Mac service package.\n"), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("fake codex service package run")
}
`)
	root := t.TempDir()
	planOut := filepath.Join(root, "managed-mac-service")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "managed-mac-service",
		"--out", planOut,
		"--binary", filepath.Join(root, "rdev"),
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--repo", t.TempDir(),
		"--label", "com.example.rdev-acceptance",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(planStdout.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid plan json: %v\n%s", err, planStdout.String())
	}
	managedOut := filepath.Join(root, "managed-mac-run")
	var managedStdout bytes.Buffer
	managedApp := NewApp(&managedStdout, &bytes.Buffer{})
	if err := managedApp.Run(context.Background(), []string{
		"acceptance", "managed-mac",
		"--out", managedOut,
		"--codex-command", fakeCodex,
	}); err != nil {
		t.Fatal(err)
	}
	var managedPayload struct {
		Report string `json:"report"`
	}
	if err := json.Unmarshal(managedStdout.Bytes(), &managedPayload); err != nil {
		t.Fatalf("invalid managed mac json: %v\n%s", err, managedStdout.String())
	}
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	evidence := writeManagedMacServicePackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-managed-mac-service",
		"--plan", planPayload.Plan,
		"--out", filepath.Join(root, "managed-mac-service-evidence"),
		"--review-transcript", evidence.reviewTranscriptPath,
		"--start-transcript", evidence.startTranscriptPath,
		"--inspect-transcript", evidence.inspectTranscriptPath,
		"--logs", evidence.logsPath,
		"--release-gate", evidence.releaseGatePath,
		"--audit", evidence.auditPath,
		"--reconnect", evidence.reconnectPath,
		"--managed-report", managedPayload.Report,
		"--stop-transcript", evidence.stopTranscriptPath,
		"--uninstall-transcript", evidence.uninstallTranscriptPath,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Schema  string `json:"schema"`
		Package string `json:"package"`
		Files   []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected package ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance-package.managed-mac-service.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if _, err := os.Stat(payload.Package); err != nil {
		t.Fatalf("expected package manifest: %v", err)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
	}
	output := stdout.String()
	for _, expected := range []string{"launch-agent-plist", "managed-mac-report", "managed-mac-evidence", "checksums.txt"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected packaged output containing %q, got %s", expected, output)
		}
	}
}

func TestAcceptanceWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--manifest-url", "https://agent.example.com/j/ABCD-1234/manifest",
		"--manifest-root-public-key", "manifest-root:abc",
		"--release-manifest-url", "https://agent.example.com/rdev-host.exe.rdev-release.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Launcher string `json:"launcher"`
		Commands []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
		NoPersistenceChecks []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"no_persistence_checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected windows temporary plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.windows-temporary-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	for _, path := range []string{payload.Plan, payload.Launcher} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	output := stdout.String()
	if !strings.Contains(output, "run_foreground_temporary_host") || !strings.Contains(output, "Get-ScheduledTask") {
		t.Fatalf("expected foreground and no-persistence commands, got %s", output)
	}
	launcher := readFileForTest(t, payload.Launcher)
	if strings.Contains(launcher, "-ReleaseBundleRequiredArtifacts") {
		t.Fatalf("manifest-only launcher should not contain bundle args: %s", launcher)
	}
}

func TestAcceptanceWindowsTemporaryBundlePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--release-bundle-url", "https://agent.example.com/release-bundle.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Launcher string `json:"launcher"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected windows temporary bundle plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.windows-temporary-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	launcher := readFileForTest(t, payload.Launcher)
	if !strings.Contains(launcher, "-ReleaseBundleUrl 'https://agent.example.com/release-bundle.json'") {
		t.Fatalf("expected release bundle launcher arg, got %s", launcher)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected bundle verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceWindowsManagedServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-managed-service",
		"--out", out,
		"--binary", `C:\Program Files\rdev\rdev.exe`,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--label", "RemoteDevSkillkitHost",
		"--workspace-lock-store", `C:\ProgramData\rdev\workspace-locks`,
		"--release-bundle", `C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key", "release-root:abc123",
		"--release-require-artifacts", "rdev.exe,rdev-host.exe,rdev-verify.exe",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK          bool   `json:"ok"`
		Schema      string `json:"schema"`
		Plan        string `json:"plan"`
		ServiceName string `json:"service_name"`
		StartType   string `json:"start_type"`
		Commands    []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected windows managed service plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.windows-managed-service-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if payload.ServiceName != "RemoteDevSkillkitHost" || payload.StartType != "demand" {
		t.Fatalf("unexpected service identity: %#v", payload)
	}
	if _, err := os.Stat(payload.Plan); err != nil {
		t.Fatalf("expected generated plan %s: %v", payload.Plan, err)
	}
	output := stdout.String()
	for _, expected := range []string{
		"sc.exe create RemoteDevSkillkitHost",
		"sc.exe delete RemoteDevSkillkitHost",
		"verify-windows-managed-service",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output containing %q, got %s", expected, output)
		}
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-managed-service",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyWindowsManagedServicePlanRejectsTampering(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-managed-service",
		"--out", out,
		"--binary", `C:\Program Files\rdev\rdev.exe`,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--release-bundle", `C:\Program Files\rdev\release-bundle.json`,
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	var planDoc map[string]any
	if err := json.Unmarshal([]byte(readFileForTest(t, payload.Plan)), &planDoc); err != nil {
		t.Fatal(err)
	}
	uninstall, ok := planDoc["uninstall"].(map[string]any)
	if !ok {
		t.Fatalf("expected uninstall object in plan")
	}
	uninstall["commands"] = []any{[]any{"Set-ExecutionPolicy", "Bypass"}}
	tampered, err := json.MarshalIndent(planDoc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload.Plan, append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-managed-service",
		"--plan", payload.Plan,
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": false`) || !strings.Contains(verifyStdout.String(), "sc_delete_present") {
		t.Fatalf("expected structured tampered failure, got %s", verifyStdout.String())
	}
}

func TestAcceptanceLinuxManagedServicePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "linux-managed-service",
		"--out", out,
		"--binary", "/opt/rdev/rdev",
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--label", "rdev-host.service",
		"--workspace-lock-store", "/var/lib/rdev/workspace-locks",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
		"--release-require-artifacts", "rdev,rdev-host,rdev-verify",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK       bool   `json:"ok"`
		Schema   string `json:"schema"`
		Plan     string `json:"plan"`
		Unit     string `json:"unit"`
		UnitName string `json:"unit_name"`
		Commands []struct {
			Name  string `json:"name"`
			Shell string `json:"shell"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected linux managed service plan ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance.linux-managed-service-plan.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if payload.UnitName != "rdev-host.service" {
		t.Fatalf("unexpected unit name %#v", payload)
	}
	for _, path := range []string{payload.Plan, payload.Unit} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	output := stdout.String()
	for _, expected := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable --now rdev-host.service",
		"systemctl --user disable --now rdev-host.service",
		"verify-linux-managed-service",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output containing %q, got %s", expected, output)
		}
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-linux-managed-service",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyLinuxManagedServicePlanRejectsTampering(t *testing.T) {
	out := filepath.Join(t.TempDir(), "linux-managed-service")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "linux-managed-service",
		"--out", out,
		"--binary", "/opt/rdev/rdev",
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	var planDoc map[string]any
	if err := json.Unmarshal([]byte(readFileForTest(t, payload.Plan)), &planDoc); err != nil {
		t.Fatal(err)
	}
	start, ok := planDoc["start"].(map[string]any)
	if !ok {
		t.Fatalf("expected start object in plan")
	}
	start["commands"] = []any{[]any{"sudo", "systemctl", "enable", "--now", "remote-dev-skillkit-host.service"}}
	tampered, err := json.MarshalIndent(planDoc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload.Plan, append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	err = verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-linux-managed-service",
		"--plan", payload.Plan,
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": false`) || !strings.Contains(verifyStdout.String(), "systemctl_daemon_reload_present") {
		t.Fatalf("expected structured tampered failure, got %s", verifyStdout.String())
	}
}

func TestAcceptanceVerifyWindowsTemporaryPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "windows-temporary")
	script := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", out,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--release-manifest-url", "https://agent.example.com/rdev-host.exe.rdev-release.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Plan     string `json:"plan"`
		Launcher string `json:"launcher"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}

	var verifyStdout bytes.Buffer
	verifyApp := NewApp(&verifyStdout, &bytes.Buffer{})
	if err := verifyApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", payload.Plan,
	}); err != nil {
		t.Fatalf("expected verification to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("expected ok verification, got %s", verifyStdout.String())
	}

	if err := os.WriteFile(payload.Launcher, []byte("Set-ExecutionPolicy Bypass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var tamperedStdout bytes.Buffer
	tamperedApp := NewApp(&tamperedStdout, &bytes.Buffer{})
	err := tamperedApp.Run(context.Background(), []string{
		"acceptance", "verify-windows-temporary",
		"--plan", payload.Plan,
	})
	if err == nil {
		t.Fatalf("expected tampered verification to fail: %s", tamperedStdout.String())
	}
	if !strings.Contains(tamperedStdout.String(), `"ok": false`) || !strings.Contains(tamperedStdout.String(), "launcher_has_no_forbidden_side_effects") {
		t.Fatalf("expected structured tampered failure, got %s", tamperedStdout.String())
	}
}

func TestAcceptancePackageWindowsTemporary(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "windows-temporary")
	script := filepath.Join(root, "windows-temporary.ps1")
	if err := os.WriteFile(script, []byte("Write-Host 'bootstrap'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "windows-temporary",
		"--out", planOut,
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--download-url", "https://agent.example.com/rdev-host.exe",
		"--expected-sha256", strings.Repeat("a", 64),
		"--bootstrap-script", script,
		"--release-bundle-url", "https://agent.example.com/release-bundle.json",
		"--release-root-public-key", "release-root:abc",
		"--verifier-download-url", "https://agent.example.com/rdev-verify.exe",
		"--verifier-sha256", strings.Repeat("b", 64),
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(planStdout.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid plan json: %v\n%s", err, planStdout.String())
	}
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	transcriptPath, releaseVerificationPath, auditPath, noPersistenceDir, approvalProbesDir := writeWindowsPackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-windows-temporary",
		"--plan", planPayload.Plan,
		"--out", filepath.Join(root, "windows-evidence"),
		"--transcript", transcriptPath,
		"--release-verification", releaseVerificationPath,
		"--audit", auditPath,
		"--no-persistence-dir", noPersistenceDir,
		"--approval-probes-dir", approvalProbesDir,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Schema  string `json:"schema"`
		Package string `json:"package"`
		Files   []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected package ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance-package.windows-temporary.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if _, err := os.Stat(payload.Package); err != nil {
		t.Fatalf("expected package manifest: %v", err)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
	}
}

func TestAcceptancePackageLinuxManagedService(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "linux-managed-service")
	var planStdout bytes.Buffer
	planApp := NewApp(&planStdout, &bytes.Buffer{})
	if err := planApp.Run(context.Background(), []string{
		"acceptance", "linux-managed-service",
		"--out", planOut,
		"--binary", "/opt/rdev/rdev",
		"--gateway", "https://api.example.com/v1",
		"--ticket-code", "ABCD-1234",
		"--release-bundle", "/opt/rdev/release-bundle.json",
		"--release-root-public-key", "release-root:abc123",
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(planStdout.Bytes(), &planPayload); err != nil {
		t.Fatalf("invalid plan json: %v\n%s", err, planStdout.String())
	}
	fakeGitHubToken := "ghp_" + "abcdefghijklmnopqrstuvwx"
	evidence := writeLinuxPackageEvidenceForCLITest(t, root, `{"ok": true, "token": "`+fakeGitHubToken+`"}`)

	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-linux-managed-service",
		"--plan", planPayload.Plan,
		"--out", filepath.Join(root, "linux-evidence"),
		"--start-transcript", evidence.startTranscriptPath,
		"--status-transcript", evidence.statusTranscriptPath,
		"--logs", evidence.logsPath,
		"--release-gate", evidence.releaseGatePath,
		"--audit", evidence.auditPath,
		"--reconnect", evidence.reconnectPath,
		"--job-evidence-dir", evidence.jobEvidenceDir,
		"--stop-transcript", evidence.stopTranscriptPath,
		"--uninstall-transcript", evidence.uninstallTranscriptPath,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Schema  string `json:"schema"`
		Package string `json:"package"`
		Files   []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK {
		t.Fatalf("expected package ok, got %s", stdout.String())
	}
	if payload.Schema != "rdev.acceptance-package.linux-managed-service.v1" {
		t.Fatalf("unexpected schema %q", payload.Schema)
	}
	if _, err := os.Stat(payload.Package); err != nil {
		t.Fatalf("expected package manifest: %v", err)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github_token redaction count, got %#v", payload.RedactionRuleCounts)
	}
	output := stdout.String()
	for _, expected := range []string{"start-transcript", "release-gate", "job-evidence", "checksums.txt"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected packaged output containing %q, got %s", expected, output)
		}
	}
}

func TestAcceptancePackageRelayAdapter(t *testing.T) {
	root := t.TempDir()
	relayOut := filepath.Join(root, "relay")
	var relayStdout bytes.Buffer
	app := NewApp(&relayStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"relay-adapter", "package",
		"--out", relayOut,
		"--adapter", "chisel",
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeRelayAdapterEvidenceForCLITest(t, root)
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-relay-adapter",
		"--relay-package", relayOut,
		"--out", filepath.Join(root, "relay-evidence"),
		"--evidence-dir", evidence.dir,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK            bool     `json:"ok"`
		Schema        string   `json:"schema"`
		Package       string   `json:"package"`
		SelectedPath  string   `json:"selected_path"`
		AcceptedPaths []string `json:"accepted_paths"`
		Files         []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
		RedactionRuleCounts map[string]int `json:"redaction_rule_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-package.relay-adapter.v1" {
		t.Fatalf("unexpected package output: %s", stdout.String())
	}
	if payload.SelectedPath != "existing-wireguard-vpn" ||
		!slices.Contains(payload.AcceptedPaths, "existing-ssh-tunnel") {
		t.Fatalf("unexpected connectivity path output: %#v", payload)
	}
	if payload.RedactionRuleCounts["github_token"] != 1 {
		t.Fatalf("expected github token redaction, got %#v", payload.RedactionRuleCounts)
	}
	var packagedPaths []string
	for _, file := range payload.Files {
		packagedPaths = append(packagedPaths, file.Path)
	}
	if !slices.Contains(packagedPaths, "evidence/audit.jsonl") {
		t.Fatalf("expected evidence-dir package files, got %#v", packagedPaths)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "verify-relay-adapter-package",
		"--package", payload.Package,
	}); err != nil {
		t.Fatalf("expected verify command to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.relay-adapter-package.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("unexpected verify output: %s", verifyStdout.String())
	}
}

func TestAcceptancePackageHostedProviderRuntime(t *testing.T) {
	root := t.TempDir()
	providerOut := filepath.Join(root, "hosted-provider")
	var providerStdout bytes.Buffer
	app := NewApp(&providerStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"hosted-provider", "package",
		"--out", providerOut,
		"--storage-provider", "file",
		"--auth-provider", "hosted-ed25519-jwt",
	}); err != nil {
		t.Fatal(err)
	}
	evidence := writeHostedProviderRuntimeEvidenceForCLITest(t, root)
	var stdout bytes.Buffer
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-hosted-provider-runtime",
		"--hosted-provider-package", providerOut,
		"--out", filepath.Join(root, "hosted-runtime-evidence"),
		"--gateway-startup", evidence.gatewayStartup,
		"--storage-verification", evidence.storageVerification,
		"--auth-verification", evidence.authVerification,
		"--backup-evidence", evidence.backupEvidence,
		"--restore-evidence", evidence.restoreEvidence,
		"--retention-evidence", evidence.retentionEvidence,
		"--role-mapping-evidence", evidence.roleMappingEvidence,
		"--failure-mode-evidence", evidence.failureModeEvidence,
		"--audit", evidence.audit,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK           bool              `json:"ok"`
		Schema       string            `json:"schema"`
		Package      string            `json:"package"`
		RuntimeClaim string            `json:"runtime_claim"`
		Redactions   map[string]int    `json:"redaction_rule_counts"`
		Files        []json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-package.hosted-provider-runtime.v1" ||
		payload.RuntimeClaim != "single-node-hosted-smoke" {
		t.Fatalf("unexpected hosted runtime package output: %s", stdout.String())
	}
	if payload.Redactions["github_token"] != 1 {
		t.Fatalf("expected github token redaction, got %#v", payload.Redactions)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "verify-hosted-provider-runtime-package",
		"--package", payload.Package,
	}); err != nil {
		t.Fatalf("expected verify command to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.hosted-provider-runtime-package.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("unexpected hosted runtime verify output: %s", verifyStdout.String())
	}
}

func TestAcceptancePackagePostReleaseDownload(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadEvidenceForCLITest(t, root)
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "package-post-release-download",
		"--plan", fixture.plan,
		"--plan-verification", fixture.planVerification,
		"--out", filepath.Join(root, "post-release-download-evidence"),
		"--evidence-dir", fixture.evidenceDir,
		"--skillkit-evidence-dir", fixture.skillkitDir,
	}); err != nil {
		t.Fatalf("expected package command to pass: %v\n%s", err, stdout.String())
	}
	var payload struct {
		OK              bool              `json:"ok"`
		Schema          string            `json:"schema"`
		Package         string            `json:"package"`
		Repo            string            `json:"repo"`
		Tag             string            `json:"tag"`
		PlatformTargets []string          `json:"platform_targets"`
		Skillkit        bool              `json:"skillkit_included"`
		Redactions      map[string]int    `json:"redaction_rule_counts"`
		Files           []json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid package json: %v\n%s", err, stdout.String())
	}
	if !payload.OK || payload.Schema != "rdev.acceptance-package.post-release-download.v1" ||
		payload.Repo != "EitanWong/remote-dev-skillkit" || payload.Tag != "v0.1.18-dev" ||
		len(payload.PlatformTargets) != 2 || !payload.Skillkit {
		t.Fatalf("unexpected post-release download package output: %s", stdout.String())
	}
	if payload.Redactions["github_token"] != 2 {
		t.Fatalf("expected github token redaction, got %#v", payload.Redactions)
	}

	var verifyStdout bytes.Buffer
	app = NewApp(&verifyStdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "verify-post-release-download-package",
		"--package", payload.Package,
	}); err != nil {
		t.Fatalf("expected verify command to pass: %v\n%s", err, verifyStdout.String())
	}
	if !strings.Contains(verifyStdout.String(), `"schema": "rdev.acceptance-verification.post-release-download-package.v1"`) ||
		!strings.Contains(verifyStdout.String(), `"ok": true`) {
		t.Fatalf("unexpected post-release download verify output: %s", verifyStdout.String())
	}
}

func TestAcceptanceScaffoldPostReleaseDownload(t *testing.T) {
	root := t.TempDir()
	fixture := writePostReleaseDownloadEvidenceForCLITest(t, root)
	out := filepath.Join(root, "post-release-scaffold")
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "scaffold-post-release-download",
		"--plan", fixture.plan,
		"--plan-verification", fixture.planVerification,
		"--out", out,
		"--create-placeholders",
	}); err != nil {
		t.Fatalf("expected scaffold command to pass: %v\n%s", err, stdout.String())
	}
	for _, expected := range []string{
		`"schema": "rdev.post-release-download-evidence-scaffold.v1"`,
		`"ready_for_packaging": false`,
		`"skillkit_included": true`,
		`"platform_evidence_dir"`,
		`"package-post-release-download"`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("expected %q in scaffold output: %s", expected, stdout.String())
		}
	}
	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	err := app.Run(context.Background(), []string{
		"acceptance", "post-release-evidence-status",
		"--scaffold", out,
	})
	if err == nil {
		t.Fatal("placeholder post-release evidence status should fail")
	}
	if !strings.Contains(stdout.String(), `"schema": "rdev.post-release-download-evidence-status.v1"`) ||
		!strings.Contains(stdout.String(), `"placeholder_count": 8`) ||
		!strings.Contains(stdout.String(), `"ready_for_packaging": false`) {
		t.Fatalf("unexpected placeholder status: %s", stdout.String())
	}

	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-transcript.txt"), filepath.Join(out, "platform-download-evidence", "linux-amd64-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-candidate-verify.json"), filepath.Join(out, "platform-download-evidence", "linux-amd64-candidate-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "linux-amd64-bundle-verify.json"), filepath.Join(out, "platform-download-evidence", "linux-amd64-bundle-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-transcript.txt"), filepath.Join(out, "platform-download-evidence", "windows-amd64-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-candidate-verify.json"), filepath.Join(out, "platform-download-evidence", "windows-amd64-candidate-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.evidenceDir, "windows-amd64-bundle-verify.json"), filepath.Join(out, "platform-download-evidence", "windows-amd64-bundle-verify.json"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-transcript.txt"), filepath.Join(out, "skillkit-download-evidence", "skillkit-transcript.txt"))
	copyFileForCLITest(t, filepath.Join(fixture.skillkitDir, "skillkit-verify.json"), filepath.Join(out, "skillkit-download-evidence", "skillkit-verify.json"))

	stdout.Reset()
	app = NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"acceptance", "post-release-evidence-status",
		"--scaffold", out,
	}); err != nil {
		t.Fatalf("real post-release evidence status should pass: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"ready_for_packaging": true`) ||
		!strings.Contains(stdout.String(), `"required_ready": 8`) {
		t.Fatalf("unexpected ready status: %s", stdout.String())
	}
}

func timeNowForTest() time.Time {
	return time.Now().UTC().Add(-time.Minute)
}

type relayAdapterEvidenceForCLITest struct {
	dir              string
	runnerResult     string
	helperTranscript string
	gatewayStatus    string
	hostStatus       string
	connectionStatus string
	audit            string
	evidenceReport   string
}

func writeRelayAdapterEvidenceForCLITest(t *testing.T, root string) relayAdapterEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "relay-package-fixture")
	runnerResult := filepath.Join(evidenceRoot, "runner-result.json")
	helperTranscript := filepath.Join(evidenceRoot, "helper-transcript.txt")
	gatewayStatus := filepath.Join(evidenceRoot, "gateway-status.json")
	hostStatus := filepath.Join(evidenceRoot, "host-status.json")
	connectionStatus := filepath.Join(evidenceRoot, "connection-status.json")
	audit := filepath.Join(evidenceRoot, "audit.jsonl")
	evidenceReport := filepath.Join(evidenceRoot, "evidence-report.json")
	writeFileForCLITest(t, runnerResult, `{"schema_version":"rdev.connection-entry.runner-result.v1","selected_path":"existing-wireguard-vpn","helper_started":true}`+"\n")
	writeFileForCLITest(t, helperTranscript, "started reviewed relay helper\ntoken ghp_abcdefghijklmnopqrstuvwx\n")
	writeFileForCLITest(t, gatewayStatus, `{"ok":true,"status":"healthy"}`+"\n")
	writeFileForCLITest(t, hostStatus, `{"ok":true,"host_status":"active"}`+"\n")
	writeFileForCLITest(t, connectionStatus, `{"ok":true,"connected":true}`+"\n")
	writeFileForCLITest(t, audit, `{"event":"helper_start"}`+"\n"+`{"event":"host_registered"}`+"\n"+`{"event":"cleanup"}`+"\n")
	writeFileForCLITest(t, evidenceReport, `{"schema_version":"rdev.connection-entry.runner-evidence.v1","connected":true}`+"\n")
	return relayAdapterEvidenceForCLITest{
		dir:              evidenceRoot,
		runnerResult:     runnerResult,
		helperTranscript: helperTranscript,
		gatewayStatus:    gatewayStatus,
		hostStatus:       hostStatus,
		connectionStatus: connectionStatus,
		audit:            audit,
		evidenceReport:   evidenceReport,
	}
}

type hostedProviderRuntimeEvidenceForCLITest struct {
	gatewayStartup      string
	storageVerification string
	authVerification    string
	backupEvidence      string
	restoreEvidence     string
	retentionEvidence   string
	roleMappingEvidence string
	failureModeEvidence string
	audit               string
}

func writeHostedProviderRuntimeEvidenceForCLITest(t *testing.T, root string) hostedProviderRuntimeEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "hosted-runtime-fixture")
	gatewayStartup := filepath.Join(evidenceRoot, "gateway-startup.txt")
	storageVerification := filepath.Join(evidenceRoot, "storage-verification.json")
	authVerification := filepath.Join(evidenceRoot, "auth-verification.json")
	backupEvidence := filepath.Join(evidenceRoot, "backup-evidence.txt")
	restoreEvidence := filepath.Join(evidenceRoot, "restore-evidence.txt")
	retentionEvidence := filepath.Join(evidenceRoot, "retention-evidence.txt")
	roleMappingEvidence := filepath.Join(evidenceRoot, "role-mapping-evidence.json")
	failureModeEvidence := filepath.Join(evidenceRoot, "failure-mode-evidence.json")
	audit := filepath.Join(evidenceRoot, "audit.txt")
	writeFileForCLITest(t, gatewayStartup, "gateway started with hosted provider package\ntoken ghp_abcdefghijklmnopqrstuvwx\n")
	writeFileForCLITest(t, storageVerification, `{"ok":true,"provider":"file"}`+"\n")
	writeFileForCLITest(t, authVerification, `{"ok":true,"provider":"hosted-ed25519-jwt"}`+"\n")
	writeFileForCLITest(t, backupEvidence, "snapshot copied to reviewed backup location\n")
	writeFileForCLITest(t, restoreEvidence, "restored snapshot and verified audit chain\n")
	writeFileForCLITest(t, retentionEvidence, "retention policy reviewed for release smoke\n")
	writeFileForCLITest(t, roleMappingEvidence, `{"probes":[{"role":"operator","authorized":true},{"role":"viewer","authorized":false}]}`+"\n")
	writeFileForCLITest(t, failureModeEvidence, `{"ok":true,"failure_mode_tested":true,"mode":"invalid auth rejected"}`+"\n")
	writeFileForCLITest(t, audit, "gateway_start\nstorage_verify\nauth_verify\nrole_probe\nfailure_probe\ncleanup\n")
	return hostedProviderRuntimeEvidenceForCLITest{
		gatewayStartup:      gatewayStartup,
		storageVerification: storageVerification,
		authVerification:    authVerification,
		backupEvidence:      backupEvidence,
		restoreEvidence:     restoreEvidence,
		retentionEvidence:   retentionEvidence,
		roleMappingEvidence: roleMappingEvidence,
		failureModeEvidence: failureModeEvidence,
		audit:               audit,
	}
}

type postReleaseDownloadEvidenceForCLITest struct {
	plan             string
	planVerification string
	evidenceDir      string
	skillkitDir      string
}

func writePostReleaseDownloadEvidenceForCLITest(t *testing.T, root string) postReleaseDownloadEvidenceForCLITest {
	t.Helper()
	dir := filepath.Join(root, "post-release-download")
	evidenceDir := filepath.Join(dir, "platform-evidence")
	skillkitDir := filepath.Join(dir, "skillkit-evidence")
	for _, path := range []string{evidenceDir, skillkitDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	targets := []string{"linux/amd64", "windows/amd64"}
	platforms := make([]map[string]string, 0, len(targets))
	write := func(path, content string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writeJSON := func(path string, value any) string {
		t.Helper()
		content, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return write(path, string(content)+"\n")
	}
	for _, target := range targets {
		platforms = append(platforms, map[string]string{"target": target})
		slug := strings.ReplaceAll(target, "/", "-")
		write(filepath.Join(evidenceDir, slug+"-transcript.txt"), "downloaded "+target+"\nghp_abcdefghijklmnopqrstuvwx\n")
		writeJSON(filepath.Join(evidenceDir, slug+"-candidate-verify.json"), map[string]any{"ok": true})
		writeJSON(filepath.Join(evidenceDir, slug+"-bundle-verify.json"), map[string]any{"ok": true})
	}
	write(filepath.Join(skillkitDir, "skillkit-transcript.txt"), "downloaded skillkit\n")
	writeJSON(filepath.Join(skillkitDir, "skillkit-verify.json"), map[string]any{"ok": true})
	plan := writeJSON(filepath.Join(dir, "post-release-install-plan.json"), map[string]any{
		"schema_version": "rdev.post-release-install-plan.v1",
		"repo":           "EitanWong/remote-dev-skillkit",
		"tag":            "v0.1.18-dev",
		"platforms":      platforms,
		"skillkit":       map[string]any{"archive": map[string]any{"name": "remote-dev-skillkit.tar.gz"}},
	})
	planVerification := writeJSON(filepath.Join(dir, "post-release-install-verification.json"), map[string]any{
		"schema_version": "rdev.post-release-install-verification.v1",
		"ok":             true,
	})
	return postReleaseDownloadEvidenceForCLITest{
		plan:             plan,
		planVerification: planVerification,
		evidenceDir:      evidenceDir,
		skillkitDir:      skillkitDir,
	}
}

type linuxPackageEvidenceForCLITest struct {
	startTranscriptPath     string
	statusTranscriptPath    string
	logsPath                string
	releaseGatePath         string
	auditPath               string
	reconnectPath           string
	jobEvidenceDir          string
	stopTranscriptPath      string
	uninstallTranscriptPath string
}

type managedMacServicePackageEvidenceForCLITest struct {
	reviewTranscriptPath    string
	startTranscriptPath     string
	inspectTranscriptPath   string
	logsPath                string
	releaseGatePath         string
	auditPath               string
	reconnectPath           string
	stopTranscriptPath      string
	uninstallTranscriptPath string
}

func writeManagedMacServicePackageEvidenceForCLITest(t *testing.T, root, releaseGate string) managedMacServicePackageEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "managed-mac-service-package-fixture")
	reviewTranscriptPath := filepath.Join(evidenceRoot, "review.txt")
	startTranscriptPath := filepath.Join(evidenceRoot, "start.txt")
	inspectTranscriptPath := filepath.Join(evidenceRoot, "inspect.txt")
	logsPath := filepath.Join(evidenceRoot, "logs.txt")
	releaseGatePath := filepath.Join(evidenceRoot, "release-gate.json")
	auditPath := filepath.Join(evidenceRoot, "audit.jsonl")
	reconnectPath := filepath.Join(evidenceRoot, "reconnect.txt")
	stopTranscriptPath := filepath.Join(evidenceRoot, "stop.txt")
	uninstallTranscriptPath := filepath.Join(evidenceRoot, "uninstall.txt")
	writeFileForCLITest(t, reviewTranscriptPath, "plutil -lint com.example.rdev-acceptance.plist\nOK\n")
	writeFileForCLITest(t, startTranscriptPath, "rdev host service-control --platform macos --action start --execute\n")
	writeFileForCLITest(t, inspectTranscriptPath, "rdev host service-control --platform macos --action inspect --execute\nstate = running\n")
	writeFileForCLITest(t, logsPath, "managed host log release gate passed\n")
	writeFileForCLITest(t, releaseGatePath, releaseGate+"\n")
	writeFileForCLITest(t, auditPath, `{"event":"host.registered"}`+"\n"+`{"event":"job.completed"}`+"\n")
	writeFileForCLITest(t, reconnectPath, "logout/login complete; host hst_123 reconnected\n")
	writeFileForCLITest(t, stopTranscriptPath, "rdev host service-control --platform macos --action stop --execute\n")
	writeFileForCLITest(t, uninstallTranscriptPath, "rdev host uninstall-service --platform macos --removed true\n")
	return managedMacServicePackageEvidenceForCLITest{
		reviewTranscriptPath:    reviewTranscriptPath,
		startTranscriptPath:     startTranscriptPath,
		inspectTranscriptPath:   inspectTranscriptPath,
		logsPath:                logsPath,
		releaseGatePath:         releaseGatePath,
		auditPath:               auditPath,
		reconnectPath:           reconnectPath,
		stopTranscriptPath:      stopTranscriptPath,
		uninstallTranscriptPath: uninstallTranscriptPath,
	}
}

func writeLinuxPackageEvidenceForCLITest(t *testing.T, root, releaseGate string) linuxPackageEvidenceForCLITest {
	t.Helper()
	evidenceRoot := filepath.Join(root, "linux-package-fixture")
	startTranscriptPath := filepath.Join(evidenceRoot, "start.txt")
	statusTranscriptPath := filepath.Join(evidenceRoot, "status.txt")
	logsPath := filepath.Join(evidenceRoot, "logs.txt")
	releaseGatePath := filepath.Join(evidenceRoot, "release-gate.json")
	auditPath := filepath.Join(evidenceRoot, "audit.jsonl")
	reconnectPath := filepath.Join(evidenceRoot, "reconnect.txt")
	stopTranscriptPath := filepath.Join(evidenceRoot, "stop.txt")
	uninstallTranscriptPath := filepath.Join(evidenceRoot, "uninstall.txt")
	jobEvidenceDir := filepath.Join(evidenceRoot, "job-evidence")
	writeFileForCLITest(t, startTranscriptPath, "systemctl --user daemon-reload\nsystemctl --user enable --now remote-dev-skillkit-host.service\n")
	writeFileForCLITest(t, statusTranscriptPath, "systemctl --user status remote-dev-skillkit-host.service\nactive (running)\n")
	writeFileForCLITest(t, logsPath, "journalctl --user -u remote-dev-skillkit-host.service\nrelease gate passed\n")
	writeFileForCLITest(t, releaseGatePath, releaseGate+"\n")
	writeFileForCLITest(t, auditPath, `{"event":"host.registered"}`+"\n"+`{"event":"job.completed"}`+"\n")
	writeFileForCLITest(t, reconnectPath, "rebooted host reconnected as hst_123\n")
	writeFileForCLITest(t, stopTranscriptPath, "systemctl --user disable --now remote-dev-skillkit-host.service\n")
	writeFileForCLITest(t, uninstallTranscriptPath, "rdev host uninstall-service --platform linux --removed true\n")
	writeFileForCLITest(t, filepath.Join(jobEvidenceDir, "manifest.json"), `{"schema_version":"rdev.evidence-bundle.v1"}`+"\n")
	writeFileForCLITest(t, filepath.Join(jobEvidenceDir, "artifacts", "approval-required.json"), `{"schema_version":"rdev.approval-required.v1"}`+"\n")
	return linuxPackageEvidenceForCLITest{
		startTranscriptPath:     startTranscriptPath,
		statusTranscriptPath:    statusTranscriptPath,
		logsPath:                logsPath,
		releaseGatePath:         releaseGatePath,
		auditPath:               auditPath,
		reconnectPath:           reconnectPath,
		jobEvidenceDir:          jobEvidenceDir,
		stopTranscriptPath:      stopTranscriptPath,
		uninstallTranscriptPath: uninstallTranscriptPath,
	}
}

func writeFileForCLITest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyFileForCLITest(t *testing.T, source, dest string) {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFileForCLITest(t, dest, string(content))
}

func signReleaseArtifactWithCLIForTest(t *testing.T, dir, keyPath, name, content string) string {
	t.Helper()
	artifactPath := filepath.Join(dir, name)
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"release", "sign",
		"--artifact", artifactPath,
		"--key", keyPath,
		"--key-id", "release-root",
	}); err != nil {
		t.Fatal(err)
	}
	var signed struct {
		RootPublicKey string `json:"root_public_key"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &signed); err != nil {
		t.Fatalf("invalid sign output: %v\n%s", err, stdout.String())
	}
	if signed.RootPublicKey == "" {
		t.Fatalf("expected root public key in sign output: %s", stdout.String())
	}
	return signed.RootPublicKey
}

func createReleaseBundleForHostServeTest(t *testing.T, dir, keyPath, artifacts string) string {
	t.Helper()
	var stdout bytes.Buffer
	app := NewApp(&stdout, &bytes.Buffer{})
	if err := app.Run(context.Background(), []string{
		"release", "create-bundle",
		"--dir", dir,
		"--artifacts", artifacts,
		"--require-artifacts", artifacts,
		"--key", keyPath,
	}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Bundle string `json:"bundle"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid bundle output: %v\n%s", err, stdout.String())
	}
	if payload.Bundle == "" {
		t.Fatalf("expected bundle path in output: %s", stdout.String())
	}
	return payload.Bundle
}

func writeJSONForTest(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTrustBundleForTest(t *testing.T, path string) model.SignedTrustBundle {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle model.SignedTrustBundle
	if err := json.Unmarshal(content, &bundle); err != nil {
		t.Fatalf("invalid trust bundle: %v\n%s", err, string(content))
	}
	return bundle
}

func assertTrustVerifyOK(t *testing.T, content []byte, wantSequence int) {
	t.Helper()
	var payload struct {
		OK       bool `json:"ok"`
		Sequence int  `json:"sequence"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("invalid trust verify output: %v\n%s", err, string(content))
	}
	if !payload.OK || payload.Sequence != wantSequence {
		t.Fatalf("unexpected trust verify output: %s", string(content))
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func fileExistsForCLITest(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeWindowsPackageEvidenceForCLITest(t *testing.T, root, releaseVerification string) (string, string, string, string, string) {
	t.Helper()
	transcriptPath := filepath.Join(root, "transcript.txt")
	releaseVerificationPath := filepath.Join(root, "rdev-verify.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	noPersistenceDir := filepath.Join(root, "no-persistence")
	approvalProbesDir := filepath.Join(root, "approval-probes")
	if err := os.WriteFile(transcriptPath, []byte("temporary host transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseVerificationPath, []byte(releaseVerification+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, []byte(`{"event":"host.registered"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeNamedFilesForTest(t, noPersistenceDir, []string{
		"services.txt",
		"scheduled_tasks.txt",
		"hkcu_run_key.txt",
		"hklm_run_key.txt",
		"startup_folders.txt",
		"firewall_rules.txt",
	})
	writeNamedFilesForTest(t, approvalProbesDir, []string{
		"package.install.txt",
		"elevation.request.txt",
		"service.manage.txt",
		"gui.control.txt",
		"credential.change.txt",
	})
	return transcriptPath, releaseVerificationPath, auditPath, noPersistenceDir, approvalProbesDir
}

func writeNamedFilesForTest(t *testing.T, dir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+" ok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runHostServeWithIdentityStore(t *testing.T, gatewayURL, ticketCode, identityPath, name string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)
	err := app.Run(context.Background(), []string{
		"host", "serve",
		"--mode", "temporary",
		"--gateway", gatewayURL,
		"--ticket-code", ticketCode,
		"--identity-store", identityPath,
		"--identity-key-id", "host-test",
		"--name", name,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Host struct {
			IdentityFingerprint string `json:"identity_fingerprint"`
		} `json:"host"`
		Identity struct {
			Fingerprint       string `json:"fingerprint"`
			Stored            bool   `json:"stored"`
			ProofSchema       string `json:"proof_schema"`
			RegistrationProof bool   `json:"registration_proof"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Identity.Fingerprint == "" {
		t.Fatalf("expected identity fingerprint in output, got %s", stdout.String())
	}
	if !payload.Identity.Stored {
		t.Fatalf("expected identity to be stored, got %s", stdout.String())
	}
	if !payload.Identity.RegistrationProof || payload.Identity.ProofSchema != model.HostRegistrationProofSchemaVersion {
		t.Fatalf("expected registration proof schema %q, got %s", model.HostRegistrationProofSchemaVersion, stdout.String())
	}
	if payload.Host.IdentityFingerprint != payload.Identity.Fingerprint {
		t.Fatalf("expected host identity fingerprint %q, got %q", payload.Identity.Fingerprint, payload.Host.IdentityFingerprint)
	}
	return payload.Identity.Fingerprint
}

func requireGitForCLITest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func initGitRepoForCLITest(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitForCLITest(t, repo, "init")
	runGitForCLITest(t, repo, "config", "user.email", "rdev-test@example.com")
	runGitForCLITest(t, repo, "config", "user.name", "Rdev Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForCLITest(t, repo, "add", "README.md")
	runGitForCLITest(t, repo, "commit", "-m", "initial")
	return repo
}

func runGitForCLITest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func buildCLITestBinary(t *testing.T, source string) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is required")
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	binaryName := "rdev-cli-test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(dir, binaryName)
	cmd := exec.Command("go", "build", "-o", binaryPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(output))
	}
	return binaryPath
}

type gatewayTLSMaterial struct {
	CACert     string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

func writeGatewayTLSMaterial(t *testing.T) gatewayTLSMaterial {
	t.Helper()
	dir := t.TempDir()
	caCert, caKey := createTestCertificateAuthority(t)
	serverCert, serverKey := createSignedTestCertificate(t, caCert, caKey, "rdev-gateway-test-server", []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")}, nil)
	clientCert, clientKey := createSignedTestCertificate(t, caCert, caKey, "rdev-gateway-test-client", nil, nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	material := gatewayTLSMaterial{
		CACert:     filepath.Join(dir, "ca.pem"),
		ServerCert: filepath.Join(dir, "server-cert.pem"),
		ServerKey:  filepath.Join(dir, "server-key.pem"),
		ClientCert: filepath.Join(dir, "client-cert.pem"),
		ClientKey:  filepath.Join(dir, "client-key.pem"),
	}
	writePEMFile(t, material.CACert, "CERTIFICATE", caCert.Raw)
	writePEMFile(t, material.ServerCert, "CERTIFICATE", serverCert.Raw)
	writePEMFile(t, material.ServerKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey))
	writePEMFile(t, material.ClientCert, "CERTIFICATE", clientCert.Raw)
	writePEMFile(t, material.ClientKey, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(clientKey))
	return material
}

func createTestCertificateAuthority(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rdev gateway test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func createSignedTestCertificate(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, commonName string, dnsNames []string, ipAddresses []net.IP, extKeyUsage []x509.ExtKeyUsage) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(extKeyUsage) == 0 {
		extKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  extKeyUsage,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func writePEMFile(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	var content bytes.Buffer
	if err := pem.Encode(&content, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForJobStatus(t *testing.T, gw *gateway.MemoryGateway, jobID string, status model.JobStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := gw.Job(jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := gw.Job(jobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timed out waiting for job %s status %s, got %s", jobID, status, job.Status)
}

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
