# Remote Engineering Toolchain Plane

## Purpose

`rdev` treats remote engineering setup as a typed, audited host task rather
than an agent-issued shell script. A gateway-side coordinator can prepare a
remote host for Codex or Claude Code, then submit bounded engineering tasks to
the configured agent.

The first implementation is intentionally user-scoped and reversible:

- installs the selected agent package into a per-user npm prefix;
- never invokes sudo, UAC, system package managers, or global npm config;
- selects only registries named by a signed task policy;
- inherits an already configured host proxy without placing its URL in a task,
  profile, or artifact;
- stores API credentials only as host environment-variable *names*.

## Execution flow

```mermaid
sequenceDiagram
  participant G as Gateway coordinator
  participant M as rdev.sessions.task
  participant H as Target hostrunner
  participant P as Toolchain policy
  participant A as Codex or Claude Code

  G->>M: typed toolchain_request + package.install authorization
  M->>H: signed adapter=toolchain task
  H->>P: validate exact version, HTTPS sources, region, endpoint refs
  H->>H: npm user-prefix install; try ordered trusted sources
  H->>H: local --version verification
  H-->>G: secret-free runtime profile ID + install evidence
  G->>M: bounded engineering task with toolchain_profile_id
  M->>H: signed adapter=codex or claude-code task
  H->>A: isolated command + ephemeral runtime environment
  A-->>H: bounded artifact, diff and verification evidence
  H-->>G: redacted result artifact
```

## Contracts

### `rdev.toolchain-request.v1`

A request contains:

| Field | Rule |
|---|---|
| `tool` | `codex` or `claude-code` |
| `version` | Exact version only; floating tags and ranges are rejected |
| `policy.registries` | Ordered `https` sources, each with a safe ID and region scope |
| `policy.proxy_mode` | `inherit` or `disabled`; no proxy value is accepted in the task |
| `policy.endpoint` | Optional HTTPS base URL, model, auth mode, and credential environment-variable name |
| `execute` | `false` produces a zero-side-effect plan; `true` enables install |

The source order is the fallback order. For a Mainland target, define a
trusted mirror first and official registry second with `cn-mainland` in both
source region lists. When the mirror fails, `rdev` tries the next policy source
and records only the source ID and exit status.

For fresh hosts with no `npm`, `node_bootstrap` supplies the same ordered
source policy for a portable Node runtime. Each archive source must declare a
format, exact SHA-256, compressed-byte cap, and extracted-byte cap. rdev
downloads into a private staging directory, verifies the digest before any
extraction, rejects archive traversal/unsafe entry types, locates both `node`
and `npm`, then atomically installs the verified runtime below the user root.

Example policy structure (all values are placeholders):

```json
{
  "schema_version": "rdev.toolchain-policy.v1",
  "region": "cn-mainland",
  "proxy_mode": "inherit",
  "registries": [
    {
      "id": "cn-trusted-mirror",
      "url": "https://MIRROR.example/npm",
      "regions": ["cn-mainland"]
    },
    {
      "id": "official-fallback",
      "url": "https://REGISTRY.example/npm",
      "regions": ["global", "cn-mainland"]
    }
  ],
  "node_bootstrap": {
    "version": "NODE_VERSION",
    "max_archive_bytes": 300000000,
    "max_extracted_bytes": 1000000000,
    "sources": [
      {
        "id": "cn-node-mirror",
        "url": "https://MIRROR.example/node-ARCHIVE.zip",
        "sha256": "EXACT_64_CHARACTER_SHA256_HEX",
        "format": "zip",
        "regions": ["cn-mainland"]
      }
    ]
  },
  "endpoint": {
    "base_url": "https://AGENT_GATEWAY.example/v1",
    "model": "MODEL_ID",
    "credential_env": "RDEV_AGENT_API_KEY",
    "auth_mode": "api-key"
  }
}
```

No credential value belongs in this document, a policy file, a task payload,
or a result artifact.

## Runtime profiles

A successful install writes a secret-free profile beneath the target user's
managed toolchain root. Its opaque `id` is returned in the toolchain result.
A later Codex or Claude Code task supplies only:

```json
{ "toolchain_profile_id": "codex-VERSION" }
```

The hostrunner resolves this ID below its own configured toolchain root; it
does not accept arbitrary profile paths. A profile must match the requested
adapter. When present, it pins the executable path, so a task may not override
it with `codex_command` or `claude_code_command`.

- **Codex:** rdev writes an isolated `CODEX_HOME` profile with a custom model
  provider reference. Codex reads the configured credential environment name
  itself; the value is not copied into rdev state.
- **Claude Code:** at process launch only, rdev maps the host-local credential
  into `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN`, plus
  `ANTHROPIC_BASE_URL` and `ANTHROPIC_MODEL`. Git evidence and declared
  verification commands run without those overrides.
- **Managed Node:** when a portable Node bootstrap was required, its private
  `bin` directory is prepended only to the npm install and agent child PATH;
  it does not modify the target's global PATH or package-manager state.

## Gateway coordination procedure

1. Enroll the target with the normal `codex.run` and/or `claude-code.run`
   capability plus `package.install.requiresAuthorization` only when the host
   owner permits user-scoped installation.
2. Send a `rdev.sessions.task` request with `adapter: "toolchain"`, the typed
   request, and exactly `package.install.requiresAuthorization`.
3. Require `status: "installed"` and local command verification in the result
   before scheduling an engineering task. Preserve the returned profile ID.
4. Submit a bounded Codex or Claude Code engineering task with the profile ID,
   declared write scope, plan, acceptance criteria, and allowlisted verification
   commands.
5. Use the existing engineering-loop artifact/diff/test evidence for retries
   and final acceptance; do not treat an installer exit code as a successful
   development run.

## Operator CLI

On a target host, the same typed contract is available locally:

```text
rdev toolchain plan   --tool TOOL --version VERSION --policy-file POLICY.json
rdev toolchain ensure --tool TOOL --version VERSION --policy-file POLICY.json --execute
```

`plan` never writes a profile or starts npm. `ensure` refuses to execute unless
`--execute` is explicit.

## Current prerequisite boundary

v1 now bootstraps a verified portable Node runtime when host npm is absent.
Other language runtimes (Go, Python, Java, Rust, and platform SDKs) are not
implicitly installed through arbitrary package-manager commands. They should
join this same profile model only with an explicit, checksum-pinned artifact
contract and a target-specific acceptance test.

The same profile model is intended to expand to Go, Python/uv, Rust, Java, and
project-specific build tools: discovery → exact policy plan → user-scoped
bootstrap → command verification → runtime profile → bounded engineering task.
