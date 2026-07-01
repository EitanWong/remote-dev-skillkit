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

From a checkout:

```bash
go install ./cmd/rdev
rdev doctor
```

Export and verify a portable Skillkit bundle:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
rdev skillkit verify --bundle dist/remote-dev-skillkit
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

## Agent-First Remote Session

When a machine needs help, the agent should create an invite instead of asking
the human to assemble flags by hand:

```bash
rdev invite create \
  --gateway https://api.example.com/v1 \
  --reason "repair target development environment" \
  --capabilities shell.user,codex.run,git.diff \
  --transport auto
```

The JSON output includes `schema_version: rdev.agent-invite.v1`, a short-lived
ticket, the manifest URL, a copyable `host_command`, a transport fallback plan,
`customer_bootstrap`, and the next MCP tools the agent should call. The human
action is intentionally small: open the generated customer link on the target
computer, run the visible platform command, and approve the host when policy
requires it. The agent does the waiting, probing, job creation, status tracking,
evidence review, and revocation.

For customer support, send the invite's `customer_bootstrap.customer_link`.
The join page serves inspectable `/bootstrap.sh` and `/bootstrap.ps1` helpers
that start a visible attended host session with `--transport auto`. They do not
create hidden persistence, weaken OS policy, or open inbound firewall ports.

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
`Accept-Language`. Agents should detect the target host/customer language from
host locale, OS UI language, browser hints, and explicit preferences, then
localize customer instructions, skills, MCP summaries, approval prompts, job
status, and evidence summaries. Protocol keys, schema versions, commands,
paths, JSON fields, checksums, and code blocks stay unchanged.

For your own computer or workstation fleet, invite output includes
`managed_development_plan`. That path is built for long-running development:
managed host mode, `--once=false`, service-backed restart plans,
`--transport auto`, release-bundle startup gates, enrollment renewal,
revocation refresh, workspace locks, Git worktrees, host-local context caches,
evidence bundles, and reconnect proof. Temporary customer sessions stay light;
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
| LAN gateway URL | implemented when routable | Agent server and target host share a LAN/VPN segment |

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
  customer context into the Agent server by default; use host-side indexes and
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
