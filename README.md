# Remote Dev Skillkit

Agent-native remote development tools for safely delegating coding and repair work to enrolled Mac, Windows, and Linux hosts.

Multilingual quick starts: [English](README.md), [简体中文](docs/i18n/README.zh-CN.md), [Español](docs/i18n/README.es.md), [Français](docs/i18n/README.fr.md), [Deutsch](docs/i18n/README.de.md), [日本語](docs/i18n/README.ja.md), [한국어](docs/i18n/README.ko.md), [Português](docs/i18n/README.pt-BR.md), [हिन्दी](docs/i18n/README.hi.md), [العربية](docs/i18n/README.ar.md), [Русский](docs/i18n/README.ru.md).

This project is the implementation home for the `rdev` toolchain:

- `rdev`: operator CLI and local debugging surface.
- `rdev-host`: target-machine agent for temporary or managed hosts.
- `rdev-gateway`: agent/operator-side control plane for tickets, host registry, jobs, artifacts, and audit.
- `rdev-mcp`: MCP tools exposed to Hermes, Codex, Claude Code, OpenCode, and other agents.
- `skills/`: Agent Skills that teach agents how to use the remote development workflow safely.

For repository layout and package boundaries, see [Project Structure](docs/project/PROJECT_STRUCTURE.md).
The project is released under the [Apache-2.0 license](LICENSE).

## Core Promise

Remote Dev Skillkit is not a hidden remote-control tool. It is consent-first infrastructure for visible, auditable, policy-bound remote support and remote coding.

Temporary third-party machines use foreground, time-limited support sessions. Long-lived unattended service mode is reserved for operator-owned or formally managed devices.

## Final Shape

The finished project is a universal safety layer for agents working on real machines:

```text
Agent Skills + MCP tools
        |
        v
rdev-gateway: tickets, hosts, jobs, approvals, artifacts, audit
        |
        v
outbound HTTPS/WSS host channels
        |
        v
rdev-host: identity, trust bundle, local policy, adapters
        |
        v
shell, PowerShell, Git, Codex, Claude Code, ACP, browser, GUI, mesh, Coder, DevPod
```

The project intentionally reuses mature ecosystems where they fit: MCP for agent tools, Tailscale/headscale or SSH for owned-host connectivity, Coder/DevPod for governed workspaces, RustDesk/MeshCentral for explicit GUI sessions, and platform/Sigstore-style signing for release trust. What `rdev` owns is the missing agent safety kernel: signed job envelopes, host-side policy, approval gates, workspace locks, redaction, evidence bundles, audit chains, and revocation.

The canonical endgame is locked in [Perfect Ending Solution](docs/architecture/PERFECT_ENDING_SOLUTION.md). Keep that document as the architecture decision layer instead of adding new one-off "final" sections. The compressed product decision is one signed control protocol, two host products, separated trust roots, non-bypassable join/run/approve/prove paths, and proof packages for releases, Skillkit installs, platform acceptance, evidence/audit, and adapter conformance. The design intentionally separates temporary attended repair from explicit managed service mode, and it treats Codex, Claude Code, ACP/acpx, GUI, mesh, Coder, DevPod, shell, and PowerShell as adapters behind the same signed-job, evidence, approval, and revocation contract.

[Final Closure Blueprint](docs/architecture/FINAL_CLOSURE_BLUEPRINT.md) is the concise release-facing summary. [Ultimate Closure Design](docs/architecture/ULTIMATE_CLOSURE_DESIGN.md) and [Final System Design](docs/architecture/FINAL_SYSTEM_DESIGN.md) remain supporting rationale and implementation detail.

## Current Status

This repository is in Phase 1: project foundation and safe MVP.

Implemented now:

