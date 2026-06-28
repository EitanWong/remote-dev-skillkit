# Threat Model

## Assets

- Target host access.
- Hermes/Lucky operator identity.
- Host identity keys.
- Job transcripts and artifacts.
- Audit logs.
- Source code and local secrets on target machines.

## Threats And Controls

| Threat | Control |
|---|---|
| Public scanning of target hosts | No inbound listeners in temporary mode. Target connects outbound to gateway. |
| Leaked join ticket | Short TTL, one-time use, pending approval, scoped capabilities, revocation. |
| Prompt injection asks agent to do dangerous work | MCP policy gates, host-side policy gates, explicit approval for dangerous actions. |
| Host binary replacement | TLS, manifest checksums, code signing, signed updates. |
| Silent persistence on third-party hosts | Temporary mode is foreground-only and TTL-bound. Managed mode requires explicit setup. |
| Credential exfiltration | Secret redaction, deny credential-dump capabilities, no raw prompt secrets. |
| Gateway compromise | Per-host revocation, signing key separation, audit, emergency revoke-all. |
| Tampered job envelope | Canonical signed envelopes, host binding, expiry, nonce replay protection, host-side verification. |
| Workspace escape | Canonical path checks, scoped write roots, symlink escape rejection, adapter-local policy enforcement. |
| Agent self-approval | Approval tokens are separate from agent tool calls; dangerous actions require operator or local-user approval. |
| Audit tampering | Append-only audit events, durable store, planned hash chain and export verification. |

## Required Audit Fields

- job id;
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
- approval decisions;
- start/end timestamps.

## Perfect End-State References

The complete end-state trust model, protocol design, threat matrix, and acceptance tests live in `docs/architecture/PERFECT_END_STATE.md`.
