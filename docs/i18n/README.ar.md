# Remote Dev Skillkit

Remote Dev Skillkit هو نواة أمان للتطوير البعيد بواسطة الوكلاء. ليس أداة تحكم
بعيد مخفية؛ بل يسمح لـ Codex وClaude Code وHermes وOpenClaw/OpenCode ووكلاء MCP
العامة بتفويض العمل إلى أجهزة حقيقية عبر مهام موقعة، وسياسات محلية على الجهاز،
وموافقات، وأدلة، وسجل تدقيق.

## ما الذي يقدمه

- واجهة `rdev` وبinaries للـ host وgateway وMCP وverifier.
- حزم Agent Skillkit قابلة للتصدير والتحقق والتثبيت.
- Job envelopes موقعة، workspace locks، approval gates، evidence bundles، وaudit chains.
- محولات Codex وClaude Code وACP/acpx وshell وPowerShell.
- ترخيص مفتوح المصدر Apache-2.0.

## التحقق محليا

```bash
go test ./...
./scripts/check.sh
./scripts/ci/release-smoke.sh
```

## تثبيت Skillkit

```bash
rdev skillkit export --source-root . --out dist/remote-dev-skillkit
rdev skillkit verify --bundle dist/remote-dev-skillkit
rdev skillkit plan-install --bundle dist/remote-dev-skillkit --out dist/skillkit-install
rdev skillkit verify-install-plan --plan dist/skillkit-install/install-plan.json
```

المرجع التقني المعتمد هو [README](../../README.md) باللغة الإنجليزية.
