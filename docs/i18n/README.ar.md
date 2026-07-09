# Remote Dev Skillkit

Remote Dev Skillkit هو skillkit مفتوح المصدر ومصمم لوكلاء الذكاء الاصطناعي. يساعد Codex و Claude Code و Hermes و OpenClaw/OpenCode ووكلاء MCP على العمل على أجهزة Mac و Windows و Linux حقيقية بدون منح shell غير محدود.

## ماذا يفعل

| ما يحصل عليه الوكيل | ما يبقى بيد الإنسان |
|---|---|
| Skills وأدوات MCP ومكيفات الملفات/سطح المكتب/tasks | الرؤية، الموافقة، الإلغاء، والتدقيق |
| Tasks موقعة بقدرات واضحة | سياسة محلية على المضيف وحدود أمان |
| Artifacts و session evidence | التحكم بما يعمل ومتى يتوقف |

## التثبيت عبر الوكيل

انسخ النص التالي وأرسله إلى الوكيل:

```text
Please install Remote Dev Skillkit for your own agent runtime:
https://github.com/EitanWong/remote-dev-skillkit
```

العقد الكامل موجود في [Agent Bootstrap Prompt](https://github.com/EitanWong/remote-dev-skillkit/blob/main/docs/operations/AGENT_BOOTSTRAP_PROMPT.md).

<details>
<summary>أوامر يدوية</summary>

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

## الاستخدام

1. صل جهازا:

```text
Use Remote Dev Skillkit to connect this computer for a visible support session.
```

```bash
rdev support-session connect --start
```

2. اعرض الأدوات وشغل العرض المحلي:

```bash
rdev mcp tools
rdev demo local
```

3. راجع الأدلة الأساسية:

```bash
go test ./...
rdev acceptance fresh-agent-support-session --out .rdev/acceptance/fresh-agent-support-session
```

## الأمان

Remote Dev Skillkit مخصص لدعم تطوير بعيد واضح وبموافقة صريحة. يجب أن تكون الجلسات المؤقتة لطرف ثالث مرئية، محدودة الوقت، قابلة للإلغاء، قابلة للتدقيق، وبصلاحيات مستخدم افتراضيا. لا يقبل المشروع الاستمرارية المخفية، أو تجاوز UAC/sudo، أو تعطيل ضوابط الأمان المحلية، أو shell بدون سياسة. الرخصة Apache-2.0.

## الوثائق

- ملف README الإنجليزي المعتمد: [../../README.md](../../README.md)
- فهرس الوثائق: [../README.md](../README.md)
