# Remote Dev Skillkit

Remote Dev Skillkit هو طبقة أمان لوكلاء الذكاء الاصطناعي الذين يحتاجون إلى العمل على أجهزة Mac وWindows وLinux حقيقية. ليس أداة تحكم بعيد مخفية؛ بل يسمح لـ Codex وClaude Code وHermes وOpenClaw/OpenCode ووكلاء MCP العامة بتنفيذ عمل تطوير حقيقي عبر مهام موقعة، وسياسة محلية على الجهاز، وموافقات، وحزم أدلة، وسجل تدقيق.

## أهم النقاط

- Agent Skillkit محمول يمكن تثبيته في Agent Frameworks الشائعة.
- يتم توقيع jobs والتحقق منها ثم تنفيذها؛ لا يحصل الوكيل على shell خام بلا حدود.
- تتحقق skills أولا من OS وshell وservice manager وgateway وworkspace وadapter والصلاحيات؛ إذا كان شيء غير واضح فهي تسأل بدلا من التخمين.
- دعم Codex وClaude Code وACP/acpx وshell وPowerShell وcustom adapters.
- ترخيص مفتوح المصدر Apache-2.0.

## التثبيت السريع

```bash
go install ./cmd/rdev
rdev doctor
```

صدّر وتحقق من Skillkit bundle:

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit --gateway-url https://api.example.com/v1
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
