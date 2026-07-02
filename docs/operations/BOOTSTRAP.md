# Bootstrap Design

## Temporary Windows Flow

1. Operator creates a ticket.
2. Remote user opens the join URL.
3. Join page displays operator, server, mode, capabilities, and expiration.
4. User runs the visible PowerShell bootstrap or a signed self-contained
   connection entry package.
5. Bootstrap downloads or unpacks the signed `rdev-host` binary.
6. Bootstrap verifies checksum/signature and the signed release bundle.
7. Host generates a keypair.
8. Host registers using the one-time ticket.
9. Gateway marks the host pending.
10. Operator approves.
11. Host receives signed policy and jobs.

During development, `rdev host serve --trust-pin sha256:<hex>` can pin the gateway signing public key from `GET /v1/trust`. Production bootstrap should derive this trust pin from the signed join manifest or a pinned trust root, not from unauthenticated chat text.

## Draft Script

The repository includes a visible foreground bootstrap draft:

```text
scripts/bootstrap/windows-temporary.ps1
```

It accepts:

- `GatewayUrl`
- `TicketCode`
- `DownloadUrl`
- `ExpectedSha256`
- optional `ManifestUrl`
- optional `ManifestRootPublicKey`
- optional `ReleaseManifestUrl`
- optional `ReleaseBundleUrl`
- optional `ReleaseBundleRequiredArtifacts`
- optional `ReleaseRootPublicKey`
- optional `VerifierDownloadUrl`
- optional `VerifierExpectedSha256`
- optional `TrustPin`
- optional `HostName`

The script downloads `rdev-host.exe` into a temp directory, verifies SHA-256, and runs:

```powershell
rdev-host.exe host serve --mode temporary --manifest-url <manifest-url>
```

It does not install a Windows Service, write registry persistence, weaken execution policy, or bypass UAC.

Managed Windows Service onboarding is separate from temporary bootstrap. Use
`rdev acceptance windows-managed-service` and
`rdev acceptance verify-windows-managed-service` to generate and check reviewed
`sc.exe` command plans before any owned-host Windows Service acceptance run.

Managed Linux systemd onboarding is also separate from temporary bootstrap. Use
`rdev acceptance linux-managed-service` and
`rdev acceptance verify-linux-managed-service` to generate and check a reviewed
systemd user-unit plan before any owned-host Linux acceptance run. These
commands write and verify the plan only; they do not run `systemctl`. After a
real Linux run, use `rdev acceptance package-linux-managed-service` to archive
the reviewed plan, unit, release-gate output, service transcripts, audit,
reconnect proof, and managed job evidence.

The repository also includes release artifact signature primitives:

```bash
rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
rdev-verify.exe --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:<base64url_ed25519_public_key>
rdev release create-bundle --dir dist --artifacts rdev-host.exe,rdev-verify.exe --require-artifacts rdev-host.exe,rdev-verify.exe --key .rdev/keys/release-root.json
rdev release verify-bundle --bundle dist/release-bundle.json --root-public-key release-root:<base64url_ed25519_public_key>
rdev-verify.exe --bundle dist/release-bundle.json --root-public-key release-root:<base64url_ed25519_public_key> --require-artifacts rdev-host.exe,rdev-verify.exe
```

This signs per-artifact manifests containing artifact names, SHA-256 values,
sizes, signing key ids, and Ed25519 signatures. The bundle command signs an
index binding the release artifacts to their manifests and expected hashes. The
standalone `rdev-verify` binary can verify either one artifact manifest or the
full signed bundle index after it has been hash-pinned by the bootstrap.

For hosts that already have the release bundle and root key locally, `rdev host
serve` can also enforce the same release gate before host registration:

```bash
rdev host serve \
  --mode managed \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --release-bundle /opt/rdev/release-bundle.json \
  --release-root-public-key release-root:<base64url_ed25519_public_key> \
  --release-require-artifacts rdev-host,rdev-verify
```

