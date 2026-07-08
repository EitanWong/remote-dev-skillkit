# Multilingual Quick Starts

Remote Dev Skillkit is an open-source, agent-native remote development
skillkit. These short quick starts mirror the same install spine as the English
README so users can copy a prompt into Codex, Claude Code, Hermes,
OpenClaw/OpenCode, or any MCP-capable agent.

The English [README](../../README.md) remains the authoritative technical
source. The detailed AI-native installer contract is the
[Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md).

All translations keep these shared steps in sync:

1. copy the short AI-native install prompt into the agent;
2. have the agent read the full
   [Agent Bootstrap Prompt](../operations/AGENT_BOOTSTRAP_PROMPT.md);
3. build or locate `rdev`;
4. run `rdev doctor`;
5. export and verify the Skillkit bundle;
6. generate and verify a framework install plan;
7. dry-run `rdev skillkit install`, then rerun it with `--execute`;
8. configure MCP with the install report's `mcp_command`.

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

Maintainers can run `scripts/audit-i18n-quickstarts.sh` to verify that the
translations still include the shared framework list, install commands,
Agent Bootstrap Prompt link, local demo commands, safety posture, and
Apache-2.0 license reference.
