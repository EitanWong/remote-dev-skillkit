# Release Checklist

## Pre-Release

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `./scripts/check.sh`
- [ ] `./scripts/ci/release-smoke.sh`
- [ ] Confirm the release still satisfies `Definitive Perfect-Ending Blueprint - 2026-06-30` in `docs/architecture/PERFECT_ENDING_SOLUTION.md`, especially mode separation, host sovereignty validation, adapter lifecycle, evidence/audit proof, and the v1.0 gate table.
- [ ] Confirm adapter scaffolds, lifecycle manifests, runtime fixtures, hostrunner `--capture-runtime-fixture` artifacts, built-in adapter result artifacts, and cancellation artifacts pass `pkg/adapterkit`, `adapterkit.RunLifecycle`, `rdev adapter scaffold`, `rdev adapter verify-lifecycle`, `rdev adapter verify-runtime`, `rdev adapter verify-result`, `rdev adapter verify-cancellation`, `rdev.adapter.verify_lifecycle`, `rdev.adapter.verify_runtime`, `rdev.adapter.verify_result`, and `rdev.adapter.verify_cancellation` conformance tests.
- [ ] GitHub Actions CI passes for the release commit.
- [ ] Build binaries for macOS, Linux, and Windows with `scripts/release/build-artifacts.sh`.
- [ ] Review `rdev.build-artifacts.v1` and `checksums.txt`.
- [ ] Confirm macOS artifacts that claim Keychain-backed managed host identity/trust support were built natively with `cgo_enabled=true`, or explicitly document that `keychain:` stores are unavailable in that artifact.
- [ ] Confirm Windows artifacts that claim DPAPI-backed managed host identity/trust support were built for Windows and have at least cross-compiled `internal/protectedstore` / `rdev-host` coverage; real Windows runtime acceptance remains release-blocking before support claims.
- [ ] Confirm Linux artifacts that claim libsecret-backed managed host identity/trust support were built for Linux and document the runtime `secret-tool` plus Secret Service requirement; confirm Linux artifacts that claim keyctl-backed managed host identity/trust support were built for Linux and document the runtime `keyctl` plus user keyring requirement. Real Linux service reboot/reconnect acceptance remains release-blocking before support claims.
- [ ] Generate SHA-256 checksums.
- [ ] Sign release artifacts.
- [ ] Sign and verify the release manifest index.
- [ ] Export and verify the Skillkit bundle with `rdev skillkit export` and `rdev skillkit verify`.
- [ ] Generate and verify a framework install plan with `rdev skillkit plan-install` and `rdev skillkit verify-install-plan`; archive `rdev.skillkit-install-plan.v1`, `rdev.skillkit-install-plan-verification.v1`, `INSTALL_COMMANDS.md`, and generated scripts.
- [ ] Run `rdev skillkit install` dry-run and execute smoke into a temporary target directory; archive `rdev.skillkit-install-report.v1` outputs proving `external_mutation=false`.
- [ ] Prepare and verify per-platform candidates with `scripts/release/prepare-platform-candidates.sh`.
- [ ] Review `rdev.platform-release-candidates.v1`.
- [ ] Generate and review a multi-platform GitHub Release dry-run plan with `scripts/github/plan-platform-release.sh`.
- [ ] Review `rdev.github-platform-release-plan.v1`, `rdev.platform-release-index.v1`, `rdev.github-platform-release-verification.v1`, and `INSTALL_PLATFORMS.md`.
- [ ] Run `scripts/github/audit-project-readiness.sh --repo <owner/repo> --out <path>` and archive `rdev.github-project-readiness.v1` before external GitHub mutation.
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
- [ ] Smoke `rdev host serve --release-bundle ... --release-root-public-key ...` before release publication.
- [ ] Smoke `rdev enrollment sign-certificate`, `rdev enrollment verify-certificate`, `rdev enrollment init-revocations`, `rdev enrollment renew-certificate --revocations`, `rdev enrollment revoke-certificate --current`, `rdev enrollment verify-revocations`, `GET /v1/enrollment/revocations` with both empty and non-empty lists, `rdev enrollment fetch-revocations`, `rdev enrollment verify-certificate --revocations`, MCP tool `rdev.enrollment.verify_certificate` with revocation-list input, `rdev gateway serve --dev --enrollment-root-public-key ... --enrollment-revocations ...`, and `rdev host serve --enrollment-certificate ...` before making enrollment-certificate or revocation support claims.
- [ ] Smoke `rdev gateway serve --dev --tls-cert ... --tls-key ... --client-ca ...` with one request that fails without a client certificate, one request that succeeds with a CA-signed client certificate, and `rdev host serve --gateway https://127.0.0.1:<port> --gateway-ca ... --gateway-client-cert ... --gateway-client-key ...` registering through the mTLS gateway before making dev mTLS support claims.
- [ ] Confirm generated managed LaunchAgent/systemd definitions and Windows Service command plans include the release bundle gate when managed hosts are in scope.
- [ ] Generate and verify managed Mac LaunchAgent acceptance plans with `rdev acceptance managed-mac-service` and `rdev acceptance verify-managed-mac-service` before any service-backed managed Mac support claim.
- [ ] Package managed Mac LaunchAgent acceptance evidence with `rdev acceptance package-managed-mac-service` when managed Mac service-backed support is in scope.
- [ ] Generate and verify Windows managed-service acceptance plans with `rdev acceptance windows-managed-service` and `rdev acceptance verify-windows-managed-service` before any Windows Service support claim.
- [ ] Generate and verify Linux managed-service acceptance plans with `rdev acceptance linux-managed-service` and `rdev acceptance verify-linux-managed-service` before any Linux systemd managed-service support claim.
- [ ] Package Linux managed-service acceptance evidence with `rdev acceptance package-linux-managed-service` when Linux systemd managed-service support is in scope.
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
