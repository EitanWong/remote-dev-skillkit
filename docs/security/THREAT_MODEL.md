# Threat Model

## Assets

- Target host access.
- Hermes operator identity.
- Host identity keys.
- Task transcripts and artifacts.
- Audit logs.
- Source code and local secrets on target machines.

## Threats And Controls

| Threat | Control |
|---|---|
| Public scanning of target hosts | No inbound listeners in temporary mode. Target connects outbound to gateway. |
| Leaked join ticket | Short TTL, one-time use, pending authorization, scoped capabilities, revocation. |
| Prompt injection asks agent to do dangerous work | MCP policy gates, host-side policy gates, explicit session capability or session interrupt for dangerous actions. |
| Host binary replacement | TLS, manifest checksums, code signing, signed updates. |
| Silent persistence on third-party hosts | Temporary mode is foreground-only and TTL-bound. Managed mode requires explicit setup. |
| Credential exfiltration | Secret redaction, deny credential-dump capabilities, no raw prompt secrets. |
| Gateway compromise | Per-host revocation, signing key separation, audit, emergency revoke-all. |
| Tampered task payload | Canonical session task records, endpoint binding, expiry, idempotency, and host-side verification. |
| Workspace escape | Canonical path checks, scoped write roots, symlink escape rejection, adapter-local policy enforcement. |
| Agent self-authorization | Session interrupts are explicit audited events; dangerous actions require scoped session capability or operator-reviewed continuation. |
| Audit tampering | Append-only audit events, durable store, planned hash chain and export verification. |

## Required Audit Fields

- task id;
- ticket id;
- operator;
- host id;
- mode;
- capabilities;
- commands;
- working directory;
- files read/written;
- processes touched;
- elevation events;
- artifacts;
- authorization decisions;
- start/end timestamps.

## Final Architecture Reference

The complete final trust model, protocol design, threat matrix, and acceptance
gates live in `docs/architecture/PERFECT_ENDING_SOLUTION.md`.
