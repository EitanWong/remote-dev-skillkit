# Remote Dev Skillkit

Remote Dev Skillkit उन AI Agent के लिए safety layer है जिन्हें असली Mac, Windows और Linux machines पर काम करना होता है। यह कोई छिपा हुआ remote-control tool नहीं है। Codex, Claude Code, Hermes, OpenClaw/OpenCode और generic MCP Agent signed jobs, host-local policy, approvals, evidence bundles और audit chains के साथ असली development work चला सकते हैं।

इसका connection model Agent के लिए remote-control connector जैसा है: target machine एक visible connector खोलती है, Agent standard Support Device Entry (`support_device_id` + session password) इस्तेमाल करता है, और operator के स्पष्ट अनुरोध से पहले disconnect नहीं करता।

## मुख्य बातें

- Popular Agent Frameworks में install होने वाला portable Agent Skillkit।
- Jobs पहले signed और verified होते हैं, फिर run होते हैं; Agent को raw shell खुली छूट नहीं मिलती।
- Standard `rdev.files.*` / `rdev.desktop.*` surfaces file transfer/delete, screenshots/frames, windows, keyboard/mouse, clipboard, apps और URLs को cover करते हैं।
- Skills पहले OS, shell, service manager, gateway, workspace, adapter और permissions detect करती हैं। unclear हो तो guess नहीं करतीं, पूछती हैं।
- Codex, Claude Code, ACP/acpx, shell, PowerShell और custom adapters के लिए support।
- Apache-2.0 open-source license।

## Fast Install

अगर आप पहले से Codex, Claude Code, Hermes, OpenClaw/OpenCode या किसी दूसरे MCP Agent में हैं, तो Agent को यह लिंक भेजें और कहें कि action लेने से पहले पूरा prompt पढ़े:

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

यही prompt installation का source of truth है: repo clone या update करना, current Agent runtime detect करना, Skillkit install करना, MCP configure करना, और कोई required value safely detect न हो पाए तो सिर्फ एक छोटा सवाल पूछना।

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

अगर `rdev` `PATH` में नहीं है, तो `rdev bootstrap agent-plan` या install report के `mcp_command` field में दिया गया absolute MCP command इस्तेमाल करें।

Skillkit bundle export और verify करें:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
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
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

Technical authority English [README](../../README.md) है। Translation अलग लगे तो English README follow करें।
