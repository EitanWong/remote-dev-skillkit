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

First clone or update the repository in a safe user/workspace location. Then
read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md` from the checkout and follow
that full prompt as the source of truth for installation. If cloning is blocked,
read the prompt from:
https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md

Probe my OS, shell, Git, Go, `rdev`, current agent framework, skill directory,
MCP config, network reachability, proxy/DNS state, NAT/firewall/CGNAT hints,
SSH config, installed tunnel/mesh tools, and available connection modes before
acting. If a required value is unclear, ask me one short question instead of
guessing. For a personal computer agent install, use local MCP stdio with
`rdev mcp serve`; do not require a hosted gateway URL. When remote hosts are
needed, auto-select the safest working mode: local dev gateway, LAN-reachable
gateway, hosted gateway, SSH tunnel, or relay/mesh/VPN. For remote hosts, create
an invite and present `connection_entry.entry_url` or a signed connection entry
package instead of asking humans to assemble ticket, root, gateway, or transport
flags. If tunneling is needed,
prefer open-source/free options first, such as frp, Chisel, headscale, or
WireGuard, after probing what is already installed. Dry-run before execute. Do
not hardcode private paths, secrets, or server addresses; treat
`https://api.example.com/v1` only as optional hosted-gateway placeholder
metadata.
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
