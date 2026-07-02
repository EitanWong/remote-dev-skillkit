# MCP Stdio Server

`rdev mcp serve` exposes the current Remote Dev Skillkit tool contract over a newline-delimited JSON-RPC stdio loop.

Implemented methods:

- `initialize`
- `notifications/initialized`
- `tools/list`
- `tools/call`

The server currently uses an in-memory gateway. It is suitable for local integration tests and early agent wiring, not persistent production use.

Agent-first session tools include:

- `rdev.support_session.prepare`
- `rdev.support_session.plan`
- `rdev.support_session.create`
- `rdev.support_session.status`
- `rdev.invites.create`
- `rdev.connection_entry.plan`

Connection Entry is the universal target-side handoff. MCP clients should not
invent narrower public names such as customer link or connector package plan:
the same contract covers owned managed hosts, third-party temporary support,
LAN, hosted, relay, mesh, SSH, and VPN-assisted connectivity.

`rdev.support_session.create` returns `rdev.support-session-created.v1` in
`structuredContent`. Agents should prefer it when a reachable gateway is
already running. It creates the scoped attended-temporary ticket and returns a
ready target-machine command, join URL, manifest URL, pinned manifest root,
ticket code, and status watcher command. The returned command strings have the
real ticket code already filled in; Agents must not ask humans to assemble
ticket/root/gateway/transport values or write replacement bootstrap code.

`rdev.support_session.prepare` returns `rdev.support-session-prepare.v1` in
`structuredContent`. Fresh Agent sessions should call it when local `rdev`,
gateway state, helper assets, or one-command target readiness is unclear. It
reports the detected OS/arch, Go/Git/`rdev` paths, resolved repo/work dirs,
gateway URL candidates, Windows/macOS/Linux helper asset URLs and SHA-256
hashes, `connection_readiness`, `missing_inputs`, and standard recovery
actions. Agents should use the `recommended=true` item from
`gateway_url_candidates` for target-side commands and should not ask humans to
assemble gateway URLs. Wildcard listen addresses such as `0.0.0.0` are never a
target URL; loopback candidates are same-machine only. By default it is
read-only. With `build_assets=true`, it builds local helper binaries from the
checked-out source so target bootstraps can download verified helpers when the
target machine does not already have `rdev`.

When no suitable gateway is running yet, Agents should run
`rdev support-session start` as a visible foreground CLI process. It starts the
local gateway, creates the attended-temporary ticket, prints
`rdev.support-session-started.v1` with an embedded
`rdev.support-session-created.v1`, includes `asset_report` and
`connection_readiness`, includes the same recommended gateway URL candidates,
then keeps serving until interrupted. This is a CLI foreground process rather
than an MCP tool because MCP calls should not hide or orphan a long-running
gateway.

`rdev.support_session.plan` returns `rdev.support-session-plan.v1` in
`structuredContent`. Agents should call it before inventing any gateway,
PowerShell, relay, nohup, ticket, root, transport, approval, or helper install
steps when they need review/debug access to the underlying gateway argv. The
plan returns:

- build commands for a local `rdev` plus Windows/macOS/Linux helper binaries;
- recommended gateway URL candidates derived from the listen address and local
  private interfaces, with explicit gateway overrides preserved;
- a gateway start argv with state, signing keys, audit log, and verified helper
  asset flags;
- HTTP and CLI invite creation commands with scoped attended-temporary
  `auto_approve` when enabled;
- localized target-user instructions for Windows and macOS/Linux;
- forbidden-action guardrails such as no `ExecutionPolicy Bypass`, hidden
  install, unverified binary download, or human assembly of ticket/root/gateway
  values.

The plan is read-only. It does not start the gateway, create a ticket, approve a
host, install software, or execute on the target host. Agents execute only the
returned argv steps they have verified in the current environment.

`rdev.support_session.status` returns `rdev.support-session-status.v1` in
`structuredContent`. Agents should call it after giving the target user the
Connection Entry command, and should continue watching until `connected=true` or
`status=pending-approval`. When `connected=true`, the Agent must proactively
tell the user that the connection has been established before creating jobs. The
tool returns localized `feedback` and `next_action` strings so the Agent does
not need to invent status wording.