- Project plan, architecture, security model, and versioning docs.
- Initial `rdev` CLI plus thin `rdev-host`, `rdev-gateway`, and `rdev-mcp` entrypoints that route into the same command surface.
- `rdev doctor` capability detection.
- `rdev ticket create` local ticket preview.
- `rdev policy explain` local policy simulation.
- `rdev policy explain-shell` shell job policy preflight explanation.
- `rdev mcp tools` tool-contract listing, including `rdev.enrollment.verify_certificate` for agent-side enrollment proof checks and `rdev.adapter.verify_result`, `rdev.adapter.verify_lifecycle`, `rdev.adapter.verify_runtime`, and `rdev.adapter.verify_cancellation` for agent-side adapter conformance checks.
- `rdev mcp serve` minimal MCP stdio server for initialize, tools/list, and tools/call.
- `rdev gateway serve --dev` local HTTP development gateway.
- Optional development gateway state snapshots through `rdev gateway serve --dev --signing-key ... --state ...`, preserving tickets, hosts, jobs, artifacts, audit events, and trust bundles across gateway restarts while keeping the same signing key.
- Optional development gateway TLS and mTLS listener through `rdev gateway serve --dev --tls-cert <server.pem> --tls-key <server-key.pem> [--client-ca <ca.pem>]`, requiring verified client certificates when `--client-ca` is set. This is a transport/authentication primitive for local and pre-production testing, not the full production WSS host channel.
- Host-side dev gateway HTTPS/mTLS client support through `rdev host serve --gateway-ca <ca.pem> [--gateway-client-cert <client.pem> --gateway-client-key <client-key.pem>]` for local HTTPS registration, trust fetches, polling, completion, and join-manifest fetches. This closes the dev mTLS loop without changing the signed-envelope or host-local authorization model.
- `rdev demo local` in-memory ticket, host approval, job, artifact, and audit flow.
- Development signed job envelopes using Ed25519 in-memory gateway keys.
- Local dev host registration, job polling, and job completion loop over HTTP or local HTTPS.
- Development HTTPS long-poll host job transport via `rdev host serve --transport long-poll`.
- Development trust bundle endpoint for host-side envelope signature verification.
- Host-bound trust bundle update checks for managed host trust-store refresh.
- macOS LaunchAgent plist generation, status inspection, safe plist removal, and opt-in lifecycle control via `rdev host install-service`, `rdev host service-status`, `rdev host service-control`, and `rdev host uninstall-service`, with `--execute` required before running `launchctl`.
- Linux systemd user-unit generation, status inspection, safe unit removal, and opt-in lifecycle control via the same host service commands, with `--execute` required before running `systemctl --user`.
- Linux managed-service acceptance planning and verification via `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service`, producing and checking a machine-readable plan with a written `0600` systemd user unit, reviewed `systemctl --user daemon-reload/enable --now/status/disable --now` commands, managed-host args, hardening flags, release-bundle startup gate, required evidence checklist, and explicit warnings that no `systemctl` command was executed.
- Linux managed-service acceptance evidence packaging via `rdev acceptance package-linux-managed-service`, collecting a verified plan, generated unit, plan verifier output, start/status/log/release-gate/audit/reconnect/stop/uninstall transcripts, and managed job evidence into a redacted checksummed package.
- Windows Service managed-host planning, status-command planning, dry-run control, and uninstall-command planning via the same host service commands. The Windows path emits reviewed `sc.exe create/query/qc/start/stop/delete` command plans, carries the managed-host release-bundle gate, uses `start= demand`, and requires explicit `service-control --execute` before running `sc.exe`; real Windows Service acceptance is still a future gate.
- Windows managed-service acceptance planning and verification via `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service`, producing and checking a machine-readable plan with reviewed `sc.exe create/description/query/qc/start/stop/delete` commands, managed-host args, `start= demand`, release-bundle startup gate, required evidence checklist, and explicit warnings that no PowerShell or `sc.exe` command was executed.
- Persistent development gateway signing key files plus host trust pin checks.
- Trust lifecycle operator commands via `rdev trust init`, `rdev trust rotate`, `rdev trust revoke`, and `rdev trust verify`, producing signed `rdev.trust-bundle.v1` bundles with sequence, previous-hash, key rotation, key retirement, key revocation, and pinned-root verification.
- File-backed host identity key store with registration fingerprint preservation and signed job identity binding.
- macOS Keychain, Windows DPAPI, Linux libsecret, and Linux keyctl protected-store references for managed host identity and trust bundle storage via `--identity-store keychain:<service>/<account>` / `--trust-store keychain:<service>/<account>` on macOS, `--identity-store dpapi:<service>/<account>` / `--trust-store dpapi:<service>/<account>` on Windows, `--identity-store libsecret:<service>/<account>` / `--trust-store libsecret:<service>/<account>` on Linux hosts with `secret-tool` and a reachable Secret Service, and `--identity-store keyctl:<service>/<account>` / `--trust-store keyctl:<service>/<account>` on Linux hosts with a user keyring, preserving the same identity fingerprint, trust-bundle sequence, rollback checks, and host-bound update rules as file-backed development stores. The keyctl path protects runtime/user-keyring storage but does not by itself prove reboot persistence; real Linux reboot/reconnect acceptance remains a separate release gate.
- Signed host registration proofs via `rdev.host-registration-proof.v1`; hosts that present identity keys must prove possession of the matching private key before the gateway preserves the identity fingerprint and binds future job envelopes to it.
- Host enrollment certificates and revocations via `rdev.host-enrollment-certificate.v1` and `rdev.host-enrollment-revocations.v1`; `rdev enrollment sign-certificate`, `issue-certificate`, `verify-certificate`, and `renew-certificate` locally sign, request from a configured dev/hosted gateway, verify, and extend certificate files that bind ticket code, mode, host name, OS/arch, capabilities, validity window, and host identity fingerprint to an enrollment root. `POST /v1/enrollment/certificates` plus `rdev enrollment issue-certificate --gateway ... --root-public-key ...` gives operators a first hosted issuance primitive; issued certificates are pinned-root verified before they are written locally, and requested capabilities cannot exceed the ticket capabilities. Dev gateways can optionally protect issuance, hosted renewal, and hosted revocation-list fetches with `--enrollment-issuer-token-file`, while clients pass `--issuer-token-file` or hosts pass `--enrollment-issuer-token-file` so the bearer token is read from a local file instead of appearing in command output. `rdev enrollment renew-certificate --revocations <file>` verifies the current certificate and rejects revoked certificates before issuing a refreshed certificate with the same scope locally; `POST /v1/enrollment/certificates/renew` plus `rdev enrollment renew-certificate --gateway ... --root-public-key ...` provides the matching dev hosted renewal primitive with pinned-root, previous-fingerprint, signature, and new-fingerprint verification before local write. `rdev host serve --renew-enrollment-certificate --enrollment-root-public-key ...` renews a near-expiring local enrollment certificate from the configured gateway before registration, writes the pinned-root-verified replacement back to the certificate path, and keeps using the gateway/mTLS-aware host client plus optional `--enrollment-issuer-token-file`. `rdev enrollment init-revocations`, `revoke-certificate`, and `verify-revocations` produce an empty signed revocation baseline, append revoked certificate fingerprints, and verify signed certificate revocation lists. Dev gateways expose configured revocation lists through `GET /v1/enrollment/revocations`, and `rdev enrollment fetch-revocations --issuer-token-file ...` downloads protected lists, verifies against a pinned enrollment root, and writes the list with private file permissions. `rdev host serve --fetch-enrollment-revocations --enrollment-root-public-key <key_id>:<base64url_public_key>` fetches the signed gateway revocation list before registration, optionally authenticates with `--enrollment-issuer-token-file`, verifies the local certificate against that pinned root, and refuses locally revoked certificates before sending the registration payload. `rdev.enrollment.verify_certificate` exposes certificate verification plus optional revocation-list checks over MCP for agents that review registrations before trust. `rdev gateway serve --dev --enrollment-key <file> --enrollment-revocations <file>` can issue, renew, publish revocations, and require certificates during host registration, `rdev gateway serve --dev --enrollment-root-public-key <key_id>:<base64url_public_key> --enrollment-revocations <file>` can require externally issued certificates, and `rdev host serve --enrollment-certificate <file>` attaches the certificate to the registration payload.
- Host-side nonce replay cache with in-memory and file-backed development stores.
- Hash-chained audit export and verification via `rdev audit export` / `rdev audit verify`.
- Local evidence bundle export via `rdev evidence export`.
- Gateway-backed evidence bundle export from a job id via `rdev evidence export --gateway ... --job-id ...`.
- Skillkit bundle export, verification, install-plan generation, install-plan verification, and dry-run-by-default direct installation via `rdev skillkit export`, `rdev skillkit verify`, `rdev skillkit plan-install`, `rdev skillkit verify-install-plan`, and `rdev skillkit install` for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and generic MCP agents.
- Managed Mac coding acceptance harness via `rdev acceptance managed-mac`, producing a report, locked-worktree Codex evidence bundle, and approval-gate evidence bundle.
- Acceptance report verification via `rdev acceptance verify --report ...`, including evidence manifest checksums, artifact index validation, audit-chain verification, approval-gate evidence, and workspace-lock release checks.
- Managed Mac LaunchAgent acceptance planning and verification via `rdev acceptance managed-mac-service` and `rdev acceptance verify-managed-mac-service`, producing and checking a `0600` LaunchAgent plist, release-bundle startup gate, launchctl command plans, service-backed acceptance commands, and uninstall steps without auto-starting launchd.
- Managed Mac LaunchAgent acceptance evidence packaging via `rdev acceptance package-managed-mac-service`, collecting a verified plan, plist, plan verifier output, start/inspect/log/release-gate/audit/reconnect/stop/uninstall transcripts, and a verified managed Mac report/evidence bundle into a redacted checksummed package.
- Windows temporary acceptance planning and verification via `rdev acceptance windows-temporary` and `rdev acceptance verify-windows-temporary`, producing and checking a machine-readable plan, reviewed PowerShell launcher, signed release manifest or bundle verification requirements, approval probes, no-persistence inspection commands, and required evidence checklist without executing PowerShell.
- Windows temporary acceptance evidence packaging via `rdev acceptance package-windows-temporary`, collecting a verified plan, launcher, transcript, release verifier output, audit, approval probes, and no-persistence evidence into a redacted checksummed package.
- Workspace lock and Git worktree foundation via `rdev workspace lock`, `rdev workspace status`, `rdev workspace unlock`, and `rdev workspace prepare-worktree`.
- Host job execution can enforce one-writer workspace locks through `rdev host serve --workspace-lock-store`.
- Codex adapter MVP through `adapter=codex`: runs `codex exec` or a signed payload-provided command inside the validated workspace, requires `codex.run` and `git.diff`, gates push/merge/deploy/publish/credential/service intents on approval, and captures `rdev.codex-result.v1` evidence with Codex output, Git status, Git diff/stat, optional verification command results, `go test -json` test reports, output caps, and redaction.
- Codex adapter conformance coverage for canonical workspace roots, write-scope escape rejection before execution, failure evidence, redaction, output truncation, and timeout cancellation evidence.
- Claude Code adapter MVP through `adapter=claude-code`: runs `claude -p <prompt>` or a signed payload-provided command inside the validated workspace, requires `claude-code.run` and `git.diff`, gates the same high-risk intents on approval, and captures `rdev.claude-code-result.v1` evidence with Claude Code output, Git status, Git diff/stat, optional verification command results, `go test -json` test reports, output caps, redaction, and cooperative cancellation.
- ACP/acpx adapter MVP through `adapter=acpx`: runs `acpx --cwd <workspace> codex exec <prompt>` by default or a signed payload-provided `acpx_command` / `acpx_agent` / `acpx_args` override, requires `acpx.run` and `git.diff`, gates the same high-risk intents on approval, and captures `rdev.acpx-result.v1` evidence with acpx output, Git status, Git diff/stat, optional verification command results, `go test -json` test reports, output caps, redaction, and cooperative cancellation. The upstream acpx CLI is still alpha, so payload overrides remain the compatibility valve.
- PowerShell adapter MVP through `adapter=powershell`: runs an explicit PowerShell command through an allowlisted `pwsh`, `powershell`, `powershell.exe`, or payload-provided executable, requires `powershell.user`, never adds `-ExecutionPolicy Bypass`, gates high-risk commands on approval, and captures `rdev.powershell-result.v1` evidence with redaction.
- Codex, Claude Code, acpx, shell, and PowerShell adapter cooperative cancellation through context-aware hostrunner execution and host-side gateway job status monitoring.
- Canceled Codex, Claude Code, acpx, shell, and PowerShell jobs can append cancellation evidence artifacts while preserving the gateway job's `canceled` terminal state.
- Public adapter onboarding and conformance through `pkg/adapterkit`, `adapterkit.RunLifecycle`, `rdev adapter scaffold`, `rdev adapter verify-result`, `rdev adapter verify-lifecycle`, `rdev adapter verify-cancellation`, `rdev adapter verify-runtime`, and MCP tools `rdev.adapter.verify_result` / `rdev.adapter.verify_lifecycle` / `rdev.adapter.verify_cancellation` / `rdev.adapter.verify_runtime`, covering generated lifecycle manifest templates, runtime lifecycle fixtures, result artifacts, cancellation artifacts, required phases, safety boundaries, cleanup, result schemas, timing, redaction, command evidence, canceled-vs-timeout proof, and secret-pattern rejection.
- Built-in shell, PowerShell, Codex, Claude Code, and acpx hostrunner adapters can opt into runtime fixture capture with `rdev host serve --capture-runtime-fixture`, preserving the primary adapter result while appending a verified `rdev.adapter-runtime-fixture.v1` artifact for completed, failed, or canceled jobs.
- Structured host-side denial artifacts via `rdev.host-denial.v1` for missing envelopes, wrong host, identity mismatch, expired/tampered/replayed envelopes, unsupported adapters, missing capabilities, missing workspaces, non-allowlisted commands, and workspace escapes.
- Structured host-side approval-required artifacts via `rdev.approval-required.v1`; jobs with unsatisfied signed approval requirements pause before adapter execution, and gateway-approved jobs receive signed `rdev.approval-token.v1` tokens.
- Built-in shell, PowerShell, Codex, Claude Code, and acpx jobs run an implicit approval preflight before adapter execution for package installation, elevation, GUI control, service management, push, merge, deploy, publish, credential changes, and execution-policy changes.
- Durable host-side approval token consumption stores with in-memory and file-backed development modes, exposed through `rdev host serve --approval-store`.
- Signed development join manifest endpoint for manifest-driven temporary host registration.
- Join manifests can be signed by a separate bootstrap/release trust root and verified by hosts with a pinned root public key.
- Release artifact signing and verification primitives via `rdev release sign` / `rdev release verify`.
- Signed release bundle indexes via `rdev release create-bundle` / `rdev release verify-bundle`, checking the signed index, every artifact manifest, artifact and manifest SHA-256/size, and required artifact presence before publishing.
- The standalone `rdev-verify` helper can verify either a single signed artifact manifest or a full signed release bundle before host execution.
- `rdev host serve` can enforce an optional startup release gate with `--release-bundle`, `--release-root-public-key`, and `--release-require-artifacts`; verification runs before host registration and job polling. Managed LaunchAgent and systemd unit generation can carry the same gate so owned hosts self-check signed release bundles on restart.
- Release candidate packaging via `rdev release prepare-candidate`, staging built artifacts, signed manifests, a signed release bundle, a verified Skillkit bundle, checksums, and `release-candidate.json`.
- Release candidate verification via `rdev release verify-candidate`, checking a staged or downloaded candidate's summary, checksums, signed bundle, manifests, artifacts, Skillkit bundle, required artifacts, and unlisted files.
- Real release artifact builds via `scripts/release/build-artifacts.sh`, producing target directories, `rdev.build-artifacts.v1`, and checksums for `rdev`, `rdev-host`, `rdev-gateway`, `rdev-mcp`, and `rdev-verify`.
- Per-platform release candidate preparation via `scripts/release/prepare-platform-candidates.sh`, grouping `rdev.build-artifacts.v1` by `GOOS/GOARCH`, generating one verified candidate per target, and writing `rdev.platform-release-candidates.v1` without external mutation.
- Multi-platform GitHub Release dry-run planning via `scripts/github/plan-platform-release.sh`, producing unique platform archives, a platform release index, verification summary, install guide, release notes, and command previews without mutating GitHub.
- Local GitHub project readiness auditing via `scripts/github/audit-project-readiness.sh`, producing `rdev.github-project-readiness.v1` from docs, issue/PR templates, CI, release planning scripts, and bootstrap-project dry-run output without mutating GitHub.
- Post-release install verification planning via `scripts/github/plan-post-release-install.sh`, producing `rdev.post-release-install-plan.v1`, `VERIFY_INSTALL.md`, platform verification scripts, and a Skillkit verification script from a local GitHub Release dry-run plan without mutating GitHub.
- Post-release install plan verification via `scripts/github/verify-post-release-install-plan.sh`, checking generated scripts, download URLs, checksum commands, candidate verification, bundle verification, Skillkit verification, and no-mutation constraints.
- GitHub Release dry-run planning via `scripts/github/plan-release.sh`, producing a local release plan, commands preview, generated release notes, Skillkit tarball, and candidate verification output without mutating GitHub.
- GitHub Actions CI for tests, vet, shell syntax, release candidate verification, and GitHub Release dry-run planning.
- Windows bootstrap can hash-pin `rdev-verify.exe` and use it to verify either the signed `rdev-host.exe` release manifest or the signed release bundle before starting the host.
- Host-reported failed jobs with audit events.
- Real development scoped shell adapter execution with allowlisted argv, workspace checks, timeouts, cooperative cancellation, output caps, schema-versioned redacted evidence, and failure artifacts.
- Real development scoped PowerShell adapter execution with allowlisted PowerShell executable, workspace checks, timeouts, cooperative cancellation, output caps, schema-versioned redacted evidence, and failure artifacts.
- Foreground `rdev host serve --mode temporary` placeholder.
- Agent Skills drafts.

