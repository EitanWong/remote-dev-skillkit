package contracts

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Safety      string         `json:"safety"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

func Tools() []Tool {
	gatewayURL := gatewayURLField()
	sessionID := stringField()
	payloadObject := map[string]any{"type": "object", "additionalProperties": true}

	return []Tool{
		{
			Name:        "rdev.sessions.create",
			Description: "Create a Control Plane v1 session and return compact status with user_summary, agent_next_action, recoverable, retry_after_ms, join_code, and session snapshot fields. This is the first Agent-facing MCP entrypoint for new remote work.",
			Safety:      "Creates a session record only. Does not execute on a target, install persistence, mutate network settings, or bypass local OS controls.",
			InputSchema: object(map[string]any{
				"gateway_url":          gatewayURL,
				"profile":              enum("attended-temporary", "managed", "break-glass", "workspace-provider", "ci-worker"),
				"reason":               stringField(),
				"capabilities":         stringArray(),
				"join_policy":          enum("single-target", "multi-target", "agent-only"),
				"authority_id":         stringField(),
				"selected_gateway_url": stringField(),
				"reconnect_grace_ms":   integer(0, 3600000),
				"retry_after_ms":       integer(0, 60000),
				"expires_at":           stringField(),
			}, []string{"reason"}),
		},
		{
			Name:        "rdev.sessions.status",
			Description: "Read a Control Plane v1 session snapshot and derived status with user_summary, agent_next_action, recoverable, retry_after_ms, last_seq, snapshot_seq, selected gateway, endpoints, recent tasks, and artifact references.",
			Safety:      "Read-only. Does not expose endpoint lease secrets, raw unbounded logs, or target-local credentials.",
			InputSchema: object(map[string]any{
				"gateway_url": gatewayURL,
				"session_id":  sessionID,
			}, []string{"session_id"}),
		},
		{
			Name:        "rdev.sessions.events",
			Description: "Read Control Plane v1 events after a cursor and return events plus snapshot_required, snapshot_seq, last_seq, retry_after_ms, user_summary, and agent_next_action so Agents can recover from compaction or weak networks.",
			Safety:      "Read-only Agent event replay. Does not expose endpoint lease secrets and does not mutate target state.",
			InputSchema: object(map[string]any{
				"gateway_url":    gatewayURL,
				"session_id":     sessionID,
				"after_seq":      integer(0, 1_000_000_000),
				"limit":          integer(1, 100),
				"endpoint_id":    stringField(),
				"received_seq":   integer(0, 1_000_000_000),
				"processed_seq":  integer(0, 1_000_000_000),
				"include_status": boolField(),
			}, []string{"session_id"}),
		},
		{
			Name:        "rdev.sessions.task",
			Description: "Submit or inspect a Control Plane v1 task through the session event stream and return task, event, user_summary, agent_next_action, recoverable, and retry_after_ms. Tasks are routed through session endpoints.",
			Safety:      "Creates a routed task event for an already joined session endpoint. Adapter payloads remain policy-bound by the target runtime and session capabilities.",
			InputSchema: object(map[string]any{
				"gateway_url":        gatewayURL,
				"session_id":         sessionID,
				"target_endpoint_id": stringField(),
				"adapter":            stringField(),
				"intent":             stringField(),
				"capabilities":       stringArray(),
				"payload":            payloadObject,
				"limits":             payloadObject,
				"idempotency_key":    stringField(),
			}, []string{"session_id", "adapter", "idempotency_key"}),
		},
		{
			Name:        "rdev.sessions.interrupt",
			Description: "Append a small Control Plane v1 interrupt event for local consent, policy pause, cancellation intent, or user stop, and return event, user_summary, agent_next_action, recoverable, and retry_after_ms.",
			Safety:      "Adds a bounded interrupt event only. It is not a separate authorization subsystem and does not bypass local OS or enterprise controls.",
			InputSchema: object(map[string]any{
				"gateway_url":     gatewayURL,
				"session_id":      sessionID,
				"task_id":         stringField(),
				"to_endpoint_id":  stringField(),
				"reason":          stringField(),
				"payload":         payloadObject,
				"idempotency_key": stringField(),
			}, []string{"session_id", "reason", "idempotency_key"}),
		},
		{
			Name:        "rdev.sessions.artifacts",
			Description: "Record or inspect Control Plane v1 artifact references with size, sha256, upload_offset, complete, user_summary, agent_next_action, recoverable, and retry_after_ms. Large content stays out of MCP responses.",
			Safety:      "Handles artifact metadata and references only. Does not inline large files, screenshots, logs, diffs, or binary content.",
			InputSchema: object(map[string]any{
				"gateway_url":    gatewayURL,
				"session_id":     sessionID,
				"id":             stringField(),
				"task_id":        stringField(),
				"kind":           stringField(),
				"name":           stringField(),
				"size_bytes":     integer(0, 1_000_000_000),
				"sha256":         stringField(),
				"content_type":   stringField(),
				"upload_offset":  integer(0, 1_000_000_000),
				"complete":       boolField(),
				"include_status": boolField(),
			}, []string{"session_id"}),
		},
		{
			Name:        "rdev.sessions.close",
			Description: "Close a Control Plane v1 session and return final status with user_summary, agent_next_action, recoverable, retry_after_ms, and close event. Event reads remain available until retention expiry.",
			Safety:      "Closes the session control plane. Does not uninstall software, delete local data, or mutate target OS settings.",
			InputSchema: object(map[string]any{
				"gateway_url":     gatewayURL,
				"session_id":      sessionID,
				"reason":          stringField(),
				"idempotency_key": stringField(),
			}, []string{"session_id"}),
		},
		{
			Name:        "rdev.sessions.connect",
			Description: "Create or route the standard visible support-session entry, including region-aware tunnel availability, readiness, and the single human handoff contract.",
			Safety:      "Does not bypass local controls or accept provider terms. Without an explicit start action it returns bounded commands/contracts; direct single-entry handoffs remain non-sendable unless the caller explicitly enables the degraded override.",
			InputSchema: object(map[string]any{
				"repo_root":                     stringField(),
				"work_dir":                      stringField(),
				"addr":                          stringField(),
				"gateway_url":                   gatewayURL,
				"target":                        enum("auto", "windows", "macos", "linux"),
				"reason":                        stringField(),
				"ttl_seconds":                   integer(60, 86400),
				"auto_activate":                 boolField(),
				"capabilities":                  stringArray(),
				"locale":                        stringField(),
				"rdev_command":                  stringField(),
				"region":                        enum("global", "cn-mainland"),
				"provider_policy":               stringField(),
				"allow_degraded_direct_handoff": boolField(),
			}, nil),
		},
	}
}

func object(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringField() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

func stringArray() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string", "minLength": 1},
	}
}

func integer(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}

func boolField() map[string]any {
	return map[string]any{"type": "boolean"}
}

func gatewayURLField() map[string]any {
	return map[string]any{
		"type":        "string",
		"minLength":   1,
		"description": "Optional gateway base URL to proxy this session call. Overrides the server-level gateway URL for this single MCP request.",
	}
}

func enum(values ...string) map[string]any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return map[string]any{"type": "string", "enum": items}
}
