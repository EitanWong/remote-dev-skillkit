# Remote Dev Skillkit

Remote Dev Skillkit is a safety layer for AI agents that need to work on real
Mac, Windows, and Linux machines.

It gives agents a portable Skillkit, MCP tools, signed jobs, approval gates,
host-local policy, evidence bundles, and release verification so they can fix
real development environments without being handed the keys to the whole
building.

The product is AI-native: the human talks to an agent, and the agent starts
ordinary "connect this computer" work with `rdev.support_session.connect`.
That high-level entry returns the one target-side handoff to send, the standard
Support Device Entry (`support_device_id` + session passcode), status watcher,
connection supervision, and recovery guidance. It behaves more like a
remote-control connector for agents than a pile of ticket flags: the target
opens a visible connector, the agent uses the standard entry, and the connector
stays online until the operator explicitly asks to disconnect. Lower-level
`rdev.invites.create`, `rdev.connection_entry.plan`, `rdev.hosts.*`, and
`rdev.jobs.*` tools remain available for reviewed packaging, managed hosts,
scoped work, approvals, and evidence after the connection path is established.

Multilingual quick starts: [English](README.md), [简体中文](docs/i18n/README.zh-CN.md), [Español](docs/i18n/README.es.md), [Français](docs/i18n/README.fr.md), [Deutsch](docs/i18n/README.de.md), [日本語](docs/i18n/README.ja.md), [한국어](docs/i18n/README.ko.md), [Português](docs/i18n/README.pt-BR.md), [हिन्दी](docs/i18n/README.hi.md), [العربية](docs/i18n/README.ar.md), [Русский](docs/i18n/README.ru.md).

## What It Does

Modern agents are great at coding. Real machines are messy. Remote Dev Skillkit
connects those two worlds with a workflow that is visible, auditable, revocable,
and boring in the best possible way.

```text
Codex / Claude Code / Hermes / OpenClaw/OpenCode / MCP agents
        |
        v
Agent Skills + MCP tool contracts
        |
        v
rdev gateway: tickets, jobs, approvals, artifacts, audit
        |
        v
rdev host: identity, local policy, adapters, evidence
        |
        v
shell, PowerShell, Git, Codex, Claude Code, ACP/acpx, custom adapters
```

Use it when an agent needs to diagnose, repair, test, or review work on a
machine while the operator keeps control of what can run, what needs approval,
and what proof comes back.

## Why Developers Like It

- **Portable agent workflows.** Export one Skillkit for Codex, Claude Code,
  Hermes, OpenClaw/OpenCode, and generic MCP agents.
- **No raw remote shell free-for-all.** Jobs are signed, policy-checked,
  capability-scoped, host-bound, and revocable.
- **Humans stay in charge.** Risky actions can require explicit approval before
  the host runs them.
- **Proof over vibes.** Jobs produce structured artifacts, audit chains, adapter
  results, cancellation evidence, and release verification output.
- **Adaptive by default.** Skills probe the installed OS, shell, service manager,
  framework paths, gateway config, workspace, and permissions. When the safe
  answer is not discoverable, they ask instead of guessing.
- **Open-source ready hygiene.** Public docs use placeholders, no private server
  addresses, no personal paths, no hidden deployment assumptions.
- **Supply-chain care included.** Release candidates can carry checksums, SBOMs,
  provenance attestations, signed bundles, and install-plan verification.

## Install Fast

### Let Your Agent Install It

Already inside Codex, Claude Code, Hermes, OpenClaw/OpenCode, or another
MCP-capable agent? Send it this link and ask it to read the prompt before
acting:

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

That prompt is the source of truth for cloning or updating the repo, probing the
current runtime, installing the Skillkit, configuring MCP, and asking one short
question only when a required value cannot be discovered safely.

From a checkout:

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

If `rdev` is not on `PATH` after `go install`, use the absolute command shown
by `rdev bootstrap agent-plan` or the Skillkit install report's `mcp_command`
field, for example `~/go/bin/rdev mcp serve`.

