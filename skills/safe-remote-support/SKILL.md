---
name: safe-remote-support
description: Use when an agent needs to connect to, operate, or audit a remote machine through Remote Dev Skillkit — covering Windows, macOS, and Linux targets, temporary and managed sessions, public-tunnel first connectivity, scoped jobs, evidence, and cleanup.
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
   - Let `rdev` manage the public tunnel internally. It prefers configured stable gateways, then starts Cloudflare Quick Tunnel with HTTP/2 first, then falls back to localhost.run SSH tunnel when needed.
   - Read the generated `target_handoff_envelope.full_text` or `handoff_text_file.path`; do not manually start tunnels or assemble `--gateway-url`.
3. LAN/private-IP candidates are acceptable as **secondary** fallbacks only after the managed public-tunnel path fails or a stable configured gateway is already present.

**Never send a raw `http://192.168.x.x:port` or `http://[fe80::...]:port` address to a remote user.**

### Rule 2 — One command to the user, nothing else

The target-side human receives **one** thing: `target_handoff_envelope.full_text`.
Do not ask them to:
- Install rdev manually
- Copy ticket codes
- Pick a gateway URL
- Choose a network path
- Enter checksums

Everything is already embedded in the handoff. If it is not, fix the session setup rather than asking the user.

### Adaptive Configuration Contract

Before acting, probe the available `rdev` CLI/MCP surface, gateway state, network reachability, tunnel/mesh helpers, workspace path, target OS/shell, permissions, and connection modes. If any required value is unclear after read-only probes, ask one short question instead of inventing ticket codes, gateway URLs, root keys, checksums, workspace paths, adapter choices, tunnel commands, or approval policy.

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

### Rule 5 — Gateway process MUST run in foreground

`rdev support-session connect --start` is a **foreground process**. It manages the public tunnel, serves helper assets, and holds session state in memory. If it exits, the tunnel drops and the target machine loses connectivity.

Never run it with `&`, `nohup`, or in a background terminal. Keep it alive for the whole session.

If the gateway process unexpectedly exits:
1. Re-run `rdev support-session connect --start` with the same `--work-dir`.
2. Wait for the previous host heartbeat to go stale (up to 90 seconds).
3. Let the target machine re-register and receive automatic approval.
4. Resume from the generated `target_handoff_envelope.full_text` / `handoff_text_file.path`.

### Rule 6 — MCP tools must target the active gateway

The MCP server (`rdev mcp serve`) can maintain its own separate in-memory gateway. It may not see hosts or jobs created by `rdev support-session connect --start`.

Pass `"gateway_url": "<active-gateway-url>"` explicitly on **every** `rdev.hosts.*`, `rdev.jobs.*`, `rdev.artifacts.*`, and `rdev.audit.query` call:

```
rdev.jobs.create(gateway_url="http://127.0.0.1:8787", host_id="hst_...", ...)
rdev.hosts.capabilities(gateway_url="https://<tunnel>.trycloudflare.com", host_id="hst_...")
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
1. Verify the local gateway is running: `curl -fsS http://127.0.0.1:8787/healthz`
2. If it returns an error, start the gateway first with `rdev support-session connect --start`.
3. Never call `rdev.support_session.create` or `rdev.hosts.*` when no gateway URL is available — the call will fail with a missing-parameter error.

### Rule 10 — Send `target_handoff_envelope.full_text`, not a bare URL

When the session is ready, deliver exactly `target_handoff_envelope.full_text` (or `handoff_text_file.path` content) to the target-side human. This already contains the PowerShell command in `irm 'URL' | iex` form. Do NOT:
- Send a bare `https://...trycloudflare.com/join/XXXX` link alone (the user may not know to run it)
- Extract the URL and reconstruct a custom command
- Use `powershell -EncodedCommand` — Base64 blobs are truncated by chat interfaces

### Rule 11 — Status polling has a 3-minute deadline; then diagnose

After sending the handoff:
1. Poll with `rdev.support_session.status wait=true` or watch `status_file.path`.
2. **After 3 minutes without `connected=true`**, stop polling and switch to diagnosis mode:
   - Was a public-tunnel URL sent, or a LAN IP?
   - Is the gateway still running? (`curl http://127.0.0.1:8787/healthz`)
   - Did the target machine go to sleep or lock?
   - Is the Cloudflare tunnel still alive? (check process)
3. Present the user with specific, actionable next steps — not another "still waiting" message.

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
Forward it verbatim. It will look like:

> **Connect to the remote support session:**
> Open this link on the target machine: `https://<tunnel>.trycloudflare.com/join/<TICKET>/...`
> Or run in PowerShell: `irm 'https://...' | iex`

Nothing more. No explanation of tickets, no network configuration.

### Step 4 — Wait for connection (auto)

```
rdev support-session status --ticket-code <TICKET> --wait --gateway-url <TUNNEL_URL>
```

Or use MCP: `rdev.support_session.status` with `wait=true` and `gateway_url: "<TUNNEL_URL>"`.

When `connected=true`, immediately tell the user: "Connected to `<hostname>`."
If the status is `stale`, do **not** create jobs. Report that the target runner
was seen previously but is no longer job-ready, then use the generated recovery
instructions instead of manually building new bootstrap scripts.

### Step 5 — Run capability tests then report

After connection, run the built-in audit first (no user prompts):

```
rdev support-session audit-capabilities --gateway-url <ACTIVE_GATEWAY_URL> --host-id <HOST_ID>
```

This command owns the OS-specific probe commands, short timeouts, scoped write
test, job waiting, and artifact collection. Do not write ad-hoc `curl` loops or
custom Windows/Unix capability scripts unless this built-in command is missing.

