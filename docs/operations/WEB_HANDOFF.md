# Browser Host Handoff

A browser handoff turns a normal session into a short-lived Windows host
connection flow:

```text
operator MCP -> session handoff URL -> browser fragment claim
             -> PowerShell bootstrap download -> verified rdev-host.exe
             -> outbound long-poll session join
```

## Gateway configuration

The gateway remains loopback-only. The HTTPS reverse proxy owns its public
listener and routes the public base URL to the local gateway process.

```bash
rdev-gateway \
  --addr 127.0.0.1:8787 \
  --operator-auth-file /protected/operators.json \
  --public-base-url https://gateway.example \
  --windows-amd64-host-binary /opt/rdev-gateway/bin/rdev-host.exe
```

`--public-base-url` and `--windows-amd64-host-binary` are a required pair. The
binary is loaded and hashed at gateway startup. Readiness reports
`web_handoff_enabled: true` only after both values are valid.

## Operator flow

1. Create a session with `selected_gateway_url` set to the configured public
   base URL.
2. Call `rdev.sessions.handoff` with the session id and `windows-amd64`.
3. Send the returned URL to the Windows operator.
4. The operator opens the link and explicitly downloads `Connect-Rdev.ps1`.
5. The operator runs the downloaded script in a visible PowerShell window.
6. Wait for the target endpoint through `rdev.sessions.status` before sending a
   task.

The current default link lifetime is 30 minutes. The operator may request a
lifetime between one minute and 24 hours. Each link has exactly one claim; a
fresh link is required after expiry or claim.

## Capability handling

The URL has the form:

```text
https://gateway.example/connect/HANDOFF_ID#FRAGMENT_PROOF
```

Only `HANDOFF_ID` reaches the gateway in the initial HTTP request. The browser
reads `FRAGMENT_PROOF` locally, removes it from browser history, and submits it
in a same-origin POST body. The gateway stores only a SHA-256 hash of the proof.

After a successful claim, the browser receives a PowerShell bootstrap script.
That script contains a short-lived artifact ticket and downloads the Windows
host binary using `X-Rdev-Handoff-Ticket`; it verifies the configured SHA-256
before execution. Operator credentials, gateway private keys, and session join
codes do not appear in the initial page, HTTP URL, referrer, or query string.

The script starts the host visibly with managed mode and outbound long-poll.
It persists identity, trust, and workspace-lock state under the current
Windows user's `%LOCALAPPDATA%\RemoteDevSkillkit\managed-host`. It creates no
Windows service, scheduled task, firewall rule, inbound listener, elevation
request, or execution-policy bypass.

## Operational boundaries

- A `single-target` session with an existing endpoint rejects a new web handoff.
- A closed, revoked, failed, or expired session rejects a web handoff.
- Gateway restart invalidates active pilot sessions and in-memory handoffs.
- `/healthz` proves that the gateway process is reachable; a browser handoff
  requires the new gateway binary and the configured host binary asset.
- The first task after join should be read-only host triage. Later work remains
  bounded by the session capability policy, host policy, workspace scope, and
  normal Windows controls.