Not implemented yet:

- Production gateway networking, authentication, and storage beyond the development HTTP gateway and JSON snapshot path.
- Full production host enrollment authority lifecycle beyond the current local certificate, dev hosted issuance/renewal primitives, optional dev issuer bearer token for issuance/renewal/revocation refresh, local renewal, host-side near-expiry renewal, signed empty revocation baseline, and dev revocation-list distribution primitives: operator roles, CA/key custody, production operator identity, fleet renewal policy, and emergency drills.
- Production signing key storage beyond local key files.
- Authenticated durable managed host trust distribution beyond the current development endpoints and local protected stores.
- Full production bootstrap trust root lifecycle and release signing policy.
- Platform-native code signing / Authenticode policy for Windows releases.
- Production WSS host transport beyond the current HTTPS long-poll fallback and dev gateway TLS/mTLS client/listener path.
- Production-grade shell adapter hardening beyond the development scoped executor.
- Full production adapter SDK beyond the current lifecycle runner and lifecycle/result/cancellation/runtime-fixture conformance verifiers.
- Hardware-backed or fleet-managed protected host identity/trust storage beyond the current macOS Keychain, Windows DPAPI, Linux libsecret, Linux keyctl, and file-backed store paths.
- Artifact streaming.
- Real Windows Service managed-host acceptance execution and reconnect proof beyond dry-run `sc.exe` plans and verified acceptance-plan generation.
- Real Linux systemd managed-host acceptance execution and reboot/reconnect proof beyond verified acceptance-plan and evidence-package generation.
- Tailscale/headscale adapter.
- GUI adapter.

