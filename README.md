# Remote Dev Skillkit

Remote Dev Skillkit is a safety layer for AI agents that need to work on real
Mac, Windows, and Linux machines.

It gives agents a portable Skillkit, MCP tools, signed jobs, approval gates,
host-local policy, evidence bundles, and release verification so they can fix
real development environments without being handed the keys to the whole
building.

The product is AI-native: the human talks to an agent, and the agent uses
`rdev.invites.create`, `rdev.hosts.*`, and `rdev.jobs.*` to prepare the remote
session, wait for the host, request approval when needed, run scoped work, and
bring back evidence.

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
MCP-capable agent? Copy this into the agent:

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

If `rdev` is missing, do not stop. Recover it by using an existing local rdev
binary if known, building this checkout with `go install ./cmd/rdev`, using
`go run ./cmd/rdev bootstrap agent-plan --repo-root .` as a temporary planner,
or cloning then building in a safe user/workspace location. After `rdev` works,
run `rdev bootstrap agent-plan --repo-root .` and follow the JSON plan.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. First call `rdev.support_session.handoff` or run
`rdev support-session handoff`; follow its `selected_path`,
`mcp_next_tool`, `mcp_next_arguments`, or `foreground_start_command` instead of
guessing whether to prepare, create, start, or plan. If `rdev`, the gateway, or helper assets are
unclear, call `rdev.support_session.prepare` or run
`rdev support-session prepare --build-assets`; follow its standard recovery
actions and use its recommended `gateway_url_candidates` entry instead of
asking me to choose or assemble a gateway URL. Never give a remote target
`0.0.0.0` or same-machine loopback unless this is explicitly same-machine
testing. If this runtime has configured gateway fallbacks such as
`RDEV_HOSTED_GATEWAY_URL`, `RDEV_RELAY_GATEWAY_URL`,
`RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, or
`RDEV_SSH_GATEWAY_URL`, `rdev` automatically appends them to
`gateway_url_candidates` after direct/LAN candidates and before loopback so the
target command can fail over without Agent-written glue. `handoff` and
`create` also use the first configured `RDEV_*_GATEWAY_URL` when no explicit
`gateway_url` is supplied, so fresh Agents do not need to ask me which gateway
URL to use. If a reachable gateway is already running, call
`rdev.support_session.create` through MCP or
`rdev support-session create` through the CLI. That returns the ready
target-machine command, ticket, join URL, and status watcher in one structured
payload. It also returns `target_bootstrap_requirements` and, for CLI calls,
`target_bootstrap_readiness`, so the Agent can detect whether an existing
gateway can serve verified helper assets for clean targets that do not already
have `rdev`. If readiness is false for a platform command, run
`rdev support-session start` or `rdev support-session prepare --build-assets`
instead of asking the target-side human to install `rdev` manually. The target
command already tries ordered gateway URL candidates with bounded timeout/retry
behavior; do not write a custom PowerShell, shell, relay, or polling fallback.
Read `connection_continuity_policy` as well: if
`stable_after_lan_change=false`, treat LAN as only the first opportunistic path
and prefer a configured hosted/relay/mesh/VPN/SSH gateway before claiming the
connection is robust for long-running work.
Prefer `user_handoff.message` plus
`user_handoff.copy_paste` when telling me what to run. If no gateway is running
yet, run `rdev support-session start` in a visible foreground terminal; it
auto-prepares verified Windows/macOS/Linux
helper assets when a source checkout and Go are available, starts the local
gateway, and prints the same ready session payload before listening. Use
`rdev.support_session.plan` or `rdev support-session plan` only for review/debug
planning. After giving me the target-machine command, watch
`rdev.support_session.status`
or `rdev support-session status --wait`; the CLI status command can omit
`--gateway-url` when a configured `RDEV_*_GATEWAY_URL` exists. Created session
payloads include `watch_connection_status_configured_gateway` with this shorter
watcher command, and Agents should use it when a configured gateway is present.
When `connected=true`, tell me the connection has been established before creating jobs. Then follow
`connected_next_steps`: report `user_report`, inspect
`rdev.hosts.capabilities`, and create only the smallest scoped job for my task.
If waiting times out or
the target does not appear, read the returned `connection_recovery` field and
follow its standard tools instead of writing custom recovery scripts.
Do not ask humans to assemble ticket, root, gateway, transport, release, or
checksum flags. Use `rdev.invites.create`, then materialize it with
`rdev.connection_entry.plan` or `rdev connection-entry plan`.
Give the target side only the selected link, visible script, or signed package;
keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off
repair machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed; prefer
existing, open-source/free options such as frp, Chisel, headscale, or WireGuard;
ask before privileged, persistent, paid, firewall, DNS, cloud, or
security-policy changes. For company or third-party machines, ask only for
authorization first, then default to visible attended-temporary mode; let the
Connection Entry and target-side probes detect Windows/macOS/Linux. Dry-run
before execute. Do not hardcode private paths, secrets, or server addresses;
example URLs are placeholders only.
```

