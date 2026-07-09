# Changelog

All notable local development changes are recorded here. The public repository
is maintained at `https://github.com/EitanWong/remote-dev-skillkit`; release
publication still requires explicit operator authorization.

## 0.1.43-dev

Current phase: make remote-control adapter validation Agent-native instead of
hand-written task probes.

### Added

- Added optional remote-control probes to `rdev support-session smoke-test`
  through `--remote-control`. The switch adds only low-risk standard adapter
  checks: file adapter `list` and desktop adapter `window.inspect`.
- Added MCP parity for `rdev.support_session.smoke_test` via
  `remote_control=true`, including `remote_control_requested` and
  `remote_control_probe_count` report fields.
- Added tool-contract coverage and regenerated `mcp/tools.json` so MCP-capable
  Agents can discover the remote-control smoke-test switch without guessing.

### Changed

- Updated source skills so Agents keep baseline smoke tests light by default and
  use the remote-control smoke-test switch only when validating native
  `rdev.files.*` / `rdev.desktop.*` adapter surfaces.

## 0.1.42-dev

Current phase: close remote-control surface gaps exposed by the first
AI-native remote-control kernel pass.

### Added

- Added native `file.delete` support across the file adapter, policy
  explanations, hostrunner capability checks, CLI, MCP tools, generated
  `mcp/tools.json`, and policy templates. Deletes are scoped to
  `write_scope`, require `file.delete` authorization, and use non-recursive removal.
- Added standard clipboard read/write task surfaces across CLI and MCP:
  `rdev desktop clipboard`, `rdev.desktop.clipboard`, `clipboard.read`, and
  `clipboard.write`.
- Added Windows native clipboard backend using Win32 Unicode clipboard APIs.
  Non-Windows desktop backends continue to fail closed with
  `desktop_session_unavailable`.

### Changed

- Updated README, multilingual quick starts, source skills, and exported skills
  so Agents prefer `rdev.files.*` / `rdev.desktop.*` for file delete and
  clipboard work instead of hand-written OS scripts.

## 0.1.41-dev

Current phase: AI-native remote-control kernel, native-first and Windows-first.
This slice adds first-class file and desktop control surfaces to the existing
signed task, authorization, audit, and evidence pipeline so Agents stop writing
ad-hoc GUI/file scripts for ordinary remote-control tasks.

### Added

- Added remote-control capability taxonomy for file transfer, app/URL control,
  screenshots, PNG frame recording, window inspection/focus/move, keyboard,
  mouse, clipboard, and unattended-access policy gates.
- Added built-in `file` adapter with workspace-scoped list/read/download and
  write/upload actions, including canonical path, symlink escape, write-scope,
  SHA-256, UTF-8/base64, and truncation evidence.
- Added built-in `desktop` adapter with a Windows native backend for
  `EnumWindows`, `SetForegroundWindow`, `SetWindowPos`, `WM_CLOSE`,
  `ShellExecuteW`, `SendInput`, GDI/BitBlt screenshot capture, and PNG frame
  recording. Non-Windows platforms fail closed with
  `desktop_session_unavailable` until native backends are added.
- Added Agent-facing CLI surfaces: `rdev files ...` and `rdev desktop ...`.
- Added Agent-facing MCP tools: `rdev.files.*` and `rdev.desktop.*`.
- Added policy-template support for `file.transfer.*`, `screen.*`,
  `window.*`, `input.*`, `app.*`, `url.open`, clipboard, and
  `unattended.access` capabilities.
- Added unit coverage for file adapter path boundaries, desktop fail-closed
  behavior, hostrunner adapter dispatch, policy templates, CLI/MCP task
  wrappers, and remote-control policy gates.

### Changed

- Hostrunner now supports `file` and `desktop` adapters as first-class signed
  task targets with adapter-specific capability checks and result schemas.
- HTTP task creation now prechecks `file` and `desktop` policies before queuing
  tasks, matching shell/PowerShell behavior.
- `safe-remote-support` and `remote-vibe-coding` now describe Remote Dev
  Skillkit as an AI Agent remote-control kernel and require native
  `rdev.files.*` / `rdev.desktop.*` surfaces before any hand-written scripts.

### Security

- Desktop GUI/input/screenshot/record/app/URL/clipboard/unattended actions are
  authorization-gated. GUI control requires an unlocked interactive user session and
  does not bypass lock screens, OS privacy prompts, MDM, UAC, sudo, or
  enterprise policy.

## 0.1.40-dev

Current phase: remote-control-style support entry hardening from real Agent
support transcripts. This slice turns temporary support from a task-scoped
ticket mental model into a stable Agent-facing Support Device Entry while
preserving visible, attended, revocable target-side consent.

### Added

- Added `rdev.support-session-remote-control-entry.v1` to support-session
  created/status/report/smoke-test payloads. Agents now receive a
  DeviceID-style `support_device_id`, a ticket-scoped `session_passcode`, and
  `explicit_disconnect_required=true` instead of treating ticket/root/gateway
  internals as the user-facing connection handle.
- Added visible host-side remote-control connector output from `rdev host
  serve`, showing Device ID, Session Password, and the keep-open rule in the
  target terminal.
- Added target-local persistent identity stores to generated Windows and
  macOS/Linux bootstrap scripts so the same target connector can derive a
  stable host identity across restarts instead of becoming a random new device
  every time the PowerShell or shell window is reopened.

### Changed

- Updated support-session connected reports, CLI report/smoke-test, MCP
  report/smoke-test, README, multilingual README pages, and core remote
  skills to describe Remote Dev Skillkit as an AI-native remote-control
  connector: temporary customer support remains visible and attended, but the
  Agent must not disconnect it after task completion unless the operator
  explicitly asks.
- Clarified the managed-service boundary: third-party temporary support stays
  visible and non-service by default; operator-owned recurring machines should
  use a reviewed managed-service upgrade only after explicit ownership and
  persistence authorization plus a stable gateway.

### Fixed

- Fixed generated macOS/Linux bootstrap scripts so already-installed `rdev`
  paths still initialize OS/architecture variables before keep-awake and host
  startup logic.
- Added regression coverage for remote-control entry handoff/status/report
  surfaces and for generated bootstrap scripts carrying `--identity-store`.

## 0.1.39-dev

Current phase: fresh-Agent remote-session hardening from real Codex support
transcripts. This slice removes more model-dependent recovery work from the
human/Agent conversation and moves it into stable CLI, MCP, and gateway
contracts.

### Added

- Added `rdev version --json` and expanded `rdev doctor` with
  `rdev.runtime-info.v1`, including executable path, PATH `rdev`, build
  commit/time/source metadata, source-root discovery, and installed Skillkit
  manifests.
- Added `.remote-dev-skillkit/install.json` manifests during Skillkit install
  so future Agents can discover framework, bundle, target, MCP, and tool paths
  without searching private directories.
- Added planned `install_manifest` and `write_install_manifest` action reporting
  to Skillkit install dry-runs, so Agents can verify the diagnostic manifest
  will exist before executing a local install.
- Added per-skill `SKILL.md` SHA-256 entries to Skillkit install manifests and
  surfaced manifest integrity status through `rdev version --json` / `doctor`,
  allowing Agents to detect tampered or partially overwritten skill installs
  even when the original source checkout is unavailable.
- Added `rdev.skill-install-status.v1` to runtime info when a source checkout
  and install manifest are both discoverable. It hashes installed core
  `SKILL.md` files against the current checkout and reports stale/missing
  skills so fresh Agent sessions know when to reinstall the Skillkit.
- Added legacy installed-skill detection for common Codex, Claude Code, Hermes,
  OpenClaw, OpenCode, and configured generic skill directories even when older
  installs do not yet have `.remote-dev-skillkit/install.json`.
- Added `--ticket-code` support to `rdev support-session report` and MCP
  `rdev.support_session.report`. When exactly one active host exists for the
  ticket, the report now selects it as `recommended_task_host_id` and includes
  stale/pending host diagnostics.
- Added regression coverage for ticket-code report selection, MCP report
  selection, report schema flexibility, and authenticated host task claim after
  an old heartbeat.
- Added Agent-friendly command-group help for primary CLI groups, so
  `rdev <group> --help` is a successful discovery path instead of an unknown
  subcommand error.
- Added stable Cloudflare Named Tunnel configuration surfaces for repeated
  support sessions: `RDEV_CLOUDFLARED_NAMED_TUNNEL_URL` plus a reviewed token,
  token file, tunnel name, or start argv can now be detected and used before
  falling back to ephemeral Quick Tunnel URLs.

