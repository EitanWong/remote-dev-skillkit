# Remote Dev Skillkit

Remote Dev Skillkit is a safety layer for AI agents that need to work on real
Mac, Windows, and Linux machines.

It gives agents a portable Skillkit, MCP tools, signed jobs, approval gates,
host-local policy, evidence bundles, and release verification so they can fix
real development environments without being handed the keys to the whole
building.

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
adapter paths for shell, PowerShell, Codex, Claude Code, and ACP/acpx.

Still gated before a production-grade hosted release: real platform acceptance
evidence for Windows/Linux/macOS service modes, production WSS/mTLS, full hosted
enrollment authority operations, and final external GitHub publishing steps.

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
- Every serious remote job must be signed, policy-checked, auditable, and
  revocable.
- Destructive actions and high-risk capabilities need explicit approval gates.

## License

Remote Dev Skillkit is released under the [Apache-2.0](LICENSE) license.
