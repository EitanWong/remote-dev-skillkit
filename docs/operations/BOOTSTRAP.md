# Bootstrap Design

## Temporary Windows Flow

1. Operator creates a ticket.
2. Remote user opens the join URL.
3. Join page displays operator, server, mode, capabilities, and expiration.
4. User runs PowerShell bootstrap.
5. Bootstrap downloads the signed `rdev-host` binary.
6. Bootstrap verifies checksum/signature.
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

## Agent-Created Customer Link

Agents should create customer-facing bootstrap links with `rdev.invites.create`
or `rdev invite create`. The resulting `rdev.agent-invite.v1` payload includes
`customer_bootstrap`:

- `customer_link`: the page to send to the target-side user;
- `one_line_commands`: inspectable macOS/Linux and Windows commands;
- `customer_steps`: the minimal target-side actions;
- `agent_after_connect`: the actions the agent should perform after the host
  appears;
- `revocation_instructions`: how to stop the session and revoke access.

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

This is the one-link customer path for support and debugging. It is an attended
temporary session: no background service is installed, no persistent execution
policy is changed, and no persistence is created by the temporary bootstrap.
The Windows one-line command may use process-scoped
`-ExecutionPolicy Bypass` so the bootstrap can run in locked-down default shell
contexts, but it must not call `Set-ExecutionPolicy` or change machine/user
policy.

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
