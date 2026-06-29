package contracts

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Safety      string         `json:"safety"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

func Tools() []Tool {
	return []Tool{
		{
			Name:        "rdev.tickets.create",
			Description: "Create a short-lived support or remote development ticket for a target host.",
			Safety:      "Creates access path; does not execute on host.",
			InputSchema: object(map[string]any{
				"mode":         enum("attended-temporary", "managed", "break-glass"),
				"ttl_seconds":  integer(60, 86400),
				"capabilities": stringArray(),
				"reason":       stringField(),
			}, []string{"mode", "ttl_seconds", "reason"}),
		},
		{
			Name:        "rdev.tickets.revoke",
			Description: "Revoke a support ticket and prevent new host enrollment through it.",
			Safety:      "Revocation only.",
			InputSchema: object(map[string]any{
				"ticket_id": stringField(),
				"reason":    stringField(),
			}, []string{"ticket_id", "reason"}),
		},
		{
			Name:        "rdev.hosts.list",
			Description: "List enrolled or pending hosts visible to the operator.",
			Safety:      "Read-only.",
			InputSchema: object(map[string]any{
				"status": enum("pending", "active", "revoked", "offline"),
			}, nil),
		},
		{
			Name:        "rdev.hosts.capabilities",
			Description: "Inspect detected and approved capabilities for one host.",
			Safety:      "Read-only.",
			InputSchema: object(map[string]any{
				"host_id": stringField(),
			}, []string{"host_id"}),
		},
		{
			Name:        "rdev.hosts.approve",
			Description: "Approve a pending host for a ticket-scoped policy.",
			Safety:      "Access-granting; requires operator approval.",
			InputSchema: object(map[string]any{
				"host_id":      stringField(),
				"ticket_id":    stringField(),
				"capabilities": stringArray(),
			}, []string{"host_id", "ticket_id", "capabilities"}),
		},
		{
			Name:        "rdev.hosts.revoke",
			Description: "Revoke host access and disconnect active sessions.",
			Safety:      "Revocation only.",
			InputSchema: object(map[string]any{
				"host_id": stringField(),
				"reason":  stringField(),
			}, []string{"host_id", "reason"}),
		},
		{
			Name:        "rdev.jobs.create",
			Description: "Create a signed, policy-bound remote job for an approved host.",
			Safety:      "May execute on host after policy checks.",
			InputSchema: object(map[string]any{
				"host_id": stringField(),
				"adapter": enum("shell", "powershell", "acpx", "codex", "claude"),
				"intent":  stringField(),
				"policy":  map[string]any{"type": "object"},
			}, []string{"host_id", "adapter", "intent", "policy"}),
		},
		{
			Name:        "rdev.jobs.status",
			Description: "Read status and latest events for a remote job.",
			Safety:      "Read-only.",
			InputSchema: object(map[string]any{
				"job_id": stringField(),
			}, []string{"job_id"}),
		},
		{
			Name:        "rdev.jobs.cancel",
			Description: "Request cooperative cancellation of a running job.",
			Safety:      "Cancellation only.",
			InputSchema: object(map[string]any{
				"job_id": stringField(),
				"reason": stringField(),
			}, []string{"job_id", "reason"}),
		},
		{
			Name:        "rdev.jobs.approve",
			Description: "Approve a pending high-risk job action such as elevation or package install.",
			Safety:      "Dangerous-action approval.",
			InputSchema: object(map[string]any{
				"job_id":      stringField(),
				"approval_id": stringField(),
				"decision":    enum("approved", "denied"),
				"reason":      stringField(),
			}, []string{"job_id", "approval_id", "decision", "reason"}),
		},
		{
			Name:        "rdev.artifacts.list",
			Description: "List artifacts produced by a job.",
			Safety:      "Read-only metadata.",
			InputSchema: object(map[string]any{
				"job_id": stringField(),
			}, []string{"job_id"}),
		},
		{
			Name:        "rdev.artifacts.read",
			Description: "Read an artifact produced by a job.",
			Safety:      "Read-only; may expose sensitive job output.",
			InputSchema: object(map[string]any{
				"artifact_id": stringField(),
			}, []string{"artifact_id"}),
		},
		{
			Name:        "rdev.artifacts.export_bundle",
			Description: "Export a reviewable evidence bundle for one job, including manifest, checksums, envelope, artifacts, and audit slice.",
			Safety:      "Read-only evidence export; may expose sensitive job output.",
			InputSchema: object(map[string]any{
				"job_id": stringField(),
			}, []string{"job_id"}),
		},
		{
			Name:        "rdev.audit.query",
			Description: "Query redacted audit events for tickets, hosts, and jobs.",
			Safety:      "Read-only audit access.",
			InputSchema: object(map[string]any{
				"target_id": stringField(),
				"limit":     integer(1, 500),
			}, []string{"target_id"}),
		},
		{
			Name:        "rdev.policy.explain",
			Description: "Explain whether a requested action would be allowed by policy.",
			Safety:      "Read-only policy simulation.",
			InputSchema: object(map[string]any{
				"mode":       enum("attended-temporary", "managed", "break-glass"),
				"capability": stringField(),
			}, []string{"mode", "capability"}),
		},
		{
			Name:        "rdev.policy.explain_shell",
			Description: "Explain whether a shell job policy is structurally allowed before creating a signed job.",
			Safety:      "Read-only policy simulation; host still re-validates before execution.",
			InputSchema: object(map[string]any{
				"mode":   enum("attended-temporary", "managed", "break-glass"),
				"policy": map[string]any{"type": "object"},
			}, []string{"mode", "policy"}),
		},
		{
			Name:        "rdev.adapter.verify_result",
			Description: "Verify an adapter result artifact against the public result-artifact conformance contract.",
			Safety:      "Read-only artifact validation; does not execute on a host.",
			InputSchema: object(map[string]any{
				"adapter":                stringField(),
				"schema":                 stringField(),
				"artifact_json":          stringField(),
				"artifact_id":            stringField(),
				"command_fields":         stringArray(),
				"required_string_fields": stringArray(),
				"require_timing":         boolField(),
				"require_redaction":      boolField(),
				"reject_secret_patterns": boolField(),
			}, []string{"adapter", "schema"}),
		},
		{
			Name:        "rdev.adapter.verify_lifecycle",
			Description: "Verify an adapter lifecycle manifest against the public Adapter SDK lifecycle conformance contract.",
			Safety:      "Read-only manifest validation; does not execute on a host.",
			InputSchema: object(map[string]any{
				"adapter":                stringField(),
				"schema":                 stringField(),
				"artifact_json":          stringField(),
				"artifact_id":            stringField(),
				"required_phases":        stringArray(),
				"require_safety":         boolField(),
				"require_cancellation":   boolField(),
				"require_result_schema":  boolField(),
				"reject_secret_patterns": boolField(),
			}, []string{"adapter"}),
		},
		{
			Name:        "rdev.adapter.verify_cancellation",
			Description: "Verify an adapter cancellation result artifact against the public cancellation conformance contract.",
			Safety:      "Read-only artifact validation; does not execute on a host.",
			InputSchema: object(map[string]any{
				"adapter":                stringField(),
				"schema":                 stringField(),
				"artifact_json":          stringField(),
				"artifact_id":            stringField(),
				"command_fields":         stringArray(),
				"required_string_fields": stringArray(),
				"require_timing":         boolField(),
				"require_redaction":      boolField(),
				"reject_secret_patterns": boolField(),
			}, []string{"adapter", "schema"}),
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

func enum(values ...string) map[string]any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return map[string]any{"type": "string", "enum": items}
}
