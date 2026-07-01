# Agent Bootstrap Prompt

Copy this prompt into Codex, Claude Code, Hermes, OpenClaw/OpenCode, or another
MCP-capable agent when you want the agent to install Remote Dev Skillkit for its
own runtime.

If you reached this file from the README short prompt, treat this file as the
source of truth. Prefer reading it from the local cloned checkout so relative
paths, scripts, and docs can be inspected directly.

The prompt is intentionally agent-facing. It asks the agent to probe first,
install only verified files, avoid hardcoded local paths, and ask one short
question when a required value cannot be discovered safely.

## Copy-Paste Prompt

```text
Install and connect Remote Dev Skillkit for this agent runtime.

Repository:
https://github.com/EitanWong/remote-dev-skillkit

Goal:
Make this agent able to use Remote Dev Skillkit skills and MCP tools for safe,
auditable remote development sessions.

Rules:
- Clone or update the repository first in a safe user/workspace location unless
  a current checkout already exists. Read this file from the checkout before
  executing the remaining steps.
- Probe before acting. Do not guess paths, framework names, gateway URLs, or MCP
  config locations.
- If a required value is unclear, ask exactly one short question, wait for the
  answer, then continue.
- Do not hardcode private server addresses, personal paths, secrets, dates, or
  machine-specific values.
- Do not weaken OS security policy, create hidden persistence, or install
  system-wide components unless I explicitly approve that specific action.
- Prefer user-scoped or workspace-scoped installation.
- Verify before copying files. Dry-run before execute when the command supports it.

Steps:
1. Detect the current OS, shell, working directory, installed Git, installed Go,
   and this agent framework. Identify whether this runtime is Codex, Claude Code,
   Hermes, OpenClaw, OpenCode, or a generic MCP-capable agent.
2. Clone or update `https://github.com/EitanWong/remote-dev-skillkit` into a
   safe user/workspace location. If a checkout already exists, inspect it and
   update only after checking for local changes.
3. Read `docs/operations/AGENT_BOOTSTRAP_PROMPT.md`,
   `docs/operations/SKILLKIT_INSTALL.md`, and the README from the checkout.
4. Check whether `rdev` is already installed with `rdev doctor`. If it is not
   available, build/install the CLI with the repository's documented Go workflow.
5. Run:
   - `rdev doctor`
   - `rdev mcp tools`
6. Create and verify a portable Skillkit bundle from the checked-out repository.
   If I have not provided a hosted gateway URL yet, use
   `https://api.example.com/v1` only as a placeholder in local bundle metadata
   and tell me that real remote sessions need my actual gateway URL later.
7. Generate and verify an install plan for all mainstream frameworks:
   `codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent`.
8. Determine the correct skill/instruction target directory for this current
   agent runtime. Use environment variables, framework docs, existing config,
   and installed runtime layout. If the target is still unclear, ask me one
   short question instead of inventing it.
9. Run `rdev skillkit install` as a dry-run for the detected framework and
   target directory. Review the JSON report.
10. If the dry-run is safe and there are no overwrite conflicts, run the same
   install with `--execute`. If there are conflicts, ask before using any force
   option.
11. Configure this agent's MCP client to run `rdev mcp serve`, or produce the
   exact MCP config snippet and file path if the framework requires manual
   review. Do not silently overwrite existing MCP config.
12. Verify the installed skill folders exist, verify `.remote-dev-skillkit/mcp/tools.json`
    exists, and run any available framework command that lists skills or MCP
    tools.
13. Report:
    - detected framework
    - installed skill target
    - whether MCP was configured or the exact snippet I need to add
    - verification commands run
    - any remaining values I must provide before using real remote sessions

After installation, use `host-triage` before remote work, `remote-vibe-coding`
or `safe-remote-support` to run sessions, and `remote-job-review` before
claiming completion.
```

## Expected Agent Behavior

The agent should prefer these commands after it has a checkout:

```bash
go install ./cmd/rdev
rdev doctor
rdev mcp tools

rdev skillkit export \
  --source-root . \
  --out dist/remote-dev-skillkit \
  --gateway-url https://api.example.com/v1

rdev skillkit verify \
  --bundle dist/remote-dev-skillkit

rdev skillkit plan-install \
  --bundle dist/remote-dev-skillkit \
  --out dist/skillkit-install \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent

rdev skillkit verify-install-plan \
  --plan dist/skillkit-install/install-plan.json
```

Then it should choose the matching framework and target directory, dry-run, and
execute only after the target is clear:

```bash
rdev skillkit install \
  --bundle dist/remote-dev-skillkit \
  --framework codex \
  --target "$HOME/.codex/skills"

rdev skillkit install \
  --bundle dist/remote-dev-skillkit \
  --framework codex \
  --target "$HOME/.codex/skills" \
  --execute
```

The framework and target path above are examples. Runtime agents must replace
them with detected or confirmed values for Codex, Claude Code, Hermes,
OpenClaw, OpenCode, or a generic MCP agent.
