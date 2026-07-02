# Multilingual Quick Starts

Remote Dev Skillkit is designed for a global agent-developer community. These
short translations explain the project, safety model, and install path in major
world languages. The English README remains the authoritative technical source.

Every quick start keeps the same install spine as the English README:

1. copy the Agent Bootstrap Prompt into Codex, Claude Code, Hermes,
   OpenClaw/OpenCode, or a generic MCP agent when the agent should install
   itself;
2. build or install `rdev`;
3. run `rdev doctor`;
4. export and verify a Skillkit bundle;
5. generate and verify a framework install plan;
6. run `rdev skillkit install` as a dry-run first, then again with `--execute`.

| Language | File |
|---|---|
| English | [../../README.md](../../README.md) |
| 简体中文 | [README.zh-CN.md](README.zh-CN.md) |
| Español | [README.es.md](README.es.md) |
| Français | [README.fr.md](README.fr.md) |
| Deutsch | [README.de.md](README.de.md) |
| 日本語 | [README.ja.md](README.ja.md) |
| 한국어 | [README.ko.md](README.ko.md) |
| Português | [README.pt-BR.md](README.pt-BR.md) |
| हिन्दी | [README.hi.md](README.hi.md) |
| العربية | [README.ar.md](README.ar.md) |
| Русский | [README.ru.md](README.ru.md) |

If a translation conflicts with the English README, follow the English README
and open an issue or pull request to update the translation.

Maintainers can run `scripts/audit-i18n-quickstarts.sh` to verify that all
translations still include the common framework list, quick install commands,
Agent Bootstrap Prompt link, connection entry wording, local demo commands,
safety posture, and Apache-2.0 license reference.
