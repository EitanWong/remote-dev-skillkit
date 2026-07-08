# Remote Dev Skillkit

Remote Dev Skillkit AI agents के लिए एक open-source, agent-native remote development Skillkit है। यह Codex, Claude Code, Hermes, OpenClaw/OpenCode और MCP agents को असली Mac, Windows और Linux hosts पर काम करने देता है, बिना उन्हें unrestricted shell देने के।

यह project Agent Skills, MCP tools, signed jobs, host-local policy, approval gates, audit logs और evidence bundles को एक साथ रखता है। License Apache-2.0 है।

## Agent को भेजने वाला install prompt

नीचे का prompt अपने agent में copy-paste करें:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

पूरा contract [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md) में है।

## Manual quick start

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

## Local test

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

## Safety

Remote Dev Skillkit explicit, visible और consent-based remote development support के लिए है। Temporary third-party sessions visible, time-limited, revocable, auditable और default user-level होने चाहिए। Hidden persistence, UAC/sudo bypass, local security controls disable करना, या policy के बिना shell access स्वीकार नहीं है।

English [README](../../README.md) authoritative technical source है।
