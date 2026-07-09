---
name: safe-remote-support
description: Use when an agent needs to connect to, operate, or audit a remote machine through Remote Dev Skillkit — covering Windows, macOS, and Linux targets, temporary and managed sessions, public-tunnel first connectivity, scoped session tasks, evidence, and cleanup.
---

# Safe Remote Support

Use this skill whenever a user says "connect to my machine", "remote debug", "help with this computer", or similar.

---

## CRITICAL CONNECTIVITY RULES (read before every session)

### Rule 1 — Public tunnel FIRST, LAN never assumed

**The target machine may be on a completely different network. NEVER use a LAN/private-IP address as the primary endpoint.**

Before creating any session:
1. Check `gateway_candidate_summary.needs_public_tunnel` from `rdev.support_session.prepare` output.
2. If `needs_public_tunnel=true` OR if no stable configured gateway exists:
   - Run `rdev support-session connect --start`.
   - Let `rdev` manage the public tunnel internally. It prefers configured stable gateways (`RDEV_HOSTED_GATEWAY_URL`, `RDEV_CLOUDFLARED_NAMED_TUNNEL_URL`, relay/mesh/VPN/SSH URLs), then starts Cloudflare Quick Tunnel with HTTP/2 first, then falls back to localhost.run SSH tunnel when needed.
   - Read the generated `target_handoff_envelope.full_text` or `handoff_text_file.path`; do not manually start tunnels or assemble `--gateway-url`.
3. LAN/private-IP candidates are acceptable as **secondary** fallbacks only after the managed public-tunnel path fails or a stable configured gateway is already present.

**Never send a raw `http://192.168.x.x:port` or `http://[fe80::...]:port` address to a remote user.**

**Do not persist `https://*.trycloudflare.com` Quick Tunnel URLs as durable
configuration.** They are emergency/default fallback URLs for the current
foreground session. For repeated sessions on the same Agent machine or a cloud
server, prefer:

- `RDEV_HOSTED_GATEWAY_URL=https://your-domain-or-public-gateway` when the
  Agent runs on a VPS/cloud server with its own reachable domain or IP.
- `RDEV_CLOUDFLARED_NAMED_TUNNEL_URL=https://rdev.example.com` plus
  `RDEV_CLOUDFLARED_TUNNEL_TOKEN`,
  `RDEV_CLOUDFLARED_TUNNEL_TOKEN_FILE`,
  `RDEV_CLOUDFLARED_TUNNEL_NAME`, or
  `RDEV_CLOUDFLARED_NAMED_TUNNEL_START_ARGV_JSON` when a reusable Cloudflare
  Named Tunnel has been configured.

If no stable URL is configured, connect first with the managed Quick Tunnel
fallback, then tell the operator how to configure a stable URL for the next
session.

### Rule 2 — One command to the user, nothing else

The target-side human receives **one** thing: `target_handoff_envelope.full_text`.
Do not ask them to:
- Install rdev manually
- Copy ticket codes
- Pick a gateway URL
- Choose a network path
- Enter checksums

Everything is already embedded in the handoff. If it is not, fix the session setup rather than asking the user.

Do not send `join_url` alone. If `ready_to_send_to_human=false` or
`asset_report.all_ready=false`, stop and fix the session setup before sending
anything to the target-side human.

### Adaptive Configuration Contract

Before acting, probe the available `rdev` CLI/MCP surface, gateway state, network reachability, tunnel/mesh helpers, workspace path, target OS/shell, permissions, and connection modes. If any required value is unclear after read-only probes, ask one short question instead of inventing ticket codes, gateway URLs, root keys, checksums, workspace paths, adapter choices, tunnel commands, or interrupt policy.

### Rule 3 — Never ask about network configuration

Do not ask the user:
- "Are you on the same LAN?"
- "What is your IP address?"
- "Which tunnel do you want to use?"
- "Can you open port X?"

