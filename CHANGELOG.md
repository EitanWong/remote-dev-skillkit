# Changelog

All notable local development changes are recorded here. The public repository
is maintained at `https://github.com/EitanWong/remote-dev-skillkit`; release
publication still requires explicit operator approval.

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