## Quick Start

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
scripts/release/build-artifacts.sh --out dist/artifacts --version v0.1.0 --targets darwin/arm64,linux/amd64,windows/amd64
scripts/release/prepare-platform-candidates.sh --build-manifest dist/artifacts/build-artifacts.json --out dist/release-candidates --source-root . --gateway-url https://api.example.com/v1 --key .rdev/keys/release-root.json
go run ./cmd/rdev version
go run ./cmd/rdev-host --mode temporary
go run ./cmd/rdev-mcp tools
go run ./cmd/rdev doctor
go run ./cmd/rdev ticket create --ttl-seconds 7200 --reason "repair Windows dev environment"
go run ./cmd/rdev policy explain --mode attended-temporary --capability shell.user
go run ./cmd/rdev policy explain-shell --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}'
go run ./cmd/rdev demo local
go run ./cmd/rdev mcp tools
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}' | go run ./cmd/rdev mcp serve
go run ./cmd/rdev enrollment sign-certificate --out .rdev/enrollment/host-enrollment.json --key .rdev/keys/enrollment-root.json --ticket-code ABCD-1234 --mode managed --name managed-mac --os darwin --arch arm64 --identity-key-id host --identity-public-key <base64url> --identity-fingerprint sha256:... --capabilities codex.run,git.diff
go run ./cmd/rdev enrollment init-revocations --out .rdev/enrollment/revocations.json --key .rdev/keys/enrollment-root.json
go run ./cmd/rdev gateway serve --dev --addr 127.0.0.1:8787 --signing-key .rdev/keys/gateway-signing-key.json --state .rdev/gateway/state.json --enrollment-key .rdev/keys/enrollment-root.json --enrollment-issuer-token-file .rdev/enrollment/issuer-token.txt --enrollment-revocations .rdev/enrollment/revocations.json
go run ./cmd/rdev enrollment issue-certificate --gateway http://127.0.0.1:8787 --out .rdev/enrollment/host-enrollment-issued.json --root-public-key enrollment-root:... --ticket-code ABCD-1234 --name managed-mac --os darwin --arch arm64 --identity-key-id host --identity-public-key <base64url> --identity-fingerprint sha256:... --capabilities codex.run,git.diff --issuer-token-file .rdev/enrollment/issuer-token.txt
go run ./cmd/rdev enrollment verify-certificate --certificate .rdev/enrollment/host-enrollment.json --root-public-key enrollment-root:...
go run ./cmd/rdev enrollment renew-certificate --certificate .rdev/enrollment/host-enrollment.json --out .rdev/enrollment/host-enrollment-renewed.json --key .rdev/keys/enrollment-root.json --revocations .rdev/enrollment/revocations.json --valid-minutes 120
go run ./cmd/rdev enrollment renew-certificate --certificate .rdev/enrollment/host-enrollment-issued.json --out .rdev/enrollment/host-enrollment-hosted-renewed.json --gateway http://127.0.0.1:8787 --root-public-key enrollment-root:... --issuer-token-file .rdev/enrollment/issuer-token.txt --valid-minutes 120
go run ./cmd/rdev enrollment verify-certificate --certificate .rdev/enrollment/host-enrollment-renewed.json --root-public-key enrollment-root:... --revocations .rdev/enrollment/revocations.json
# Optional revocation smoke for a retired or compromised certificate:
go run ./cmd/rdev enrollment revoke-certificate --current .rdev/enrollment/revocations.json --out .rdev/enrollment/revocations.json --key .rdev/keys/enrollment-root.json --certificate .rdev/enrollment/host-enrollment.json --reason "host retired" --force
go run ./cmd/rdev enrollment verify-revocations --revocations .rdev/enrollment/revocations.json --root-public-key enrollment-root:...
# With a dev gateway configured with --enrollment-revocations:
go run ./cmd/rdev enrollment fetch-revocations --gateway http://127.0.0.1:8787 --root-public-key enrollment-root:... --issuer-token-file .rdev/enrollment/issuer-token.txt --out .rdev/enrollment/fetched-revocations.json --force
go run ./cmd/rdev host serve --mode managed --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234 --identity-store .rdev/host/identity.json --enrollment-certificate .rdev/enrollment/host-enrollment-renewed.json --renew-enrollment-certificate --enrollment-issuer-token-file .rdev/enrollment/issuer-token.txt --enrollment-renew-before 24h --enrollment-renew-valid-minutes 120 --fetch-enrollment-revocations --enrollment-root-public-key enrollment-root:...
# This should fail after the certificate is listed as revoked:
go run ./cmd/rdev enrollment verify-certificate --certificate .rdev/enrollment/host-enrollment.json --root-public-key enrollment-root:... --revocations .rdev/enrollment/revocations.json
go run ./cmd/rdev mcp tools | rg 'rdev.enrollment.verify_certificate'
go run ./cmd/rdev trust init --out .rdev/trust/trust-bundle.json --root-key .rdev/keys/trust-root.json --gateway-key .rdev/keys/gateway-prod.json
go run ./cmd/rdev trust rotate --current .rdev/trust/trust-bundle.json --out .rdev/trust/trust-bundle-next.json --root-key .rdev/keys/trust-root.json --gateway-key .rdev/keys/gateway-next.json --gateway-key-id gateway-next --retire-key gateway-prod
go run ./cmd/rdev trust revoke --current .rdev/trust/trust-bundle-next.json --out .rdev/trust/trust-bundle-revoked.json --root-key .rdev/keys/trust-root.json --key-id gateway-next --reason "key compromise drill"
go run ./cmd/rdev trust verify --bundle .rdev/trust/trust-bundle-revoked.json --root-public-key trust-root:...
go run ./cmd/rdev audit export --input .rdev/audit/events.jsonl --out .rdev/audit/audit-chain.json
go run ./cmd/rdev audit verify --input .rdev/audit/audit-chain.json
go run ./cmd/rdev evidence export --job-json job.json --artifacts-json artifacts.json --audit-jsonl .rdev/audit/events.jsonl --out job_evidence
go run ./cmd/rdev evidence export --gateway http://127.0.0.1:8787 --job-id job_... --out job_evidence
go run ./cmd/rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
go run ./cmd/rdev skillkit verify --bundle dist/remote-dev-skillkit
go run ./cmd/rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install --frameworks codex,hermes,generic-mcp-agent
go run ./cmd/rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
go run ./cmd/rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
go run ./cmd/rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
go run ./cmd/rdev adapter scaffold --adapter claude-code --out examples/adapters/claude-code-lifecycle.json --force
go run ./cmd/rdev adapter verify-result --artifact shell-result.json --adapter shell --schema rdev.shell-result.v1
go run ./cmd/rdev adapter verify-lifecycle --artifact examples/adapters/claude-code-lifecycle.json --adapter claude-code
go run ./cmd/rdev adapter verify-cancellation --artifact shell-result.json --adapter shell --schema rdev.shell-result.v1
go run ./cmd/rdev adapter verify-runtime --artifact adapter-runtime-fixture.json --adapter claude-code --require-result-artifact
go run ./cmd/rdev acceptance managed-mac --out .rdev/acceptance/managed-mac --repo .
go run ./cmd/rdev acceptance managed-mac-service --out .rdev/acceptance/managed-mac-service --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --repo . --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev,rdev-host,rdev-verify
go run ./cmd/rdev acceptance verify-managed-mac-service --plan .rdev/acceptance/managed-mac-service/service-plan.json
go run ./cmd/rdev acceptance windows-temporary --out .rdev/acceptance/windows-temporary --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --download-url https://agent.example.com/rdev-host.exe --expected-sha256 <sha256> --release-bundle-url https://agent.example.com/release-bundle.json --release-root-public-key release-root:... --verifier-download-url https://agent.example.com/rdev-verify.exe --verifier-sha256 <sha256>
go run ./cmd/rdev acceptance verify-windows-temporary --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json
go run ./cmd/rdev acceptance windows-managed-service --out .rdev/acceptance/windows-managed-service --binary 'C:\Program Files\rdev\rdev.exe' --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle 'C:\Program Files\rdev\release-bundle.json' --release-root-public-key release-root:... --release-require-artifacts rdev.exe,rdev-host.exe,rdev-verify.exe
go run ./cmd/rdev acceptance verify-windows-managed-service --plan .rdev/acceptance/windows-managed-service/windows-managed-service-plan.json
go run ./cmd/rdev acceptance linux-managed-service --out .rdev/acceptance/linux-managed-service --binary /opt/rdev/rdev --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev,rdev-host,rdev-verify
go run ./cmd/rdev acceptance verify-linux-managed-service --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json
go run ./cmd/rdev acceptance package-managed-mac-service --plan .rdev/acceptance/managed-mac-service/service-plan.json --out .rdev/acceptance/managed-mac-service-evidence --review-transcript review.txt --start-transcript start.txt --inspect-transcript inspect.txt --logs launchagent.log --release-gate release-gate.json --audit audit.jsonl --reconnect reconnect.txt --managed-report .rdev/acceptance/managed-mac/report.json --stop-transcript stop.txt --uninstall-transcript uninstall.txt
go run ./cmd/rdev acceptance package-windows-temporary --plan .rdev/acceptance/windows-temporary/windows-temporary-plan.json --out .rdev/acceptance/windows-temporary-evidence --transcript transcript.txt --release-verification rdev-verify.json --audit audit.jsonl --no-persistence-dir no-persistence --approval-probes-dir approval-probes
go run ./cmd/rdev acceptance package-linux-managed-service --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json --out .rdev/acceptance/linux-managed-service-evidence --start-transcript start.txt --status-transcript status.txt --logs journal.txt --release-gate release-gate.json --audit audit.jsonl --reconnect reconnect.txt --job-evidence-dir job-evidence --stop-transcript stop.txt --uninstall-transcript uninstall.txt
go run ./cmd/rdev acceptance verify --report .rdev/acceptance/managed-mac/report.json
go run ./cmd/rdev workspace lock --repo . --host-id hst_... --job-id job_... --adapter codex
go run ./cmd/rdev workspace status --repo .
go run ./cmd/rdev workspace unlock --repo . --job-id job_...
go run ./cmd/rdev workspace prepare-worktree --repo . --host-id hst_... --job-id job_... --adapter codex
curl -s -X POST http://127.0.0.1:8787/v1/jobs -H 'content-type: application/json' -d '{"host_id":"hst_...","adapter":"codex","intent":"update README","policy":{"workspace_root":".","capabilities":["codex.run","git.diff"],"prompt":"Update README and run checks.","verification_commands":[["git","status","--short"]],"allow_verification_commands":["git"],"max_duration_seconds":1800,"max_output_bytes":1048576}}'
curl -s -X POST http://127.0.0.1:8787/v1/jobs -H 'content-type: application/json' -d '{"host_id":"hst_...","adapter":"claude-code","intent":"update README","policy":{"workspace_root":".","capabilities":["claude-code.run","git.diff"],"prompt":"Update README and run checks.","verification_commands":[["git","status","--short"]],"allow_verification_commands":["git"],"max_duration_seconds":1800,"max_output_bytes":1048576}}'
curl -s -X POST http://127.0.0.1:8787/v1/jobs -H 'content-type: application/json' -d '{"host_id":"hst_...","adapter":"acpx","intent":"update README through ACP","policy":{"workspace_root":".","capabilities":["acpx.run","git.diff"],"prompt":"Update README and run checks.","acpx_agent":"codex","verification_commands":[["git","status","--short"]],"allow_verification_commands":["git"],"max_duration_seconds":1800,"max_output_bytes":1048576}}'
curl -s -X POST http://127.0.0.1:8787/v1/jobs -H 'content-type: application/json' -d '{"host_id":"hst_...","adapter":"powershell","intent":"diagnose Windows user environment","policy":{"workspace_root":".","capabilities":["powershell.user"],"command":"Get-ChildItem Env:","allow_commands":["pwsh","powershell","powershell.exe"],"max_duration_seconds":120,"max_output_bytes":65536}}'
go run ./cmd/rdev release sign --artifact dist/artifacts/windows-amd64/rdev-host.exe --key .rdev/keys/release-root.json
go run ./cmd/rdev-verify --artifact dist/artifacts/windows-amd64/rdev-host.exe --manifest dist/artifacts/windows-amd64/rdev-host.exe.rdev-release.json --root-public-key release-root:...
go run ./cmd/rdev release create-bundle --dir dist/artifacts/windows-amd64 --artifacts rdev.exe,rdev-host.exe,rdev-verify.exe --require-artifacts rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json
go run ./cmd/rdev release verify-bundle --bundle dist/artifacts/windows-amd64/release-bundle.json --root-public-key release-root:...
go run ./cmd/rdev-verify --bundle dist/artifacts/windows-amd64/release-bundle.json --root-public-key release-root:... --require-artifacts rdev-host.exe,rdev-verify.exe
go run ./cmd/rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234 --enrollment-certificate .rdev/enrollment/host-enrollment.json --renew-enrollment-certificate --fetch-enrollment-revocations --enrollment-root-public-key enrollment-root:... --release-bundle dist/artifacts/darwin-arm64/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev-host,rdev-verify
go run ./cmd/rdev host serve --mode temporary --gateway https://127.0.0.1:8787 --gateway-ca .rdev/tls/gateway-ca.pem --gateway-client-cert .rdev/tls/host-client.pem --gateway-client-key .rdev/tls/host-client-key.pem --ticket-code ABCD-1234
go run ./cmd/rdev release prepare-candidate --source-root . --out dist/release-candidate-windows-amd64 --version v0.1.0 --artifacts dist/artifacts/windows-amd64/rdev.exe,dist/artifacts/windows-amd64/rdev-host.exe,dist/artifacts/windows-amd64/rdev-verify.exe --require-artifacts rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json --gateway-url https://api.example.com/v1
go run ./cmd/rdev release verify-candidate --candidate dist/release-candidate-windows-amd64 --require-artifacts rdev-host.exe,rdev-verify.exe
scripts/github/plan-release.sh --candidate dist/release-candidates/windows-amd64 --repo OWNER/remote-dev-skillkit --require-artifacts rdev-host.exe,rdev-verify.exe
scripts/github/plan-platform-release.sh --platform-candidates dist/release-candidates/platform-candidates.json --repo OWNER/remote-dev-skillkit
scripts/github/audit-project-readiness.sh --repo OWNER/remote-dev-skillkit --out dist/github-project-readiness.json
scripts/github/plan-post-release-install.sh --release-plan dist/release-candidates/github-platform-release-plan/plan.json
scripts/github/verify-post-release-install-plan.sh --plan dist/release-candidates/github-platform-release-plan/post-release-install/post-release-install-plan.json
go run ./cmd/rdev host serve --mode temporary
go run ./cmd/rdev host serve --mode temporary --gateway http://127.0.0.1:8787 --ticket-code ABCD-1234 --once=false --transport long-poll --workspace-lock-store .rdev/workspace-locks --capture-runtime-fixture
go run ./cmd/rdev host install-service --platform macos --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --workspace-lock-store ~/.rdev/host/workspace-locks --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev-host,rdev-verify --plist-out ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host service-status --platform macos --plist ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host service-control --platform macos --action start --plist ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host uninstall-service --platform macos --plist ./com.remote-dev-skillkit.host.plist
go run ./cmd/rdev host install-service --platform linux --label rdev-host.service --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --workspace-lock-store ~/.rdev/host/workspace-locks --release-bundle /opt/rdev/release-bundle.json --release-root-public-key release-root:... --release-require-artifacts rdev-host,rdev-verify --unit-out ./rdev-host.service
go run ./cmd/rdev host service-status --platform linux --label rdev-host.service --unit ./rdev-host.service
go run ./cmd/rdev host service-control --platform linux --action start --label rdev-host.service --unit ./rdev-host.service
go run ./cmd/rdev host uninstall-service --platform linux --label rdev-host.service --unit ./rdev-host.service
go run ./cmd/rdev host install-service --platform windows --label RemoteDevSkillkitHost --binary 'C:\Program Files\rdev\rdev.exe' --gateway https://api.example.com/v1 --ticket-code ABCD-1234 --workspace-lock-store 'C:\ProgramData\rdev\workspace-locks' --release-bundle 'C:\Program Files\rdev\release-bundle.json' --release-root-public-key release-root:... --release-require-artifacts rdev-host.exe,rdev-verify.exe
go run ./cmd/rdev host service-status --platform windows --label RemoteDevSkillkitHost
go run ./cmd/rdev host service-control --platform windows --action start --label RemoteDevSkillkitHost
go run ./cmd/rdev host uninstall-service --platform windows --label RemoteDevSkillkitHost
```

## Design Invariants

- No hidden persistence.
- No UAC bypass or silent privilege escalation.
- No inbound ports on temporary target hosts.
- No raw unrestricted shell for agents.
- Every future remote job must be signed, policy-checked, auditable, and revocable.
- Destructive actions and high-risk capabilities require explicit approval gates.

## Documentation

- [Architecture](docs/architecture/ARCHITECTURE.md)
- [Perfect Ending Solution](docs/architecture/PERFECT_ENDING_SOLUTION.md) — canonical final architecture lock and execution spec
- [Final Closure Blueprint](docs/architecture/FINAL_CLOSURE_BLUEPRINT.md) — concise release-facing closure summary
- [Ultimate Closure Design](docs/architecture/ULTIMATE_CLOSURE_DESIGN.md) — supporting implementation detail and rationale
- [Final System Design](docs/architecture/FINAL_SYSTEM_DESIGN.md) — broad product reasoning record
- [Perfect End-State Architecture](docs/architecture/PERFECT_END_STATE.md)
- [Final Architecture](docs/architecture/FINAL_ARCHITECTURE.md)
- [Project Plan](docs/project/PLAN.md)
- [Acceptance Tests](docs/project/ACCEPTANCE_TESTS.md)
- [GitHub Project Management](docs/project/GITHUB_PROJECT_MANAGEMENT.md)
- [Release Checklist](docs/project/RELEASE_CHECKLIST.md)
- [Roadmap](docs/project/ROADMAP.md)
- [Versioning](docs/project/VERSIONING.md)
- [Threat Model](docs/security/THREAT_MODEL.md)
- [Release Key Lifecycle](docs/security/RELEASE_KEY_LIFECYCLE.md)
- [Bootstrap Design](docs/operations/BOOTSTRAP.md)
- [Acceptance Operations](docs/operations/ACCEPTANCE.md)
- [Adapter SDK](docs/operations/ADAPTER_SDK.md)
- [Development Gateway](docs/operations/DEV_GATEWAY.md)
- [MCP Stdio](docs/operations/MCP_STDIO.md)
- [Skillkit Install](docs/operations/SKILLKIT_INSTALL.md)
- [MCP Tools](mcp/tools.json)

## Sources

This project follows the Agent Skills progressive disclosure model and MCP tool exposure model:

- https://agentskills.io/specification
- https://modelcontextprotocol.io/specification/2025-11-25
- https://modelcontextprotocol.io/specification/draft/server/tools