Auto-detect everything. If detection is ambiguous, default to public tunnel (cloudflared).

### Rule 4 — rdev recovery is automatic

If `rdev` is not found in PATH:
1. Try `./work/rdev-support-session/bin/rdev` (repo-local pre-built).
2. Try `go run ./cmd/rdev` from a valid checkout.
3. Try `go install ./cmd/rdev` if a checkout exists.
4. Never stop and ask the user to install rdev.

### Rule 5 — Temporary gateway process MUST run in foreground

For attended-temporary sessions, `rdev support-session connect --start` is a
**foreground process**. It manages the public tunnel, serves helper assets, and
holds session state in memory. If it exits, the tunnel drops and the target
machine loses connectivity.

Never run it with `&`, `nohup`, or in a background terminal. Keep it alive for the whole session.

If the gateway process unexpectedly exits:
1. Re-run `rdev support-session connect --start` with the same `--work-dir`.
2. Wait for the previous host heartbeat to go stale (up to 90 seconds).
3. Let the target machine rejoin and receive automatic session authorization.
4. Resume from the generated `target_handoff_envelope.full_text` / `handoff_text_file.path`.

For an operator-owned recurring machine, do not try to make the temporary
PowerShell or shell window persistent. First confirm ownership and persistence
authorization in one short question, configure a stable gateway
(`RDEV_HOSTED_GATEWAY_URL` or `RDEV_CLOUDFLARED_NAMED_TUNNEL_URL`), then use the
reviewed managed Connection Entry / Windows Service / systemd / LaunchAgent
plan surfaces. Never install persistence for third-party temporary support.

### Rule 6 — MCP tools must target the active gateway

The MCP server (`rdev mcp serve`) can maintain its own separate in-memory gateway. It may not see sessions, endpoints, tasks, or artifacts created by `rdev support-session connect --start`.

Pass `"gateway_url": "<active-gateway-url>"` explicitly on every
gateway-backed `rdev.sessions.*`, `rdev.audit.query`, and support-session
status/report call:

```
rdev.sessions.status(gateway_url="<ACTIVE_GATEWAY_URL>", session_id="ses_...")
rdev.sessions.task(gateway_url="<ACTIVE_GATEWAY_URL>", session_id="ses_...", ...)
```

Omitting `gateway_url` can hit the wrong empty gateway and produce misleading "not found" or empty-list results.

### Rule 7 — Find executables with `command -v`, not `find`

Do NOT use `find` to locate executables. Use:
- `command -v rdev` or `which rdev` (shell)
- Known install paths: `~/go/bin/rdev`, `./work/rdev-support-session/bin/rdev-<os>-<arch>`
- `Get-Command rdev -ErrorAction SilentlyContinue` (PowerShell)

`find` scans the entire filesystem and hangs on cache directories. It is never appropriate for locating a known executable.

### Rule 8 — Discover CLI parameters before using them

Before calling any `rdev` subcommand with unfamiliar flags:
1. Run `rdev <subcommand> --help` and read the output.
2. Only use flags that appear in the `--help` output.
3. Never invent flag names (e.g. `--public-tunnel` does not exist; the CLI manages tunnels internally via `connect --start`).

### Rule 9 — MCP tool prerequisites must be satisfied before calling

Before calling any MCP tool that requires a running gateway:
1. Resolve the active gateway URL from the current `connect --start` payload,
   `ready_file.path`, `target_handoff_envelope`, `connection_supervision`, or a
   configured `RDEV_*_GATEWAY_URL`.
2. Verify that exact gateway: `curl -fsS <ACTIVE_GATEWAY_URL>/healthz`.
3. If no active gateway URL exists, start the standard foreground flow with
   `rdev support-session connect --start`.
4. Never call `rdev.support_session.create` or `rdev.sessions.*` when no
   gateway URL is available — the call will fail or hit the wrong in-memory
   gateway.