### Changed

- Windows support-session handoff generation now uses a short readable
  `powershell -NoProfile -Command "irm '.../bootstrap.ps1' -UseBasicParsing | iex"`
  command instead of Agent-fragile `-EncodedCommand`/Base64 handoffs. Signed
  gateway candidates still travel through the bootstrap URL and join manifest.
- `support-session connect --start` now resolves the source checkout through a
  standard discovery order: explicit hint, `RDEV_SOURCE_ROOT`, build metadata,
  current executable parent, and current working directory.
- Updated `safe-remote-support` guidance so Agents use the ticket-level report
  to obtain `recommended_task_host_id` before creating host-scoped work.
- Updated `safe-remote-support` gateway checks to verify the active gateway URL
  from the session payload or configured `RDEV_*_GATEWAY_URL`, instead of
  assuming the default local `127.0.0.1:8787` gateway.
- Updated generated `skillkit plan-install` scripts to call the standard
  `rdev skillkit install --execute` path, so copied skills, reference files, and
  install manifests are produced by one installer instead of duplicated script
  copy logic.
- Updated support-session guidance so Agent installs on a VPS/cloud server or
  machines with their own public DNS/IP recommend `RDEV_HOSTED_GATEWAY_URL`,
  while Cloudflare users can configure `RDEV_CLOUDFLARED_NAMED_TUNNEL_URL` for
  stable reuse. `https://*.trycloudflare.com` Quick Tunnel URLs are now treated
  as current-session fallback URLs, not durable configuration.

### Fixed

- Fixed host-side task claim stability by adding an authenticated gateway claim
  path that validates `host_secret`, refreshes heartbeat, and claims queued work
  atomically. HTTP polling, long-polling, and WSS now avoid rejecting a real
  connected host just because its previous `last_seen_at` crossed the stale
  window before claim.
- Fixed support-session reporting paths that forced Agents to discover and pass
  `host_id` manually even when the ticket had a single active task-ready host.
- Fixed stale MCP/static metadata drift by keeping `mcp/tools.json` aligned
  with the live `ticket_code` report schema.
- Fixed runtime install-manifest discovery so `rdev doctor` also checks the
  resolved source root's `.remote-dev-skillkit/install.json`, not only home and
  current-working-directory candidates.
- Fixed top-level CLI examples that still showed `support-session report`
  using a raw `--host-id`; the first-contact path now demonstrates
  `--ticket-code` so Agents use `recommended_task_host_id` before creating work.
- Fixed support-session handoff and prepare commands so opportunistic LAN or
  loopback gateway candidates stay diagnostic and are not promoted into
  generated `--gateway-url` arguments, preserving managed tunnel selection in
  `connect --start`.
- Fixed `connect --start` tunnel selection so an explicit operator-provided
  `--gateway-url` is respected and is not overwritten by automatic public
  tunnel startup.
- Fixed `rdev doctor` health aggregation so stale, missing, tampered,
  unreadable, or unverifiable Skillkit installs make top-level `ok=false` and
  return a concrete `rdev skillkit install --execute` refresh action instead of
  giving fresh Agents a false green light.
- Fixed README, MCP stdio docs, Agent bootstrap prompt, and
  `remote-vibe-coding` skill wording that could still imply Agents should turn
  `gateway_url_candidates` into hand-written target commands. The docs now
  treat candidates as diagnostic/signed-runtime metadata and route Agents back
  to `connect --start`, configured gateways, or `target_handoff_envelope.full_text`.
- Fixed local Agent install recommendations for shells where `go install`
  writes `rdev` to `GOBIN`/`GOPATH/bin` but that directory is not on `PATH`.
  Bootstrap plans, Skillkit install plans, and Skillkit install reports now
  prefer a stable absolute `rdev` binary path for MCP stdio config instead of
  blindly telling Agents to configure bare `rdev mcp serve`.
- Fixed MCP tool descriptions and support-session recovery actions that still
  described gateway URL candidates as a recommended value source. Tool
  metadata now treats candidates as diagnostic/preflight data and routes Agents
  to `connect --start`, configured gateways, or Connection Entry recovery.
- Fixed `GOBIN`/`GOPATH/bin` fallback detection so non-executable `rdev` files
  are not recommended as long-lived MCP stdio commands on POSIX systems.
- Fixed Skillkit install freshness diagnostics that only hashed `SKILL.md`
  files. Install manifests now hash `.remote-dev-skillkit/mcp/tools.json` and
  the framework reference doc, and `rdev doctor` fails closed when those
  installed reference files are missing, overwritten, or stale.
- Fixed the `rdev skillkit install` CLI JSON wrapper so it actually exposes the
  install report's `mcp_command` field that README, install docs, and bootstrap
  prompts tell Agents to use for local MCP stdio configuration.
- Fixed `rdev skillkit plan-install` so omitting `--rdev-command` no longer
  forces bare `rdev`; it now lets the Skillkit planner auto-detect the same
  stable `rdev` binary used by install reports.
- Fixed `rdev bootstrap agent-plan --remote-requested` remote-host defaults
  that still told fresh Agents to create invites and materialize Connection
  Entries after authorization. The planner now routes ordinary remote support
  through `rdev.support_session.connect` / `rdev support-session connect`,
  sends only `handoff_text_file.path` or `target_handoff_envelope.full_text`,
  and reserves Connection Entry runner materialization for reviewed package,
  managed owned-host, or restrictive-network recovery paths.
- Fixed bootstrap/dev-gateway operation docs that could be read as
  invite-first guidance for ordinary remote support. They now explicitly keep
  low-level invite creation behind reviewed dev-gateway workflows and point
  fresh Agents to the high-level support-session connect entry.
- Fixed `support-session connect --start` so the foreground gateway's internal
  prepare/readiness payload inherits the resolved `--rdev-command`. This keeps
  nested Agent runbooks aligned with the stable local `rdev` path instead of
  drifting back to bare `rdev` during recovery in fresh Agent environments.
- Fixed README and multilingual quick-start install entries so they no longer
  embed a short English Agent prompt that models can partially rewrite. The
  homepage now routes Agents directly to the canonical Agent Bootstrap Prompt,
  and the i18n audit checks that shared link instead of enforcing English
  prompt text in every translation.
- Fixed remaining Agent-facing docs that could still route ordinary target
  connection work through low-level invite creation or describe
  `gateway_url_candidates` as a recommended URL source. README, Bootstrap, MCP
  stdio docs, and task tracking now separate the high-level
  `rdev.support_session.connect` path from explicit package/materialization
  workflows.
- Fixed top-level `rdev --help` ordering so fresh Agents see
  `rdev support-session connect --start` and `rdev support-session --help`
  before lower-level support-session, invite, or Connection Entry materializer
  examples. Added regression coverage for that ordering and split the README
  command examples into ordinary connect, status/report, review/debug, and
  explicit package materialization sections.

## 0.1.38-dev

Current phase: MCP/CLI contract convergence for ordinary fresh Agents. This
slice closes the gap where installed MCP metadata, live MCP handlers, CLI
watchers, and public docs could point at different gateways or describe
different target handoff behavior.

### Added

- Added shared `internal/tasktemplate` policy templates so
  `rdev task policy-template` returns the same starter policy for common scoped
  probes.
- Added MCP `rdev.support_session.report`, matching the CLI
  `rdev support-session report` path, so Agents can summarize connected hosts,
  tasks, artifact counts, and first evidence snippets without hand-written
  `curl`.
- Added a contract test that compares static `mcp/tools.json` with
  `contracts.Tools()`, making stale MCP metadata a CI-visible failure.

### Changed

- Regenerated `mcp/tools.json` from the live `rdev mcp tools` contract so
  `gateway_url` and `rdev.support_session.report` are visible to installed
  Agent runtimes.
- Updated support-session status/supervision/runbook contracts to carry
  `gateway_url` through MCP and CLI watch calls, avoiding accidental reads from
  an empty local in-memory gateway.
- Updated README, MCP docs, Agent bootstrap prompt, remote-vibe-coding skill,
  safe-remote-support skill, and architecture wording to send
  `target_handoff_envelope.full_text` verbatim for `target=auto` instead of a
  bare join URL first.
- Clarified that Agents must not write their own PowerShell
  `-EncodedCommand`/Base64 bootstrap blobs, while reviewed `rdev`-generated
  fallback commands inside `target_handoff_envelope.full_text` may be forwarded
  unchanged.

