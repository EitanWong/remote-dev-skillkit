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
			Name:        "rdev.invites.create",
			Description: "Create an Agent-first remote session invite with a universal connection entry, manifest URL, pinned manifest root, transport plan, and next MCP actions.",
			Safety:      "Creates an access path and connection metadata; does not execute on the host.",
			InputSchema: object(map[string]any{
				"gateway_url":           stringField(),
				"mode":                  enum("attended-temporary", "managed", "break-glass"),
				"ttl_seconds":           integer(60, 86400),
				"capabilities":          stringArray(),
				"reason":                stringField(),
				"transport":             enum("auto", "wss", "long-poll", "poll"),
				"network_scope":         enum("auto", "internet", "lan", "relay", "mesh", "ssh"),
				"authority_profile":     enum("standard", "max-control"),
				"rdev_command":          stringField(),
				"once":                  boolField(),
				"require_host_approval": boolField(),
				"auto_approve":          boolField(),
			}, []string{"gateway_url", "reason"}),
		},
		{
			Name:        "rdev.support_session.handoff",
			Description: "Return the single standard first-contact decision for a fresh Agent that needs to connect a target machine. If gateway_url is provided or a configured RDEV_*_GATEWAY_URL fallback exists, call rdev.support_session.create next; if not, run the returned foreground rdev support-session start command. Agents should use this before choosing between prepare/create/start/plan/status, and must not write their own bootstrap, relay, approval-polling, or recovery scripts.",
			Safety:      "Planning only; does not create tickets, start a gateway, approve hosts, execute on the target host, or install persistence.",
			InputSchema: object(map[string]any{
				"repo_root":    stringField(),
				"work_dir":     stringField(),
				"gateway_url":  stringField(),
				"addr":         stringField(),
				"target":       enum("auto", "windows", "macos", "linux"),
				"reason":       stringField(),
				"ttl_seconds":  integer(60, 86400),
				"auto_approve": boolField(),
				"locale":       stringField(),
				"rdev_command": stringField(),
			}, nil),
		},
		{
			Name:        "rdev.support_session.prepare",
			Description: "Inspect a fresh Agent runtime for one-command support-session readiness: local rdev recovery, Go/Git checkout state, gateway URL candidates, configured RDEV_*_GATEWAY_URL fallbacks, platform helper asset availability, target bootstrap self-repair, and standard recovery actions. Agents should call this before improvising setup when rdev or gateway state is unclear, then use the recommended gateway_url_candidates entry instead of asking humans to assemble gateway values.",
			Safety:      "Read-only by default; with build_assets=true it builds local helper binaries from the checked-out source but does not create tickets, start a gateway, approve hosts, or execute on a target machine.",
			InputSchema: object(map[string]any{
				"repo_root":    stringField(),
				"work_dir":     stringField(),
				"gateway_url":  stringField(),
				"addr":         stringField(),
				"target":       enum("auto", "windows", "macos", "linux"),
				"build_assets": boolField(),
			}, nil),
		},
		{
			Name:        "rdev.support_session.create",
			Description: "Create an attended temporary support session through an already reachable gateway and return a ready user_handoff, target command, status watcher, and connection_continuity_policy. If gateway_url is omitted, the tool uses the first configured RDEV_*_GATEWAY_URL fallback. The target command is the standardized fallback surface and can try ordered gateway URL candidates, including configured RDEV_*_GATEWAY_URL hosted/relay/mesh/VPN/SSH fallbacks, with bounded timeout/retry behavior without Agent-authored scripts. Agents should read stable_after_lan_change before promising durable connectivity, prefer user_handoff.message plus user_handoff.copy_paste for the human-facing response, and use the foreground CLI command rdev support-session start when no gateway is running instead of manually creating an invite, substituting ticket codes, or writing bootstrap glue.",
			Safety:      "Creates a scoped attended-temporary ticket with first-host auto approval by default; does not execute on the target host or install hidden persistence.",
			InputSchema: object(map[string]any{
				"gateway_url":         stringField(),
				"target":              enum("auto", "windows", "macos", "linux"),
				"reason":              stringField(),
				"ttl_seconds":         integer(60, 86400),
				"auto_approve":        boolField(),
				"locale":              stringField(),
				"operator_token_file": stringField(),
				"rdev_command":        stringField(),
			}, nil),
		},
		{
			Name:        "rdev.support_session.plan",
			Description: "Create a standardized Agent-native support session plan for review/debug access to gateway startup, recommended gateway URL candidates, configured RDEV_*_GATEWAY_URL fallbacks, verified helper assets, invite creation, localized target commands with built-in candidate fallback/timeout policy, and scoped attended-temporary auto-approval. Agents should prefer rdev.support_session.create when a gateway is running and the foreground CLI command rdev support-session start when no gateway is running.",
			Safety:      "Planning only; does not start a gateway, create a ticket, approve a host, install software, or execute on the target host.",
			InputSchema: object(map[string]any{
				"repo_root":    stringField(),
				"work_dir":     stringField(),
				"gateway_url":  stringField(),
				"addr":         stringField(),
				"target":       enum("auto", "windows", "macos", "linux"),
				"reason":       stringField(),
				"ttl_seconds":  integer(60, 86400),
				"auto_approve": boolField(),
				"locale":       stringField(),
			}, nil),
		},
		{
			Name:        "rdev.support_session.status",
			Description: "Read or wait on standardized support-session connection status for a ticket code so Agents can tell the user when the target host has connected, is pending approval, timed out, or is still waiting.",
			Safety:      "Read-only status check; does not approve hosts or execute jobs.",
			InputSchema: object(map[string]any{
				"ticket_code":     stringField(),
				"locale":          stringField(),
				"wait":            boolField(),
				"timeout_seconds": integer(0, 3600),
				"interval_millis": integer(100, 60000),
			}, []string{"ticket_code"}),
		},
		{
			Name:        "rdev.connection_entry.plan",
			Description: "Materialize an invite into the universal Connection Entry handoff and self-contained Connection Entry runner package. Humans receive only a link, visible launcher, or signed package; ticket, gateway, manifest root, transport, release, relay, mesh, VPN, SSH, checksum, helper startup, and approved dependency install values stay in Agent/tool metadata.",
			Safety:      "Planning only; writes no remote state and does not execute on the target host.",
			InputSchema: object(map[string]any{
				"invite_json":                       stringField(),
				"target_os":                         enum("windows", "darwin", "linux"),
				"target_arch":                       enum("amd64", "arm64"),
				"ownership":                         enum("owned", "third-party"),
				"session_mode":                      enum("attended-temporary", "managed", "break-glass"),
				"release_bundle_url":                stringField(),
				"release_bundle_path":               stringField(),
				"release_bundle_required_artifacts": stringField(),
				"release_root_public_key":           stringField(),
				"out_dir":                           stringField(),
				"managed_binary_path":               stringField(),
				"managed_service_name":              stringField(),
				"managed_service_label":             stringField(),
				"managed_unit_name":                 stringField(),
				"windows_host_download_url":         stringField(),
				"windows_host_sha256":               stringField(),
				"windows_verifier_download_url":     stringField(),
				"windows_verifier_sha256":           stringField(),
				"windows_bootstrap_script":          stringField(),
				"windows_bootstrap_script_url":      stringField(),
				"windows_bootstrap_script_sha256":   stringField(),
				"host_name":                         stringField(),
				"rdev_command":                      stringField(),
				"force":                             boolField(),
			}, []string{"invite_json"}),
		},
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
				"adapter": enum("shell", "powershell", "acpx", "codex", "claude-code"),
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
			Name:        "rdev.enrollment.verify_certificate",
			Description: "Verify a host enrollment certificate and optional signed revocation list against a pinned enrollment root before trusting a host registration.",
			Safety:      "Read-only certificate and revocation validation; does not issue certificates or grant host access.",
			InputSchema: object(map[string]any{
				"certificate_json":        stringField(),
				"artifact_id":             stringField(),
				"root_public_key":         stringField(),
				"revocations_json":        stringField(),
				"revocations_artifact_id": stringField(),
				"verify_at":               stringField(),
			}, []string{"root_public_key"}),
		},
		{
			Name:        "rdev.update.check",
			Description: "Check the latest GitHub Release for Remote Dev Skillkit and compare it with the installed version.",
			Safety:      "Read-only GitHub release metadata lookup; does not download, install, or replace files.",
			InputSchema: object(map[string]any{
				"repo":            stringField(),
				"api_base_url":    stringField(),
				"current_version": stringField(),
			}, nil),
		},
		{
			Name:        "rdev.update.plan",
			Description: "Create a dry-run update plan from the latest GitHub Release, including platform archive selection and verification steps.",
			Safety:      "Read-only planning; does not download, install, replace binaries, restart services, or mutate configuration.",
			InputSchema: object(map[string]any{
				"repo":            stringField(),
				"api_base_url":    stringField(),
				"current_version": stringField(),
				"platform":        stringField(),
			}, nil),
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
		{
			Name:        "rdev.adapter.verify_runtime",
			Description: "Verify an adapter runtime fixture generated by the public Adapter SDK lifecycle runner.",
			Safety:      "Read-only fixture validation; does not execute on a host.",
			InputSchema: object(map[string]any{
				"adapter":                 stringField(),
				"artifact_json":           stringField(),
				"artifact_id":             stringField(),
				"required_phases":         stringArray(),
				"require_successful":      boolField(),
				"require_cleanup":         boolField(),
				"require_result_artifact": boolField(),
				"require_cancellation":    boolField(),
			}, []string{"adapter"}),
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