### Rule 10 — Send `target_handoff_envelope.full_text`, not a bare URL

When the session is ready, deliver exactly `target_handoff_envelope.full_text` (or `handoff_text_file.path` content) to the target-side human. For `target=auto`, this is a multi-platform handoff with Windows PowerShell, macOS/Linux terminal, and browser fallback sections. Do NOT:
- Send a bare `https://...trycloudflare.com/join/XXXX` link alone (the user may not know to run it)
- Extract the URL and reconstruct a custom command
- Write your own `powershell -EncodedCommand` or Base64 bootstrap blob. The
  standard Windows human command should be a short readable
  `powershell -NoProfile -Command "irm '.../bootstrap.ps1' -UseBasicParsing | iex"`
  command generated by `rdev`; if it is not, regenerate the support session
  instead of hand-editing it.

### Rule 11 — Status polling has a 3-minute deadline; then diagnose

After sending the handoff:
1. Poll with `rdev.support_session.status wait=true` or watch `status_file.path`.
2. **After 3 minutes without `connected=true`**, stop polling and switch to diagnosis mode:
   - Was a public-tunnel URL sent, or a LAN IP?
   - Is the active gateway still running? (`curl <ACTIVE_GATEWAY_URL>/healthz`)
   - Did the target machine go to sleep or lock?
   - Is the Cloudflare tunnel still alive? (check process)
3. Present the user with specific, actionable next steps — not another "still waiting" message.

### Rule 12 — Host sessions keep awake, but never bypass lock policy

Temporary host sessions use built-in best-effort keep-awake protection while
`rdev host serve` is active:

- macOS: `caffeinate -dimsu`
- Linux: `systemd-inhibit --what=sleep:idle`
- Windows: `SetThreadExecutionState(ES_CONTINUOUS|ES_SYSTEM_REQUIRED|ES_DISPLAY_REQUIRED)`
- CLI runtime: `rdev host serve --keep-awake=true` by default

This prevents idle sleep/display sleep where the OS allows it. It does **not**
bypass lock-screen policy, enterprise device controls, user authentication, or
screen-unlock requirements. If a host becomes `stale`, treat sleep/lock/network
loss as likely causes and use the standard recovery/status flow instead of
asking the user to edit bootstrap scripts.

### Rule 13 — Do not disconnect automatically

Completing a task is not a signal to disconnect. Keep the host/session alive
until the operator explicitly asks to disconnect, revoke, stop the gateway, or
uninstall a managed service. Final reports should state connection continuity:
ephemeral Quick Tunnel vs stable configured gateway, and whether managed
reconnect is ready.

### Rule 14 — Treat sessions as Support Device Entries

Remote Dev Skillkit is an AI-native remote-control connector. Do not expose
ticket/root/gateway internals as the user's mental model. Use the standard
`remote_control_entry` returned by `connect`, `status`, `report`, and
`smoke-test`:

- `support_device_id` is the DeviceID-like handle. When the target connector
  has a persisted host identity, it stays stable across restarts.
- `session_passcode` is a ticket-scoped session password/passcode. It is not a
  long-lived shared host password.
- `explicit_disconnect_required=true` means even temporary customer support
  stays connected until the operator explicitly asks to disconnect, revoke, or
  stop it.

For third-party temporary support, keep the connector visible and attended; do
not install service persistence. For an operator-owned recurring machine, ask
one short ownership/persistence authorization question, require a stable gateway,
then use the reviewed managed-service upgrade path.

### Rule 15 — Use native remote-control tools before scripts

Remote Dev Skillkit is an Agent remote-control kernel. Before shell or
PowerShell workarounds, use `rdev.sessions.task` with the `file` or
`desktop` adapter, or the CLI `rdev files ... --session-id ...` and
`rdev desktop ... --session-id ...` wrappers.

