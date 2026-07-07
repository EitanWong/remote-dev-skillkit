# Remote Dev Skillkit

Remote Dev Skillkit هو طبقة أمان لوكلاء الذكاء الاصطناعي الذين يحتاجون إلى العمل على أجهزة Mac وWindows وLinux حقيقية. ليس أداة تحكم بعيد مخفية؛ بل يسمح لـ Codex وClaude Code وHermes وOpenClaw/OpenCode ووكلاء MCP العامة بتنفيذ عمل تطوير حقيقي عبر مهام موقعة، وسياسة محلية على الجهاز، وموافقات، وحزم أدلة، وسجل تدقيق.

نموذج الاتصال يشبه موصل تحكم بعيد للوكلاء: يفتح الجهاز الهدف موصلا مرئيا، ويستخدم الوكيل Support Device Entry القياسي (`support_device_id` + كلمة مرور الجلسة)، ولا يقطع الاتصال إلا عندما يطلب المشغل ذلك صراحة.

## أهم النقاط

- Agent Skillkit محمول يمكن تثبيته في Agent Frameworks الشائعة.
- يتم توقيع jobs والتحقق منها ثم تنفيذها؛ لا يحصل الوكيل على shell خام بلا حدود.
- تتحقق skills أولا من OS وshell وservice manager وgateway وworkspace وadapter والصلاحيات؛ إذا كان شيء غير واضح فهي تسأل بدلا من التخمين.
- دعم Codex وClaude Code وACP/acpx وshell وPowerShell وcustom adapters.
- ترخيص مفتوح المصدر Apache-2.0.

## التثبيت السريع

إذا كنت داخل Codex أو Claude Code أو Hermes أو OpenClaw/OpenCode أو وكيل MCP آخر، أرسل هذا الرابط إلى الوكيل واطلب منه قراءة prompt كاملا قبل التنفيذ:

[Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md)

هذا prompt هو مصدر الحقيقة لنسخ المستودع أو تحديثه، واكتشاف Agent runtime الحالي، وتثبيت Skillkit، وإعداد MCP، وطرح سؤال قصير واحد فقط عندما لا يمكن اكتشاف قيمة مطلوبة بأمان.

```bash
go install ./cmd/rdev
rdev doctor
rdev bootstrap agent-plan --repo-root .
```

إذا لم يكن `rdev` داخل `PATH`، فاستخدم أمر MCP المطلق الذي يعرضه `rdev bootstrap agent-plan` أو حقل `mcp_command` في تقرير التثبيت.

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
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
rdev demo local
rdev mcp tools
```

المرجع التقني المعتمد هو [README](../../README.md) باللغة الإنجليزية؛ عند التعارض اتبع النسخة الإنجليزية.
