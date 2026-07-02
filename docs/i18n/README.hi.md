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

Clone or update the repository in a safe user/workspace location. Then read
`docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow it as
the source of truth. If cloning is blocked, read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Install the Skillkit and configure MCP for this agent. Probe OS, shell, Git, Go,
`rdev`, current agent framework, skill directory, MCP config, and network state
before acting. Ask one short question when a required value is unclear. For this
personal computer, prefer local MCP stdio with `rdev mcp serve`; do not require a
hosted gateway URL.

When I ask you to work on another machine, always create a Connection Entry.
Treat it as the universal target-side handoff for every scenario: my own durable
computer, a third-party temporary repair machine, LAN, hosted, relay, mesh, SSH,
or VPN-assisted connectivity. Do not ask humans to assemble ticket, root,
gateway, transport, release, or checksum flags. Use `rdev.invites.create`, then
materialize it with `rdev.connection_entry.plan` or `rdev connection-entry plan`.
Give the target side only the selected link, visible script, or signed package;
keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed;
prefer existing, open-source/free options such as frp, Chisel, headscale, or
WireGuard; ask before privileged, persistent, paid, firewall, DNS, cloud, or
security-policy changes. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
```

पूरा copy-paste Prompt: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
```

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
rdev demo local
rdev mcp tools
```

Technical authority English [README](../../README.md) है। Translation अलग लगे तो English README follow करें।