### Fixed

- Fixed MCP `rdev.support_session.status` so per-call `gateway_url` proxies to
  the active foreground/tunneled/hosted gateway instead of checking only the MCP
  server's local memory gateway.

## 0.1.37-dev

Current phase: fresh-Agent usability hardening from another real attended
Windows support-session transcript. This slice moves more operator work from
Agent improvisation into first-class CLI/tool contracts so ordinary Agents do
not need to search source code, hand-write `curl`, or send bare invite links.

### Added

- Added `rdev task artifacts`, a first-class CLI reader for
  `/v1/tasks/<id>/artifacts`, so Agents can retrieve evidence without guessing
  raw HTTP routes.
- Added `rdev task policy-template --capability ... --target-os ...` for common
  safe probes (`shell.user`, `fs.read`, `fs.write.scoped`, `process.inspect`,
  and `tool.availability`). The command returns a ready policy object for
  `rdev task create`.
- Added `rdev support-session report`, a read-only session summarizer that
  fetches the host, tasks, artifact summaries, and a human-readable report for
  completed capability tests or scoped remote work.

### Changed

- `target=auto` support-session handoff now emits a multi-platform handoff
  containing Windows PowerShell, macOS/Linux terminal, and browser fallback
  sections instead of making `target_handoff_envelope.full_text` a bare join
  URL.
- `rdev support-session start` now accepts `--repo-root`, and
  `support-session connect --start --repo-root ...` preserves that checkout
  through the start path when building helper assets for a new work directory.
- Updated `safe-remote-support` guidance to prefer `task artifacts`,
  `task policy-template`, and `support-session report` before MCP or raw HTTP
  fallback paths.

### Fixed

- Fixed `rdev task wait` to read both raw task payloads and gateway responses of
  the form `{"task": {...}}`, preventing false timeouts after tasks have already
  reached `succeeded`.
- Simplified the Windows `process.inspect` audit probe to run `tasklist`
  directly, avoiding fragile nested quoting in `cmd /c tasklist /fi ...`.

## 0.1.36-dev

Current phase: real attended Windows support-session recovery hardening. This
slice fixes stale host storms, helper asset mismatch, and Agent missteps found
in a fresh remote-connection transcript.

### Added

- Added `rdev support-session recover`, an operator-side cleanup command that
  reads the active support-session status, revokes stale hosts for the ticket,
  cancels their queued/running tasks, and returns a structured recovery report
  instead of making Agents ask users to paste manifest roots or delete helper
  binaries.
- Added timeout aliases across support-session and task commands:
  `support-session status --timeout-seconds`, `support-session
  audit-capabilities --timeout-seconds`, and `task wait --timeout`.

### Changed

- Generated attended `/join/.../bootstrap.sh` and `bootstrap.ps1` now start
  visible temporary hosts with `--transport long-poll` for the default
  one-command support path. WSS remains available, but the ordinary handoff uses
  the more stable HTTPS long-poll path.
- `support-session connect --start` now fails closed when helper assets are not
  ready, instead of printing a target handoff that may later 404 on
  `/assets/*`.
- Helper assets are rebuilt from the current checkout during `--build-assets`
  even when an older helper file already exists, reducing gateway/helper
  version skew.
- Support-session ready/status/handoff files are written atomically so Agents
  do not read half-written JSON.

### Fixed

- WSS host connections now refresh host liveness before stale checks and host
  WSS clients also send heartbeat pings, preventing active WebSocket sessions
  from being marked `stale` after the gateway heartbeat window.
- Registered `GET /v1/tasks` in the HTTP router so CLI/MCP task listing and
  recovery flows hit the documented endpoint.
- Updated `safe-remote-support` guidance to use `support-session recover`, avoid
  bare `join_url` handoffs, and stop manual helper-binary replacement.

## 0.1.35-dev

Current phase: keep visible host sessions connected during longer AI-native
remote work. This slice adds best-effort sleep/display-sleep prevention while a
foreground host runner is active, without bypassing lock-screen or enterprise
security policy.

### Added

- Added `internal/hostawake`, a runtime keep-awake lease used by `rdev host
  serve` when `--once=false`. The lease is enabled by default through
  `--keep-awake=true` and can be disabled with `--keep-awake=false`.
- Added platform keep-awake implementations: macOS `caffeinate -dimsu`, Linux
  `systemd-inhibit --what=sleep:idle`, and Windows
  `SetThreadExecutionState(ES_CONTINUOUS|ES_SYSTEM_REQUIRED|ES_DISPLAY_REQUIRED)`.
- Added keep-awake status to the host registration payload so Agents can see
  whether sleep prevention is active or unavailable on the target.

### Changed

- Updated generated shell and PowerShell bootstrap scripts to keep the visible
  host session awake while the runner is active, then release the inhibitor
  before exit.
- Updated `safe-remote-support` guidance to diagnose stale hosts as possible
  sleep/lock/network loss and to state clearly that Remote Dev Skillkit never
  bypasses lock-screen, unlock, MDM, or enterprise policy.

## 0.1.34-dev

Current phase: AI-native remote support reliability hardening from real
fresh-Agent support-session transcripts. This slice removes more places where
Agents had to guess CLI, MCP, HTTP, Windows command, or liveness behavior.

### Added

- Added projected `stale` host status for active hosts whose heartbeat is older
  than the gateway liveness window. Support-session status responses now expose
  `stale_hosts` and no longer report `connected=true` for hosts that are not
  task-ready.
- Added operator-side `rdev task create` with `--policy-json` and
  `--policy-file`, so Agents can create scoped tasks through a first-class CLI
  command instead of hand-writing raw HTTP `curl` requests.
- Added `rdev support-session audit-capabilities`, a built-in OS-aware audit
  runner for `shell.user`, `fs.read`, `fs.write.scoped`, and `process.inspect`
  that creates bounded tasks, waits for terminal status, and summarizes
  artifacts.

### Fixed

- Host heartbeats now refresh the host's `last_seen_at`, making host lists,
  support-session status, and debugging output reflect actual runner activity.
- `CreateJob` and `NextJobForHost` now reject stale hosts before new work is
  queued or claimed, preventing Agents from sending tasks to a runner that has
  stopped polling.
- Attended temporary auto-authorization now supersedes matching stale host
  registrations for the same ticket and machine identity/name, revoking the old
  host and canceling its queued/running tasks so Agents do not accidentally pick
  an obsolete host.
- Polling and WSS host runners now report failed/denied/authorization-required tasks
  as failed task records and keep serving subsequent work instead of terminating
  the entire host process after one bad task.
- Updated `safe-remote-support` guidance to treat `stale` as not task-ready, use
  `rdev support-session audit-capabilities` for capability testing, and prefer
  first-class `rdev task` CLI commands over raw HTTP or Agent-authored scripts.

## 0.1.33-dev

Current phase: real support-session usability hardening from fresh-Agent
Windows testing. This slice removes fragile multi-step tunnel handling and adds
operator-side CLI parity for tasks.

### Fixed

- Removed the separate `--public-tunnel` knob from the support-session
  connect/start flow. `rdev support-session connect --start` now decides
  automatically: prefer configured stable gateway candidates, otherwise start a
  managed public tunnel before falling back to LAN-only connectivity.
- Made Cloudflare Quick Tunnel startup force HTTP/2 first to avoid QUIC/UDP EOF
  failures on restrictive networks, then retry without the protocol flag for
  older `cloudflared` builds.
- Added localhost.run SSH reverse tunnel fallback when Cloudflare Quick Tunnel
  cannot start or cannot produce a public URL.
- Hardened generated Windows bootstrap commands: single-URL handoffs now use
  the simple `irm '<url>' -UseBasicParsing | iex` form, while multi-URL retry
  scripts use PowerShell `-EncodedCommand` to avoid variable expansion and
  quoting failures in existing PowerShell sessions.
- Closed shell task stdin explicitly so shell/PowerShell tasks cannot hang waiting
  for interactive input through an inherited pipe.
- Added operator-side `rdev task list|get|wait|cancel` CLI commands plus gateway
  `GET /v1/tasks` and `POST /v1/tasks/<id>/cancel`, so Agents can inspect, wait
  for, and cancel tasks through the CLI instead of dropping to raw HTTP or
  MCP-only workflows.
- Updated support-session runbooks to forbid manual tunnel process management,
  manual `--gateway-url` handoff assembly, and unnecessary multi-step setup
  flows when the standard `rdev support-session connect --start` path can manage
  the gateway and tunnel lifecycle.