`rdev.invites.create` returns `rdev.agent-invite.v1` in `structuredContent`.
It creates a ticket and returns a manifest URL, `host_command`,
`transport_plan`, `connection_plan`, `connection_entry`,
`connection_entry.package_catalog`, `connection_entry_plan`,
`host_context_plan`, `agent_provisioning_plan`,
`agent_collaboration_plan`, `localization_plan`,
`managed_development_plan`, `fallback_commands`, `authority_profile`,
`connectivity_checks`, `human_next_actions`, and `agent_next_actions`. Agents
should call this before asking a human to connect a new target host. The command
still requires consent on the target machine; it does not execute remotely by
itself. The default transport is `auto`, which tries WSS first and falls back to
HTTPS long-poll and then short polling when restrictive networks block
WebSocket upgrades or long-held requests.

When `auto_approve=true`, `rdev.invites.create` creates ticket metadata that can
auto-activate the first host only for an attended-temporary Connection Entry.
This is meant for explicit visible support sessions generated by
`rdev.support_session.create` or `rdev.support_session.plan`; it is not a global
approval bypass and is rejected for managed or break-glass tickets.

For every new target host, use `connection_entry.entry_url` or a signed
connection entry package over manually copying host flags. The join page
provides visible, inspectable platform bootstrap commands for the target
machine. Agents should verify the signed join manifest, read its
`package_catalog`, and select a package candidate from target OS/architecture
probes. When package assets are not published or release inputs are missing,
use the catalog's visible script fallback instead of asking a human to assemble
raw connection values. Agents should treat those commands as consented startup,
then choose the correct host mode from
`connection_entry_plan.target_selection_policy`: `managed` for operator-owned
machines that need durable development access, or `attended-temporary` for
third-party or one-off repair. If ownership or persistence approval is unclear,
ask one short question before managed mode. After the host connects, the Agent
watches `rdev.hosts.list`, approves the expected host when policy requires
approval, runs scoped jobs, exports evidence, and revokes or stops the session
when finished.

`rdev.connection_entry.plan` turns an invite into
`rdev.connection-entry.materialization-plan.v1`. MCP clients should call it
before showing target-side instructions. It returns the selected
ownership/session-mode decision, human-facing surfaces, Agent-only metadata,
network strategy, package-catalog candidate choice, missing release inputs, and,
when enough release material is available, an
`rdev.connection-entry.package-plan.v1` wrapper around the platform
package/launcher plan. When the agent supplies `out_dir`, the MCP tool also
writes `CONNECTION_ENTRY.md`, `connection-entry-plan.json`, and generated
launcher/package planning files into that empty directory. Target-side humans
receive only the selected Connection Entry surface. Ticket codes, manifest
roots, gateway URLs, transport preferences, release roots, and checksums stay in
Agent/tool metadata.

For owned managed machines, `rdev.connection_entry.plan` can also generate
reviewed macOS LaunchAgent, Linux systemd user-service, or Windows Service
package plans when the agent supplies the target-local `managed_binary_path`,
`release_bundle_path`, release root, and optional service label/name/unit. This
is still planning only: service start/install/uninstall remains an explicit
operator-reviewed follow-up step.

The `host_context_plan` is the standard for AI-native context management. It
sets `storage_location=remote-host-first` and
`server_context_budget=index-and-on-demand-slices`. Agents should keep project
structure, environment probes, requirement notes, transcripts, logs, and large
evidence on the host; the gateway should expose only host ids, workspace
handles, artifact ids, checksums, sizes, redaction metadata, and freshness
timestamps until the agent explicitly requests a slice.

The `agent_provisioning_plan` is the standard for adaptive target-host setup.
MCP clients should detect installed Skillkit skills, MCP tool contracts,
adapters, shells, package managers, language runtimes, project lockfiles,
framework paths, proxy settings, permissions, and trust roots before installing
anything. Policy may allow user-scoped or workspace-scoped installation of
verified skills, MCP metadata, adapter helpers, and project dependencies. It
must ask for approval before elevation, system-wide packages, services,
credentials, firewall changes, external accounts, paid resources, publish,
deploy, push, or security-policy mutation.