Do not write `xdotool`, `cliclick`, AppleScript/System Events, Win32
PowerShell GUI scripts, raw `SendKeys`, mouse movement scripts, screenshot
scripts, file-transfer shell glue, or custom Base64 file upload/download code
when a native session-task file or desktop surface exists.

GUI and input tasks require an unlocked interactive user session. If the target
is locked, logged out, asleep, or blocked by OS/enterprise policy, report
`desktop_session_unavailable` and use the standard recovery path. Do not bypass
lock screens, privacy prompts, MDM, UAC, sudo, or enterprise controls.

---

## Workflow (5 steps, no branching for the user)

### Step 1 — Locate rdev (auto, no user input)

```
# Priority order:
go run ./cmd/rdev support-session prepare --build-assets --repo-root . --target windows
# or if installed:
rdev support-session prepare --build-assets --target windows
```

Read the JSON output. Check:
- `connection_readiness.target_bootstrap_self_repair` — assets ready?
- `gateway_candidate_summary.needs_public_tunnel` — need cloudflared?
- `gateway_candidate_summary.cloudflared_in_path` — already available?

### Step 2 — Start gateway + managed public tunnel (foreground, auto)

If `needs_public_tunnel=true`:

```bash
rdev support-session connect --start
```

Do not add `--public-tunnel`; that option no longer exists. Do not start `cloudflared` in a separate terminal. Do not run this command with `&`, `nohup`, or any background terminal. The CLI owns tunnel selection, process lifetime, HTTP/2 fallback, localhost.run fallback, helper assets, and in-memory session state. Keep this foreground process alive for the whole session.

### Step 3 — Send ONE thing to the user

Read `handoff_text_file.path` (or `target_handoff_envelope.full_text` from the JSON output).
Forward it verbatim. For unknown targets it will look like:

> **Connect to the remote support session:**
> Windows PowerShell: `powershell -NoProfile -Command "irm 'https://.../bootstrap.ps1' -UseBasicParsing | iex"`
> macOS/Linux terminal: `curl -fsSL 'https://...' | sh`
> Browser fallback: `https://<tunnel>.trycloudflare.com/join/<TICKET>/...`

Nothing more. No explanation of tickets, no network configuration.

### Step 4 — Wait for connection (auto)

```
rdev support-session status --ticket-code <TICKET> --wait --gateway-url <TUNNEL_URL>
```

Or use MCP: `rdev.support_session.status` with `wait=true` and `gateway_url: "<TUNNEL_URL>"`.

When `connected=true`, immediately tell the user: "Connected to `<hostname>`."
If the status is `stale`, do **not** create session tasks. Report that the target runner
was seen previously but is no longer task-ready, then use the generated recovery
instructions instead of manually building new bootstrap scripts.

Before any command that needs `--host-id`, prefer:

```
rdev support-session report --gateway-url <ACTIVE_GATEWAY_URL> --ticket-code <TICKET>
```

Use `recommended_target_endpoint_id` from that report. If the report says there are no
active hosts or multiple active hosts, follow its `next_action` instead of
guessing from stale/pending host IDs.

Also read `remote_control_entry` from the report/status. Use
`support_device_id` and `session_passcode` as the standard Agent-facing
connection handle; do not ask humans to copy raw ticket/root/gateway fields.

If multiple stale hosts appear for the same ticket, run:

```
rdev support-session recover --gateway-url <ACTIVE_GATEWAY_URL> --ticket-code <TICKET>
```

Do not ask the user to delete cached helper binaries, paste manifest roots, or
switch transports manually unless the recover command is unavailable.

### Step 5 — Run capability tests then report

After connection, run the built-in smoke test first (no user prompts):

```
rdev support-session smoke-test --gateway-url <ACTIVE_GATEWAY_URL> --session-id <SESSION_ID>
```

This command owns OS-specific probe tasks, PowerShell/cmd fallback semantics,
short timeouts, scoped write test, task waiting, artifact collection, and
remote-control entry plus connection-continuity reporting. Do not write ad-hoc
`curl` loops or custom Windows/Unix capability scripts unless this built-in command is missing. If
`smoke-test` is unavailable in an older install, use
`rdev support-session audit-capabilities` as the compatibility fallback.

