# Remote Dev Skillkit

Remote Dev Skillkit Agent-आधारित remote development के लिए एक safety kernel है। यह कोई छिपा हुआ remote-control tool नहीं है। यह Codex, Claude Code, Hermes, OpenClaw/OpenCode और generic MCP Agents को signed jobs, host-local policy, approvals, evidence और audit trail के साथ वास्तविक machines पर काम delegate करने देता है।

## यह क्या देता है

- `rdev` CLI और host, gateway, MCP, verifier binaries।
- Export, verify और install किए जा सकने वाले Agent Skillkit bundles।
- Signed job envelopes, workspace locks, approval gates, evidence bundles और audit chains।
- Codex, Claude Code, ACP/acpx, shell और PowerShell adapters।
- Apache-2.0 open-source license।

## Local Verification

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## Skillkit Install

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Technical authority के लिए English [README](../../README.md) देखें।