Export and verify a portable Skillkit bundle. A hosted gateway URL is optional;
omit `--gateway-url` for local Agent installs that only need local MCP stdio:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

If you already have a hosted gateway, include it as bundle metadata:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
```

Generate a reviewable install plan for mainstream agent frameworks:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Direct install is dry-run by default. Review the report first, then add
`--execute` when the target directory is correct:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

That second line is the "one-command install" path once a verified bundle exists.
For other frameworks, swap `codex` and the target directory for the plan that
`rdev skillkit plan-install` generated.

For the full installer contract, framework notes, and safe deployment rules, see
[Skillkit Install](docs/operations/SKILLKIT_INSTALL.md).

Check for newer GitHub releases without changing local files:

```bash
rdev update check --repo EitanWong/remote-dev-skillkit
rdev update plan --repo EitanWong/remote-dev-skillkit
```

The update plan is review-first: it selects the matching platform archive,
emits download and checksum commands, and reminds the agent to verify
`release-bundle.json` with the configured release root before replacing any
binary or restarting a managed service.

## Agent-First Remote Session

When a machine needs help, talk to the Agent in plain language:

```text
I have a Windows/Mac/Linux machine that needs repair. Use Remote Dev Skillkit,
create a Connection Entry, choose the right connection mode, and give the target
side only the link, visible script, or signed package it should run.
```

The Agent should start with `rdev.support_session.connect` or
`rdev support-session connect`. That first-contact contract returns
`rdev.support-session-connect.v1` and collapses the ordinary decision tree: when
a reachable gateway or configured `RDEV_*_GATEWAY_URL` exists, it creates the
session and returns a ready `target_handoff_envelope.full_text`; when no gateway is running, it
returns `cli_start_now_command`, the standard visible foreground
`rdev support-session connect --start` command, plus a `support-session start`
compatibility fallback.
`rdev.support_session.handoff` remains available for review/debug routing
details, but fresh Agents should not use it as the normal first step. The
created/started session then returns
`rdev.support-session-created.v1`: the ready `target_handoff_envelope.full_text`,
Windows/macOS/Linux target command,
join URL, manifest root, real ticket code, and status watcher with no
placeholders. It also includes `target_bootstrap_requirements` and, for CLI
create calls against an existing gateway, `target_bootstrap_readiness`. Those
fields prevent the common failure where a manually started gateway has no
downloadable `rdev` helper for a clean Windows target. If readiness is false,
use the standard `support-session connect --start` or `prepare --build-assets` path
instead of inventing an install script or telling the target user to assemble
prerequisites. The command itself loops through ordered Connection Entry URLs on
the target machine with bounded timeouts/retries, so fallback stays in `rdev`
instead of Agent-written glue. After registration, `rdev host serve --transport
auto` keeps the signed join-manifest gateway candidates and can switch the job
runtime to another reachable candidate if the current gateway fails before any
work is processed. The same payload includes
`target_handoff_envelope.full_text`, which is the exact human-facing text and
command/link the Agent should send. `user_handoff` remains for compatibility.
When the target OS is unknown, `target_handoff_envelope.auto_target_rule` and
`user_handoff.auto_target_rule` tell the Agent to send the multi-platform
`full_text` verbatim, with Windows PowerShell, macOS/Linux terminal, and browser
fallback sections already included. The join URL is a compatibility/browser
fallback field, not a separate first message for the Agent to extract.
When the handoff/readiness output shows `rdev`, gateway state, or target helper
assets are unclear, the Agent
should call `rdev.support_session.prepare` or run
`rdev support-session prepare --build-assets` from a checkout. That returns
`rdev.support-session-prepare.v1` with local recovery, helper asset hashes,
one-command target readiness, `gateway_url_candidates`,
`gateway_candidate_preflight`, `connectivity_helper_preflight`,
`agent_connection_runbook`, and standard recovery actions. The Agent should
use that preflight table to decide whether the standard foreground
`connect --start` path or a configured hosted/relay/mesh/VPN/SSH gateway is
ready, then send only `target_handoff_envelope.full_text`. It should not turn
LAN, loopback, or diagnostic candidates into a hand-written target command. If
no gateway is running yet, the Agent should run `rdev support-session connect --start` in a
visible foreground terminal; that command auto-prepares verified helper assets when
possible, starts the local gateway, and prints
`rdev.support-session-started.v1` with top-level `target_handoff_envelope`,
`user_handoff`,
`target_command`, `join_url`, `connection_supervision`, and status watcher
fields plus the same `gateway_candidate_preflight` decision table and
`agent_connection_runbook`. It also includes `foreground_feedback`, a stderr event contract for the
same foreground command; when the Agent sees `event=connected`, it should
immediately report that the connection has been established. It keeps the full
created session under `session` for compatibility, but fresh Agents should send
only the top-level `target_handoff_envelope.full_text`, then
use top-level `connection_supervision` or foreground feedback to wait, report
`connected=true`, and choose standard upgrade/recovery tools. It also
writes the same payload to `ready_file.path` as
`support-session-ready.json` under the session work directory by default, so
Agents can read the file when a long-running foreground terminal makes stdout
hard to parse. It also writes `handoff_text_file.path` as
`target-handoff.txt` by default, which is the easiest fresh-Agent path: read
that plain-text file and send it to the target-side human verbatim, without
parsing JSON or rebuilding commands. It also writes the latest foreground status event to
`status_file.path` as `support-session-status.json`, so Agents can detect
`event=connected` without writing their own polling loop. When the target
connects, it writes `connected_report_file.path` as `connected-report.txt` by
default, so the Agent can proactively report the plain-text connection success
message before creating jobs. The Agent
should use `rdev.support_session.plan` or `rdev support-session plan` only for
review/debug planning. The Agent should not write its own PowerShell, relay,
nohup, ticket, root, gateway, transport, status polling, or approval glue.
Operators may preconfigure hosted/relay/mesh/VPN/SSH gateway URLs with
`RDEV_*_GATEWAY_URL`; support-session prepare/start/create will include those
URLs in the ordered candidate list, while keeping ticket/root/transport details
inside the structured payload. For repeated sessions, do not persist
`https://*.trycloudflare.com` Quick Tunnel URLs as stable configuration; they
are current-session fallback URLs. If the Agent runs on a VPS/cloud server with
its own public domain or IP, configure
`RDEV_HOSTED_GATEWAY_URL=https://your-domain-or-public-gateway`. If you use
Cloudflare and want a reusable address, configure
`RDEV_CLOUDFLARED_NAMED_TUNNEL_URL=https://rdev.example.com` plus a reviewed
named-tunnel token, token file, tunnel name, or start argv so
`connect --start` can reuse that address before falling back to Quick Tunnel.
They may also preconfigure reviewed helper
metadata with `RDEV_SSH_TUNNEL_START_ARGV_JSON`, `RDEV_RELAY_START_ARGV_JSON`,
`RDEV_MESH_START_ARGV_JSON`, `RDEV_VPN_START_ARGV_JSON`, or matching
`RDEV_*_INSTALL_ACTION_JSON`; support-session payloads report that state in
`connectivity_helper_preflight` so Agents use the Connection Entry runner path
instead of writing tunnel commands. `rdev support-session create` can now omit
`--gateway-url` when one of those configured URLs exists; if neither an
explicit nor configured reachable gateway exists, use
`rdev support-session connect --start`.
If an operator intentionally starts a low-level dev gateway, prefer
the default `--auto-build-rdev-assets` behavior from a source checkout with Go.
It prepares the verified Windows/macOS/Linux helpers served from `/assets`, so
clean targets do not fail with "rdev is required" even if an Agent accidentally
uses `gateway serve` plus `invite create`. Use `--rdev-assets-dir <dir>` or the
platform-specific helper flags only for reviewed explicit asset locations.

