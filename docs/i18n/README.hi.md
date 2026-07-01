# Remote Dev Skillkit

Remote Dev Skillkit उन AI Agent के लिए safety layer है जिन्हें असली Mac, Windows और Linux machines पर काम करना होता है। यह कोई छिपा हुआ remote-control tool नहीं है। Codex, Claude Code, Hermes, OpenClaw/OpenCode और generic MCP Agent signed jobs, host-local policy, approvals, evidence bundles और audit chains के साथ असली development work चला सकते हैं।

## मुख्य बातें

- Popular Agent Frameworks में install होने वाला portable Agent Skillkit।
- Jobs पहले signed और verified होते हैं, फिर run होते हैं; Agent को raw shell खुली छूट नहीं मिलती।
- Skills पहले OS, shell, service manager, gateway, workspace, adapter और permissions detect करती हैं। unclear हो तो guess नहीं करतीं, पूछती हैं।
- Codex, Claude Code, ACP/acpx, shell, PowerShell और custom adapters के लिए support।
- Apache-2.0 open-source license।

## Fast Install

अगर आप पहले से Codex, Claude Code, Hermes, OpenClaw/OpenCode या किसी दूसरे MCP Agent में हैं, तो यह अपने Agent में paste करें:

```text
Bootstrap Remote Dev Skillkit for this agent runtime.

Repository: https://github.com/EitanWong/remote-dev-skillkit

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
and MCP config before acting. If a required value is unclear, ask me one short
question instead of guessing. Dry-run before execute. Do not hardcode private
paths, secrets, or server addresses; use `https://api.example.com/v1` only as
placeholder metadata until I provide a real gateway URL.
```

पूरा copy-paste Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
```

Skillkit bundle export और verify करें:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

Agent Frameworks के लिए reviewable install plan बनाएं:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

Direct install default रूप से dry-run है। Report देखें, फिर `--execute` जोड़ें:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

दूसरी install line verified bundle मिलने के बाद one-command install path है।

## Local Demo

```bash
go test ./...
rdev demo local
rdev mcp tools
```

Technical authority English [README](../../README.md) है। Translation अलग लगे तो English README follow करें।
