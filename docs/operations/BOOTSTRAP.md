# Bootstrap Design

## Temporary Windows Flow

1. Operator creates a ticket.
2. Remote user opens the join URL.
3. Join page displays operator, server, mode, capabilities, and expiration.
4. User runs PowerShell bootstrap.
5. Bootstrap downloads the signed `rdev-host` binary.
6. Bootstrap verifies checksum/signature.
7. Host generates a keypair.
8. Host registers using the one-time ticket.
9. Gateway marks the host pending.
10. Operator approves.
11. Host receives signed policy and jobs.

## Bootstrap Requirements

- No Node/Python/Git dependency.
- No inbound port.
- Visible temporary mode by default.
- Admin elevation only via normal OS prompts.
- Clear stop and uninstall instructions.

## Managed Device Flow

Managed service installation is a separate explicit step:

```bash
rdev host install-service --mode managed
```

This command is not used for third-party temporary sessions.