For ordinary "connect this computer" work, use the high-level support-session
entry. It keeps the Agent on the standard path and gives the human one complete
handoff instead of ticket/root/gateway pieces:

```bash
rdev support-session connect \
  --target auto \
  --locale auto

rdev support-session connect \
  --start \
  --target auto \
  --locale auto
```

Use status/report after the handoff is sent:

```bash
rdev support-session status \
  --gateway-url http://<active-gateway-host>:8787 \
  --ticket-code <ticket-code> \
  --wait \
  --locale auto

# If RDEV_HOSTED_GATEWAY_URL / RDEV_CLOUDFLARED_NAMED_TUNNEL_URL / similar is configured:
rdev support-session status \
  --ticket-code <ticket-code> \
  --wait \
  --locale auto
```

Use prepare/create/plan only for review, debugging, or explicit lower-level
workflows:

```bash
rdev support-session prepare \
  --target auto \
  --build-assets

rdev support-session start \
  --addr 0.0.0.0:8787 \
  --target auto \
  --locale auto

rdev support-session create \
  --gateway-url http://<reachable-gateway-host>:8787 \
  --target auto \
  --locale auto

rdev support-session plan \
  --gateway-url http://<reachable-gateway-host>:8787 \
  --target auto \
  --locale auto
```

For explicit package materialization, the Agent creates an invite and
materializes it before asking anyone on the target side to do anything. The
public name for this universal handoff is **Connection Entry**. It covers your
own long-running workstation, someone else's temporary repair machine, LAN,
hosted, relay, mesh, SSH, and VPN-assisted paths. Humans should not
hand-assemble ticket codes, root keys, gateway URLs, transports, release roots,
or checksums.