## 0.1.32-dev

Current phase: real Windows Connection Entry recovery hardening. This slice
addresses failures found during attended Windows remote-connection testing.

### Fixed

- Updated the Windows temporary bootstrap to run `rdev host serve` with
  `--once=false` and a longer authorization timeout, so the foreground host remains
  alive to wait for authorization and poll tasks instead of exiting immediately after
  registration.
- Added retry parameters to the Windows temporary bootstrap and wired
  `AuthorizationTimeoutSeconds` / `MaxRetries` through the acceptance plan launcher.
- Added `rdev mcp serve --gateway-url` and per-call `gateway_url` MCP tool
  overrides for host/task/authorization/artifact/audit tools, allowing Agents to
  target a newly created gateway without restarting MCP.
- Added Cloudflare Quick Tunnel as a detected connectivity helper and exposed
  `RDEV_CLOUDFLARED_GATEWAY_URL` plus runbook hints when only loopback/LAN
  gateway candidates are available.
- Added `rdev support-session connect --start --public-tunnel auto|always|off`.
  Auto mode starts a Cloudflare Quick Tunnel when no stable gateway candidate is
  configured, uses the resulting public URL as the session gateway, and exports
  `RDEV_CLOUDFLARED_GATEWAY_URL` for MCP tool calls.
- Added prebuilt support-session asset fallback copying from
  `work/rdev-support-session/bin/` when Go cross-compilation is unavailable or
  fails, preventing Windows helper asset 404s on hosts without Go.
- Synchronized MCP tool metadata in both `mcp/tools.json` and the exported
  `dist/remote-dev-skillkit/mcp/tools.json`.
- Rewrote `skills/safe-remote-support/SKILL.md` around the one-command
  handoff contract, public-tunnel-first connectivity, automatic `rdev`
  recovery, and the rule that Cloudflare-backed MCP calls must carry the
  effective `gateway_url`.

## 0.1.31-dev

Current phase: fresh-Agent and hosted-gateway reliability hardening. This slice
fixes MCP gateway visibility, host-side authentication, Windows output
encoding, and retry behavior for tunnel-backed gateways.

### Fixed

- Made `rdev mcp serve` proxy host, task, authorization, artifact, and audit tool
  calls to a configured gateway URL (`RDEV_HOSTED_GATEWAY_URL` or another
  `RDEV_*_GATEWAY_URL`) instead of reading an empty local in-memory gateway.
- Added per-host `host_secret` issuance on registration and required it for
  host-side task claim, heartbeat, and WSS upgrade requests. Host registration
  now fails closed client-side when a gateway response omits the secret.
- Added host heartbeat endpoint/client pings so gateways can track host
  liveness during polling sessions.
- Added gateway-side shell/PowerShell policy preflight for task creation so
  invalid tasks are rejected with `422 policy_violation` before they become
  queued ghost tasks.
- Forced UTF-8 output handling for Windows `cmd.exe /c` and PowerShell adapter
  runs to avoid localized output corruption in artifacts.
- Added retrying HTTP transports for idempotent gateway GET/HEAD requests in
  CLI host clients and MCP remote gateway proxy clients.

## 0.1.30-dev

Current phase: production protocol foundations and adapter surface layer. Added
authenticated managed-host enrollment protocol model, TPM/MDM protectedstore
stub backends, Adapter SDK policy/workspace helpers for third-party adapters,
and package/verifier surfaces for RustDesk/MeshCentral, Coder, and DevPod.
Cleaned up two stale doc strings that described missing capabilities as still
absent.

### Added

- Added `internal/managedhost` with production managed-host enrollment protocol
  model: `EnrollmentRequest`/`EnrollmentResponse` (operator bearer-token auth,
  Ed25519 identity proof, anti-replay nonce), `TrustFetchRequest` (host-signed
  authenticated trust-bundle fetch), and `ReEnrollmentRequest` (prior-key
  continuity proof for near-expiry certificate rotation). All three flows use
  schema-versioned, canonically signed payloads and are tested against tampered
  inputs and role-check failures.
- Added `tpm:` prefix backend for `internal/protectedstore`: `TPMStore`,
  `tpmBackend` interface, `tpm_linux.go` (file-backed stub with inline
  documentation for replacing with tpm2-tss/go-tpm sealing), and
  `tpm_unsupported.go` (returns error on non-Linux). `IsRef`/`ParseRef`/`Open`
  extended.
- Added `mdm:` prefix backend for `internal/protectedstore`: `MDMStore`,
  `mdmBackend` interface, `mdm_darwin.go` (file-backed stub with inline
  documentation for replacing with `CFPreferencesCopyValue` MDM managed
  preferences), and `mdm_unsupported.go` (returns error on non-Darwin). Both
  backends have `SetTPMBackendForTest`/`SetMDMBackendForTest` helpers following
  the existing pattern.
- Added `pkg/adapterkit/policy.go` with `PolicyPlan`
  (`rdev.adapter-policy-plan.v1`), `NewPolicyPlan`, `PolicyPlanContract`,
  `PolicyPlanReport`, and `VerifyPolicyPlanJSON`. Third-party adapters can now
  declare `ExternalConsequences`, `RequiredAuthorizations`, and
  `WorkspaceBoundaries` in the plan phase and get a machine-verifiable
  conformance report.
- Added `pkg/adapterkit/workspace.go` with `WorkspaceSession`
  (`rdev.adapter-workspace-session.v1`), `PrepareWorkspaceSession` (validates
  root existence, resolves symlinks, enforces write-boundary containment),
  `MarkCleaned`, `WorkspaceSessionContract`, and `VerifyWorkspaceSessionJSON`.
- Added `internal/rustdeskadapter` with RustDesk/MeshCentral remote-desktop
  adapter **package/verifier surface**: `Build`, `Verify`,
  `AcceptanceEvidencePlan`, variant support (`rustdesk`/`meshcentral`),
  authorization boundaries, and evidence plan requiring session teardown proof.
- Added `internal/coderadapter` with Coder workspace adapter **package/verifier
  surface**: `Build`, `Verify`, `AcceptanceEvidencePlan`, `runner.env.example`
  with `RDEV_CODER_URL`/`TOKEN`/`WORKSPACE`, and evidence plan requiring
  workspace stop proof.
- Added `internal/devpodadapter` with DevPod/devcontainer workspace adapter
  **package/verifier surface**: `Build`, `Verify`, `AcceptanceEvidencePlan`,
  `runner.env.example` with `RDEV_DEVPOD_PROVIDER`/`WORKSPACE`/`SOURCE`, and
  evidence plan requiring workspace stop proof. Supports Docker, Kubernetes,
  and cloud providers.

### Changed

- `docs/operations/MCP_STDIO.md` — removed stale claim "production key storage
  is not implemented yet"; updated to accurately state that the stdio server is
  not itself a gateway storage authority and that `--signing-key`, a storage
  provider, or a trust bundle should be used for production deployments.
- `internal/cli/cli.go` — removed `"note": "local preview only; gateway
  persistence is not implemented yet"` field from `rdev ticket create` JSON
  output. The official support-session and gateway flows supersede this
  command.

## 0.1.29-dev

Current phase: formal release package evidence is being made as Agent-native as
hosted-provider and relay evidence. Post-release download evidence now has a
standard scaffold and readiness gate, so Agents do not invent public download
transcript file names or package commands after GitHub Release assets exist.

### Added

- Added `rdev.post-release-download-evidence-scaffold.v1` through
  `rdev acceptance scaffold-post-release-download --plan
  <post-release-install-plan.json> --plan-verification
  <post-release-install-verification.json> --out <dir>`. The scaffold writes
  `AGENT_CHECKLIST.md`, `scaffold-report.json`, copied plan/verification JSON,
  stable platform and Skillkit evidence directories, standard evidence file
  metadata, and exact package/verify commands.
- Added `rdev.post-release-download-evidence-status.v1` through
  `rdev acceptance post-release-evidence-status --scaffold
  <dir|scaffold-report.json>`. The status command fails closed until every
  planned platform transcript, candidate verification, bundle verification,
  and required Skillkit evidence file exists, is non-empty, and is not a
  scaffold placeholder.
- Added MCP tools `rdev.acceptance.scaffold_post_release_download` and
  `rdev.acceptance.post_release_evidence_status` so fresh Agents can prepare
  and inspect public-download evidence without writing custom shell,
  PowerShell, or file-layout code.