If bundle verification fails, the host does not register and does not poll for
jobs. `rdev host install-service` accepts the same three release flags and
writes them into macOS LaunchAgent and Linux systemd managed-host definitions,
or includes them in Windows `sc.exe create` command plans, so the managed host
self-checks the signed release bundle on restart. This is a startup integrity
gate, not a full auto-update or rollback system.

When `ReleaseManifestUrl` or `ReleaseBundleUrl` is provided, the Windows bootstrap requires `ReleaseRootPublicKey`, `VerifierDownloadUrl`, and `VerifierExpectedSha256`. It downloads `rdev-verify.exe`, checks the verifier SHA-256 first, then uses that verifier to validate either the signed `rdev-host.exe` release manifest or the signed release bundle before starting the host. Bundle mode downloads the bundle's listed manifest files, then runs `rdev-verify --bundle ... --require-artifacts rdev-host.exe,rdev-verify.exe` by default.

This avoids asking the untrusted host binary to verify itself. Production release signing and platform-native Windows code signing are governed by [Release Key Lifecycle](../security/RELEASE_KEY_LIFECYCLE.md).

When `ManifestUrl` is provided, the host fetches a signed `rdev.join-manifest.v1`, verifies it, then uses the manifest-provided gateway URL, ticket code and trust fingerprint. In production, the manifest should be rooted in a release/bootstrap trust root rather than only the gateway key advertised by the manifest.

`ManifestRootPublicKey` is formatted as:

```text
<key_id>:<base64url_ed25519_public_key>
```

When it is present, `rdev-host` verifies the join manifest with that pinned root before it trusts the embedded gateway job-signing bundle. Without it, `rdev-host` only uses the development self-trust path where the manifest is signed by the same gateway key it advertises.

## Bootstrap Requirements

- No Node/Python/Git dependency.
- No inbound port.
- Visible temporary mode by default.
- Admin elevation only via normal OS prompts.
- Clear stop and uninstall instructions.

## Agent-Created Connection Entry

Connection Entry is the universal name for the target-side handoff. Avoid
narrower names such as customer link or connector package plan in public
contracts: the same handoff covers owned managed workstations, third-party
temporary repair, LAN, hosted, relay, mesh, SSH, and VPN-assisted paths.

Agents should create target-side connection entries with `rdev.invites.create`
or `rdev invite create`. The resulting `rdev.agent-invite.v1` payload includes
`connection_entry`:

- `entry_url`: the page to open on the target machine;
- `package_catalog`: per-OS package candidates, package availability status,
  fallback script URLs, and release inputs the Agent needs before selecting a
  signed package;
- `one_line_commands`: inspectable macOS/Linux and Windows commands;
- `human_steps`: the minimal target-side actions;
- `agent_after_connect`: the actions the agent should perform after the host
  appears;
- `revocation_instructions`: how to stop the session and revoke access.

After invite creation, agents must call `rdev.connection_entry.plan` through
MCP or `rdev connection-entry plan` through the CLI before giving target-side
instructions. That materializes the invite into
`rdev.connection-entry.materialization-plan.v1`, including:

- the selected ownership/session-mode decision;
- the target OS/architecture package candidate selected from
  `connection_entry.package_catalog` or the signed join manifest's
  `package_catalog`;
- human-facing entry surfaces such as link, visible script, or signed package;
- Agent-only metadata such as ticket, gateway, manifest root, transport, release,
  and checksum inputs;
- missing release inputs that must be provided before packaging is ready;
- `runner_plan` with the self-contained Connection Entry runner manifest,
  launcher, helper policy, and connection-path selection order;
- `entry_package_plan` when a platform package/launcher plan can be generated.

The same payload includes `connection_entry_plan.target_selection_policy`.
Agents should use it before materialization: owned personal or fleet machines
default to `managed`, third-party or one-off machines default to
`attended-temporary`, and ambiguous ownership or persistence approval requires
one short operator question before creating a managed entry.

When an agent provides an empty `out_dir`, the MCP and CLI materializers write a
target-side `CONNECTION_ENTRY.md`, the machine-readable
`connection-entry-plan.json`, a `connection-entry-runner/` directory with
`connection-entry-runner.json`, a visible platform launcher, and any additional
launcher/package planning files. Those generated files are the target-side
handoff; ticket, gateway, root, transport, release, relay, mesh, VPN, SSH, and
checksum values remain inside the plan for Agent/tool use.

