# Release Checklist

## Pre-Release

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] Build binaries for macOS, Linux, and Windows.
- [ ] Generate SHA-256 checksums.
- [ ] Sign release artifacts.
- [ ] Sign and verify the release manifest index.
- [ ] Export and verify the Skillkit bundle with `rdev skillkit export` and `rdev skillkit verify`.
- [ ] Prepare a local release candidate with `rdev release prepare-candidate`.
- [ ] Verify the local release candidate with `rdev release verify-candidate` once implemented.
- [ ] Create signed release bundle index with `rdev release create-bundle`.
- [ ] Verify signed release bundle index with `rdev release verify-bundle`.
- [ ] Verify the same signed bundle with standalone `rdev-verify --bundle`.
- [ ] Authenticode-sign Windows binaries and scripts.
- [ ] Verify Windows Authenticode signatures in CI.
- [ ] Confirm release key ids are active and not revoked.
- [ ] Package Windows temporary acceptance evidence with `rdev acceptance package-windows-temporary` when Windows support is in scope.
- [ ] Archive release evidence: checksums, manifests, signatures, SBOM, verification logs, redacted acceptance packages, and audit-chain exports.
- [ ] Update `CHANGELOG.md`.
- [ ] Update install docs.
- [ ] Review `SECURITY.md` and threat model.
- [ ] Run the acceptance checklist for the release target.

## Security Gates

- [ ] No hidden persistence.
- [ ] No UAC bypass.
- [ ] No unrestricted shell tool.
- [ ] No hardcoded secrets.
- [ ] No target-host inbound public port by default.
- [ ] Dangerous actions require approval gates.
- [ ] Bootstrap does not weaken PowerShell execution policy or Group Policy.
- [ ] Temporary Windows bootstrap verifies signed manifests and binaries before execution.
- [ ] Acceptance evidence has been collected and redacted.

## Post-Release

- [ ] Create GitHub release.
- [ ] Verify release downloads.
- [ ] Install on a clean Windows VM.
- [ ] Install on macOS.
- [ ] Install on Linux.
- [ ] Publish release notes.