- Extended release smoke and tests to prove placeholder post-release download
  evidence is not ready, filled scaffold evidence reports
  `ready_for_packaging=true`, and
  `rdev acceptance package-post-release-download --scaffold <dir>` consumes the
  scaffold's copied plan plus standard platform/Skillkit evidence directories
  without requiring Agents to pass individual evidence path flags.
- Hardened post-release download evidence packaging and verification so
  scaffold placeholders under platform, Skillkit, and post-release
  verification evidence paths are rejected even if an Agent skips the
  readiness status command or an archived package manifest is tampered to match
  placeholder file checksums.
- Added standard helper transcript output to `rdev connection-entry run` through
  `--helper-transcript-out <path>`. Relay/connectivity evidence plans now tell
  Agents to generate both `runner-result.json` and `helper-transcript.txt`
  from the runner, reducing hand-written helper evidence during real
  restrictive-network acceptance.
- Added `rdev connection-entry run --evidence-dir <dir>` with
  `rdev.connection-entry.runner-evidence.v1`. The runner now writes
  `runner-result.json`, `helper-transcript.txt`, `gateway-status.json`,
  `host-status.json`, `connection-status.json`, `audit.jsonl`, and
  `evidence-report.json` as one standard evidence directory. Relay/connectivity
  evidence plans now use `--evidence-dir .`, and release smoke packages the
  runner-generated status and audit files instead of hand-written fixtures.
- Added `rdev acceptance package-relay-adapter --evidence-dir <dir>`. Relay
  acceptance packaging now consumes the runner-generated evidence directory
  directly, archives `audit.jsonl` under the same standard name, and keeps
  evidence-plan package commands directory-level so Agents do not pass six
  individual evidence file flags.
- Added `rdev acceptance package-hosted-provider-runtime --evidence-dir <dir>`.
  Hosted runtime acceptance packaging now consumes the standard scaffolded
  evidence directory, archives `audit.jsonl` under the same standard name, and
  keeps hosted runtime evidence-plan package commands directory-level so Agents
  do not pass nine individual gateway/storage/auth/backup/restore/role/audit
  file flags.
- Added `rdev.acceptance-release-evidence-index.v1` through
  `rdev acceptance release-evidence-index` and MCP tool
  `rdev.acceptance.release_evidence_index`. The index verifies hosted provider
  runtime, relay/connectivity, and post-release download acceptance packages
  together, writes `release-evidence-index.json` plus `checksums.txt`, records
  package manifest hashes without copying source-path-heavy package manifests,
  and fails closed until all three release-blocking evidence tracks verify.
- Extended `rdev acceptance scaffold-evidence` and MCP tool
  `rdev.acceptance.scaffold_evidence` with package-level inputs:
  `--hosted-provider-package <dir|hosted-provider.json>` and
  `--relay-adapter-package <dir|relay-adapter.json>`. Fresh Agents can now
  scaffold hosted-provider and relay/connectivity evidence from the package
  directory directly, while `--plan` remains available for reviewed operator
  overrides.
- Updated generated hosted-provider and relay-adapter package runbooks plus
  evidence-plan Agent rules to point fresh Agents at package-level
  `rdev acceptance scaffold-evidence --hosted-provider-package` /
  `--relay-adapter-package` commands. Package verifiers now check that the
  generated evidence plans preserve those scaffold-first rules.
- Extended `rdev acceptance scaffold-post-release-download` and MCP tool
  `rdev.acceptance.scaffold_post_release_download` with
  `--post-release-install-dir <dir>` / `post_release_install_dir`. Fresh
  Agents can now scaffold public download evidence from the standard
  post-release install directory without hand-picking the plan and verification
  JSON files; explicit `--plan` / `--plan-verification` remains available for
  reviewed overrides.
- Hardened Connection Entry helper dependency repair so
  `RDEV_*_INSTALL_ACTION_JSON` must use schema
  `rdev.connection-entry.dependency-install-action.v1` and a standard
  `rdev deps install --tool ... --scope ... --url ... --expected-sha256 ...
  --execute` argv whose tool, scope, and SHA-256 match the reviewed action.
  The runner now rejects arbitrary install commands, plan-only installs, hash
  mismatches, unsupported flags, and shell/PowerShell command-string wrappers
  before helper startup.
- Hardened hosted provider runtime evidence verification so
  `failure-mode-evidence.json` must include `failure_mode_tested=true` plus a
  clear negative probe such as `rejected=true`, `denied=true`,
  `unavailable=true`, `accepted=false`, or `authorized=false`; a generic
  `ok=true` file is no longer enough to satisfy hosted auth/storage failure
  evidence.

### Remaining Gates

- Publish signed per-platform Connection Entry archives to GitHub Releases and
  collect real public download transcripts with the new scaffold/status flow.
- Run real clean-machine Windows/macOS/Linux Connection Entry and fresh-Agent
  acceptance.
- Collect real deployed Postgres/Redis/S3/OIDC/SAML hosted provider evidence
  and real restrictive-network frp/Chisel/headscale/WireGuard/SSH evidence.

## 0.1.28-dev

Current phase: real acceptance is being made more Agent-native before external
machine and hosted-provider testing. Hosted provider and connectivity packages
already carry evidence plans; this release adds a standard scaffold runner so
Agents create the exact evidence directory, checklist, package command, and
verify command through `rdev` instead of inventing local scripts or file names.

### Added

- Added `rdev.acceptance-evidence-scaffold.v1` through
  `rdev acceptance scaffold-evidence --plan <runtime-evidence-plan.json |
  acceptance-evidence-plan.json> --out <dir>`. The command supports hosted
  provider runtime evidence plans and relay/connectivity adapter evidence
  plans, writes `AGENT_CHECKLIST.md`, `scaffold-report.json`, a copy of the
  source plan, resolved package/verify commands, and standard evidence file
  metadata.
- Added MCP tool `rdev.acceptance.scaffold_evidence` so fresh Agents can
  scaffold hosted-provider or relay/mesh/VPN/SSH acceptance evidence through a
  standard tool call before collecting real transcripts.
- Added `rdev.acceptance-evidence-status.v1` through
  `rdev acceptance evidence-status --scaffold <dir|scaffold-report.json>` and
  MCP tool `rdev.acceptance.evidence_status`. Agents can now read a scaffold
  and get a fail-closed readiness report showing missing, empty, and
  placeholder evidence files before attempting `rdev acceptance package-*`.
- Kept scaffold generation non-deceptive by default: it does not create
  evidence placeholder files unless `--create-placeholders` is explicitly
  passed, and every scaffold reports `ready_for_packaging=false` until real
  evidence replaces placeholders and the matching acceptance package verifies.
- Added fail-closed placeholder detection to hosted-provider runtime and
  relay/connectivity acceptance packaging and verification, so scaffold
  placeholder files cannot be archived as release evidence.
- Extended release smoke and tests to generate hosted-provider and relay
  evidence scaffolds, verify schema/command contracts, ensure default hosted
  scaffolds do not create fake evidence files, prove placeholder packages are
  rejected, and prove evidence status stays non-ready until required files are
  real, non-empty, and not placeholders.

### Remaining Gates

- Run real clean-machine Windows/macOS/Linux Connection Entry and fresh-Agent
  acceptance.
- Publish signed per-platform Connection Entry archives to GitHub Releases and
  package real post-release download evidence.
- Collect real deployed Postgres/Redis/S3/OIDC/SAML hosted provider evidence
  and real restrictive-network frp/Chisel/headscale/WireGuard/SSH evidence.

## 0.1.27-dev

Current phase: formal release packages are closing the last local startup-gate
gap before public release evidence. Connection Entry release archives now make
their visible launchers verify the signed release bundle with the packaged
standalone verifier before running packaged `rdev`. Hosted provider and relay
adapter packages now also carry machine-readable evidence collection plans so
Agents stop inventing real-acceptance file names and package commands.

### Added

- Added `required_release_artifacts` to
  `rdev.connection-entry-release-package.v1` so the release archive records the
  exact packaged binaries that must be present in the signed release bundle
  before target-side execution.
- Updated generated Connection Entry shell and PowerShell launchers to run the
  packaged `rdev-verify --bundle release/release-bundle.json
  --root-public-key <pinned-root> --require-artifacts <packaged-artifacts>`
  gate before invoking `rdev connection-entry run`.
- Extended release-candidate verification and release smoke to fail if a
  Connection Entry archive launcher does not use the packaged verifier, pin the
  release root, pass `--bundle`, and require the packaged artifact set.
