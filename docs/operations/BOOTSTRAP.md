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

The repository also includes release artifact signature primitives:

```bash
rdev release sign --artifact ./rdev-host.exe --key .rdev/keys/release-root.json
rdev-verify.exe --artifact ./rdev-host.exe --manifest ./rdev-host.exe.rdev-release.json --root-public-key release-root:<base64url_ed25519_public_key>
```

This signs a manifest containing the artifact name, SHA-256, size, signing key id and Ed25519 signature.

When `ReleaseManifestUrl` is provided, the Windows bootstrap requires `ReleaseRootPublicKey`, `VerifierDownloadUrl`, and `VerifierExpectedSha256`. It downloads `rdev-verify.exe`, checks the verifier SHA-256 first, then uses that verifier to validate the signed `rdev-host.exe` release manifest before starting the host.

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

## Managed Device Flow

Managed service installation is a separate explicit step:

```bash
rdev host install-service \
  --platform macos \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --plist-out ~/Library/LaunchAgents/com.remote-dev-skillkit.host.plist
```

On macOS, the current command writes a LaunchAgent plist and prints the explicit `launchctl bootstrap`, `launchctl bootout`, and `launchctl print` commands. It does not run `launchctl` automatically. This command is not used for third-party temporary sessions.

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