If there is no hosted or configured gateway yet, the Agent should run
`rdev support-session connect --start` in a visible foreground terminal. It
should only escalate to hosted, SSH, relay, mesh, VPN, firewall, DNS, cloud,
paid, privileged, or persistent changes when probes show they are needed and
the operator approves.

The JSON output includes `schema_version: rdev.agent-invite.v1`, a short-lived
ticket, the manifest URL, the pinned manifest root, a machine-readable
`host_command`, a transport fallback plan, `connection_entry`,
`connection_entry.package_catalog`,
`connection_entry_plan.target_selection_policy`, and the next MCP tools the
agent should call. The Agent then materializes the invite with
`rdev.connection_entry.plan` or
`rdev connection-entry plan`. That now produces a **self-contained Connection
Entry runner package** (`rdev.connection-entry.runner.v1`) plus the generic
`rdev.connection-entry.package-plan.v1`: a runner manifest, visible launcher,
direct/proxy/LAN/relay/mesh/VPN/SSH path selection, human-facing entry
surfaces, Agent-only connection metadata, owned-vs-third-party mode selection,
missing release inputs, and platform package planning. The package catalog is also
embedded in the signed join manifest as `package_catalog`, so an Agent can
verify the manifest, select the Windows/macOS/Linux candidate from target OS and
architecture probes, and fall back to the visible script when release package
assets are not published yet. The target side opens the generated
`connection_entry.entry_url` or runs the signed connection entry package on the
target computer, then approves the host when policy requires it.
The agent does the waiting, probing, mode selection, package planning, job
creation, status tracking, evidence review, and revocation.

When status returns `connected=true`, the Agent should proactively tell the
user: "Connection established. The target host is online and ready for scoped
work." In Chinese sessions, the standardized feedback is: "连接已经建立，目标主机已在线并可用于受控任务。"