- Added `rdev.hosted-provider-runtime-evidence-plan.v1` as
  `runtime-evidence-plan.json` in hosted provider packages. The plan lists the
  standard gateway, storage, auth, backup, restore, retention, role-mapping,
  failure-mode, and audit evidence files plus the exact
  `rdev acceptance package-hosted-provider-runtime` / verify commands Agents
  should use for real deployed evidence.
- Added `rdev.relay-adapter-acceptance-evidence-plan.v1` as
  `acceptance-evidence-plan.json` in relay/connectivity adapter packages. The
  plan lists standard `runner-result.json`, helper transcript, gateway/host/
  connection status, and audit evidence files, plus the exact
  `rdev connection-entry run --result-out`,
  `rdev acceptance package-relay-adapter`, and verify commands.
- Extended hosted provider and relay adapter verification, CLI output, tests,
  and release smoke to validate the new evidence-plan schemas and command
  contracts.

### Remaining Gates

- Publish signed per-platform Connection Entry release archives to GitHub
  Releases and verify real downloads with
  `rdev acceptance package-post-release-download`.
- Run real clean-machine Windows/macOS/Linux Connection Entry and fresh-Agent
  acceptance, plus real restrictive-network frp/Chisel/headscale/WireGuard/SSH
  evidence.
- Continue real deployed Postgres/Redis/S3/OIDC/SAML hosted provider evidence.

## 0.1.26-dev

Current phase: hosted auth is closing the last provider-contract-only runtime
gap. SAML operator auth now has a built-in gateway verifier path, while real
hosted claims still require deployed IdP evidence for role mapping, certificate
rotation, failure probes, audit, and redaction.

### Added

- Added `rdev.saml-operator-auth.v1` and `operatorauth.SAMLVerifier` for
  signed SAMLResponse bearer authorization. The verifier uses established
  SAML/XMLDSig libraries, validates IdP issuer, audience, assertion consumer
  recipient, assertion time conditions, SHA-256-or-better XML signature
  algorithms, certificate trust, subject mapping, and role attributes, and
  rejects private key material in SAML certificate config.
- Added `rdev operator-auth verify-saml --auth <file>
  [--response-file <base64-saml-response-file> --role <role>]` so operators
  and Agents can verify SAML config and optionally prove a real operator
  assertion before packaging hosted auth evidence.
- Added `rdev gateway serve --saml-operator-auth <file>` so hosted gateways can
  authorize control-plane requests with signed SAMLResponse bearer tokens in
  addition to local hashed tokens, provider-neutral EdDSA JWT files, and
  OIDC/JWKS RS256 JWTs.
- Updated `rdev hosted-provider package --auth-provider saml-assertion` for
  built-in storage providers so generated gateway args use
  `--saml-operator-auth ${RDEV_SAML_OPERATOR_AUTH_FILE}` instead of a
  placeholder reviewed launcher, with release-smoke coverage for S3-compatible
  storage plus SAML auth.

### Remaining Gates

- Run a real SAML IdP integration with valid and invalid assertion probes,
  issuer/audience/recipient/certificate rejection, role mapping, certificate
  rotation, failure-mode, audit, and redaction evidence, then package it with
  `rdev acceptance package-hosted-provider-runtime`.
- Continue real deployed Postgres/Redis/S3/OIDC hosted provider evidence,
  real helper/relay adapter acceptance, and GitHub Release publication/download
  verification.

## 0.1.25-dev

Current phase: restrictive-network helpers are moving from package metadata
toward safer self-repair inside the standard Connection Entry runner. Mesh and
VPN helpers can now use the same SHA-256 verified user/workspace-scoped
dependency install path that Chisel/frpc use, while enrollment, keys, routes,
DNS, services, firewall, and privileged network changes remain authorization-gated.

### Added

- Extended `rdev deps install` and `internal/depsinstall` to support
  `tailscale` and `wg` helper binaries in addition to `chisel` and `frpc`.
  Aliases such as `headscale-tailscale`, `tailscale-compatible`,
  `wireguard`, `wireguard-tools`, and `wg-quick` normalize to the safe
  executable helper names. The installer still only downloads an explicit URL,
  verifies SHA-256, unpacks or copies the binary into a user/workspace tools
  directory, and refuses hidden services, PATH mutation, drivers, firewall,
  DNS, route, OS policy, or privileged installation.
- Updated `rdev relay-adapter package --adapter headscale-tailscale` and
  `--adapter wireguard` so generated packages expose standard
  `RDEV_MESH_INSTALL_ACTION_JSON` and `RDEV_VPN_INSTALL_ACTION_JSON` templates
  that call `rdev deps install --tool tailscale` or `--tool wg` with reviewed
  download URLs and SHA-256 values instead of returning only
  `manual-review-required`.
- Added regression coverage for mesh/VPN helper dependency install reports,
  alias normalization, package install-action generation, CLI plan-only output,
  and continued rejection of unsupported privileged helper daemons.

### Remaining Gates

- Run real restrictive-network acceptance for SSH tunnel, frp/Chisel,
  headscale/Tailscale-compatible mesh, and WireGuard paths across clean
  Windows/macOS/Linux targets, then package the resulting runner/helper/gateway
  evidence with `rdev acceptance package-relay-adapter`.
- Continue real deployed hosted provider evidence, SAML runtime integration,
  and GitHub Release publication/download verification.

## 0.1.24-dev

Current phase: hosted auth is moving from provider contracts toward real
runtime paths. The gateway now has a built-in OIDC/JWKS operator-auth verifier
for RS256 JWTs, while production hosted claims still require real identity
provider deployment evidence for key rotation, role mapping, failure modes,
audit, and redaction.

### Added

- Added `rdev.oidc-jwks-operator-auth.v1` and
  `operatorauth.OIDCJWKSVerifier` for OIDC/JWKS operator auth. The runtime
  fetches JWKS, accepts supported RS256 RSA signing keys, verifies compact JWT
  signatures, issuer, audience, `exp`, `nbf`, subject, and role claims, and
  rejects unsafe JWKS URLs containing credentials, query strings, fragments, or
  non-localhost plain HTTP.
- Added `rdev operator-auth verify-oidc-jwks --auth <file>
  [--token-file <jwt> --role <role>]` so operators and Agents can verify OIDC
  JWKS configuration and optionally prove a real operator token before
  packaging hosted auth evidence.
- Added `rdev gateway serve --oidc-jwks-operator-auth <file>` so hosted
  gateways can authorize control-plane requests with OIDC/JWKS role tokens in
  addition to local hashed tokens and provider-neutral hosted EdDSA JWT files.
- Updated `rdev hosted-provider package --auth-provider oidc-jwks` for built-in
  storage providers so generated gateway args use
  `--oidc-jwks-operator-auth ${RDEV_OIDC_OPERATOR_AUTH_FILE}` instead of a
  placeholder reviewed launcher.
- Updated release smoke to verify OIDC/JWKS RS256 token validation, unsafe JWKS
  URL rejection, OIDC hosted-provider package generation/verification, and real
  OIDC gateway args.

### Remaining Gates

- Run a real OIDC/JWKS identity-provider integration with valid and invalid
  token probes, issuer/audience/key rejection, role mapping, key rotation,
  failure-mode, audit, and redaction evidence, then package it with
  `rdev acceptance package-hosted-provider-runtime`.
- Continue SAML runtime integration beyond its current contract.
- Continue real helper/relay adapter acceptance and GitHub Release
  publication/download verification.

## 0.1.23-dev

Current phase: hosted storage continues moving from provider contracts toward
real runtime paths. The gateway now has a built-in S3-compatible object storage
state-store provider through `aws s3api`, while production hosted claims still
require real deployed object-storage versioning or backup, restore, retention,
role-mapping, failure-mode, audit, and redaction evidence.

### Added

- Added `gateway.S3CompatibleStateStore` and `--storage-provider
  s3-compatible` support for `rdev gateway serve` / `rdev gateway storage
  verify`. The provider stores the current `rdev.gateway-snapshot.v1` at
  `s3://bucket/prefix/snapshot-current.json`, performs put/get/delete runtime
  probes through `aws s3api`, and rejects credentials, query strings, and
  fragments in storage locations so credentials stay in `AWS_PROFILE`, `AWS_*`
  environment injection, endpoint config, or an operator-authorized secret
  manager.
