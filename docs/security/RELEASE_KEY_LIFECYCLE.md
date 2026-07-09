# Release Key Lifecycle

This document defines the production release-key lifecycle for Remote Dev Skillkit. It covers release artifact signing, bootstrap manifest signing, key rotation, revocation, emergency response, and Windows Authenticode policy.

The policy applies to every public or production release of:

- `rdev`
- `rdev-host`
- `rdev-gateway`
- `rdev-mcp`
- `rdev-verify`
- bootstrap scripts
- release manifests
- join/bootstrap manifests

## Authority Split

Production must use separate trust authorities.

| Authority | Key scope | Online? | Purpose |
|---|---|---:|---|
| Release root | offline root or KMS/HSM root | no | root of trust for release artifact signatures |
| Release artifact signer | short-lived or delegated signing key | no by default | signs `rdev.release-artifact.v1` manifests |
| Bootstrap manifest signer | delegated key | limited | signs join/bootstrap manifests |
| Gateway session signer | gateway-held key | yes | signs session/control-plane records when deployment requires signed control state |
| Windows code-signing certificate | CA-issued or enterprise-trusted cert | signing service only | Authenticode signs Windows binaries/scripts |

No single key should be able to both publish software and authorize remote execution.

## Required Release Bundle

Every production release must publish:

- source archive;
- binaries for supported OS/arch targets;
- SHA-256 checksum file;
- `rdev.release-artifact.v1` manifest for each binary;
- release manifest index that lists every artifact and manifest;
- signed `rdev.release-bundle.v1` manifest index;
- SBOM when packaging is public;
- changelog and upgrade notes;
- Windows Authenticode signatures for Windows executables and scripts;
- notarization evidence for macOS releases when applicable.

The per-artifact release manifest must include key ids, artifact names, SHA-256
digests, sizes, timestamps, and signatures. The bundle index must include every
artifact path, manifest path, artifact and manifest SHA-256/size, required
artifact ids, signing key id, and index signature.

## Signing Rules

- Build artifacts in CI from a clean tag.
- Generate checksums before signing.
- Sign release manifests, not only release notes.
- Sign the release manifest index after all artifact manifests are complete.
- Verify the signed release bundle index with `rdev release verify-bundle`
  before publishing download links or bootstrap instructions.
- Time-stamp Windows Authenticode signatures.
- Never sign from a developer laptop for production releases.
- Never store release private keys in the repository, chat, CI logs, or local test fixtures.
- Development keys must use obvious development key ids such as `release-dev` and must not be trusted by production bootstrap.

## Windows Authenticode Policy

Windows releases must use platform-native Authenticode in addition to `rdev.release-artifact.v1`.

Requirements:

- `rdev-host.exe`, `rdev.exe`, `rdev-gateway.exe`, `rdev-mcp.exe`, and `rdev-verify.exe` must be Authenticode-signed.
- PowerShell bootstrap scripts should be Authenticode-signed when distributed as files.
- Signatures must use SHA-256 or stronger digest policy.
- Signatures must be time-stamped.
- CI must verify Authenticode signatures before publishing.
- The bootstrap path must reject invalid, missing, revoked, or unexpected signer certificates when Authenticode policy is enabled.
- The expected publisher/thumbprint or certificate chain policy must come from the pinned release policy, not from untrusted chat text.

Target-machine verification order for Windows temporary bootstrap:

1. Download `rdev-verify.exe`.
2. Check the pinned SHA-256 for `rdev-verify.exe`.
3. Check Authenticode for `rdev-verify.exe` when policy data is present.
4. Use `rdev-verify.exe` to verify the signed release bundle when a bundle
   index is distributed, or the signed `rdev-host.exe` release manifest for the
   current single-artifact bootstrap path.
5. Check Authenticode for `rdev-host.exe` when policy data is present.
6. Run `rdev-host.exe` in foreground temporary mode.

`Get-AuthenticodeSignature` is the baseline target-host check because it is available through PowerShell on Windows. `signtool verify` is the preferred CI/release check when Windows SDK tooling is available.

The bootstrap must not run `Set-ExecutionPolicy Bypass`, weaken Group Policy, disable Defender, or silently trust unsigned scripts.

References:

- Microsoft `Get-AuthenticodeSignature`: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.security/get-authenticodesignature
- Microsoft SignTool: https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool
- Microsoft SignTool verify: https://learn.microsoft.com/en-us/windows/win32/seccrypto/using-signtool-to-verify-a-file-signature
- Microsoft PowerShell execution policies: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_execution_policies

## Rotation

Routine rotation must be non-emergency and auditable.

Rotation steps:

1. Generate a new key with a new key id.
2. Publish the new public trust material in a signed trust bundle.
3. Keep the old key valid for a grace period.
4. Sign releases with the new key after clients can verify it.
5. Stop producing new signatures with the old key.
6. Mark the old key as retired after the grace period.
7. Keep retired public keys available for historical verification.

Managed hosts receive trust-bundle updates through signed managed-host policy updates. Temporary hosts receive trust material through the signed join/bootstrap manifest for that session.

The trust update primitive is `rdev.trust-bundle.v1`. A trust bundle contains:

- bundle id;
- monotonically increasing sequence;
- validity window;
- previous bundle hash;
- key id, public key, algorithm, status, and key validity;
- bundle signing key id;
- Ed25519 signature.

Key statuses are:

- `active`: may verify new tasks or manifests for its authority scope;
- `retired`: retained for historical verification but not new tasks;
- `revoked`: must be rejected for new verification and treated as an incident signal.

Managed hosts should accept an update only when:

- the signature verifies against an already trusted active root;
- the sequence increases;
- `previous_bundle_hash` matches the current bundle hash;
- the bundle is inside its validity window;
- the signing key has not been revoked.

The development host store writes verified trust bundles to a local `0600` JSON file using schema `rdev.host-trust-store.v1`. Production managed hosts should use OS-protected storage where available, but must keep the same update rules.

Operator-side bundle creation and drills are available through:

```bash
rdev trust init --out trust-bundle.json --root-key trust-root.json --gateway-key gateway-prod.json
rdev trust rotate --current trust-bundle.json --out trust-bundle-next.json --root-key trust-root.json --gateway-key gateway-next.json --gateway-key-id gateway-next --retire-key gateway-prod
rdev trust revoke --current trust-bundle-next.json --out trust-bundle-revoked.json --root-key trust-root.json --key-id gateway-next --reason "key compromise drill"
rdev trust verify --bundle trust-bundle-revoked.json --root-public-key trust-root:...
```

These commands are local-only. They generate and verify trust material, but do
not distribute it to managed hosts by themselves.

## Revocation

Revocation is required when a key is lost, exposed, misused, or suspected compromised.

Revocation actions:

- mark the key id revoked in the trust bundle;
- stop accepting artifacts or manifests signed by that key after the revocation time;
- publish a security advisory when public users may be affected;
- rotate to a new key;
- revoke affected tickets and host sessions if bootstrap trust is impacted;
- preserve audit evidence for the incident.

Revocation policy by authority:

| Compromised authority | Immediate action |
|---|---|
| Release root | freeze releases, publish emergency root migration, invalidate affected artifacts |
| Release artifact signer | stop releases, rotate signer, re-sign current artifacts |
| Bootstrap manifest signer | revoke active tickets/manifests, rotate signer, require new join URLs |
| Gateway task signer | revoke host trust bundle, cancel queued/running tasks, rotate gateway key |
| Windows code-signing cert | revoke cert, re-sign binaries, publish advisory |

## Emergency Stop

The gateway must have an operator-visible emergency stop that can:

- stop issuing new tickets;
- stop issuing new tasks;
- revoke all active temporary tickets;
- revoke a selected gateway session-signing key;
- mark release/bootstrap keys revoked in the trust bundle;
- cancel queued/running tasks for affected hosts;
- export audit evidence.

Emergency stop does not delete audit records.

## Acceptance Criteria

This policy is implemented when:

- release manifests include key ids and can be verified with pinned public roots;
- signed release bundle indexes verify every listed artifact manifest, binary,
  hash, size, and required artifact before publishing;
- Windows release artifacts are Authenticode-signed and CI verifies them;
- bootstrap rejects missing/invalid release signatures when policy requires them;
- managed hosts can receive signed trust-bundle updates;
- revoked keys stop new tasks, tickets, or release verification according to authority scope;
- release evidence is stored with the release artifacts.
