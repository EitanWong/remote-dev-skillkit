# Changelog

All notable local development changes are recorded here. The public repository
is maintained at `https://github.com/EitanWong/remote-dev-skillkit`; release
publication still requires explicit operator approval.

## 0.1.17-dev

Current phase: restrictive-network adapter packaging now covers more of the
standard Connection Entry helper paths. Agents can generate verified packages
for SSH tunnels, headscale/Tailscale-compatible mesh, and WireGuard in addition
to Chisel/frpc, which keeps helper setup inside `rdev` metadata instead of
model-authored tunnel scripts. This is package/verifier coverage, not real
cross-network execution evidence.

### Added

- Extended `rdev relay-adapter package` / `rdev relay-adapter verify` to
  support `ssh-tunnel`, `headscale-tailscale`, and `wireguard` adapter kinds.
  The generated packages now expose the matching runner metadata:
  `RDEV_SSH_GATEWAY_URL` / `RDEV_SSH_TUNNEL_START_ARGV_JSON` /
  `RDEV_SSH_INSTALL_ACTION_JSON`,
  `RDEV_MESH_GATEWAY_URL` / `RDEV_MESH_START_ARGV_JSON` /
  `RDEV_MESH_INSTALL_ACTION_JSON`, and
  `RDEV_VPN_GATEWAY_URL` / `RDEV_VPN_START_ARGV_JSON` /
  `RDEV_VPN_INSTALL_ACTION_JSON`.
- Kept non-relay helpers scoped to existing reviewed configurations by default.
  SSH, mesh, and WireGuard packages use `manual-review-required` install-action
  metadata because key creation, profile import, enrollment, route/DNS/firewall
  mutation, and service persistence remain operator-approved actions.
- Updated MCP tool metadata and release smoke so MCP-capable Agents discover
  the new adapter kinds and CI verifies package/verify output for
  `ssh-tunnel`, `headscale-tailscale`, and `wireguard`.

### Remaining Gates

- Run real restrictive-network acceptance for SSH tunnel, headscale/Tailscale,
  WireGuard, Chisel, and frpc paths across clean Windows/macOS/Linux targets,
  then package evidence with `rdev acceptance package-relay-adapter`.
- Add deeper runtime execution support where appropriate for mesh/VPN helper
  startup after explicit operator approval, without weakening the existing
  no-hidden-persistence and no-privilege-bypass rules.

## 0.1.16-dev

Current phase: hosted provider work now has a runtime acceptance evidence
package/verifier, so deployed gateway storage/auth proof can be archived with
the same release-gate discipline as relay evidence. This is still not a claim
that third-party Postgres, object storage, Redis, OIDC, or SAML providers are
bundled.

### Added

- Added `rdev.acceptance-package.hosted-provider-runtime.v1` through
  `rdev acceptance package-hosted-provider-runtime`. The package archives a
  verified `hosted-provider.json`, hosted provider verification output,
  gateway startup transcript, storage verification, hosted auth verification,
  backup evidence, restore evidence, retention policy evidence, role mapping
  and authorization probes, failure-mode evidence, audit transcript, redaction
  metadata, and checksums.
- Added `rdev.acceptance-verification.hosted-provider-runtime-package.v1`
  through `rdev acceptance verify-hosted-provider-runtime-package`.
  Verification requires provider-package checks to pass, storage/auth
  verification to report `ok=true`, role probes to include both an authorized
  and denied decision, failure-mode evidence to pass, required evidence files
  to exist, and package files to avoid private endpoints, secrets, local paths,
  or organization-specific values.
- Updated release smoke to generate and verify a single-node hosted runtime
  smoke package for the built-in `file` storage provider plus
  `hosted-ed25519-jwt` verifier, while keeping the runtime claim scoped as
  `single-node-hosted-smoke`.

### Remaining Gates

- Implement or integrate real durable hosted provider runtime packages for
  external providers such as Postgres, S3-compatible object storage,
  Redis-stream, OIDC/JWKS, and SAML.
- Run deployed hosted gateway acceptance with backup, restore, retention,
  role-mapping, authz denial, and failure-mode evidence, then publish the
  resulting hosted provider runtime acceptance package as release evidence.
- Continue GitHub Release publication/download verification and additional
  relay/mesh/VPN/SSH adapter acceptance work.

## 0.1.15-dev

Current phase: helper/relay work now has a first standard adapter-package
surface for Chisel/frpc relay paths. This moves restrictive-network setup away
from model-authored tunnel scripts and into verifiable `rdev` metadata, but it
is not yet real cross-network acceptance evidence for a deployed relay service.

### Added

- Added `rdev.relay-adapter-package.v1` through
  `rdev relay-adapter package`. The generated package writes
  `relay-adapter.json`, `RELAY_ADAPTER.md`, and `runner.env.example` for
  Chisel or frpc without real relay endpoints, credentials, private IPs, local
  paths, organization IDs, or secrets. It declares the standard
  `RDEV_RELAY_GATEWAY_URL`, `RDEV_RELAY_START_ARGV_JSON`, and
  `RDEV_RELAY_INSTALL_ACTION_JSON` runner surface, helper install template,
  approval boundaries, evidence requirements, and Agent rules.
- Added `rdev.relay-adapter-package-verification.v1` through
  `rdev relay-adapter verify`. Verification checks schema, supported adapter
  kind, safe helper argv, safe dependency install action, file
  checksums/sizes, unlisted files, and no-private-surface hygiene.
- Added MCP tools `rdev.relay_adapter.package` and
  `rdev.relay_adapter.verify` so fresh Agents can discover and verify relay
  adapter packages instead of inventing PowerShell, shell, tunnel, approval, or
  polling scripts.
- Added `rdev.acceptance-package.relay-adapter.v1` through
  `rdev acceptance package-relay-adapter`, plus
  `rdev.acceptance-verification.relay-adapter-package.v1` through
  `rdev acceptance verify-relay-adapter-package`. The package/verifier archives
  the verified relay adapter package, Connection Entry runner result, helper
  transcript, gateway status, host status, connection status, audit transcript,
  checksums, and redacted evidence. It fails unless the runner selected
  `existing-frp-or-chisel-relay` and the connection status reports
  `connected=true`.
- Updated release smoke to generate and verify a Chisel relay adapter package
  and a relay adapter acceptance package, reporting
  `relay_adapter_package_schema`,
  `relay_adapter_package_verification_schema`,
  `relay_adapter_acceptance_package_schema`, and
  `relay_adapter_acceptance_verification_schema`.

### Remaining Gates

- Run real restrictive-network acceptance for Chisel and frpc across clean
  Windows/macOS/Linux targets with a deployed relay endpoint and publish the
  resulting `rdev.acceptance-package.relay-adapter.v1` evidence bundle.
- Add headscale/Tailscale-compatible mesh, WireGuard, and SSH tunnel adapter
  packages with equivalent verification and runner evidence.
- Continue real hosted provider runtime and GitHub Release publication work.

## 0.1.14-dev

Current phase: hosted provider work now has a package and verification surface
instead of only a storage/auth boundary. This is a provider-package contract
and release-smoke gate, not a claim that external databases, object stores, or
identity-provider consoles are bundled.

### Added

- Added `rdev.hosted-provider-package.v1` through
  `rdev hosted-provider package`. The generated package writes
  `hosted-provider.json`, `HOSTED_PROVIDER.md`, and `gateway.env.example`
  without credentials, private endpoints, local paths, organization IDs, or
  provider-specific domains. The package declares the hosted storage provider,
  hosted auth provider, gateway argument template, required environment
  variables, approval boundaries, and Agent rules.
- Added `rdev.hosted-provider-package-verification.v1` through
  `rdev hosted-provider verify`. Verification checks schema, supported
  provider kinds, external-mutation status, gateway args, environment
  declarations, file checksums/sizes, unlisted files, and private-surface
  hygiene.
- Updated release smoke to generate and verify a hosted provider package with
  the built-in `file` storage provider plus `hosted-ed25519-jwt` auth provider,
  reporting `hosted_provider_package_schema` and
  `hosted_provider_package_verification_schema`.