- Updated `rdev hosted-provider package --storage-provider s3-compatible
  --auth-provider hosted-ed25519-jwt` so the generated gateway args use the
  built-in `rdev gateway serve --storage-provider s3-compatible` runtime path
  instead of a placeholder reviewed launcher.
- Updated hosted provider package metadata and release smoke to verify
  S3-compatible state-store tests, unsafe location rejection, S3 hosted-JWT
  package generation/verification, and S3 runtime gateway args.

### Remaining Gates

- Run a real S3-compatible gateway with endpoint credentials supplied through a
  reviewed secret path, versioning or backup, restore, retention, role-mapping,
  failure-mode, audit, and redaction evidence, then package it with
  `rdev acceptance package-hosted-provider-runtime`.
- Continue OIDC/JWKS and SAML runtime integrations beyond their current
  contracts.
- Continue real helper/relay adapter acceptance and GitHub Release
  publication/download verification.

## 0.1.22-dev

Current phase: hosted storage continues moving from provider contracts toward
real runtime paths. The gateway now has a built-in Redis stream state-store
provider through `redis-cli`, while production hosted claims still require real
deployed Redis persistence/replication, replay/restore, retention, failure-mode,
role-mapping, audit, and redaction evidence.

### Added

- Added `gateway.RedisStreamStateStore` and `--storage-provider redis-stream`
  support for `rdev gateway serve` / `rdev gateway storage verify`. The provider
  uses `redis-cli`, stores the current `rdev.gateway-snapshot.v1` at a snapshot
  key, appends snapshot/probe events to a Redis stream, performs runtime probe
  readback/cleanup, and rejects inline Redis URL credentials so secrets stay in
  `REDISCLI_AUTH`, environment injection, or an operator-authorized secret
  manager.
- Updated `rdev hosted-provider package --storage-provider redis-stream
  --auth-provider hosted-ed25519-jwt` so the generated gateway args use the
  built-in `rdev gateway serve --storage-provider redis-stream` runtime path
  instead of a placeholder reviewed launcher.
- Updated hosted provider package metadata, acceptance docs, and release smoke
  to verify Redis stream state-store tests, inline credential rejection,
  Redis hosted-JWT package generation/verification, and Redis runtime gateway
  args.

### Remaining Gates

- Run a real Redis-backed gateway with persistence/replication policy, replay or
  restore evidence, retention, role-mapping, failure-mode, audit, and redaction
  evidence, then package it with
  `rdev acceptance package-hosted-provider-runtime`.
- Continue S3-compatible object storage, OIDC/JWKS, and SAML runtime
  integrations beyond their current contracts.
- Continue real helper/relay adapter acceptance and GitHub Release
  publication/download verification.

## 0.1.21-dev

Current phase: helper/relay adapter evidence moved from package metadata and
synthetic acceptance input toward the real Connection Entry runner path. The
standard runner now has regression coverage for executing configured SSH,
headscale/Tailscale-compatible mesh, and WireGuard helper startup paths before
host registration, while production claims still require real cross-network
Windows/macOS/Linux acceptance evidence.

### Added

- Added regression coverage proving `connectionrunner.Run` can execute
  authorized, already-configured `RDEV_SSH_TUNNEL_START_ARGV_JSON`,
  `RDEV_MESH_START_ARGV_JSON`, and `RDEV_VPN_START_ARGV_JSON` helper paths,
  wait for their configured gateway override, run `rdev host serve`, and clean
  up the helper process through the same standard runner flow used by
  Chisel/frpc relay paths.
- Added `rdev connection-entry run --result-out <path>` so Agents and release
  automation can archive the raw `rdev.connection-entry.runner-result.v1`
  evidence file without parsing stdout or reconstructing result JSON.
- Updated release smoke so relay/connectivity acceptance evidence is generated
  by a real `rdev connection-entry plan` plus `rdev connection-entry run`
  invocation against a temporary gateway and fake reviewed WireGuard helper,
  instead of hand-writing a runner-result JSON fixture.

### Remaining Gates

- Run real restrictive-network acceptance for SSH tunnel, frp/Chisel,
  headscale/Tailscale-compatible mesh, and WireGuard paths across clean
  Windows/macOS/Linux targets, then package the resulting runner/helper/gateway
  evidence with `rdev acceptance package-relay-adapter`.
- Continue real deployed hosted provider evidence, real GitHub Release
  publication/download verification, and remaining durable provider runtime
  integrations.

## 0.1.20-dev

Current phase: hosted storage moved from provider contracts toward a real
runtime path. The gateway now has a built-in Postgres state-store provider
implemented through `psql`/libpq, while production hosted claims still require
a real deployed database run and packaged backup/restore/retention evidence.

### Added

- Added `gateway.PostgresStateStore` and `--storage-provider postgres` support
  for `rdev gateway serve` / `rdev gateway storage verify`. The provider stores
  `rdev.gateway-snapshot.v1` as JSONB in `rdev_gateway_snapshots`, performs
  schema bootstrap, upsert, load, and runtime probe SQL through `psql`, and
  rejects inline passwords in connection info so credentials stay in libpq
  service files, `.pgpass`, environment injection, or an operator-authorized
  secret manager.
- Updated `rdev hosted-provider package --storage-provider postgres
  --auth-provider hosted-ed25519-jwt` so the generated gateway args use the
  built-in `rdev gateway serve --storage-provider postgres` runtime path
  instead of a placeholder reviewed launcher.
- Updated release smoke to verify the Postgres state-store fake-`psql`
  round-trip, inline password rejection, and Postgres hosted-JWT package
  gateway args.

### Remaining Gates

- Run a real Postgres-backed gateway with managed backup, restore, retention,
  role-mapping, failure-mode, audit, and redaction evidence, then package it
  with `rdev acceptance package-hosted-provider-runtime`.
- Continue S3-compatible object storage, Redis stream, OIDC/JWKS, and SAML
  runtime integrations beyond their current contracts.
- Continue real helper/relay adapter acceptance and GitHub Release
  publication/download verification.

## 0.1.19-dev

Current phase: formal release packaging now has a standard post-release
download evidence package. This still does not create or publish a GitHub
Release; it gives operators and Agents a reproducible way to archive real
download/install verification transcripts after release assets exist.

### Added

- Added `rdev.acceptance-package.post-release-download.v1` through
  `rdev acceptance package-post-release-download`. The package archives a
  verified `rdev.post-release-install-plan.v1`, the corresponding
  `rdev.post-release-install-verification.v1`, per-platform download
  transcripts, per-platform `rdev release verify-candidate` outputs,
  per-platform `rdev-verify --bundle` outputs, optional Skillkit
  download/verify evidence, checksums, redaction metadata, and no-private
  surface checks.
- Added `rdev.acceptance-verification.post-release-download-package.v1`
  through `rdev acceptance verify-post-release-download-package`.
  Verification requires the plan-verification output to report `ok=true`, every
  planned platform to have transcript/candidate/bundle evidence, candidate and
  bundle verification evidence to report `ok=true`, and Skillkit verification
  evidence to report `ok=true` when the plan includes Skillkit.
- Updated release smoke so the local formal-release gate now packages and
  verifies synthetic post-release download evidence in addition to generating
  and verifying the post-release install plan.

### Remaining Gates

- Create the real GitHub Release only after explicit operator authorization.
- Run the generated post-release verification scripts against real public
  release downloads on macOS, Linux, Windows, and Skillkit, then package the
  transcripts with `rdev acceptance package-post-release-download`.
- Continue real hosted provider runtime deployment evidence and real
  restrictive-network helper/relay adapter execution evidence.

## 0.1.18-dev

Current phase: hosted provider packages now include provider-specific runtime
contracts instead of treating Postgres, S3-compatible storage, Redis streams,
OIDC/JWKS, and SAML as generic external placeholders. This is still not a
claim that a deployed third-party provider has been accepted; real production
claims require runtime evidence from an actual deployment.

### Added

- Added `rdev.hosted-provider-runtime-contract.v1` to hosted provider packages.
  `rdev hosted-provider package` now writes `runtime-contract.json` and
  `HOSTED_PROVIDER_RUNTIME.md` alongside `hosted-provider.json`, the runbook,
  and `gateway.env.example`.
- Added provider-specific runtime descriptors and environment requirements for
  `postgres`, `s3-compatible`, `redis-stream`, `oidc-jwks`, and
  `saml-assertion`. The package records required verification, backup,
  restore, retention, role-mapping, failure-mode, and audit evidence without
  embedding endpoints, credentials, tenant identifiers, private paths, or
  organization-specific values.