The runner is real executable product surface, not only documentation. On the
target side it can be dry-run with:

```sh
rdev connection-entry run --runner-manifest connection-entry-runner.json --dry-run
```

Without `--dry-run`, it probes direct gateway reachability first, lets
`rdev host serve --transport auto` handle WSS, HTTPS long-poll, and short-poll
fallback, then considers already configured helper paths. Helper paths are
selected only when both the tool and an explicit gateway override are present,
for example `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`,
`RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL`. The runner may reuse existing
SSH/frp/Chisel/headscale/Tailscale-compatible/WireGuard routing. When an
approved dependency install action is present, it can also invoke
`rdev deps install` to download, SHA-256 verify, unpack, and use user/workspace
scoped helper binaries such as `chisel` or `frpc` without changing PATH,
installing services, or mutating firewall/DNS/routes. Creating credentials,
changing routes, firewall, DNS, paid/cloud resources, mesh/VPN service
enrollment, drivers, or persistent services remains an approval-gated follow-up.

For Windows attended temporary support, the current package implementation wraps
the existing Windows temporary acceptance plan as
`rdev.connection-entry.package-plan.v1`. For operator-owned managed machines,
the same package surface now wraps reviewed macOS LaunchAgent, Linux systemd
user-service, and Windows Service plans. The materializer writes those plans and
service files for review; it does not start, install, persist, or uninstall a
service unless the operator later runs the explicit service-control commands.
LAN, hosted, relay, mesh, SSH, and VPN planners now attach to the same generic
Connection Entry runner and Package Plan surface rather than creating new
human-facing parameter lists.

Agents should treat `connection_entry_plan` as the universal connection
decision contract rather than exposing ticket codes, gateway URLs, manifest
roots, or transport flags to a human. For a managed owned host, the plan should
include release gates, enrollment renewal, revocation refresh, reconnect proof,
and stop/uninstall instructions. For attended temporary support, the plan should
keep the session visible with no persistence by default.

The same invite also includes `host_context_plan`, which tells agents to keep
remote environment details, project file structure, requirement notes,
transcripts, logs, and large evidence on the target host by default. The Agent
server should keep small indexes and request exact context slices only when a
job step needs them.

`agent_provisioning_plan` defines the companion setup rule. After the host
connects, the Agent may probe installed skills, MCP tools, adapters, language
runtimes, package managers, project dependency manifests, framework paths,
network/proxy settings, and permissions. If missing setup is needed for the
support task, the Agent should prefer verified, user-scoped or workspace-scoped
installs on the target host and record the install plan, source, checksum,
commands, exit codes, and post-install capability report as evidence. It should
pause for approval before elevation, system-wide packages, service changes,
credential changes, firewall changes, external account mutation, paid resource
use, or publish/deploy/push actions.

`agent_collaboration_plan` defines the peer-Agent rule. When the target host
has other AI tools or Agents installed, the Agent may discover configured A2A
Agent Cards, local MCP servers, and local Agent CLIs. It can ask those peers to
perform bounded diagnostics, summaries, coding subtasks, or tool-specific
checks, but all delegation must remain inside rdev signed jobs, host policy,
workspace locks, redaction, approval gates, audit, and evidence.

`localization_plan` defines the language rule. The join page supports `?lang=`
and `Accept-Language` matching for the repository's supported languages:
English, Simplified Chinese, Spanish, French, German, Japanese, Korean,
Brazilian Portuguese, Hindi, Arabic, and Russian. After the host connects, the
Agent should also inspect the target OS locale and user language settings, then
localize target-side instructions and approval text. Commands, paths, schema
keys, checksums, and code blocks are not translated.

`managed_development_plan` defines the long-running owned-workstation rule.
For machines controlled by the operator, prefer managed mode with explicit
service plans, `--once=false`, `--transport auto`, release-bundle startup
verification, enrollment renewal, revocation refresh, workspace locks, Git
worktrees, host-local context caches, reconnect proof, audit slices, and
evidence bundles. This is separate from attended temporary third-party support.

