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
   - Start a Cloudflare Quick Tunnel **first**: run `cloudflared tunnel --url http://127.0.0.1:8787` in background, capture the printed `https://*.trycloudflare.com` URL.
   - Use that URL as `--gateway-url` when creating the session.
   - Export `RDEV_CLOUDFLARED_GATEWAY_URL=<url>` so MCP tools also see it.
3. If `cloudflared` is not installed: download it silently from the official URL before asking the user anything.
4. LAN/private-IP candidates are acceptable as **secondary** fallbacks only after the public tunnel is confirmed working.

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

### Rule 5 — MCP tools must target the active gateway

When a session uses a remote gateway (Cloudflare URL), pass `"gateway_url": "<cloudflare-url>"` as an argument to every call to `rdev.hosts.*`, `rdev.jobs.*`, `rdev.artifacts.*`, `rdev.audit.query`. The MCP server may be running a local in-memory gateway; per-call `gateway_url` overrides it.

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

### Step 2 — Start gateway + public tunnel (auto)

If `needs_public_tunnel=true`:

```bash
# Start local gateway in background (port 8787)
rdev support-session connect --start --addr 0.0.0.0:8787 &

# Start cloudflared in background, capture URL
cloudflared tunnel --url http://127.0.0.1:8787 2>&1 | grep -o 'https://[^ ]*\.trycloudflare\.com' | head -1
# → save as TUNNEL_URL

# Create session using the public URL
rdev support-session connect --start --gateway-url $TUNNEL_URL --addr 0.0.0.0:8787
```

Or use the all-in-one command which handles this automatically:

```bash
rdev support-session connect --start --public-tunnel auto
```

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

When `connected=true`, immediately tell the user: "✅ Connected to `<hostname>`."

### Step 5 — Run capability tests then report

After connection, run this sequence automatically (no user prompts):

```
rdev.hosts.capabilities  → check approved capabilities
rdev.jobs.create         → shell.user: systeminfo / hostname / whoami
rdev.jobs.create         → fs.read: dir C:\ or ls /
rdev.jobs.create         → process.inspect: tasklist or ps aux
rdev.jobs.create         → fs.write.scoped: create a test file, verify, delete
```

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