`connection_entry` is the universal target-side entry point. The join page
serves inspectable `/bootstrap.sh` and `/bootstrap.ps1` helpers that start a
visible host session with the pinned `--manifest-root-public-key` and
`--transport auto`. A materialized runner package can also be launched with
`rdev connection-entry run --runner-manifest connection-entry-runner.json`; it
probes gateway reachability, standard proxy settings, LAN/private routes, and
already configured open-source/free helper paths such as SSH, frp, Chisel,
headscale/Tailscale-compatible mesh, and WireGuard. It uses direct WSS/HTTPS
fallback first, then configured helper gateway overrides such as
`RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, or
`RDEV_SSH_GATEWAY_URL` when they are unambiguous. If an approved user-scoped
install action is present, the runner can use `rdev deps install` to download,
SHA-256 verify, unpack, and use helper binaries such as `chisel`, `frpc`,
`tailscale`, or `wg` from a user/workspace tools directory without changing
PATH or installing services.
Creating credentials, changing firewall/DNS/routes, starting persistent
connectivity, installing mesh/VPN drivers or services, or using paid/cloud
relays still requires explicit approval. Real signed
per-platform release archives now include visible launchers that verify the
packaged signed release bundle with `rdev-verify` before running packaged
`rdev`, but public GitHub Release downloads still need real post-release
evidence. The runner contract is real code rather than a script-only fallback
plan. It does not create hidden persistence, weaken OS policy, or open inbound
firewall ports.

`connection_entry_plan.target_selection_policy` tells the Agent how to choose
the right shape. If the target is your own personal or fleet machine, the Agent
should plan a durable managed connection with an explicit reviewed service
lifecycle, release gates, renewal/revocation refresh, reconnect proof, and
uninstall instructions. If the target belongs to someone else or is a one-off
repair, the Agent should use a temporary attended connection with no persistence
by default. If ownership or persistence approval is unclear, the Agent asks one
short question before creating a managed entry.

In both modes the preferred package is self-contained: platform `rdev` binary,
signed release bundle, manifest URL, manifest root, release root, visible
launcher, stop/revoke text, and network fallback logic so the target machine
does not need Go, Git, Node, Python, or manual flag assembly before it can
connect. For owned managed machines, the package can also include a reviewed
macOS LaunchAgent, Linux systemd user-service, or Windows Service plan; the
materializer writes those plans for review and does not silently install or
start a service. If a repair requires administrator/root privileges, the entry
should request them through normal OS authorization prompts and record the
approval evidence; it must not hide elevation or permanently weaken security
policy.

Invite output also includes `host_context_plan`. Remote Dev Skillkit is
host-context-first: environment probes, project file trees, requirement notes,
logs, transcripts, and large evidence stay on the remote host by default. The
Agent server should keep only indexes, summaries, checksums, and artifact IDs,
then load exact slices on demand. Tiny context, sharp tools, fewer headaches.

For setup drift, invite output includes `agent_provisioning_plan`. Agents should
probe the target host for installed skills, MCP tools, adapters, runtimes,
package managers, lockfiles, framework paths, proxy settings, and permissions.
When something is missing, they may install verified user-scoped or
workspace-scoped skills/tools/dependencies automatically when policy allows.
Elevation, services, credentials, firewall changes, external accounts, paid
resources, publish/deploy/push actions, and security-policy mutation require an
approval gate and evidence.

If the target host already has other AI tools or Agents, invite output includes
`agent_collaboration_plan`. rdev can discover local or configured Agent peers,
including A2A-compatible agents through Agent Cards, and use them for bounded
subtasks, diagnostics, summaries, or project-local expertise. Peer agents are
collaborators, not authorization roots: delegation still runs through signed
rdev jobs, host policy, workspace locks, redaction, approval gates, audit, and
evidence.

Everything user-facing is language-aware. Invite output includes
`localization_plan`, and join pages can match `?lang=` or the target browser's
`Accept-Language`. Agents should detect the target-side language from
host locale, OS UI language, browser hints, and explicit preferences, then
localize target-side instructions, skills, MCP summaries, approval prompts, job
status, and evidence summaries. Protocol keys, schema versions, commands,
paths, JSON fields, checksums, and code blocks stay unchanged.

For your own computer or workstation fleet, invite output includes
`managed_development_plan`. That path is built for long-running development:
managed host mode, `--once=false`, service-backed restart plans,
`--transport auto`, release-bundle startup gates, enrollment renewal,
revocation refresh, workspace locks, Git worktrees, host-local context caches,
evidence bundles, and reconnect proof. Temporary third-party sessions stay light;
owned developer machines get the durable treatment.

`--transport auto` is the field-friendly default: the host tries outbound WSS
first, falls back to HTTPS long-poll when WebSocket upgrades are blocked, and
keeps short polling as the maximum-compatibility path for stubborn networks.
No inbound port on the target machine is required.

MCP-capable agents should start ordinary "connect this computer" work with
`rdev.support_session.connect` through `rdev mcp serve`. Lower-level
`rdev.invites.create` remains available for reviewed package materialization,
approved managed owned-host planning, or explicit recovery paths. No private
server addresses or local paths are baked into the project.

## Connection Paths

Native rdev host connections are outbound-first:

| Path | Status | Best Fit |
|---|---|---|
| WSS over TLS/mTLS | implemented | Low-latency public or LAN gateway |
| HTTPS long-poll | implemented | Proxies and firewalls that allow HTTPS but block WebSocket upgrades |
| HTTPS short-poll | implemented | Maximum compatibility when long-held requests are unstable |
| LAN/private gateway URL | implemented when routable and signed-manifest verified | Agent server and target host share a LAN/VPN segment, including different private subnets |

Invite output also includes agent-managed options for HTTPS relay, mesh/VPN,
and SSH tunnel scenarios. Agents may inspect local network interfaces, route
tables, proxy settings, SSH config, and installed mesh tooling; they may also
probe scoped LAN/private-network reachability and use configured relay/mesh/SSH
paths automatically. Those are connectivity assists, not authorization
shortcuts: rdev still requires target consent, host approval, signed jobs,
local policy checks, and evidence.

For maximum autonomy, invites default to `authority_profile: max-control`. That
lets the connected remote host act as the Agent's field workstation: it can run
heuristic discovery from that vantage point, inventory reachable devices, use
configured SSH/mesh/relay/API paths, and control downstream authorized hosts or
devices when the job policy grants `downstream.control.scoped`. Every downstream
action is still tied to the task intent and captured as evidence.

## Long-Running Development

For stable ongoing work on your own machine, use managed mode instead of
attended temporary mode. Generate a reviewed service plan for the target OS,
start it explicitly, then let the Agent use MCP to select that host, lock a
workspace, run Codex/Claude/acpx/shell/PowerShell jobs, review artifacts, and
keep evidence.

```bash
rdev host install-service \
  --platform macos \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --workspace-lock-store ~/.rdev/host/workspace-locks \
  --plist-out ~/Library/LaunchAgents/com.remote-dev-skillkit.host.plist