When validating native remote-control surfaces, run the same smoke test with the
low-risk probe switch:

```
rdev support-session smoke-test --gateway-url <ACTIVE_GATEWAY_URL> --session-id <SESSION_ID> --remote-control
```

For MCP, call `rdev.support_session.smoke_test` with `"remote_control": true`
and the same explicit `gateway_url`. It adds only file-list and window-inspect
adapter probes, never screenshot, recording, input, clipboard, app launch, URL
open, or delete actions.

For subsequent scoped work:

- Use `rdev files ... --session-id ...` or MCP `rdev.sessions.task` with the
  `file` adapter for file list/read/write/download/upload/delete.
- Use `rdev desktop ... --session-id ...` or MCP `rdev.sessions.task` with the
  `desktop` adapter for screenshots, PNG frame
  recording, window inspection/focus/move, keyboard/mouse input, clipboard
  read/write, app launch or close, and URL open.
- Use `rdev.sessions.task`, `rdev.sessions.events`, and
  `rdev.sessions.artifacts` for lower-level adapter work.

If you need a safe policy JSON, run
`rdev task policy-template --capability <capability> --target-os <os>` and pass
the returned `policy` object as the reviewed session task payload. Prefer capabilities such as
`file.transfer.read`, `file.transfer.write`, `window.inspect`,
`screen.screenshot`, `screen.record`, `input.keyboard`, `input.mouse`,
`app.launch`, `app.close`, and `url.open` instead of shell scripts.

For the final summary, prefer:

```bash
rdev support-session report --gateway-url <ACTIVE_GATEWAY_URL> --ticket-code <TICKET>
```

Then produce the Audit Report and keep the connection alive. Do not revoke or
disconnect after the report unless the operator explicitly asks for cleanup.

---

## Connection Auto-Recovery

If the target does not appear within 2 minutes:

1. Check `gateway_candidate_summary` — was a public tunnel URL sent?
2. If LAN URL was sent by mistake: restart with cloudflared URL, give user new command.
3. If tunnel dropped: restart cloudflared, get new URL, give user new command.
4. If stale endpoints or queued tasks accumulated: run `rdev support-session recover`.
5. **Do not write custom PowerShell/bash polling scripts.**
6. Use `rdev.support_session.status` or `connection_recovery` returned fields.

---

## Audit Report Template

After the session, produce a compact capability audit with: session mode,
gateway continuity, Support Device Entry, host OS/arch, connection time,
capabilities tested, evidence/artifact IDs, what the Agent can and cannot do,
gaps, cleanup state, residual risk, and whether the connection remains alive.
Include file-transfer evidence when used, and screenshot/record/window/input
audit plus interrupt/event IDs when desktop control was used or attempted.

---

## Default Capabilities for Temporary Sessions

- `shell.user`
- `powershell.user`
- `fs.read`
- `fs.write.scoped`
- `file.transfer.read`
- `file.transfer.write`
- `process.inspect`
- `elevation.request`

Never add `service.manage`, `credential.change`, `gui.control`,
`screen.screenshot`, `screen.record`, `window.focus`, `window.move`,
`input.keyboard`, `input.mouse`, `app.launch`, `app.close`, `url.open`,
`clipboard.read`, `clipboard.write`, or `unattended.access` without explicit
authorization.

---

## Forbidden

- Sending LAN-only IPs to users who might be remote
- Asking the user to choose a network path
- Writing custom PowerShell/bash bootstrap or polling scripts
- Writing custom file transfer, screenshot, xdotool, cliclick, AppleScript,
  Win32 PowerShell GUI, SendKeys, mouse, or keyboard scripts instead of native
  session-task file/desktop surfaces
