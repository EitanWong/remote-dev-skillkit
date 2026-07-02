# Remote Dev Skillkit

Remote Dev Skillkit هو طبقة أمان لوكلاء الذكاء الاصطناعي الذين يحتاجون إلى العمل على أجهزة Mac وWindows وLinux حقيقية. ليس أداة تحكم بعيد مخفية؛ بل يسمح لـ Codex وClaude Code وHermes وOpenClaw/OpenCode ووكلاء MCP العامة بتنفيذ عمل تطوير حقيقي عبر مهام موقعة، وسياسة محلية على الجهاز، وموافقات، وحزم أدلة، وسجل تدقيق.

## أهم النقاط

- Agent Skillkit محمول يمكن تثبيته في Agent Frameworks الشائعة.
- يتم توقيع jobs والتحقق منها ثم تنفيذها؛ لا يحصل الوكيل على shell خام بلا حدود.
- تتحقق skills أولا من OS وshell وservice manager وgateway وworkspace وadapter والصلاحيات؛ إذا كان شيء غير واضح فهي تسأل بدلا من التخمين.
- دعم Codex وClaude Code وACP/acpx وshell وPowerShell وcustom adapters.
- ترخيص مفتوح المصدر Apache-2.0.

## التثبيت السريع

إذا كنت داخل Codex أو Claude Code أو Hermes أو OpenClaw/OpenCode أو وكيل MCP آخر، انسخ هذا إلى الوكيل:

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
Read `connection_entry.package_catalog` and the signed join manifest
`package_catalog` to select the target OS/architecture package candidate.
Give the target side only the selected link, visible runner/launcher,
visible script, or signed package. Prefer the materialized Connection Entry
runner when `runner_plan` is available; dry-run it before execute when network
reliability is uncertain. If release package assets are not published yet, use
the visible fallback script. Keep low-level parameters in Agent/tool metadata. Choose `managed` for my own
long-running machines and `attended-temporary` for third-party or one-off repair
machines by following `connection_entry_plan.target_selection_policy`.
Auto-select LAN, hosted, SSH, relay, mesh, or VPN paths as needed. Use
existing open-source/free options through configured helper gateway overrides such
as `RDEV_RELAY_GATEWAY_URL`, `RDEV_MESH_GATEWAY_URL`,
`RDEV_VPN_GATEWAY_URL`, or `RDEV_SSH_GATEWAY_URL` when unambiguous; prefer frp,
Chisel, headscale-compatible mesh, or WireGuard before paid relays. If a
user-space helper is missing and an explicit URL/SHA-256 is approved, use
`rdev deps install` or reviewed `RDEV_*_INSTALL_ACTION_JSON` metadata for
user/workspace-scoped helper repair before helper startup; ask before
privileged, persistent, paid, firewall, DNS, cloud, credential, route, or
security-policy changes. Dry-run before execute. Do not hardcode private paths,
secrets, or server addresses; example URLs are placeholders only.
```

النص الكامل للنسخ: [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

```bash
go install ./cmd/rdev
rdev doctor
```

صدّر وتحقق من Skillkit bundle:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
```

أنشئ خطة تثبيت قابلة للمراجعة لأطر الوكلاء:

```bash
rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

التثبيت المباشر يعمل كـ dry-run افتراضيا. راجع التقرير ثم أضف `--execute`:

```bash
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills
rdev skillkit install --bundle dist/remote-dev-skillkit --framework codex --target ~/.codex/skills --execute
```

سطر install الثاني هو مسار one-command install بعد التحقق من bundle.

## تجربة محلية

```bash
go test ./...
rdev demo local
rdev mcp tools
```

المرجع التقني المعتمد هو [README](../../README.md) باللغة الإنجليزية؛ عند التعارض اتبع النسخة الإنجليزية.