For subsequent scoped work, use `rdev job create`, `rdev job wait`, `rdev job
get`, `rdev job list`, and `rdev job cancel` before considering MCP or raw HTTP.
Then revoke the session and produce the Audit Report.

---

## Connection Auto-Recovery

If the target does not appear within 2 minutes:

1. Check `gateway_candidate_summary` — was a public tunnel URL sent?
2. If LAN URL was sent by mistake: restart with cloudflared URL, give user new command.
3. If tunnel dropped: restart cloudflared, get new URL, give user new command.
4. **Do not write custom PowerShell/bash polling scripts.**
5. Use `rdev.support_session.status` or `connection_recovery` returned fields.

---

## Audit Report Template

After the session, produce a report with these sections:

```
# Remote Dev Skillkit Capability Audit — <date> — <hostname>

## Session
- Mode: attended-temporary
- Gateway: <cloudflare URL or LAN if same network>
- Ticket: <code>
- Host: <hostname> / <OS> / <arch>
- Connection time: <seconds>

## Capabilities Tested

| Capability         | Status  | Evidence |
|--------------------|---------|----------|
| shell.user         | ✅ PASS | <output> |
| fs.read            | ✅ PASS | dir listing |
| fs.write.scoped    | ✅ PASS | test file created+deleted |
| process.inspect    | ✅ PASS | process list retrieved |
| elevation.request  | ⚠️ N/A  | not tested |

## What the Agent Can Do
<list verified actions>

## What the Agent Cannot Do (policy/capability limits)
<list denied or unavailable actions>

## Gaps / Missing
<list gaps found>

## Cleanup
- Ticket revoked: yes/no
- No-persistence checks: pass/fail
- Files cleaned: yes/no

## Risk Assessment
- Residual risk: low/medium/high
- Recommendation: <next steps>
```

---

## Default Capabilities for Temporary Sessions

- `shell.user`
- `fs.read`
- `fs.write.scoped`
- `process.inspect`
- `elevation.request`

Never add `service.manage`, `credential.change`, or `gui.control` without explicit approval.

---

## Forbidden

- Sending LAN-only IPs to users who might be remote
- Asking the user to choose a network path
- Writing custom PowerShell/bash bootstrap or polling scripts
- Manual ticket/gateway/checksum assembly
- Hidden installation or persistence
- ExecutionPolicy Bypass
- UAC or sudo bypass without approval gate
- Storing secrets, tokens, private keys, or raw transcripts in memory
- Running `rdev support-session connect --start` in a background terminal (`&`, `nohup`, etc.)
- Calling any `rdev.*` MCP tool without passing `"gateway_url": "<active-gateway-url>"`
- Assuming plural CLI commands such as `rdev hosts` or `rdev jobs` are valid; use `rdev host` / `rdev job` or MCP tools

---

## Stage Gates — Pass ALL before advancing

Before sending the handoff to the user, verify **every** gate in order. Do not proceed until each check is green.

| Gate | Check | Fail action |
|------|-------|-------------|
| G1 — Port free | `findFreeAddr` resolves to an unbound address | Use auto-detected free port |
| G2 — Gateway healthy | `curl -fsS <gatewayURL>/healthz` returns HTTP 2xx/404 | Wait up to 15 s, then restart |
| G3 — Tunnel URL valid | The URL contains `.trycloudflare.com` and parses as HTTPS | Re-request tunnel; try localhost.run fallback |
| G4 — Assets ready | `asset_report.all_ready=true` for target platform | Run `connect --start --repo-root <checkout>` with same `--work-dir` |
| G5 — Handoff text present | `handoff_text_file` is non-empty and URL is reachable | Do not give user a dead link |

Only after all gates pass: send `target_handoff_envelope.full_text` to the user.

---

## Shell Scripting Safety Rules (if you must write a script)

These rules apply only when the built-in `rdev` CLI and MCP tools cannot accomplish the task directly.

**Variable naming**

- Never name a variable `status` in a shell script — it is a **read-only built-in** in zsh and will cause `read-only variable: status` at runtime.
- Use `job_state`, `host_state`, `rdev_state`, or any other non-conflicting name.

```bash
# WRONG — crashes in zsh
status=$(curl ...)

# CORRECT
job_state=$(curl ...)
```

**Job terminal states**

The gateway model uses `"succeeded"` (not `"completed"`) as the success terminal state. Always include all five terminal values in your polling exit condition:

```bash
case "$job_state" in
  succeeded|completed|failed|canceled) break ;;
esac
```

**CLI subcommand verification**

Before invoking any `rdev` subcommand, verify it exists by checking `rdev --help` output. The following subcommands **do not exist** and will return errors:

- `rdev hosts capabilities` → use `rdev.hosts.capabilities` MCP tool or `GET /v1/hosts`

Prefer first-class CLI commands for gateway interactions when they exist:
`rdev support-session audit-capabilities`, `rdev job create`, `rdev job list`,
`rdev job get`, `rdev job wait`, and `rdev job cancel`. Use MCP tools when the
current task needs MCP-only surfaces such as host capability inspection, and use
raw HTTP only as a last-resort diagnostic path.

**MCP gateway alignment**

The installed MCP server (`rdev-mcp`) connects to a statically configured gateway (typically `http://127.0.0.1:8787`). When a support session runs on a different port or uses a Cloudflare URL, the MCP tools will query the wrong gateway and return empty results.

Fix: pass `"gateway_url": "<active-gateway-url>"` explicitly on every MCP call:

```
rdev.jobs.create(gateway_url="http://127.0.0.1:9901", ...)
rdev.hosts.capabilities(gateway_url="http://127.0.0.1:9901", ...)
```

Or set `RDEV_CLOUDFLARED_GATEWAY_URL=<url>` before starting the MCP server.