For the full copy-paste prompt, see
[Agent Bootstrap Prompt](docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

From a checkout:

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

Export and verify a portable Skillkit bundle. A hosted gateway URL is optional;
omit `--gateway-url` for local Agent installs that only need `rdev mcp serve`:

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

The Agent should start with `rdev.support_session.handoff` or
`rdev support-session handoff`. That first-contact contract returns
`rdev.support-session-handoff.v1` and chooses the next standard path: call
`rdev.support_session.create` when a reachable gateway is already running or a
configured `RDEV_*_GATEWAY_URL` exists, or run the returned foreground
`rdev support-session start` command when no gateway is running. The
created/started session then returns
`rdev.support-session-created.v1`: the ready Windows/macOS/Linux target command,
join URL, manifest root, real ticket code, and status watcher with no
placeholders. It also includes `target_bootstrap_requirements` and, for CLI
create calls against an existing gateway, `target_bootstrap_readiness`. Those
fields prevent the common failure where a manually started gateway has no
downloadable `rdev` helper for a clean Windows target. If readiness is false,
use the standard `support-session start` or `prepare --build-assets` path
instead of inventing an install script or telling the target user to assemble
prerequisites. The command itself loops through ordered Connection Entry URLs on
the target machine with bounded timeouts/retries, so fallback stays in `rdev`
instead of Agent-written glue. The same payload includes
`user_handoff.message` and `user_handoff.copy_paste`, which is the exact
human-facing text and command/link the Agent should send. When the target OS is
unknown, `user_handoff.auto_target_rule` tells the Agent to send the join URL
first and use the returned platform command only when the human asks for a
terminal command or cannot open the page.
When the handoff/readiness output shows `rdev`, gateway state, or target helper
assets are unclear, the Agent
should call `rdev.support_session.prepare` or run
`rdev support-session prepare --build-assets` from a checkout. That returns
`rdev.support-session-prepare.v1` with local recovery, helper asset hashes,
one-command target readiness, `gateway_url_candidates`, and standard recovery
actions. The Agent should use the recommended gateway candidate for target-side
commands and keep raw address selection out of human chat. If no gateway is
running yet, the Agent should run `rdev support-session start` in a visible
foreground terminal; that command auto-prepares verified helper assets when
possible, starts the local gateway, and prints
`rdev.support-session-started.v1` with the embedded ready session. The Agent
should use `rdev.support_session.plan` or `rdev support-session plan` only for
review/debug planning. The Agent should not write its own PowerShell, relay,
nohup, ticket, root, gateway, transport, status polling, or approval glue.
Operators may preconfigure hosted/relay/mesh/VPN/SSH gateway URLs with
`RDEV_*_GATEWAY_URL`; support-session prepare/start/create will include those
URLs in the ordered candidate list, while keeping ticket/root/transport details
inside the structured payload. `rdev support-session create` can now omit
`--gateway-url` when one of those configured URLs exists; if neither an
explicit nor configured reachable gateway exists, use `rdev support-session
start`.
If an operator intentionally starts a low-level dev gateway, prefer
`--rdev-assets-dir <dir>` over five individual helper flags so `/assets`
contains the verified Windows/macOS/Linux bootstrap helpers.

For lower-level package materialization, the Agent creates an invite and
materializes it before asking anyone on the target side to do anything. The
public name for this universal handoff is **Connection Entry**. It covers your
own long-running workstation, someone else's temporary repair machine, LAN,
hosted, relay, mesh, SSH, and VPN-assisted paths. Humans should not
hand-assemble ticket codes, root keys, gateway URLs, transports, release roots,
or checksums.

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

rdev support-session status \
  --gateway-url http://<reachable-gateway-host>:8787 \
  --ticket-code <ticket-code> \
  --wait \
  --locale auto

# If RDEV_HOSTED_GATEWAY_URL / RDEV_RELAY_GATEWAY_URL / similar is configured:
rdev support-session status \
  --ticket-code <ticket-code> \
  --wait \
  --locale auto
```

If there is no hosted gateway yet, the Agent should start with local MCP stdio,
`rdev demo local`, or a local-dev/LAN gateway plan. It should only escalate to
hosted, SSH, relay, mesh, VPN, firewall, DNS, cloud, paid, privileged, or
persistent changes when probes show they are needed and the operator approves.

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
SHA-256 verify, unpack, and use helper binaries such as `chisel` or `frpc` from
a user/workspace tools directory without changing PATH or installing services.
Creating credentials, changing firewall/DNS/routes, starting persistent
connectivity, installing mesh/VPN drivers or services, or using paid/cloud
relays still requires explicit approval. Real signed
per-platform release archives still require release assets, but the runner
contract is now real code rather than a script-only fallback plan. It does not
create hidden persistence, weaken OS policy, or open inbound firewall ports.

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

MCP-capable agents can call `rdev.invites.create` directly through
`rdev mcp serve`; no private server addresses or local paths are baked into the
project.

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
rdev demo local
rdev mcp tools
```

The local demo walks through ticket creation, host registration, approval, job
execution, artifacts, and audit without needing a public gateway.

## Current Status

Remote Dev Skillkit is in Phase 1: a safe MVP foundation for professional
open-source release.

Already implemented: the `rdev` CLI, local dev gateway, MCP stdio server,
Skillkit export/verify/install planning, signed job envelopes, trust bundles,
host enrollment certificates, revocations, workspace locks, approval gates,
audit/evidence export, release bundle verification, SBOM/provenance support, and
adapter paths for shell, PowerShell, Codex, Claude Code, and ACP/acpx. The
current line also includes WSS/mTLS host job transport, hosted-auth verifier
configuration, a storage-provider boundary, and enrollment authority lifecycle
evidence commands.

Still gated before a production-grade hosted release: real platform acceptance
evidence for Windows/Linux/macOS service modes, optional third-party hosted
storage provider packages, and final external GitHub publishing steps.

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