- Manual ticket/gateway/checksum assembly
- Hidden installation or persistence
- ExecutionPolicy Bypass
- UAC or sudo bypass without explicit authorization
- Bypassing lock-screen, screen-unlock, MDM, or enterprise security policy
- Storing secrets, tokens, private keys, or raw transcripts in memory
- Running `rdev support-session connect --start` in a background terminal (`&`, `nohup`, etc.)
- Calling gateway-backed `rdev.*` MCP tools without passing `"gateway_url": "<active-gateway-url>"`
- Assuming retired plural host/task CLI groups are valid; use `rdev host` / `rdev task` or MCP tools
- Manually deleting or replacing target helper binaries instead of using
  `support-session connect --start`, `prepare --build-assets`, or
  `support-session recover`

---

## Stage Gates — Pass ALL before advancing

Before sending the handoff to the user, verify **every** gate in order. Do not proceed until each check is green.

| Gate | Check | Fail action |
|------|-------|-------------|
| G1 — Port free | `findFreeAddr` resolves to an unbound address | Use auto-detected free port |
| G2 — Gateway healthy | `curl -fsS <gatewayURL>/healthz` returns HTTP 2xx | Wait up to 15 s, then restart |
| G3 — Tunnel URL valid | Public tunnel/provider URLs parse as HTTPS with a non-empty host | Re-request tunnel; try the next managed fallback |
| G4 — Assets ready | `asset_report.all_ready=true` for target platform | Run `connect --start --repo-root <checkout>` with same `--work-dir` |
| G5 — Handoff text present | `handoff_text_file` is non-empty and URL is reachable | Do not give user a dead link |

Only after all gates pass: send `target_handoff_envelope.full_text` to the user.

---

## Shell Scripting Safety Rules (if you must write a script)

These rules apply only when the built-in `rdev` CLI and MCP tools cannot accomplish the task directly.

**Variable naming**

- Never name a variable `status` in a shell script — it is a **read-only built-in** in zsh and will cause `read-only variable: status` at runtime.
- Use `task_state`, `host_state`, `rdev_state`, or any other non-conflicting name.

```bash
# WRONG — crashes in zsh
status=$(curl ...)

# CORRECT
task_state=$(curl ...)
```

**Task terminal states**

The gateway model uses `"succeeded"` (not `"completed"`) as the success terminal state. Always include all five terminal values in your polling exit condition:

```bash
case "$task_state" in
  succeeded|completed|failed|canceled) break ;;
esac
```

**CLI subcommand verification**

Before invoking any `rdev` subcommand, verify it exists by checking `rdev --help` output. The following subcommands **do not exist** and will return errors:

- `rdev hosts capabilities` → use `rdev.sessions.status` MCP tool or
  `rdev support-session audit-capabilities`

Prefer first-class CLI commands for gateway interactions when they exist:
`rdev support-session smoke-test`, `rdev support-session audit-capabilities`,
`rdev task policy-template`, `rdev files ...`, `rdev desktop ...`, and
`rdev support-session report`. Use `rdev.sessions.*` MCP tools for session
status, task submission, event replay, interrupts, and artifact metadata. Use
raw HTTP only as a last-resort diagnostic path.

**MCP gateway alignment**

The installed MCP server (`rdev-mcp`) connects to a statically configured gateway (typically `http://127.0.0.1:8787`). When a support session runs on a different port or uses a Cloudflare URL, the MCP tools will query the wrong gateway and return empty results.

Fix: pass `"gateway_url": "<active-gateway-url>"` explicitly on every gateway-backed MCP call:

```
rdev.sessions.status(gateway_url="<ACTIVE_GATEWAY_URL>", session_id="ses_...")
rdev.sessions.task(gateway_url="<ACTIVE_GATEWAY_URL>", session_id="ses_...", ...)
```

Or set `RDEV_CLOUDFLARED_GATEWAY_URL=<url>` before starting the MCP server.