### Remaining Gates

- Implement and accept real durable third-party hosted storage/auth providers
  such as Postgres/S3-compatible/Redis/JWKS/OIDC packages.
- Prove hosted provider operation in a deployed gateway with backup, retention,
  restore, role mapping, and failure-mode evidence.
- Continue real helper/relay adapter acceptance work.

## 0.1.13-dev

Current phase: formal release packaging is moving from platform candidate
directories toward verifiable Connection Entry release archives. This is local
release evidence only; external GitHub Release publication and real download
verification still require explicit operator approval.

### Added

- Added `rdev.connection-entry-release-package.v1` and
  `connection-entry-release.zip` generation to every release candidate prepared
  by `rdev release prepare-candidate`. The archive bundles the platform
  `rdev` artifacts and release manifests under `bin/`, release metadata under
  `release/`, a generic runner manifest template, visible shell/PowerShell
  launchers, human/Agent release notes, and `connection-entry-checksums.txt`.
  It intentionally carries no ticket, gateway, server address, local path,
  credential, or session-specific root data; runtime private values still come
  from signed invite or join-manifest metadata.
- Added release-candidate verification for the Connection Entry release
  archive. `rdev release verify-candidate` now opens
  `connection-entry-release.zip`, validates required entries, schema version,
  no-private-parameter policy, runtime-invite requirement, launcher and
  artifact coverage, archive-internal checksums, manifest file hashes/sizes,
  and private-surface hygiene.
- Updated release smoke so every per-platform candidate must include and verify
  the Connection Entry release archive before GitHub release dry-run planning
  succeeds.

### Remaining Gates

- Publish the signed per-platform Connection Entry archives as GitHub Release
  assets, then verify real downloads with the post-release install plan.
- Run clean Windows/macOS/Linux target acceptance against the published
  archives.
- Continue production hosted provider and real helper/relay adapter work.

## 0.1.12-dev

Current phase: real fresh-agent support sessions are standardized so Agents no
longer improvise gateway startup, Windows bootstrap, `rdev` recovery, ticket
metadata, status watching, or approval polling by hand.

### Added

- Added plain-text foreground support-session files for fresh Agents and
  weaker harnesses. `rdev support-session connect --start` /
  `rdev support-session start` now write `target-handoff.txt` by default and
  expose it as `rdev.support-session-handoff-text-file.v1` at
  `handoff_text_file.path`, containing the exact
  `target_handoff_envelope.full_text` value to forward to the target-side
  human. The foreground watcher also writes `connected-report.txt` by default
  and exposes it as `rdev.support-session-connected-report-file.v1` at
  `connected_report_file.path` when the target connects, so Agents can
  proactively report connection success without parsing long-running terminal
  output or rebuilding messages from JSON fields.
- Added dev-gateway helper asset self-repair. `rdev gateway serve --dev` now
  defaults `--auto-build-rdev-assets=true`; when no explicit helper asset path
  is configured and a valid checkout plus Go are available, it builds the
  Windows/macOS/Linux `rdev` helpers and serves them from `/assets`. This
  hardens the accidental low-level `gateway serve` plus `invite create` path so
  clean Windows targets no longer fall back to "rdev is required" when a fresh
  Agent chooses the wrong entry surface. Explicit `--rdev-assets-dir` and
  platform-specific asset flags still override the auto-built helpers.
- Added `rdev.support-session-target-handoff-envelope.v1` to high-level
  support-session created, connected, and foreground-started payloads. Fresh
  Agents now receive `target_handoff_envelope.full_text`, a complete localized
  plain-text handoff that can be forwarded to the target-side human verbatim,
  plus structured fallbacks for join URL, Windows command, and macOS/Linux
  command. This removes another model-dependent step where Agents previously
  had to reconstruct the human message from `user_handoff.message` and
  `user_handoff.copy_paste`.
- Added `rdev.support-session-fresh-agent-connect-contract.v1` to high-level
  support-session connect, created, and started payloads. Fresh Agents now get a
  compact model-independent contract that says how to recover missing `rdev`,
  when to send the one human handoff, how to wait and report connected status,
  which values not to ask humans for, and which custom PowerShell, shell,
  tunnel, approval, or polling scripts are forbidden. The fresh-agent acceptance
  gate now fails if this contract disappears from the standard one-command
  connection path.
- Added `rdev.support-session-agent-runbook.v1` to support-session handoff,
  prepare, create, start, high-level connect, status, and recovery payloads.
  Fresh Agents now get one machine-readable order of operations for the whole
  visible connection loop: when to run `cli_start_now_command`, what to send to
  the target-side human, how to wait, when to report `connected=true`, how to
  inspect capabilities, and how to recover without choosing lower-level tools or
  writing custom PowerShell, shell, relay, approval, bootstrap, or polling
  scripts.
- Tightened `rdev.support-session-agent-runbook.v1` with
  `standard_entry_tool`, `fallback_entry_tool`, and `low_level_entry_rule`.
  Fresh Agents are now explicitly told to start ordinary "connect this
  computer" requests with `rdev.support_session.connect` /
  `rdev support-session connect`, and not with `rdev.invites.create`,
  `rdev.connection_entry.plan`, package materialization, or hand-written gateway
  setup unless the operator or a high-level recovery payload explicitly asks
  for that lower-level path.
- Added `rdev.support-session-fresh-agent-failure-prevention.v1` inside
  `rdev.support-session-agent-runbook.v1`. The runbook now records the real
  fresh-Agent failure class where an Agent manually combined gateway startup,
  invite creation, Windows bootstrap, background process glue, and approval
  polling, then handed the human a target command that failed with
  `rdev is required`. The contract tells Agents to stop before writing those
  workarounds and recover through `cli_start_now_command`, `ready_file.path`,
  `status_file.path`, `connection_supervision`, or
  `rdev.support_session.prepare` instead.
- Reordered the public MCP tool contract so `rdev.support_session.connect` is
  the first listed tool. This makes the high-level connection entry the most
  discoverable choice for Codex, Claude Code, Hermes, OpenClaw/OpenCode, and
  other MCP-capable Agents, while moving low-level `rdev.invites.create` behind
  the safer fresh-Agent entry.
- Added `rdev.acceptance.bootstrap-self-repair.v1` coverage inside
  `rdev acceptance fresh-agent-support-session`. The local contract gate now
  starts a `httptest` join server with verified helper assets, fetches the join
  page, Windows `bootstrap.ps1`, macOS/Linux `bootstrap.sh`, and asset
  `.sha256` endpoints, and fails if the target-side surface asks humans to
  install `rdev` manually or use `ExecutionPolicy Bypass`.
- Added stable fallback coverage to `rdev acceptance fresh-agent-support-session`.
  The local gate now configures `RDEV_RELAY_GATEWAY_URL` and fails unless the
  high-level handoff auto-selects that gateway, the target command uses the
  relay join URL, `connection_continuity_policy.stable_after_lan_change=true`,
  supervision avoids unnecessary upgrades, and the Agent runbook reports the
  configured stable fallback. This is a contract gate for configured
  hosted/relay/mesh/VPN/SSH paths, not proof of real restrictive-network
  execution.
- Added signed join-manifest gateway candidates for support-session bootstraps.
  Generated Windows/macOS/Linux target commands now pass ordered
  `gateway_url_candidates` into `/join/<ticket>/bootstrap.*`; the gateway signs
  those candidates into `rdev.join-manifest.v1`, and `rdev host serve` selects
  the first reachable signed candidate before registration. This moves
  configured relay/hosted/mesh/VPN/SSH fallback from handoff metadata toward
  target runtime behavior while preserving the existing primary `gateway_url`
  fallback.
- Added post-registration runtime fallback across signed join-manifest gateway
  candidates. When `rdev host serve --transport auto` registers through one
  candidate but that gateway fails before any jobs are processed, the host now
  probes the remaining signed candidates and reruns the normal WSS -> long-poll
  -> poll fallback against the next reachable gateway. Support-session
  continuity and supervision contracts now describe this standard path so
  Agents wait/recover through `rdev` instead of writing relay or polling code.
