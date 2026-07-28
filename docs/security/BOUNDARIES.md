# Security Boundaries

Remote Dev Skillkit is a policy-bound execution system, not a general remote shell.

## Host boundaries

- Hosts join outbound to a configured gateway and do not expose inbound public listeners.
- Host identity and signed trust material remain local.
- Local policy validates adapter capability, workspace scope, and execution limits.
- Temporary work remains visible and foreground.

## Agent boundaries

- MCP creates typed session operations rather than arbitrary command channels.
- Task results require events, terminal state, and artifact review before completion claims.
- Bearer material is read from a protected local file only for the configured remote gateway.

## Prohibited design changes

Do not add hidden persistence, security-control bypasses, credential harvesting, unaudited unrestricted execution, or a public target-host listener.
