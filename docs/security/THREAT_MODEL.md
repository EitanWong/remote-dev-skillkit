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
