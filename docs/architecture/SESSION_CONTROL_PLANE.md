# Session Control Plane

## Components

```text
Agent MCP client -> Control Plane -> session host -> policy-bound adapter -> events and artifacts
```

- **MCP server** exposes the eight current `rdev.sessions.*` tools.
- **Control Plane** owns session state, endpoint registration, task state, audit events, and artifact metadata.
- **Host runtime** joins with a session join code, verifies the signed trust bundle, applies local policy, and executes allowed adapter work.

## Lifecycle

1. Create a session through MCP.
2. Start a host with the returned join code and gateway URL.
3. Inspect status and events until an endpoint is ready.
4. Submit a scoped task.
5. Review events and artifact metadata.
6. Interrupt, close, or immediately revoke when the operator decides.

## Managed control

A long-lived gateway is an explicit operator deployment, not hidden host
persistence. It starts with a paired persistent state file and local signing
key plus operator auth. The snapshot restores session, endpoint lease, task,
event, and audit state after gateway restart; short-lived browser-handoff
proofs are intentionally excluded.

The target maintains an outbound authenticated long-poll. A task submitted by
an authorized operator is routed as a target-scoped task event and wakes that
poll, so the gateway can proactively dispatch approved work without opening an
inbound port on the target. `rdev.sessions.close(action="revoke")` invalidates
endpoint leases immediately.

## Invariants

- A host accepts only the active signed trust bundle.
- Task execution remains bounded by adapter, capability, workspace, and time limits.
- The host keeps local policy enforcement even when gateway-side validation succeeded.
- Events and artifacts are evidence, not a substitute for checking task outcome.
- Persistent state and signing key files must be regular `0600` files; audit records contain lifecycle metadata, not task payloads or lease secrets.
