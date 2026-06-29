# Release Checklist

## Pre-Release

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `./scripts/check.sh`
- [ ] `./scripts/ci/release-smoke.sh`
- [ ] Confirm adapter scaffolds, lifecycle manifests, built-in adapter result artifacts, and cancellation artifacts pass `pkg/adapterkit`, `rdev adapter scaffold`, `rdev adapter verify-lifecycle`, `rdev adapter verify-result`, `rdev adapter verify-cancellation`, `rdev.adapter.verify_lifecycle`, `rdev.adapter.verify_result`, and `rdev.adapter.verify_cancellation` conformance tests.
- [ ] GitHub Actions CI passes for the release commit.
- [ ] Build binaries for macOS, Linux, and Windows with `scripts/release/build-artifacts.sh`.
- [ ] Review `rdev.build-artifacts.v1` and `checksums.txt`.
- [ ] Generate SHA-256 checksums.
- [ ] Sign release artifacts.
- [ ] Sign and verify the release manifest index.
- [ ] Export and verify the Skillkit bundle with `rdev skillkit export` and `rdev skillkit verify`.
- [ ] Prepare and verify per-platform candidates with `scripts/release/prepare-platform-candidates.sh`.
- [ ] Review `rdev.platform-release-candidates.v1`.
- [ ] Generate and review a multi-platform GitHub Release dry-run plan with `scripts/github/plan-platform-release.sh`.
- [ ] Review `rdev.github-platform-release-plan.v1`, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, and `INSTALL_PLATFORMS.md`.
- [ ] Generate and review a post-release install verification plan with `scripts/github/plan-post-release-install.sh`.
- [ ] Archive `rdev.post-release-install-plan.v1`, `VERIFY_INSTALL.md`, generated platform verification scripts, and Skillkit verification script.
- [ ] Verify the post-release install plan with `scripts/github/verify-post-release-install-plan.sh`.
- [ ] Archive `rdev.post-release-install-verification.v1`.
- [ ] Prepare a local release candidate with `rdev release prepare-candidate`.
- [ ] Verify the local release candidate with `rdev release verify-candidate`.
- [ ] Generate and review a GitHub Release dry-run plan with `scripts/github/plan-release.sh`.
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
- [ ] GitHub Release commands were reviewed from a dry-run plan before any external mutation.

## Post-Release

- [ ] Create GitHub release.
- [ ] Verify release downloads.
- [ ] Run generated post-release verification scripts for macOS, Linux, Windows, and Skillkit where applicable.
- [ ] Install on a clean Windows VM.
- [ ] Install on macOS.
- [ ] Install on Linux.
- [ ] Publish release notes.