- Added `rdev.support-session-status-file.v1` metadata and default
  `support-session-status.json` output for foreground
  `rdev support-session connect --start` / `start` sessions. Fresh Agents now
  have a stable local file containing the latest machine-readable foreground
  event, so they can report `event=connected` / `status.connected=true` even
  when a harness cannot stream or parse the long-running terminal output.
  Regression coverage now exercises the watcher from `waiting` through a real
  host registration to a connected status-file event.
- Added `rdev.support-session-gateway-candidate-preflight.v1` to
  support-session prepare, create, start, and high-level connect payloads.
  Fresh Agents now get a machine-readable candidate decision table that
  classifies direct/LAN, same-machine, operator-provided, and configured
  hosted/relay/mesh/VPN/SSH gateway paths, plus the standard next action for
  each candidate. This gives Codex, Claude Code, Hermes, OpenClaw/OpenCode, and
  other MCP-capable Agents the same network-path guidance without writing
  custom PowerShell, shell, relay, tunnel, probe, or polling scripts.
- Added `rdev.support-session-connectivity-helper-preflight.v1` to
  support-session prepare, created, and plan payloads. Fresh Agents can now see
  whether standard SSH, relay, mesh, or VPN helper metadata is configured via
  `RDEV_*_GATEWAY_URL`, `RDEV_*_START_ARGV_JSON`, and
  `RDEV_*_INSTALL_ACTION_JSON`; invalid helper argv such as shell command
  strings, encoded commands, wrong tools, elevation, or `ExecutionPolicy
  Bypass` is reported as structured preflight failure. This keeps restricted
  network decisions in standard `rdev` contracts instead of model-written
  tunnel scripts.
- Added `rdev.support-session-connection-entry-runner-recommendation.v1` to
  support-session created, high-level connect, and started payloads. When a
  simple join link is not enough for durable, long-running, or restrictive
  network work, Agents now receive inline invite JSON plus the standard
  `rdev.connection_entry.plan` / `rdev connection-entry run --dry-run` route
  for the self-contained adaptive runner. This prevents fresh Agents from
  recreating invite, ticket, root, gateway, relay, mesh, VPN, SSH, or checksum
  metadata by hand.
- Added `rdev support-session connect --start` as the preferred one-command
  CLI path for fresh Agents when no hosted/relay gateway is configured. It
  delegates to the visible foreground support-session runner, builds verified
  Windows/macOS/Linux helper assets when a checkout and Go are available,
  writes `ready_file.path`, prints the top-level human handoff, and keeps the
  gateway serving without requiring Agents to hand-run gateway/invite/package
  steps.
- Added `cli_start_now_command` to `rdev.support-session-handoff.v1` and
  `rdev.support-session-connect.v1`. Fresh Agents now receive an explicit
  standard command to run locally before talking to the target human, while
  `foreground_start_command` remains as a compatibility fallback for older
  harnesses.
- Added `rdev.support-session-foreground-feedback.v1` to started support
  sessions and foreground stderr events with schema
  `rdev.support-session-foreground-event.v1`. Agents that keep
  `rdev support-session connect --start` open can now parse
  `rdev support session event: {...}` lines and report `event=connected`
  immediately, with `rdev.support_session.status` remaining the fallback source
  of truth.
- Added `rdev.support-session-handoff.v1` through CLI
  `rdev support-session handoff` and MCP tool `rdev.support_session.handoff`.
  Fresh Agents now get one standard first-contact decision: call
  `rdev.support_session.create` when a gateway URL is already reachable, or run
  the returned `cli_start_now_command` foreground
  `rdev support-session connect --start` command when no gateway is running.
  This reduces model-dependent guessing between prepare, create,
  start, plan, and ad hoc bootstrap scripts.
- Added `rdev.connection-attempt-policy.v1` to
  `rdev.support-session-created.v1`. Agents now receive the ordered target
  Connection Entry URL list plus bounded timeout/retry settings, so they can
  explain connection behavior without writing custom PowerShell, shell, relay,
  or polling glue.
- Added `rdev.support-session-continuity-policy.v1` to
  `rdev.support-session-created.v1`. Agents can now distinguish opportunistic
  LAN/direct paths from sessions that already include configured hosted, relay,
  mesh, VPN, or SSH fallback URLs, then choose standard upgrade/recovery tools
  instead of claiming durable connectivity from a LAN-only first contact.
- Added `rdev.support-session-connection-supervision.v1` to created, started,
  and high-level connect payloads. Fresh Agents now get one machine-readable
  watch/report/upgrade contract after forwarding
  `target_handoff_envelope.full_text`: wait with the returned MCP or CLI status
  command, report
  `connected_next_steps.user_report` when `connected=true`, and use standard
  prepare/runner/Connection Entry upgrade or recovery tools when a LAN-only path
  times out instead of writing polling, relay, bootstrap, or network scripts.
- Added bounded target-side bootstrap attempts: Windows commands use
  `Invoke-RestMethod -TimeoutSec`, and macOS/Linux commands use `curl`
  connect/max-time/retry limits before trying the next Connection Entry URL.
- Added MCP wait parameters to `rdev.support_session.status`
  (`wait`, `timeout_seconds`, and `interval_millis`) so Agents can wait for the
  target host through the standard tool and proactively report
  `connected=true` without writing custom polling loops.
- Added `rdev.support-session-user-handoff.v1` to
  `rdev.support-session-created.v1`. Agents now receive a localized
  `user_handoff.message` plus exact `user_handoff.copy_paste` value to send to
  the human, reducing model-dependent rewrites of the target command.
- Added `user_handoff.auto_target_rule` so unknown target platforms have a
  stable Agent rule: send the join URL first, and use returned platform
  commands only when the human needs a terminal command or cannot open the page.
- Added `rdev.support-session-connection-recovery.v1` to
  `rdev.support-session-status.v1`. Status and wait-timeout payloads now tell
  Agents which standard tools to call next, which human checks are safe, and
  which recovery improvisations are forbidden, so failed first contact does not
  turn into hand-written PowerShell, shell, relay, bootstrap, or approval
  polling code.
- Added `rdev.support-session-connected-next-steps.v1` to
  `rdev.support-session-status.v1`. When `connected=true`, Agents now receive a
  ready user report plus the next standard `rdev.hosts.capabilities` follow-up,
  so they can proactively tell the user the connection is established and
  inspect capabilities before creating the smallest scoped job.
- Added `rdev.support-session-target-bootstrap-requirements.v1` to
  `rdev.support-session-created.v1` and CLI-side
  `rdev.support-session-target-bootstrap-readiness.v1` probing. Fresh Agents can
  detect that an existing gateway lacks verified helper assets before sending a
  Windows/macOS/Linux terminal command, then recover through
  `rdev support-session connect --start` or `rdev support-session prepare --build-assets`
  instead of asking the target user to install `rdev` manually.
- Added `rdev gateway serve --rdev-assets-dir` as a lower-level convenience for
  explicitly managed dev gateways that need to serve all platform helper assets.
- Added `gateway_url_candidates` to `rdev.support-session-prepare.v1`,
  `rdev.support-session-plan.v1`, and
  `rdev.support-session-connectivity-strategy.v1`. `rdev support-session
  prepare`, `plan`, and `start` now derive target-usable gateway URLs from the
  listen address and local private interfaces, preserve explicit gateway
  overrides, and avoid handing remote targets wildcard addresses such as
  `0.0.0.0`.
- Added ordered candidate fallback inside `rdev.support-session-created.v1`
  target commands. The Windows and macOS/Linux one-line commands now try the
  ordered Connection Entry URLs on the target machine before failing, so Agents
  should not write custom PowerShell, shell, relay, ticket substitution,
  bootstrap, or approval-polling glue.