```

macOS uses reviewed LaunchAgent plans, Linux uses reviewed systemd user-unit
plans, and Windows uses reviewed Service Control Manager command plans. Service
start/stop stays explicit and reviewable so long-running Agent work is reliable
without becoming mysterious.

## Try It Locally

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

The local demo walks through ticket creation, host registration, approval, job
execution, artifacts, and audit without needing a public gateway.
The fresh-Agent support-session acceptance command is a local contract gate: it
checks that connect/handoff/create/start/status payloads still support the
one-command Agent flow before you run real Codex/Claude Code/Hermes/OpenClaw
and clean Windows/macOS/Linux acceptance.

## Current Status

Remote Dev Skillkit is in Phase 1: a safe MVP foundation for professional
open-source release.

Already implemented: the `rdev` CLI, local dev gateway, MCP stdio server,
Skillkit export/verify/install planning, signed job envelopes, trust bundles,
host enrollment certificates, revocations, workspace locks, approval gates,
audit/evidence export, release bundle verification, SBOM/provenance support, and
adapter paths for shell, PowerShell, Codex, Claude Code, and ACP/acpx. The
current line also includes WSS/mTLS host job transport, hosted-auth verifier
configuration, a storage-provider boundary with built-in `file`, `postgres`,
`redis-stream`, and `s3-compatible` state-store providers, SAML/OIDC/JWKS auth
runtime paths, and enrollment authority lifecycle evidence commands. Hosted
provider packages can be generated and verified with
`rdev hosted-provider package` / `rdev hosted-provider verify`; packages now
include `rdev.hosted-provider-runtime-contract.v1` runtime contracts for
Postgres, S3-compatible storage, Redis streams, OIDC/JWKS, and SAML runtime
evidence requirements without embedding private endpoints or credentials. The
Postgres state-store path uses `psql`/libpq and rejects inline passwords so
secrets stay outside process arguments. Chisel/frpc,
SSH tunnel, headscale/Tailscale-compatible mesh, and WireGuard connectivity
adapter packages can be generated and verified with
`rdev relay-adapter package` / `rdev relay-adapter verify`, giving Agents
standard `RDEV_RELAY_*`, `RDEV_SSH_*`, `RDEV_MESH_*`, and `RDEV_VPN_*` runner
metadata instead of custom tunnel scripts. Chisel/frpc, mesh `tailscale`, and
VPN `wg` helper binaries share the reviewed `rdev deps install` path for
SHA-256 verified user/workspace-scoped repair; enrollment, keys, routes, DNS,
firewall, services, and privileged VPN/mesh setup remain approval-gated. Real
hosted gateway storage/auth runs can be archived and verified with
`rdev acceptance package-hosted-provider-runtime` /
`rdev acceptance verify-hosted-provider-runtime-package`, covering startup,
storage/auth verification, backup, restore, retention, role probes,
failure-mode evidence, audit, redaction, and checksums. Real
relay runs can be archived and verified with
`rdev acceptance package-relay-adapter` /
`rdev acceptance verify-relay-adapter-package`. Release candidates now also
include a verifiable `connection-entry-release.zip` with platform artifacts,
release metadata, runner template, visible launchers, archive checksums, and
no-private-parameter checks.

Still gated before a production-grade hosted release: real platform acceptance
evidence for Windows/Linux/macOS service modes, real durable third-party hosted
storage/auth runtime evidence beyond the built-in Postgres, Redis,
S3-compatible, OIDC/JWKS, and SAML runtime paths, current provider-specific
runtime contracts, and runtime evidence packager, real helper/relay adapter
acceptance, and final external GitHub publishing plus real public download
verification. The post-release download verifier package now exists, so those
real transcripts can be archived with checksums and
redaction once a release is published.

## Documentation Map

| Need | Start Here |
|---|---|
| Repository layout and package boundaries | [Project Structure](docs/project/PROJECT_STRUCTURE.md) |
| Project direction and release gates | [Roadmap](docs/project/ROADMAP.md) |
| Copy-paste install prompt for agents | [Agent Bootstrap Prompt](docs/operations/AGENT_BOOTSTRAP_PROMPT.md) |
| One-shot Skillkit packaging and installation | [Skillkit Install](docs/operations/SKILLKIT_INSTALL.md) |
| MCP stdio integration | [MCP Stdio](docs/operations/MCP_STDIO.md) |
| Local development gateway | [Development Gateway](docs/operations/DEV_GATEWAY.md) |
| Acceptance evidence | [Acceptance Tests](docs/project/ACCEPTANCE_TESTS.md) |
| Release checklist | [Release Checklist](docs/project/RELEASE_CHECKLIST.md) |
| GitHub project workflow | [GitHub Project Management](docs/project/GITHUB_PROJECT_MANAGEMENT.md) |
| Security model | [Security](SECURITY.md) and [Threat Model](docs/security/THREAT_MODEL.md) |
| Contributing | [Contributing](CONTRIBUTING.md) |

## Design Invariants

- No hidden persistence.
- No silent privilege escalation.
- No private server address or personal path baked into the public project.
- No unrestricted agent shell by default.
- No dumping whole remote repositories, environment snapshots, logs, or
  target-side context into the Agent server by default; use host-side indexes and
  progressive disclosure.
- No blind dependency or tool installation: probe first, install from verified
  sources, prefer host-local/user/workspace scope, and record evidence.
- No peer Agent free-for-all: A2A/MCP/local Agent collaboration must stay
  bounded by signed jobs, policy, redaction, approvals, and evidence.
- No English-only UX assumption: target-side instructions, skills, MCP
  summaries, approvals, and evidence summaries should match the target host
  language with a predictable fallback.
- Every serious remote job must be signed, policy-checked, auditable, and
  revocable.
- Destructive actions and high-risk capabilities need explicit approval gates.

## License

Remote Dev Skillkit is released under the [Apache-2.0](LICENSE) license.
