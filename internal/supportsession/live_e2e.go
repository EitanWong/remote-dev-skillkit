package supportsession

import (
	"strconv"
	"strings"
)

const LiveE2EPlanSchemaVersion = "rdev.support-session-live-e2e-plan.v1"

type LiveE2EPlanOptions struct {
	GatewayURL       string
	TicketCode       string
	HostID           string
	SessionID        string
	TargetEndpointID string
	RdevCommand      string
	TimeoutSeconds   int
}

func BuildLiveE2EPlan(opts LiveE2EPlanOptions) map[string]any {
	gatewayURL := strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/")
	if gatewayURL == "" {
		gatewayURL = "<gateway-url>"
	}
	ticketCodeInput := strings.TrimSpace(opts.TicketCode)
	hostIDInput := strings.TrimSpace(opts.HostID)
	sessionIDInput := strings.TrimSpace(opts.SessionID)
	if sessionIDInput == "" {
		sessionIDInput = "<session-id>"
	}
	targetEndpointIDInput := strings.TrimSpace(opts.TargetEndpointID)
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
		"--session-id", sessionIDInput,
	}
	if targetEndpointIDInput != "" {
		smokeCommand = append(smokeCommand, "--target-endpoint-id", targetEndpointIDInput)
	}
	if ticketCodeInput != "" {
		smokeCommand = append(smokeCommand, "--ticket-code", ticketCodeInput)
	}
	smokeCommand = append(smokeCommand,
		"--remote-control",
		"--timeout-seconds", timeoutText,
	)
	smokeMCPArguments := map[string]any{
		"gateway_url":     gatewayURL,
		"session_id":      sessionIDInput,
		"remote_control":  true,
		"timeout_seconds": timeoutSeconds,
	}
	if targetEndpointIDInput != "" {
		smokeMCPArguments["target_endpoint_id"] = targetEndpointIDInput
	}
	if ticketCodeInput != "" {
		smokeMCPArguments["ticket_code"] = ticketCodeInput
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
		"schema_version":     LiveE2EPlanSchemaVersion,
		"dry_run":            true,
		"execute":            false,
		"gateway_url":        gatewayURL,
		"ticket_code":        ticketCodeInput,
		"host_id":            hostIDInput,
		"session_id":         sessionIDInput,
		"target_endpoint_id": targetEndpointIDInput,
		"target_os":          "windows",
		"timeout_seconds":    timeoutSeconds,
		"gates": []map[string]any{
			{
				"name":           "windows_support_session_smoke_remote_control",
				"status":         "requires_real_environment",
				"target_os":      "windows",
				"remote_control": true,
				"required_inputs": []string{
					"reachable gateway_url",
					"active Control Plane session_id",
					"optional target_endpoint_id when the session has multiple Windows targets",
				},
				"proof_command": smokeCommand,
				"mcp_tool":      "rdev.support_session.smoke_test",
				"mcp_arguments": smokeMCPArguments,
				"required_evidence": []string{
					"ok=true",
					"Windows target selected from session_id and optional target_endpoint_id",
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
					"task_id for a bounded interrupt event probe",
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
					"verify the interrupt event is replayable and duplicate delivery remains idempotent",
					"record that the current host runtime does not consume interrupt events to cancel an already running adapter process",
				},
				"required_evidence": []string{
					"rdev.sessions.interrupt returns a Control Plane event",
					"rdev.sessions.events replays the interrupt after reconnect",
					"duplicate idempotency_key does not create duplicate interrupt effects",
					"process_cancellation_proven=false",
				},
				"agent_rule": "interrupt records a replayable session-level stop intent but currently does not prove process cancellation; do not report a running Windows task as canceled until the target runtime consumes and enforces the interrupt event",
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