The development gateway serves:

```text
/join/<ticket>
/join/<ticket>/bootstrap.sh
/join/<ticket>/bootstrap.ps1
```

These helpers currently require a verified `rdev` release package to be
available on the target machine, then run a visible attended host session:

```bash
rdev host serve --manifest-url <manifest-url> --transport auto --once=false
```

This is the one-link connection entry path for support and debugging. It is an attended
temporary session: no background service is installed, no persistent execution
policy is changed, and no persistence is created by the temporary bootstrap.
The Windows one-line command may use process-scoped
`-ExecutionPolicy Bypass` so the bootstrap can run in locked-down default shell
contexts, but it must not call `Set-ExecutionPolicy` or change machine/user
policy. The bootstrap script now carries the pinned
`--manifest-root-public-key`, so target-side users do not need to copy trust
roots, ticket codes, gateway URLs, or transport flags from chat.

For new target-host connections, agents should prefer the invite's
`connection_entry.package_catalog` and the signed join manifest's
`package_catalog` before choosing a target package. The connection entry package
is the no-prerequisite path when release assets are published: one
platform-specific bundle with `rdev`/`rdev-host`, signed release-bundle
evidence, the join manifest URL, the pinned manifest root, visible stop/revoke
instructions, and `--transport auto` fallback. If package assets are not
published or release inputs are missing, the Agent should use the catalog's
visible `/bootstrap.sh` or `/bootstrap.ps1` fallback rather than asking the
target-side human to assemble raw values. It should probe WSS, HTTPS long-poll,
short-poll, LAN/private gateway reachability, and already configured
proxy/relay/mesh/SSH paths before asking the human for more network details.
Elevation is allowed only through normal UAC/sudo/system prompts for a signed
job that needs it, and the approval must be recorded as evidence.

## Managed Device Flow

Managed service installation is a separate explicit step:

```bash
rdev host install-service \
  --platform macos \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --workspace-lock-store ~/.rdev/host/workspace-locks \
  --plist-out ~/Library/LaunchAgents/com.remote-dev-skillkit.host.plist
```

On macOS, the current command writes a LaunchAgent plist and prints the explicit `launchctl bootstrap`, `launchctl bootout`, and `launchctl print` commands. It does not run `launchctl` automatically. This command is not used for third-party temporary sessions. When `--workspace-lock-store` is set, managed jobs enforce one-writer workspace locks before adapter execution.

Inspect or remove the LaunchAgent plist without executing `launchctl`:

```bash
rdev host service-status \
  --platform macos \
  --plist ~/Library/LaunchAgents/com.remote-dev-skillkit.host.plist

rdev host uninstall-service \
  --platform macos \
  --label com.remote-dev-skillkit.host \
  --plist ~/Library/LaunchAgents/com.remote-dev-skillkit.host.plist
```

`uninstall-service` removes only the plist file. It refuses to remove a plist whose embedded label does not match `--label` unless `--force` is explicitly set.

Windows managed service setup is also explicit and review-first:

```powershell
rdev host install-service `
  --platform windows `
  --label RemoteDevSkillkitHost `
  --binary 'C:\Program Files\rdev\rdev.exe' `
  --gateway https://api.example.com/v1 `
  --ticket-code ABCD-1234 `
  --workspace-lock-store 'C:\ProgramData\rdev\workspace-locks' `
  --release-bundle 'C:\Program Files\rdev\release-bundle.json' `
  --release-root-public-key release-root:<base64url_ed25519_public_key> `
  --release-require-artifacts rdev-host.exe,rdev-verify.exe
```

The Windows command emits `sc.exe create` and `sc.exe description` plans with
`start= demand`; it does not install or start the service automatically. Review
the plan, run the create command from an elevated PowerShell session only for
explicitly managed hosts, then use `rdev host service-control --platform
windows --action start --execute` when you intentionally want to invoke
`sc.exe`. This path must not be used for third-party temporary sessions.
