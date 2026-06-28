# Release Checklist

## Pre-Release

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] Build binaries for macOS, Linux, and Windows.
- [ ] Generate SHA-256 checksums.
- [ ] Sign release artifacts.
- [ ] Update `CHANGELOG.md`.
- [ ] Update install docs.
- [ ] Review `SECURITY.md` and threat model.

## Security Gates

- [ ] No hidden persistence.
- [ ] No UAC bypass.
- [ ] No unrestricted shell tool.
- [ ] No hardcoded secrets.
- [ ] No target-host inbound public port by default.
- [ ] Dangerous actions require approval gates.

## Post-Release

- [ ] Create GitHub release.
- [ ] Verify release downloads.
- [ ] Install on a clean Windows VM.
- [ ] Install on macOS.
- [ ] Install on Linux.
- [ ] Publish release notes.
