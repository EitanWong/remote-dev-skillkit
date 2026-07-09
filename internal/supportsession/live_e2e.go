package supportsession

import (
	"strconv"
	"strings"
)

const LiveE2EPlanSchemaVersion = "rdev.support-session-live-e2e-plan.v1"

type LiveE2EPlanOptions struct {
	GatewayURL     string
	TicketCode     string
	HostID         string
	RdevCommand    string
	TimeoutSeconds int
}

func BuildLiveE2EPlan(opts LiveE2EPlanOptions) map[string]any {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL = "<gateway-url>"
	}
	ticketCodeInput := strings.TrimSpace(opts.TicketCode)
	hostIDInput := strings.TrimSpace(opts.HostID)
	genericSelectorPlan := ticketCodeInput == "" && hostIDInput == ""
	smokeTicketCode := ticketCodeInput
	if smokeTicketCode == "" && genericSelectorPlan {
		smokeTicketCode = "<ticket-code>"
	}
	smokeHostID := hostIDInput
	if smokeHostID == "" && genericSelectorPlan {
		smokeHostID = "<host-id>"
	}
	hostIDForHostOnlyGates := hostIDInput
	if hostIDForHostOnlyGates == "" {
		hostIDForHostOnlyGates = "<host-id>"
	}
	rdevCommand := strings.TrimSpace(opts.RdevCommand)
	if rdevCommand == "" {
		rdevCommand = "rdev"
	}
	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 180
	}
	timeoutText := strconv.Itoa(timeoutSeconds)
	smokeCommand := []string{
		rdevCommand, "support-session", "smoke-test",
		"--gateway-url", gatewayURL,
	}
	if smokeTicketCode != "" {
		smokeCommand = append(smokeCommand, "--ticket-code", smokeTicketCode)
	}
	if smokeHostID != "" {
		smokeCommand = append(smokeCommand, "--host-id", smokeHostID)
	}
	smokeCommand = append(smokeCommand,
		"--remote-control",
		"--timeout-seconds", timeoutText,
	)
	smokeMCPArguments := map[string]any{
		"gateway_url":     gatewayURL,
		"remote_control":  true,
		"timeout_seconds": timeoutSeconds,
	}
	if smokeTicketCode != "" {
		smokeMCPArguments["ticket_code"] = smokeTicketCode
	}
	if smokeHostID != "" {
		smokeMCPArguments["host_id"] = smokeHostID
	}
	uploadCommand := []string{
		rdevCommand, "files", "upload",
		"--gateway-url", gatewayURL,
		"--host-id", hostIDForHostOnlyGates,
		"--workspace-root", "~",
		"--write-scope", "rdev-audit",
		"--path", "rdev-audit/remote-control-upload.txt",
		"--content-file", "rdev-live-e2e-upload.txt",
	}
	downloadCommand := []string{
		rdevCommand, "files", "download",
		"--gateway-url", gatewayURL,
		"--host-id", hostIDForHostOnlyGates,
		"--workspace-root", "~",
		"--path", "rdev-audit/remote-control-upload.txt",
		"--max-bytes", "1048576",
	}
	createSessionTaskCommand := []string{
		rdevCommand, "mcp", "serve",
	}
	return map[string]any{
		"schema_version":  LiveE2EPlanSchemaVersion,
		"dry_run":         true,
		"execute":         false,
		"gateway_url":     gatewayURL,
		"ticket_code":     ticketCodeInput,
		"host_id":         hostIDInput,
		"target_os":       "windows",
		"timeout_seconds": timeoutSeconds,
		"gates": []map[string]any{
			{
				"name":           "windows_support_session_smoke_remote_control",
				"status":         "requires_real_environment",
				"target_os":      "windows",
				"remote_control": true,
				"required_inputs": []string{
					"reachable gateway_url",
					"ticket_code with exactly one active Windows host, or explicit host_id",
				},
				"proof_command": smokeCommand,
				"mcp_tool":      "rdev.support_session.smoke_test",
				"mcp_arguments": smokeMCPArguments,
				"required_evidence": []string{
					"ok=true",
					"Windows host selected from ticket_code or host_id",
					"identity, fs.read, fs.write.scoped, process.inspect pass",
					"remote_control file list and window.inspect probes pass",
				},
				"agent_rule": "this plan does not satisfy the live Windows remote-control gate; run the proof command against a connected Windows host before claiming remote E2E completion",
			},
			{
				"name":      "windows_file_transfer_byte_compare",
				"status":    "requires_real_environment",
				"target_os": "windows",
				"required_inputs": []string{
					"reachable gateway_url",
					"active Windows host_id",
					"small deterministic local test file",
				},
				"proof_commands": map[string]any{
					"upload":   uploadCommand,
					"download": downloadCommand,
				},
				"proof_steps": []string{
					"create rdev-live-e2e-upload.txt with deterministic bytes",
					"upload it to the scoped Windows user-home rdev-audit path",
					"download or read the same remote file back",
					"save the download task response and any session artifact refs returned by the gateway",
					"compare local bytes and SHA-256 with transfer evidence",
				},
				"required_evidence": []string{
					"expected_bytes equals downloaded bytes",
					"expected_sha256 equals actual_sha256",
					"byte_compare=match",
				},
				"agent_rule": "do not substitute shell echo output for file transfer proof; collect byte-level upload/download evidence through rdev file transfer surfaces",
			},
			{
				"name":      "windows_session_interrupt_flow",
				"status":    "requires_real_environment",
				"target_os": "windows",
				"required_inputs": []string{
					"reachable gateway_url",
					"active Control Plane session_id",
					"Windows endpoint_id",
					"task_id for a bounded pause/cancel probe",
				},
				"proof_commands": map[string]any{
					"mcp_server": createSessionTaskCommand,
				},
				"mcp_tool": "rdev.sessions.interrupt",
				"mcp_arguments": map[string]any{
					"gateway_url":     gatewayURL,
					"session_id":      "<session-id>",
					"task_id":         "<task-id>",
					"to_endpoint_id":  "<endpoint-id>",
					"reason":          "live remote E2E session interrupt proof",
					"idempotency_key": "live-e2e-interrupt-<nonce>",
				},
				"mcp_arguments_source": "Control Plane session snapshot and task event",
				"proof_steps": []string{
					"create a bounded session task with rdev.sessions.task",
					"read session_id, endpoint_id, and task_id from rdev.sessions.status or rdev.sessions.events",
					"call rdev.sessions.interrupt with an idempotency key",
					"verify the interrupt event is replayable and the task or endpoint reports the expected paused/canceled state",
				},
				"required_evidence": []string{
					"rdev.sessions.interrupt returns a Control Plane event",
					"rdev.sessions.events replays the interrupt after reconnect",
					"duplicate idempotency_key does not create duplicate interrupt effects",
					"task or endpoint state explains the pause/cancel outcome",
				},
				"agent_rule": "interrupt is the only session-level pause/cancel primitive; do not resurrect the retired task-authorization contract",
			},
		},
		"safety": []string{
			"default dry-run creates no remote tasks",
			"interrupt/pause/cancel must be expressed through Control Plane session events, not a separate task authorization subsystem",
			"do not authorize broad or user-data deletion paths while proving the authorization chain",
		},
		"agent_rule": "run these gates against a live attended-temporary Windows host before reporting remote E2E as complete",
	}
}