The `agent_collaboration_plan` is the standard for cooperating with other AI
tools on the target host. It includes A2A-style discovery through Agent Cards,
JSON-RPC/HTTP tasks, SSE streaming, MCP stdio peers, and local Agent CLIs. MCP
clients may delegate bounded subtasks to discovered peers when doing so helps
the remote repair or development task, but peer work must be wrapped in rdev
jobs so host policy, workspace locks, redaction, cancellation, approval gates,
audit, and evidence still apply.

The `localization_plan` is the standard for cross-language behavior. MCP
clients should detect the target-side language from explicit `lang`
inputs, `Accept-Language`, `LANG`/`LC_*`/`LANGUAGE`, Windows UI culture, macOS
AppleLanguages, Linux locale settings, and operator preferences. User-facing
instructions, approvals, summaries, and evidence should use the selected BCP 47
language. Protocol keys, schema versions, capability ids, command names, paths,
JSON field names, checksums, and code blocks must remain stable and untranslated.

The `managed_development_plan` is the standard for owned long-running
developer workstations. MCP clients should prefer managed mode for machines
owned by the operator and expected to do recurring development work. The plan
points agents toward reviewed LaunchAgent, systemd user service, or Windows
Service surfaces, `--once=false`, `--transport auto`, release-bundle startup
gates, enrollment renewal, revocation refresh, trust-bundle rollback checks,
workspace locks, Git worktrees, reconnect proof, and evidence bundles.

The `connection_plan` separates native support from agent-managed paths:
implemented native paths are outbound WSS/mTLS, HTTPS long-poll, HTTPS
short-poll, and LAN-reachable gateway URLs. Agent-managed paths such as an
HTTPS relay, mesh/VPN, or SSH tunnel may be used automatically when local
configuration or credentials are already available. Missing or ambiguous
configuration should trigger a concise question; external account or credential
mutation still requires explicit approval. These paths remain connectivity
only.

The default `authority_profile` is `max-control`. It allows the approved remote
host to act as a field workstation for heuristic discovery and downstream
control when the job policy grants capabilities such as
`network.discovery.scoped`, `ssh.tunnel`, `mesh.use`, `relay.use`, and
`downstream.control.scoped`. MCP clients should treat the profile as the
machine-readable boundary for autonomous discovery, selected control paths,
stop conditions, and required evidence.

Useful read-only tools include:

- `rdev.policy.explain`
- `rdev.policy.explain_shell`
- `rdev.enrollment.verify_certificate`
- `rdev.adapter.verify_result`
- `rdev.adapter.verify_lifecycle`
- `rdev.adapter.verify_cancellation`
- `rdev.adapter.verify_runtime`

`rdev.enrollment.verify_certificate` returns
`rdev.enrollment-certificate-verification.v1` in `structuredContent`. It
accepts either `certificate_json` or `artifact_id`, plus a required
`root_public_key` encoded as `key_id:base64url_ed25519_public_key`, optional
`revocations_json` or `revocations_artifact_id`, and optional RFC3339
`verify_at`. Invalid certificates, expired windows, wrong roots, stale or
tampered revocation lists, revoked certificates, and signature mismatches return
`ok=false` with recommended actions rather than an RPC failure. Missing required
arguments remain RPC errors.

When a gateway exposes configured dev revocations through
`GET /v1/enrollment/revocations`, operators or agents should first fetch and
verify the list with `rdev enrollment fetch-revocations --root-public-key ...`,
then pass the fetched JSON as `revocations_json` or store it as an artifact and
pass `revocations_artifact_id`. Fetching a revocation list is read-only and does
not approve a host.

`rdev.adapter.verify_result` returns `rdev.adapter-conformance-report.v1` in
`structuredContent`. It accepts either `artifact_json` or `artifact_id`, plus
the expected adapter and result schema.

`rdev.adapter.verify_lifecycle` returns the same report schema for
`rdev.adapter-lifecycle.v1` manifests. It validates the required adapter
lifecycle phases, safety declarations, cancellation behavior, cleanup behavior,
and result schema declarations before a new adapter is exposed to agents.

