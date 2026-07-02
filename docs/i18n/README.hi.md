# Remote Dev Skillkit

Remote Dev Skillkit उन AI Agent के लिए safety layer है जिन्हें असली Mac, Windows और Linux machines पर काम करना होता है। यह कोई छिपा हुआ remote-control tool नहीं है। Codex, Claude Code, Hermes, OpenClaw/OpenCode और generic MCP Agent signed jobs, host-local policy, approvals, evidence bundles और audit chains के साथ असली development work चला सकते हैं।

## मुख्य बातें

- Popular Agent Frameworks में install होने वाला portable Agent Skillkit।
- Jobs पहले signed और verified होते हैं, फिर run होते हैं; Agent को raw shell खुली छूट नहीं मिलती।
- Skills पहले OS, shell, service manager, gateway, workspace, adapter और permissions detect करती हैं। unclear हो तो guess नहीं करतीं, पूछती हैं।
- Codex, Claude Code, ACP/acpx, shell, PowerShell और custom adapters के लिए support।
- Apache-2.0 open-source license।

## Fast Install

अगर आप पहले से Codex, Claude Code, Hermes, OpenClaw/OpenCode या किसी दूसरे MCP Agent में हैं, तो यह अपने Agent में paste करें:

```text
Bootstrap Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit

Clone or update the repository in a safe user/workspace location. Then read
`docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow it as
the source of truth. If cloning is blocked, read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Install the Skillkit and configure MCP for this agent. Probe OS, shell, Git, Go,
`rdev`, current agent framework, skill directory, MCP config, and network state
before acting. Ask one short question when a required value is unclear. For this
personal computer, prefer local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL.

If `rdev` is missing, do not stop. Recover it from an existing local rdev binary, build this checkout with `go install ./cmd/rdev`, use `go run ./cmd/rdev bootstrap agent-plan --repo-root .` as a temporary planner, or clone then build in a safe user/workspace location. After `rdev` works, run `rdev bootstrap agent-plan --repo-root .` and follow the JSON plan.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. If local `rdev`, gateway state,
or target helper assets are unclear, first call `rdev.support_session.prepare`
through MCP or run `rdev support-session prepare --build-assets` from the
checkout. Follow its `connection_readiness`, `asset_report`, `missing_inputs`,
and `standard_recovery` fields instead of writing custom bootstrap, approval, or
relay glue. Use the recommended `gateway_url_candidates` entry for target-side
commands; never send a remote target `0.0.0.0`, and treat loopback as
same-machine only. If a reachable gateway is already running, call
`rdev.support_session.create` through MCP or `rdev support-session create`
through CLI to get the ready target command, join URL, real ticket code,
manifest root, scoped auto-approval state, and status watch command in one
payload. Prefer `user_handoff.message` plus `user_handoff.copy_paste` for
the human-facing response. When `user_handoff.target` is `auto`, follow
`user_handoff.auto_target_rule`: send the join URL first and use returned
platform commands only when a terminal command is needed. The target command already tries ordered Connection
Entry URLs on the target machine with bounded timeout/retry behavior, so do not
write custom fallback scripts. If no gateway is
running yet, run `rdev support-session start` in a visible foreground terminal;
it prepares verified helper assets when possible, starts the local gateway,
selects a target-usable gateway URL candidate, and prints the same ready session
payload before listening. Use
`rdev.support_session.plan` or `rdev support-session plan` only
for review/debug planning. After giving the target-machine command, watch
`rdev.support_session.status` with `wait=true` or `rdev support-session status --wait`; when
`connected=true`, proactively tell me the connection has been established before
creating jobs. Use
`rdev.invites.create`, then materialize it with `rdev.connection_entry.plan` or
`rdev connection-entry plan` only when lower-level invite materialization is
needed.
Read `connection_entry.package_catalog` and the signed join manifest
`package_catalog` to select the target OS/architecture package candidate.
Give the target side only the selected link, visible runner/launcher,
visible script, or signed package. Prefer the materialized Connection Entry
runner when `runner_plan` is available; dry-run it before execute when network
reliability is uncertain. If release package assets are not published yet, use
the visible fallback script. Keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed. Use
existing open-source/free options through configured helper gateway overrides such
as `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`,
`RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL` when unambiguous; prefer frp,
Chisel, headscale-compatible mesh, or WireGuard before paid relays. If a
user-space helper is missing and an explicit URL/SHA-256 is approved, use
`rdev deps install` or reviewed `RDEV_*_INSTALL_ACTION_JSON` metadata for
user/workspace-scoped helper repair before helper startup; ask before
privileged, persistent, paid, firewall, DNS, cloud, credential, route, or
security-policy changes. For company or third-party machines, ask for authorization first, then default to visible attended-temporary mode and let Connection Entry probes detect Windows/macOS/Linux. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
```

पूरा copy-paste Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Skillkit bundle export और verify करें:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Agent Frameworks के लिए reviewable install plan बनाएं:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Direct install default रूप से dry-run है। Report देखें, फिर `--execute` जोड़ें:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

दूसरी install line verified bundle मिलने के बाद one-command install path है।

## Local Demo

```bash
go test ./...
rdev demo local
rdev mcp tools
```

Technical authority English [README](../../README.md) है। Translation अलग लगे तो English README follow करें।
