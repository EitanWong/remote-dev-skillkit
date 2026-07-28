# Session Control Plane

## Components

```text
Agent MCP client -> Control Plane -> session host -> policy-bound adapter -> events and artifacts
```

- **MCP server** exposes the seven current `rdev.sessions.*` tools.
- **Control Plane** owns session state, endpoint registration, task state, audit events, and artifact metadata.
- **Host runtime** joins with a session join code, verifies the signed trust bundle, applies local policy, and executes allowed adapter work.

## Lifecycle

1. Create a session through MCP.
2. Start a host with the returned join code and gateway URL.
3. Inspect status and events until an endpoint is ready.
4. Submit a scoped task.
5. Review events and artifact metadata.
6. Interrupt or close when the operator decides.

## Invariants

- A host accepts only the active signed trust bundle.
- Task execution remains bounded by adapter, capability, workspace, and time limits.
- The host keeps local policy enforcement even when gateway-side validation succeeded.
- Events and artifacts are evidence, not a substitute for checking task outcome.