- Updated hosted provider CLI output to expose `runtime_contract_schema` and
  `runtime_status`, so Agents can distinguish single-node smoke packages from
  durable runtime-evidence-required packages without opening package files.
- Updated release smoke to generate and verify a `postgres` + `oidc-jwks`
  hosted provider package and assert the runtime contract schema and evidence
  requirements.
- Updated hosted runtime acceptance tests so a complete external provider
  evidence package can verify with runtime claim
  `external-durable-hosted-runtime-evidence`.

### Remaining Gates

- Run a deployed hosted gateway against real Postgres/S3-compatible/Redis and
  OIDC/JWKS or SAML provider configurations, then package the resulting
  startup, storage/auth, backup, restore, retention, role-mapping,
  failure-mode, audit, and redaction evidence with
  `rdev acceptance package-hosted-provider-runtime`.
- Continue GitHub Release publication/download verification and real
  helper/relay adapter execution evidence.

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
  mutation, and service persistence remain operator-authorized actions.
- Updated MCP tool metadata and release smoke so MCP-capable Agents discover
  the new adapter kinds and CI verifies package/verify output for
  `ssh-tunnel`, `headscale-tailscale`, and `wireguard`.
- Generalized relay adapter acceptance packaging and verification so release
  evidence can now prove any standard Connection Entry connectivity path:
  `existing-frp-or-chisel-relay`, `existing-ssh-tunnel`,
  `existing-headscale-tailscale-mesh`, or `existing-wireguard-vpn`. Package and
  verification JSON now expose `selected_path` plus `accepted_paths`, keeping
  Agents from treating Chisel/frpc as the only acceptable restrictive-network
  proof surface.

### Remaining Gates

- Run real restrictive-network acceptance for SSH tunnel, headscale/Tailscale,
  WireGuard, Chisel, and frpc paths across clean Windows/macOS/Linux targets,
  then package evidence with `rdev acceptance package-relay-adapter`.
- Add deeper runtime execution support where appropriate for mesh/VPN helper
  startup after explicit operator authorization, without weakening the existing
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
  authorization boundaries, evidence requirements, and Agent rules.
- Added `rdev.relay-adapter-package-verification.v1` through
  `rdev relay-adapter verify`. Verification checks schema, supported adapter
  kind, safe helper argv, safe dependency install action, file
  checksums/sizes, unlisted files, and no-private-surface hygiene.
- Added MCP tools `rdev.relay_adapter.package` and
  `rdev.relay_adapter.verify` so fresh Agents can discover and verify relay
  adapter packages instead of inventing PowerShell, shell, tunnel, authorization, or
  polling scripts.
- Added `rdev.acceptance-package.relay-adapter.v1` through
  `rdev acceptance package-relay-adapter`, plus
  `rdev.acceptance-verification.relay-adapter-package.v1` through
  `rdev acceptance verify-relay-adapter-package`. The package/verifier archives
  the verified relay adapter package, Connection Entry runner result, helper
  transcript, gateway status, host status, connection status, audit transcript,
  checksums, and redacted evidence. It fails unless the runner selected a
  standard connectivity adapter path and the connection status reports
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
  variables, authorization boundaries, and Agent rules.
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
verification still require explicit operator authorization.

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
metadata, status watching, or authorization polling by hand.

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
  tunnel, authorization, or polling scripts are forbidden. The fresh-agent acceptance
  gate now fails if this contract disappears from the standard one-command
  connection path.
- Added `rdev.support-session-agent-runbook.v1` to support-session handoff,
  prepare, create, start, high-level connect, status, and recovery payloads.
  Fresh Agents now get one machine-readable order of operations for the whole
  visible connection loop: when to run `cli_start_now_command`, what to send to
  the target-side human, how to wait, when to report `connected=true`, how to
  inspect capabilities, and how to recover without choosing lower-level tools or
  writing custom PowerShell, shell, relay, authorization, bootstrap, or polling
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
  invite creation, Windows bootstrap, background process glue, and authorization
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
  candidate but that gateway fails before any tasks are processed, the host now
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
  turn into hand-written PowerShell, shell, relay, bootstrap, or authorization
  polling code.
- Added `rdev.support-session-connected-next-steps.v1` to
  `rdev.support-session-status.v1`. When `connected=true`, Agents now receive a
  ready user report plus the next standard `rdev.sessions.get` follow-up,
  so they can proactively tell the user the connection is established and
  inspect capabilities before creating the smallest scoped task.
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
  bootstrap, or authorization-polling glue.
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
  automatic downgrade/upgrade rules and authorization boundaries for privileged,
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
- Added scoped attended-temporary auto-authorization metadata. `auto_authorize` can
  activate the first host only for an explicit attended-temporary Connection
  Entry, with audit events for both registration and auto-authorization.
- Added `--auto-authorize` to `rdev invite create` and `auto_authorize` to
  `rdev.invites.create`.
- Added regression coverage for foreground support-session start payloads,
  support-session create payloads, support-session plans, localized target
  commands, connection supervision, status feedback, verified helper assets,
  scoped auto-authorization, and ticket metadata snapshot copying.

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
  status now also drives the capability-inspection step before task creation.
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
runtime probes and authorization boundaries instead of documentation-only guidance.

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
- Added authorized helper startup and dependency install metadata for runner paths:
  `RDEV_*_START_ARGV_JSON` starts already-authorized helper argv, and
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
  authorization-gated.

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
manifest, transport, and authorization steps.

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
  profiles. The max-control profile lets an authorized remote host act as the
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
  connectivity/managed hosts, enrollment lifecycle, adapter tasks, and
  release/acceptance details now load only when the task needs them.
- Added Skill runtime memory guidance for dynamically retaining discovered
  environment facts, configuration paths, host capabilities, adapter/tool
  availability, and operator preferences outside the public repository.
- Added runtime-memory and stable-output expectations to host triage, remote task
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
  flow: the human runs only the generated target-host command and authorizes when
  policy requires it; the Agent handles discovery, waiting, task dispatch,
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
  authorization, signed tasks, local policy checks, or evidence.
- Documented max-control behavior for using the remote host as a field control
  point over reachable downstream devices while keeping evidence requirements
  and task-intent boundaries explicit.
- Updated README, MCP stdio, bootstrap, and remote-vibe-coding docs to prefer
  the universal one-link connection entry flow for all new target hosts while
  preserving visible consent, auditability, and revocation.
- Documented host-context-first progressive disclosure as the standard
  AI-native context model for remote sessions.
- Documented adaptive host-local provisioning for skills, MCP tools,
  dependencies, and adapter helpers with authorization gates for elevated,
  system-wide, credential, service, firewall, external, paid, publish, deploy,
  push, or persistent security-policy changes.
- Documented peer-Agent collaboration as a bounded adapter/collaborator path:
  A2A/MCP/local Agent work must still use signed tasks, host policy, workspace
  locks, redaction, authorizations, audit, and evidence.
- Documented target-host language matching for skills, MCP summaries, bootstrap
  instructions, authorizations, task status, and evidence summaries.
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
  completes the task over HTTPS long-poll.
- Join page and platform bootstrap script handlers are covered by HTTP tests.

## 0.1.3-dev

Current phase: production WSS/mTLS host transport, hosted storage/auth
foundation, and enrollment authority lifecycle evidence are implemented for
local release validation. External GitHub publication and real
Windows/Linux/macOS acceptance runs still require explicit operator authorization.

### Added

- Added WSS host task transport through `rdev host serve --transport wss` and
  `GET /v1/ws/hosts/{host_id}`, including WebSocket acknowledgements for task
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
- Final external GitHub publication after explicit authorization.

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
  and final external GitHub publication after explicit authorization.

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
  paths with bounded execution, redaction, cancellation evidence, authorization
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

- Added signed task envelopes, host identity proofs, nonce replay protection,
  authorization gates, authorization token consumption, trust pins, and workspace locks.
- Added host-side denial artifacts and authorization-required artifacts before
  adapter execution.
- Added evidence bundle export and hash-chained audit verification.

## 0.0.3-dev

Gateway and host loop pass.

### Added

- Added local dev gateway HTTP APIs for tickets, hosts, tasks, artifacts, audit
  events, and trust bundles.
- Added development HTTPS and mTLS listener/client preflight for local gateway
  and host flows.
- Added restart-safe development gateway state snapshots when `--state` is used
  with a persistent signing key.
- Added foreground temporary host registration, polling, task completion,
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
