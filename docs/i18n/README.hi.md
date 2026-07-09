# Remote Dev Skillkit

Remote Dev Skillkit AI agents के लिए एक open-source, agent-native remote development Skillkit है। यह Codex, Claude Code, Hermes, OpenClaw/OpenCode और MCP agents को असली Mac, Windows और Linux hosts पर काम करने देता है, बिना उन्हें unrestricted shell देने के।

## यह क्या करता है

| Agent को मिलता है | Human के पास रहता है |
|---|---|
| Skills, MCP tools, file/desktop/task adapters | Visibility, authorization, revocation, audit |
| Clear capabilities वाले signed session tasks | Host-local policy और security boundaries |
| Artifacts और session evidence | क्या चलेगा और कब रुकेगा, इसका control |

## Agent से install

नीचे का text Agent को भेजें:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

पूरा contract [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md) में है।

<details>
<summary>Manual commands</summary>

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

</details>

## Use

1. Machine connect करें:

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. Tools और local demo देखें:

```bash
rdev mcp tools
rdev demo local
```

3. Basic evidence check करें:

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## Safety

Remote Dev Skillkit explicit, visible और consent-based remote development support के लिए है। Temporary third-party sessions visible, time-limited, revocable, auditable और default user-level होने चाहिए। Hidden persistence, UAC/sudo bypass, local security controls disable करना, या policy के बिना shell access स्वीकार नहीं है। License Apache-2.0 है।

## Docs

- Authoritative English README: [../../README.md](../../README.md)
- Docs index: [../README.md](../README.md)