`rdev.adapter.verify_cancellation` returns the same report schema for canceled
result artifacts. It accepts either `artifact_json` or `artifact_id`, plus the
expected adapter and result schema. It verifies normal result conformance first,
then requires command evidence to show `canceled=true`, `timed_out=false`, an
`exit_code`, and `output_truncated` metadata.

`rdev.adapter.verify_runtime` returns the same report schema for
`rdev.adapter-runtime-fixture.v1` fixtures generated by the public Adapter SDK
runtime lifecycle runner. It verifies phase order, evidence, timing, cleanup,
optional result artifacts, and optional cancellation behavior.

## Example

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rdev.invites.create","arguments":{"gateway_url":"https://api.example.com/v1","mode":"attended-temporary","ttl_seconds":600,"reason":"local test","transport":"auto"}}}' \
  '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"rdev.adapter.verify_result","arguments":{"adapter":"shell","schema":"rdev.shell-result.v1","artifact_json":"{\"schema_version\":\"rdev.shell-result.v1\",\"adapter\":\"shell\",\"workspace_root\":\"/tmp/repo\",\"exit_code\":0,\"timed_out\":false,\"canceled\":false,\"output_truncated\":false,\"started_at\":\"2026-06-30T00:00:00Z\",\"ended_at\":\"2026-06-30T00:00:01Z\",\"duration_millis\":1000,\"redacted\":false,\"redaction_rules\":[\"openai_api_key\"]}"}}}' \
  '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"rdev.adapter.verify_lifecycle","arguments":{"adapter":"claude-code","artifact_json":"{\"schema_version\":\"rdev.adapter-lifecycle.v1\",\"adapter\":\"claude-code\",\"phases\":{\"detect\":{\"implemented\":true,\"evidence\":[\"version\"]},\"plan\":{\"implemented\":true,\"evidence\":[\"commands\"],\"declares_external_consequences\":true,\"declares_required_approvals\":true},\"prepare\":{\"implemented\":true,\"evidence\":[\"workspace\"],\"enforces_workspace_boundary\":true,\"uses_workspace_lock\":true},\"run\":{\"implemented\":true,\"evidence\":[\"process\"],\"supports_timeout\":true,\"supports_cancellation\":true},\"collect\":{\"implemented\":true,\"evidence\":[\"result\"],\"emits_result_artifact\":true,\"result_schema\":\"rdev.claude-code-result.v1\"},\"cleanup\":{\"implemented\":true,\"evidence\":[\"cleanup\"],\"idempotent\":true,\"releases_locks\":true}},\"safety\":{\"adapter_authorizes_jobs\":false,\"adapter_approves_dangerous_actions\":false,\"adapter_installs_persistence\":false,\"host_validates_before_run\":true,\"redacts_outputs\":true},\"cancellation\":{\"supported\":true,\"evidence_field\":\"canceled\",\"timeout_exclusive\":true,\"cleanup_on_cancel\":true}}"}}}' \
  '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"rdev.adapter.verify_cancellation","arguments":{"adapter":"shell","schema":"rdev.shell-result.v1","artifact_json":"{\"schema_version\":\"rdev.shell-result.v1\",\"adapter\":\"shell\",\"workspace_root\":\"/tmp/repo\",\"exit_code\":-1,\"timed_out\":false,\"canceled\":true,\"output_truncated\":false,\"started_at\":\"2026-06-30T00:00:00Z\",\"ended_at\":\"2026-06-30T00:00:01Z\",\"duration_millis\":1000,\"redacted\":false,\"redaction_rules\":[\"openai_api_key\"]}"}}}' \
  | rdev mcp serve
```

Tool calls return:

- `content`: text content for basic MCP clients.
- `structuredContent`: machine-readable result data.

## Current Limitations

- In-memory only.
- Persistent host sessions require a gateway with configured storage.
- Job envelopes are signed with an in-memory development Ed25519 key; production key storage is not implemented yet.
- Real host networking is provided by the gateway HTTP/WSS surfaces; this stdio server is the local MCP control surface.
