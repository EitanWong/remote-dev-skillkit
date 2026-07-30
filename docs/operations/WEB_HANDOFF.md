# Browser Host Handoff

A browser handoff turns a normal session into a short-lived Windows host
connection flow:

```text
operator MCP -> session handoff URL -> browser fragment claim
             -> localized PowerShell command copy -> verified rdev-host.exe
             -> outbound long-poll session join
```

## Gateway configuration

The gateway remains loopback-only. The HTTPS reverse proxy owns its public
listener and routes the public base URL to the local gateway process.

```bash
rdev-gateway \
  --addr 127.0.0.1:8787 \
  --operator-auth-file /protected/operators.json \
  --state-file /protected/rdev-gateway/state.json \
  --signing-key-file /protected/rdev-gateway/signing-key.json \
  --public-base-url https://gateway.example \
  --windows-amd64-host-binary /opt/rdev-gateway/bin/rdev-host.exe
```

`--public-base-url` and `--windows-amd64-host-binary` are a required pair. The
binary is loaded and hashed at gateway startup. Readiness reports
`web_handoff_enabled: true` only after both values are valid.
Managed deployments require the protected state/key pair so signed trust
renewals persist across restart; see [Session host operations](SESSION_HOST.md)
for file-mode requirements.

## Operator flow

1. Create a session with `selected_gateway_url` set to the configured public
   base URL.
2. Call `rdev.sessions.handoff` with the session id and `windows-amd64`.
3. Send the returned URL unchanged to the intended target. There is no separate
   `web` or `powershell` delivery choice, and an Agent must not claim the URL on
   the user's behalf.
4. The page localizes itself and identifies the opened system. On Windows it
   displays four numbered connection steps. On macOS, Linux, or an unknown
   system it names the detected system, leaves the link unclaimed, and tells the
   user to open the same URL on the target Windows computer.
5. On the target Windows machine, open **Windows PowerShell**, click **Copy
   connection command**, paste it into the already-open PowerShell window, then
   press Enter. Keep that window open while the Agent verifies endpoint
   readiness. Clipboard denial reveals and selects the same command for manual
   copy.
6. The Windows bootstrap fetches the host binary through the short-lived ticket,
   verifies its SHA-256, and starts managed long-poll visibly. The operator does
   not download or run a `.cmd` or `.ps1` file.
7. Wait for the target endpoint through `rdev.sessions.status` before sending a
   task.

The current default link lifetime is 30 minutes. The operator may request a
lifetime between one minute and 24 hours. The effective lifetime is capped at
the session expiry when the session has one. Each link has exactly one claim;
a fresh link is required after expiry or claim.

## Capability handling

The URL has the form:

```text
https://gateway.example/connect/HANDOFF_ID#FRAGMENT_PROOF
```

Only `HANDOFF_ID` reaches the gateway in the initial HTTP request. The browser
reads `FRAGMENT_PROOF` locally, removes it from browser history, and submits it
in a same-origin POST body. The gateway stores only a SHA-256 hash of the proof.

After a successful claim, the browser receives one copyable PowerShell
connection command. It uses a short-lived artifact ticket to fetch the Windows
host binary using `X-Rdev-Handoff-Ticket` and verifies the configured SHA-256
before execution.
Operator credentials and gateway private keys do not appear in the initial
page, HTTP URL, referrer, or query string.

The bootstrap starts the host visibly with managed mode and outbound long-poll.
It persists identity, trust, and workspace-lock state under the current
Windows user's `%LOCALAPPDATA%\RemoteDevSkillkit\managed-host`. It creates no
Windows service, scheduled task, firewall rule, inbound listener, elevation
request, or execution-policy bypass.

## Native adaptive page behavior

The page has no external provisioning script or client-side configuration step:
it is served by the same session gateway and consumes the same one-time handoff
contract used by MCP. It uses `Accept-Language` for an initial server-rendered
locale, then browser language preferences for the final rendered locale.
English, Simplified Chinese, and Traditional Chinese are currently included.

The page uses `navigator.userAgentData.platform` when available, with standard
browser platform/user-agent fallbacks. It identifies Windows, macOS, Linux, or
another system before it exposes a claim action. A non-Windows page explicitly
names the detected system, explains that this handoff supplies a Windows
connector, and leaves the link unclaimed so the same URL can be opened on the
target Windows machine. On Windows, the page gives the shortest verified path:
**open PowerShell → copy command → paste → Enter → keep the window open while
the Agent verifies readiness**. Clipboard denial reveals and selects the same
command for manual copy. The host runs in that visible PowerShell window; no
JavaScript, service, elevation, or browser-control bypass is used.

## Operational boundaries

- A `single-target` session with an existing endpoint rejects a new web handoff.
- A closed, revoked, failed, or expired session rejects a web handoff.
- Claim revalidates that the session is still joinable before issuing a
  bootstrap; an ineligible session leaves the handoff unclaimed rather than
  issuing a script that cannot join.
- Gateway restart ends live long-polls and invalidates in-memory handoffs. With
  the configured state/key pair, session, audit, and trust state restore; a
  managed host reconnects through its persisted local identity.
- `GET /v1/trust-bundle` renews its signed bundle before expiry (and repairs an
  expired persisted bundle) using the same signing key and a linked sequence;
  a configured state store persists the renewal. A healthy `/healthz` alone
  does not prove that host trust verification can succeed.
- `/healthz` proves that the gateway process is reachable; a browser handoff
  requires the new gateway binary and the configured host binary asset.
- The first task after join should be read-only host triage. Later work remains
  bounded by the session capability policy, host policy, workspace scope, and
  normal Windows controls.