- Added configured gateway fallback discovery for support sessions.
  `RDEV_HOSTED_GATEWAY_URL`, `RDEV_RELAY_GATEWAY_URL`,
  `RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, and
  `RDEV_SSH_GATEWAY_URL` are now appended to `gateway_url_candidates` after
  direct/LAN candidates and before loopback. `rdev support-session create` and
  MCP `rdev.support_session.create` include those candidates in the returned
  target command, so Agents can hand over one command while the target tries
  LAN, hosted, relay, mesh, VPN, or SSH-assisted entry URLs without custom glue.
- Added configured gateway auto-selection for first-contact handoff and create.
  `rdev support-session handoff`, MCP `rdev.support_session.handoff`,
  `rdev support-session create`, and MCP `rdev.support_session.create` can now
  use the first configured `RDEV_*_GATEWAY_URL` when no explicit `gateway_url`
  is supplied. Fresh Agents therefore do not need to ask which gateway URL to
  use when the runtime already has a hosted/relay/mesh/VPN/SSH entry configured.
- Added configured gateway auto-selection for CLI status watching. `rdev
  support-session status --ticket-code ... --wait` can now omit
  `--gateway-url` when a configured `RDEV_*_GATEWAY_URL` exists, so Agents do
  not need to remember or ask for the gateway URL just to report
  `connected=true`.
- Added `watch_connection_status_configured_gateway` to created support-session
  payloads. Agents with a configured `RDEV_*_GATEWAY_URL` now get a ready
  status watcher command that omits `--gateway-url`, while the existing
  `watch_connection_status` remains available for explicit gateway calls.
- Added `rdev support-session prepare` and MCP tool
  `rdev.support_session.prepare` with schema
  `rdev.support-session-prepare.v1`. Fresh Agents can now inspect local `rdev`
  recovery, repo/workdir resolution, Go/Git availability, helper asset
  readiness, gateway defaults, missing inputs, and standard recovery actions
  before writing any setup glue.
- Added `rdev.support-session-connectivity-strategy.v1` to support-session
  prepare/start payloads. The strategy gives Agents a stable adaptive
  connection order: local MCP, direct gateway, LAN/private gateway,
  proxy-aware HTTPS, WSS-to-long-poll-to-poll native fallback, existing SSH
  tunnel, existing frp/Chisel relay, existing headscale/Tailscale mesh,
  existing WireGuard VPN, and operator-provided hosted gateway. It also records
  automatic downgrade/upgrade rules and approval boundaries for privileged,
  persistent, paid, firewall, DNS, route, or cloud changes.
- Added `rdev support-session start` with schema
  `rdev.support-session-started.v1`. When no gateway is running yet, Agents can
  start a visible foreground local gateway, create the attended-temporary ticket,
  auto-build verified Windows/macOS/Linux helper assets when a source checkout
  and Go are available, print the ready target command/join URL/status watcher
  plus asset/readiness reports, and keep the gateway serving without writing ad
  hoc background process or invite glue.
- Added `rdev.support-session-ready-file.v1` metadata to
  `rdev.support-session-started.v1`. `rdev support-session start` now writes the
  same started payload to `support-session-ready.json` by default, or to
  `--ready-file`, before serving. Fresh Agents can read `ready_file.path` when a
	  long-running foreground terminal makes stdout hard to parse, then forward
	  top-level `target_handoff_envelope.full_text` when present, falling back to
	  `user_handoff.message` plus `user_handoff.copy_paste` only for older
	  payloads, without inventing extra scripts or asking the human to assemble
	  ticket/gateway values.
- Added `rdev acceptance fresh-agent-support-session` with schema
  `rdev.acceptance.fresh-agent-support-session.v1`. This local contract gate
  verifies the fresh-Agent connect/handoff/create/start/status flow: one
	  high-level connect entry, one standard selected path, one ready
	  `target_handoff_envelope` plus compatibility `user_handoff`, ready-file fallback, waitable status,
  `connected_next_steps.user_report`, and forbidden ad hoc bootstrap, polling,
  relay, ticket/root/gateway/transport assembly, hidden install, or
  `ExecutionPolicy Bypass` shortcuts.
- Added high-level `rdev support-session connect` and MCP tool
  `rdev.support_session.connect` with schema `rdev.support-session-connect.v1`.
  Fresh Agents can now call one "connect a computer" entry first: when a
	  reachable or configured gateway exists it creates the session and returns the
	  ready `target_handoff_envelope.full_text` plus compatibility
	  `user_handoff`; when no gateway is running it returns the standard
  `cli_start_now_command` foreground `rdev support-session connect --start` command instead of failing or
  forcing model-dependent handoff/create/start decisions.
- Added `rdev support-session create` and MCP tool
  `rdev.support_session.create` with schema
  `rdev.support-session-created.v1`. When a gateway is already reachable,
  Agents can now create the attended-temporary ticket and receive the ready
  target command, join URL, real ticket code, manifest root, and status watcher
  in one payload instead of manually creating an invite and substituting
  placeholders.
- Added `rdev support-session plan` and MCP tool
  `rdev.support_session.plan` with schema `rdev.support-session-plan.v1`.
  The plan returns reviewed argv for preparing a session workdir, building the
  local `rdev`, cross-building Windows/macOS/Linux helper binaries, starting a
  dev gateway with helper asset flags, creating the invite, and giving
  localized target-side one-command instructions.
- Added `rdev support-session status`, `GET /v1/support-session/status`, and
  MCP tool `rdev.support_session.status` with schema
  `rdev.support-session-status.v1`. Agents can now watch a ticket after giving
  the target-side command, receive localized `feedback`/`next_action`, and
  proactively tell the user when `connected=true`.
- Added gateway `/assets/*` serving for configured platform `rdev` helpers plus
  `.sha256` endpoints. `/join/<ticket>/bootstrap.sh` and
  `/join/<ticket>/bootstrap.ps1` now download a verified helper when `rdev` is
  missing on the target host instead of failing with "rdev is required".
- Added scoped attended-temporary auto-approval metadata. `auto_approve` can
  activate the first host only for an explicit attended-temporary Connection
  Entry, with audit events for both registration and auto-approval.
- Added `--auto-approve` to `rdev invite create` and `auto_approve` to
  `rdev.invites.create`.
- Added regression coverage for foreground support-session start payloads,
  support-session create payloads, support-session plans, localized target
  commands, connection supervision, status feedback, verified helper assets,
  scoped auto-approval, and ticket metadata snapshot copying.

### Changed

- Shortened the root README and multilingual quick-start install prompts to a
  compact repository plus full-prompt link. The detailed Agent installation,
  adaptive connection, support-session, fallback, and recovery protocol now
  lives in `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` instead of being
  duplicated across every homepage README.
- `rdev support-session prepare` and `rdev support-session start` now default
  the listen address to `0.0.0.0:8787` while selecting a recommended private/LAN
  or explicit gateway URL for target commands. Loopback remains available but is
  marked same-machine only.
- Updated README, MCP docs, Agent Bootstrap Prompt, i18n quick starts, and core
  Skills so fresh Agents call support-session connect first, then follow the
  returned ready handoff or foreground start route. Prepare is used when readiness is unclear,
  planner access is reserved for review/debug, and the standard status watcher
  remains required before claiming the remote host is connected. Connected
  status now also drives the capability-inspection step before job creation.
- Docs and Skills now explicitly forbid manually combining
  `rdev gateway serve` plus `rdev invite create` for ordinary support sessions,
  because that path can omit verified bootstrap helper assets and recreate the
  clean-Windows "rdev is required" failure.
- Windows join commands no longer use `ExecutionPolicy Bypass`.
- Development gateway plans now carry all configured helper asset paths for
  Windows amd64, macOS arm64/amd64, and Linux amd64/arm64.
- Release smoke now runs the fresh-Agent support-session contract gate before
  release packaging so local regressions in the AI-native connection flow fail
  early.

### Still Requires Real Acceptance

- Real fresh-Agent Codex/Claude Code/Hermes/OpenClaw/OpenCode behavior still
  requires explicit multi-harness acceptance; the local contract gate does not
  prove model behavior in those runtimes.
- Clean Windows/macOS/Linux target-machine acceptance with real release assets.
- Real restrictive-network relay/mesh/frp/Chisel/headscale/WireGuard evidence
  beyond unit/smoke tests.
- Production hosted auth/storage provider integration and production enrollment
  authority drills/key custody/fleet renewal evidence.

## 0.1.11-dev

Current phase: installed Agents now get a machine-readable bootstrap recovery
plan so missing `rdev` binaries, local MCP setup, and remote-host first-contact
decisions do not collapse into multi-question manual troubleshooting.

### Added

- Added `rdev bootstrap agent-plan` with schema
  `rdev.agent-bootstrap-plan.v1`. The plan reports local MCP stdio config,
  detected runtime facts, `rdev` recovery order, Skillkit install steps,
  remote-host defaults, ask-only-when boundaries, forbidden actions, and stable
  report fields.
- Added regression coverage that requires the bootstrap plan to recover from
  missing `rdev` via PATH/current executable, checkout build, Go run fallback,
  safe clone/build, or signed release download after release verification.

### Changed

- Updated the Agent Bootstrap Prompt, README quick start, multilingual quick
  starts, and core Skills so Agents do not stop when `rdev` is missing. They
  must recover or produce the bootstrap plan before asking the user for paths.
- Remote support guidance now defaults company or third-party hosts to visible
  attended-temporary Connection Entries after one authorization confirmation.
  Agents should let Connection Entry metadata and target-side probes detect
  Windows/macOS/Linux instead of asking humans to choose OS, ticket, root,
  gateway, transport, release, checksum, or helper command details.

## 0.1.10-dev

Current phase: Connection Entry moved from package catalog plus script fallback
planning to a real self-contained runner package surface. Relay, mesh, VPN, and
SSH-assisted connectivity are now represented as executable runner paths with
runtime probes and approval boundaries instead of documentation-only guidance.

### Added

- Added `rdev.connection-entry.runner.v1` and
  `rdev.connection-entry.runner-plan.v1` through the new `internal/connectionrunner`
  package.
- Added generated runner artifacts under materialized Connection Entries:
  `connection-entry-runner.json`, a visible platform launcher, and
  `connection-entry-runner-plan.json`.
- Added `rdev connection-entry run --runner-manifest ...` so a target-side
  package can dry-run/probe or start the selected connection path instead of
  making humans assemble ticket/root/gateway/transport flags.
- Added runtime path selection for direct gateway, LAN/private gateway,
  proxy-aware HTTPS, manifest-only native fallback, existing SSH tunnels,
  existing frp/Chisel relay, existing headscale/Tailscale-compatible mesh, and
  existing WireGuard VPN.
- Added gateway override support for configured helper paths through
  `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`, `RDEV_VPN_GATEWAY_URL`, and
  `RDEV_SSH_GATEWAY_URL`.
- Added approved helper startup and dependency install metadata for runner paths:
  `RDEV_*_START_ARGV_JSON` starts already-approved helper argv, and
  `RDEV_*_INSTALL_ACTION_JSON` lets the runner repair missing user/workspace
  helper dependencies before starting the helper.
- Added `rdev deps install` plus `internal/depsinstall` for reviewed
  user/workspace-scoped helper installs. It downloads from an explicit URL,
  verifies SHA-256, unpacks `chisel` or `frpc`, and leaves PATH, services,
  firewall, DNS, routes, drivers, and OS policy unchanged.
- Added regression tests for runner package generation, dry-run path selection,
  manual-action reporting when every path is blocked, helper gateway override
  selection, helper startup, dependency installation, unsafe install rejection,
  and host registration with a signed manifest plus explicit reachable gateway
  override.

### Changed

- `rdev.connection_entry.plan` and `rdev connection-entry plan` now return
  `runner_plan` and can write a self-contained runner package even when a
  platform-specific signed release archive is not yet available.
- Host registration keeps an explicit reachable gateway override after signed
  manifest verification so relay, mesh, VPN, SSH tunnel, or LAN gateway paths
  are not overwritten by the manifest's original gateway URL.
- README, bootstrap docs, and core Skills now describe the runner as the default
  package surface. User/workspace-scoped helper installs can be automated when
  URL and checksum are explicit; credential creation, firewall, DNS, route,
  cloud, paid relay, privileged, service/driver, and persistent changes remain
  approval-gated.

## 0.1.9-dev

Current phase: Connection Entry now carries package-aware OS selection metadata
through invite output, join pages, and signed join manifests so Agents can pick
a target package or visible fallback script without asking humans to assemble
ticket/root/gateway/transport values.

### Added

- Added `rdev.connection-entry.package-catalog.v1` with Windows, macOS, and
  Linux package candidates, architecture hints, planned release-asset status,
  visible `/bootstrap.sh` or `/bootstrap.ps1` fallbacks, and required release
  inputs for signed packages.
- Added `connection_entry.package_catalog` to Agent invite output and
  `package_catalog` to signed `rdev.join-manifest.v1` so package selection
  metadata is covered by manifest signature verification.
- Added a package-aware join page section with a recommended entry and visible
  Agent Package Catalog JSON for browser or Agent OS selection.
- Added regression tests for invite, MCP, CLI, join page, manifest endpoint,
  and package-catalog tampering.

### Changed

- Updated README, bootstrap docs, MCP docs, Agent Bootstrap Prompt, and core
  Skills so Agents read the package catalog, select candidates from target
  OS/architecture probes, and use visible script fallback when release package
  assets are not published yet.
- Kept package status honest: real signed per-platform Connection Entry
  packages still require published release assets, checksums, signed release
  bundles, and release roots.

## 0.1.8-dev

Current phase: Connection Entry now carries an explicit target-selection policy
so Agents choose durable managed mode for owned machines and attended temporary
mode for third-party or one-off machines without asking humans to assemble
low-level connection flags.

### Added

- Added `connection_entry_plan.target_selection_policy` with
  `rdev.target-selection-policy.v1`, owned/third-party signals, default modes,
  Agent rules, and ask-when boundaries for ambiguous ownership or persistence.
- Added CLI and MCP regression tests that require invite output to expose the
  target-selection policy and keep raw ticket/root/gateway/transport/release
  choices out of the target-side handoff.

### Changed

- Updated README, Agent Bootstrap Prompt, operations docs, multilingual quick
  starts, and the core remote-vibe-coding Skill so all scenarios start from an
  Agent-created Connection Entry, not a human-assembled ticket/root/gateway
  command.
- Moved the README remote-session flow toward plain-language Agent requests and
  kept low-level invite commands as implementation detail for agents and
  developer docs.

## 0.1.7-dev

Current phase: Connection Entry is now the universal remote-host handoff
contract. Agents must create an invite, materialize it into a Connection Entry
Package Plan, and give the target side only a link, visible script, or signed
package instead of exposing ticket/root/gateway/transport/release/checksum
assembly.

### Added

- Added machine-readable Connection Entry handoff fields to invite output:
  `connection_entry.handoff_name`, `connection_entry.handoff_contract`,
  `connection_entry_plan.package_plan_schema`, and
  `connection_entry_plan.required_agent_flow`.
- Added materialization-plan contract fields:
  `connection_entry_name`, `entry_package_plan_schema`, and
  `handoff_contract`.
- Added tests that require MCP and CLI invite/materialization output to preserve
  the universal Connection Entry handoff contract.

### Changed

- Updated README, Agent Bootstrap Prompt, operations docs, i18n quick starts,
  and core Skills to treat Connection Entry as the only target-side handoff name
  for owned managed hosts, third-party temporary support, LAN, hosted, relay,
  mesh, SSH, and VPN-assisted paths.
- Updated MCP tool descriptions and i18n audits so public tooling keeps
  low-level connection parameters in Agent/tool metadata and requires invite
  materialization before target-side instructions.

## 0.1.6-dev

Current phase: owned managed Connection Entry package plans are implemented so
agents can materialize long-running personal or fleet machines into reviewed
macOS LaunchAgent, Linux systemd user-service, or Windows Service plans through
the same universal Connection Entry Package Plan surface.

### Added

- Added owned managed-service materialization to `rdev.connection_entry.plan`
  and `rdev connection-entry plan` for `target_os=darwin`, `linux`, and
  `windows`.
- Added managed-service inputs for Agent tooling:
  `managed_binary_path`, `release_bundle_path`, `managed_service_name`,
  `managed_service_label`, and `managed_unit_name`.
- Added generic `entry_package_plan` wrappers for
  `managed-mac-service-plan`, `linux-managed-service-plan`, and
  `windows-managed-service-plan` so owned durable hosts use the same package
  surface as temporary support hosts.

### Changed

- Managed Connection Entries now report missing local binary, release bundle,
  or release root inputs through `missing_inputs` instead of asking humans to
  assemble service-manager commands or raw connection flags.
- Managed service materialization generates reviewed plans and service files
  only; it does not start, install, persist, or uninstall services without the
  explicit service-control steps already present in the acceptance plans.

## 0.1.5-dev

Current phase: universal Connection Entry materialization is implemented so
agents can turn invites into target-side links, visible scripts, or signed
package plans without handing humans ticket, manifest root, gateway, transport,
release, or checksum parameters.

### Added

- Added `rdev.connection_entry.plan` MCP tool and `rdev connection-entry plan`
  CLI command to materialize any invite into
  `rdev.connection-entry.materialization-plan.v1`. The MCP tool also accepts an
  `out_dir` plus platform release inputs so agents can generate
  `CONNECTION_ENTRY.md`, `connection-entry-plan.json`, and launcher/package
  planning files without dropping down to manual parameter assembly.
- Added generic `rdev.connection-entry.package-plan.v1` output as the universal
  Connection Entry Package Plan surface. It separates target-side human surfaces
  from Agent-only ticket, gateway, manifest root, transport, release, and
  checksum metadata, and currently wraps the Windows temporary acceptance plan
  when release inputs are available.
- Added mode-decision fields for Connection Entry materialization so agents can
  consistently choose `managed` for operator-owned long-running machines and
  `attended-temporary` for third-party or one-off repair machines.

### Changed

- Changed README, multilingual quick-start prompts, Agent Bootstrap Prompt, MCP
  docs, bootstrap docs, and core Skills to say all new remote-host onboarding
  should produce a Connection Entry link, visible script, or signed package
  instead of handing humans raw ticket/root/gateway/transport values.
- Changed remote-support guidance so missing package or release inputs are
  reported to the operator through `missing_inputs`; target-side humans are not
  asked to assemble low-level connection parameters.

## 0.1.4-dev

Current phase: Agent-first remote session invites are implemented so AI agents
can create a target-host connection plan without hand-assembling ticket,
manifest, transport, and approval steps.

### Added

- Added `rdev.agent-invite.v1` invite plans with gateway URL, join URL,
  manifest URL, short-lived ticket, WSS host command, human next actions, Agent
  next actions, and MCP tool hints.
- Added `rdev invite create` for creating a real gateway-backed invite through
  `POST /v1/tickets` without printing operator tokens.
- Added MCP tool `rdev.invites.create` so Codex, Claude Code, Hermes,
  OpenClaw/OpenCode, and generic MCP agents can start the remote-host workflow
  directly from conversation.
- Added `--transport auto` for host runtime and invite plans. Auto transport
  tries WSS first, falls back to HTTPS long-poll, then falls back to short
  polling for restrictive networks.
- Added invite `transport_plan`, `fallback_commands`, and
  `connectivity_checks` fields so agents can reason about NAT, firewalls,
  proxies, TLS interception, VPN requirements, and relay/mesh escalation without
  guessing.
- Added invite `connection_plan` with implemented native paths for outbound
  WSS/mTLS, HTTPS long-poll, HTTPS short-poll, and LAN-reachable gateways, plus
  explicit agent-managed entries for HTTPS relay, mesh/VPN, and SSH tunnel
  scenarios when those paths are configured or discoverable.
- Added invite `discovery_plan` so agents can inspect interfaces, routes,
  proxy settings, SSH config, mesh tooling, and scoped LAN/private-network
  reachability before asking the user.
- Added `--network-scope` / `network_scope` hints for invites so agents can
  distinguish auto, internet, LAN, relay, mesh, and SSH-assisted contexts.
- Added `rdev.agent-authority.v1` with `standard` and default `max-control`
  profiles. The max-control profile lets an approved remote host act as the
  Agent's field workstation for heuristic discovery and downstream authorized
  device control.
- Added capability vocabulary for `network.discovery.scoped`,
  `network.probe.lan`, `relay.use`, `mesh.use`, `ssh.tunnel`, and
  `downstream.control.scoped`.
- Added `rdev.connection-entry.v1` inside invite output so agents can send one
  target-side entry URL, one-line platform commands, human steps, agent
  follow-up actions, and revocation instructions without hand-assembling
  bootstrap text.
- Added `rdev.connection-entry-plan.v1` inside invite output so agents can use
  the same universal entry for temporary third-party support and owned managed
  workstations. The plan carries mode selection, signed self-contained package
  guidance, platform `rdev` binary, signed release bundle, pinned
  release/manifest roots, join manifest URL, visible launcher, transport
  fallback, privilege strategy, and evidence requirements as machine-readable
  instructions.
- Added development gateway join resources at `/join/<ticket>`,
  `/join/<ticket>/bootstrap.sh`, and `/join/<ticket>/bootstrap.ps1` for visible
  attended target-host startup with `--transport auto`.
- Added `rdev.host-context-plan.v1` to invite output so agents keep remote
  environment, project structure, requirement notes, logs, transcripts, and
  large evidence on the host while loading only indexed slices or artifact
  references into Agent server context.
- Added `rdev.agent-provisioning-plan.v1` to invite output so agents can
  detect target-host skills, MCP tools, adapters, runtimes, package managers,
  project dependencies, framework paths, proxies, and permissions, then install
  verified user/workspace-scoped missing pieces when policy allows.
- Added `rdev.agent-collaboration-plan.v1` to invite output for discovering and
  using local or configured peer Agents, including A2A-style Agent Card peers,
  MCP stdio peers, and local Agent CLIs for bounded collaborative subtasks.
- Added `rdev.localization-plan.v1` to invite output so agents can detect the
  target-side language, localize user-facing surfaces, and preserve
  stable protocol keys, commands, paths, schemas, and checksums.
- Added language-aware join page matching through `?lang=` and
  `Accept-Language` for the repository's supported quick-start languages.
- Added `rdev.managed-development-plan.v1` to invite output for owned
  long-running developer workstations, covering managed mode, service-backed
  lifecycle plans, reconnect proof, workspace locks, release gates, enrollment
  renewal, revocation refresh, and evidence bundles.
- Added adaptive tunnel-selection guidance to the Agent Bootstrap Prompt,
  README quick starts, generated Skillkit install docs, and install-plan
  scripts. Agents now probe network reachability, proxy/DNS state,
  NAT/firewall/CGNAT constraints, SSH config, and installed tunnel/mesh tools
  before choosing a remote-host connection mode.
- Added an open-source/free-first relay policy for restrictive networks:
  agents should prefer existing or suitable frp, Chisel, headscale, or
  WireGuard paths before asking for paid hosted relays, and must ask before
  privileged, persistent, firewall, DNS, cloud, or security-policy changes.
- Added `rdev update check` and `rdev update plan` so installed agents and
  managed hosts can discover the latest GitHub Release, compare it with the
  current version, select the matching platform archive, and produce
  reviewable download/checksum/release-bundle verification steps before any
  binary replacement.
- Added Agent engineering discipline to contribution and Skill guidance:
  read before guessing, confirm before ambiguous execution, reuse before
  creating, verify before claiming, preserve architecture, admit unknowns, and
  refactor cautiously.
- Added clarification-first response discipline to contribution and Skill
  guidance: for ambiguous or high-impact work, ask one human question at a
  time until the real goal, constraints, and success criteria are about 95%
  clear before giving the final plan or answer.
- Added deep reasoning discipline to contribution and Skill guidance:
  requirement decomposition, multiple hypotheses, assumption testing,
  risk-scaled analysis, progress tracking, and concise auditable reasoning
  summaries without exposing private chain-of-thought.
- Added path/configuration neutrality rules to Skill guidance so agents resolve
  checkout paths, workspace roots, framework directories, gateways, repos, and
  release artifacts from manifests, probes, active configuration, or explicit
  confirmation instead of hardcoded examples.
- Added progressive-disclosure references for the core remote-vibe-coding Skill:
  connectivity/managed hosts, enrollment lifecycle, adapter jobs, and
  release/acceptance details now load only when the task needs them.
- Added Skill runtime memory guidance for dynamically retaining discovered
  environment facts, configuration paths, host capabilities, adapter/tool
  availability, and operator preferences outside the public repository.
- Added runtime-memory and stable-output expectations to host triage, remote job
  review, and safe remote support Skills so Agent/Harness runs can reuse safe
  discoveries and summarize results consistently.
- Added `agents/openai.yaml` metadata for all shipped Skills so Codex/Harness
  UIs can show clear names, concise descriptions, and default prompts.
- Added Skillkit verification for `agents/openai.yaml` so shipped Skills must
  keep useful UI/Harness metadata and `$skill-name` default prompts.
- Added `scripts/audit-skills.sh` and wired it into `./scripts/check.sh` so
  Skill structure, frontmatter, metadata, linked references, hidden files, and
  long-reference contents are checked continuously.
- Added regression tests for the Skills quality audit covering valid Skill
  trees, missing Agent metadata, broken progressive-disclosure references,
  hidden files, and long references without contents indexes.
- Added release-smoke verification that exported Skillkit bundles pass
  `rdev skillkit verify --bundle` and include passing `skill_agents_metadata`
  checks before install-plan validation.
- Added an Agent Bootstrap Prompt so developers can copy one prompt into Codex,
  Claude Code, Hermes, OpenClaw/OpenCode, or a generic MCP agent and have the
  agent probe, install, verify, and prepare MCP integration for Remote Dev
  Skillkit.

### Changed

- Updated the remote-vibe-coding skill to make invite creation the preferred
  first step when no suitable host is already active.
- Updated README, MCP stdio, and dev-gateway docs to describe the AI-native
  flow: the human runs only the generated target-host command and approves when
  policy requires it; the Agent handles discovery, waiting, job dispatch,
  status, and evidence.
- Changed invite defaults from WSS-only to auto transport for maximum
  field-connectivity while preserving WSS as the first candidate.
- Changed invite host commands, transport fallback commands, MCP invite output,
  HTTP ticket responses, and join bootstrap scripts to include the pinned
  manifest root public key automatically, removing a manual trust-root copy step
  from Windows/LAN setup.
- Changed host registration gating so direct `--gateway --ticket-code` remains
  local-dev only, while signed `--manifest-url` plus
  `--manifest-root-public-key` can register through HTTPS gateways or routable
  private/LAN HTTP gateways.
- Changed remote-session guidance to prefer `connection_entry.entry_url` or a
  signed connection entry package before asking the target-side user to install
  prerequisites or assemble command flags.
- Changed the Agent Bootstrap Prompt and multilingual quick-start copy-paste
  prompt to require connection entries for remote hosts instead of human
  assembly of ticket, root, gateway, or transport values.
- Documented that agents may scan scoped local/private networks and use
  configured relay, mesh, and SSH paths automatically, while those paths remain
  connectivity choices only and never replace rdev target consent, host
  approval, signed jobs, local policy checks, or evidence.
- Documented max-control behavior for using the remote host as a field control
  point over reachable downstream devices while keeping evidence requirements
  and task-intent boundaries explicit.
- Updated README, MCP stdio, bootstrap, and remote-vibe-coding docs to prefer
  the universal one-link connection entry flow for all new target hosts while
  preserving visible consent, auditability, and revocation.
- Documented host-context-first progressive disclosure as the standard
  AI-native context model for remote sessions.
- Documented adaptive host-local provisioning for skills, MCP tools,
  dependencies, and adapter helpers with approval gates for elevated,
  system-wide, credential, service, firewall, external, paid, publish, deploy,
  push, or persistent security-policy changes.
- Documented peer-Agent collaboration as a bounded adapter/collaborator path:
  A2A/MCP/local Agent work must still use signed jobs, host policy, workspace
  locks, redaction, approvals, audit, and evidence.
- Documented target-host language matching for skills, MCP summaries, bootstrap
  instructions, approvals, job status, and evidence summaries.
- Documented the long-running owned-workstation workflow for recurring Agent
  development work on the operator's own Mac, Windows PC, or Linux machine.
- Documented the project's anti-guesswork contribution standard for Agent-led
  development.
- Updated the remote-vibe-coding skill so agents continue one-question-at-a-time
  clarification before final planning or execution when the request is unclear
  or high-impact.
- Updated the remote-vibe-coding skill to internalize stronger reasoning
  practices for high-risk remote development work while keeping public outputs
  concise, evidence-based, and safe for open-source use.
- Reduced path-coupled command examples in Skill guidance and reframed
  placeholders as values that must be discovered or confirmed for the current
  run.
- Reworked the core remote-vibe-coding Skill into a shorter routing-oriented
  entrypoint with non-negotiables, first move, core flow, reference selection,
  and completion evidence.
- Updated the remote-vibe-coding Skill flow so agents read safe runtime memory
  before repeating probes and write scoped, redacted memory after useful
  discoveries.
- Tightened supporting Skills for professional Agent/Harness execution:
  refreshed trigger descriptions, read-before-probe memory usage, scoped memory
  writes, and stable report fields.
- Improved Skill discoverability after installation without adding icon or
  asset dependencies.
- Promoted Skill metadata from documentation nicety to release-verified
  contract.
- Added a contents index to the long runtime-memory reference so progressive
  disclosure remains scan-friendly.
- Made the Skills quality audit reusable against temporary Skill roots so CI and
  future Harness tests can validate isolated fixtures instead of only the live
  repository tree.
- Promoted Skill metadata verification into the release-smoke acceptance path,
  making installability, Harness metadata, and bundle verification one
  continuous gate.
- Updated README and Skillkit install docs to surface the agent-facing install
  path before manual checkout commands.
- Synchronized all multilingual quick starts with the Agent Bootstrap Prompt
  path and extended the i18n audit so translations must keep the copy-paste
  agent install entry.
- Reworked the short Agent install prompt into a bootstrap launcher that tells
  agents to clone or update the repository first, read the full local
  `AGENT_BOOTSTRAP_PROMPT.md`, then continue with the verified installation
  workflow.
- Made the Agent install path local-first: personal-computer installs now
  default to local MCP stdio with `rdev mcp serve`, hosted gateway URLs are
  optional metadata, and agents must probe available connection modes before
  choosing local dev, LAN, hosted, relay/mesh/VPN, or SSH-tunnel paths.

### Verification

- Targeted invite, MCP, and tool-contract tests pass locally.
- Host runtime auto-transport fallback is covered by a test that fails WSS and
  completes the job over HTTPS long-poll.
- Join page and platform bootstrap script handlers are covered by HTTP tests.

## 0.1.3-dev

Current phase: production WSS/mTLS host transport, hosted storage/auth
foundation, and enrollment authority lifecycle evidence are implemented for
local release validation. External GitHub publication and real
Windows/Linux/macOS acceptance runs still require explicit operator approval.

### Added

- Added WSS host job transport through `rdev host serve --transport wss` and
  `GET /v1/ws/hosts/{host_id}`, including WebSocket acknowledgements for job
  completion, failure, and artifact persistence.
- Wired WSS over the same gateway TLS/mTLS client configuration used by HTTPS
  registration, trust refresh, polling, completion, failure, and artifact
  upload.
- Added gateway state-store abstraction plus `--storage-provider file`,
  `--storage-path`, and `rdev gateway storage verify` so hosted storage
  providers have a clean implementation boundary.
- Added hosted operator auth via generic EdDSA JWT verifier configuration
  (`rdev.hosted-operator-auth.v1`) and `rdev operator-auth verify-hosted`,
  with issuer, audience, key id, expiry, not-before, and role-claim checks.
- Added enrollment authority lifecycle evidence commands under
  `rdev enrollment lifecycle`: key custody records, fleet renewal plans, and
  emergency drill evidence.

### Changed

- `rdev host serve` no longer describes foreground registration as lacking WSS
  transport when a gateway and ticket are supplied.
- Gateway persistence is now routed through a state-store interface while
  preserving the existing JSON snapshot provider.

### Verification

- Targeted WSS, WSS/mTLS, hosted auth, storage provider, and enrollment
  lifecycle tests pass.

### Remaining Gates

- Real clean Windows temporary acceptance evidence.
- Real managed Mac LaunchAgent acceptance evidence.
- Real Linux systemd reboot/reconnect acceptance evidence.
- Real Windows Service install/start/reconnect/stop/uninstall acceptance
  evidence.
- Optional third-party hosted storage provider plugins beyond the built-in
  file-backed provider boundary.
- Final external GitHub publication after explicit approval.

## 0.1.2-dev

Enrollment authority lifecycle operations pass.

### Added

- Added `rdev.enrollment-key-custody.v1` records for key custody owner,
  provider, rotation window, dual-control, and break-glass requirements.
- Added `rdev.enrollment-fleet-renewal-plan.v1` for renewal windows, expired
  certificates, revoked certificates, and per-host renewal decisions.
- Added `rdev.enrollment-emergency-drill.v1` for emergency revocation drill
  evidence without leaking local paths.

## 0.1.1-dev

Hosted storage/auth foundation pass.

### Added

- Added gateway state-store provider interface and built-in file provider.
- Added `rdev gateway storage verify`.
- Added hosted operator auth configuration and EdDSA JWT role verifier.

## 0.1.0-dev

Current phase: local safety kernel, open-source packaging, public-readiness
hardening, and local production-like operator auth. This stage did not yet
claim production WSS transport, hosted storage/auth foundation, enrollment
lifecycle evidence, or real Windows/Linux/macOS acceptance completion.

### Added

- Added local operator auth through `rdev.operator-auth.v1`,
  `rdev operator-auth init`, `rdev operator-auth verify`, and
  `rdev gateway serve --operator-auth`.
- Added hashed token storage and `admin`, `operator`, `issuer`, and `auditor`
  role gates for control-plane and enrollment endpoints.
- Added operator-token support for hosted enrollment issuance, renewal, host
  near-expiry renewal, and hosted revocation refresh.

### Changed

- Removed the older enrollment-specific standalone bearer-token surface from the
  current standard; hosted enrollment authority calls now use operator auth plus
  `--operator-token-file`.
- Split this changelog into staged pre-release versions so the path from
  `0.0.1-dev` to `0.1.0-dev` is auditable.

### Verification

- `go test ./...` passes.
- `go vet ./...` passes.
- `./scripts/check.sh` passes.
- `./scripts/ci/release-smoke.sh` passes with `external_mutation=false`.
- Public-surface audits pass, including injected private-pattern checks.

### Remaining Gates

- Real clean Windows temporary acceptance evidence.
- Real managed Mac LaunchAgent acceptance evidence.
- Real Linux systemd reboot/reconnect acceptance evidence.
- Real Windows Service install/start/reconnect/stop/uninstall acceptance
  evidence.
- Production WSS/mTLS host transport.
- Full production enrollment authority lifecycle beyond the local operator-role
  foundation: key custody, fleet renewal policy, emergency drills, and hosted
  authority operations.
- Production hosted storage/authentication beyond local operator-auth preflight
  and final external GitHub publication after explicit approval.

## 0.0.9-dev

Public-readiness and open-source packaging pass.

### Added

- Reworked the root README into a concise public homepage with purpose,
  highlights, quick install, local demo, status, docs map, and Apache-2.0 link.
- Added multilingual quick starts for 10 languages plus
  `scripts/audit-i18n-quickstarts.sh`.
- Added local GitHub project readiness auditing for docs, templates, CI,
  release scripts, project bootstrap dry-runs, public-surface hygiene, and
  multilingual quick-start coverage.

### Changed

- Generalized public examples to placeholders such as `example.com` and example
  user paths.
- Removed the untracked empty top-level `tools/` directory from the project
  shape; the tracked tool contract is `mcp/tools.json`.

## 0.0.8-dev

Release and supply-chain evidence pass.

### Added

- Added local release artifact builds, signed release bundles, release
  candidates, and platform candidate automation.
- Added GitHub Release dry-run planning and post-release install verification
  planning without external mutation.
- Added SPDX 2.3 SBOM generation and `rdev.release-provenance.v1`
  provenance attestations.

### Changed

- Made generated release manifests and candidates use package-relative public
  paths so local output directories do not leak into release evidence.

## 0.0.7-dev

Skillkit and mainstream Agent framework install pass.

### Added

- Added Skillkit export, verification, install-plan generation,
  install-plan verification, and dry-run-by-default direct install.
- Added support surfaces for Codex, Claude Code, Hermes, OpenClaw/OpenCode,
  OpenCode, and generic MCP agents.
- Added machine-readable adaptive configuration contracts so agents probe OS,
  shell, service manager, gateway, workspace, adapters, framework paths, and
  permissions before acting.

## 0.0.6-dev

Adapters and host runtime pass.

### Added

- Added shell, PowerShell, Codex, Claude Code, and ACP/acpx hostrunner adapter
  paths with bounded execution, redaction, cancellation evidence, approval
  preflight, and adapter conformance surfaces.
- Added `pkg/adapterkit` plus adapter result, lifecycle, cancellation, and
  runtime fixture verification through CLI and MCP tools.
- Added managed host service planning and evidence packaging surfaces for
  macOS LaunchAgent, Linux systemd user units, Windows Service plans, and clean
  Windows temporary acceptance.

## 0.0.5-dev

Enrollment authority and trust lifecycle pass.

### Added

- Added host enrollment certificates, signed enrollment revocation lists,
  hosted issuance, hosted renewal, local renewal, revocation refresh, and
  host-side pre-registration revocation checks.
- Added trust lifecycle commands for initializing, rotating, revoking, and
  verifying signed trust bundles.
- Added host-bound trust bundle update checks for managed host refresh.

## 0.0.4-dev

Security kernel pass.

### Added

- Added signed job envelopes, host identity proofs, nonce replay protection,
  approval gates, approval token consumption, trust pins, and workspace locks.
- Added host-side denial artifacts and approval-required artifacts before
  adapter execution.
- Added evidence bundle export and hash-chained audit verification.

## 0.0.3-dev

Gateway and host loop pass.

### Added

- Added local dev gateway HTTP APIs for tickets, hosts, jobs, artifacts, audit
  events, and trust bundles.
- Added development HTTPS and mTLS listener/client preflight for local gateway
  and host flows.
- Added restart-safe development gateway state snapshots when `--state` is used
  with a persistent signing key.
- Added foreground temporary host registration, polling, job completion,
  failure reporting, and artifact upload.

## 0.0.2-dev

CLI, MCP, and local demo pass.

### Added

- Implemented the `rdev` CLI plus thin `rdev-host`, `rdev-gateway`,
  `rdev-mcp`, and `rdev-verify` entrypoints.
- Added MCP stdio initialize, tools/list, and tools/call support.
- Added policy explanation, local preview ticket creation, and local demo flow.
- Added JSONL audit storage foundation.

## 0.0.1-dev

Project foundation.

### Added

- Created project skeleton.
- Added initial CLI, docs, MCP contract, and Agent Skills drafts.
- Added architecture, roadmap, versioning, threat model, and initial task board.
- Added basic tests for CLI, MCP contracts, and temporary-mode capability
  defaults.
