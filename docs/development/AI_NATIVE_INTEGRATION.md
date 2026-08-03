# AI-Native Agent Integration

Event push contract (`rdev.notification.v1`) lets agents react to session
events without polling. This document records the working Hermes integration
and the OpenClaw plan.

## Notification contract (implemented)

Registered per session via MCP `rdev.sessions.notify` or
`POST /v1/sessions/{id}/notify`:

```json
{"notify_url": "https://agent.example.com/rdev-events?secret=SUBSCRIPTION_SECRET"}
```

The `secret` query parameter is stripped from the stored URL, persisted on the
session, and sent as `X-Gitlab-Token` on every delivery so webhook platforms
can verify it. It never appears in snapshots, MCP responses, or audit records.

Delivery payload:

```json
{
  "schema_version": "rdev.notification.v1",
  "session_id": "ses_...",
  "seq": 12,
  "type": "hello | status | task | task.progress | task.result | artifact | interrupt | close",
  "from_endpoint_id": "end_...",
  "task_id": "tsk_...",
  "created_at": "...",
  "payload": { ... }
}
```

Delivery is fire-and-forget (5s timeout); failures are audited as
`session.notify.delivery_failed`. URL policy: HTTPS anywhere; HTTP on
loopback only (localhost / 127.0.0.1 / ::1).

## Hermes integration (implemented, verified live)

Hermes ships a webhook platform that consumes exactly this contract.

1. Enable the platform (config.yaml `platforms.webhook`, loopback 8644) and
   run `hermes gateway run`.
2. Subscribe:
   ```bash
   hermes webhook subscribe rdev-events \
     --prompt '🖥 rdev 事件 [{type}] session={session_id} seq={seq} endpoint={from_endpoint_id} task={task_id} at={created_at}' \
     --events 'hello,status,task,task.progress,task.result,artifact,interrupt,close' \
     --deliver weixin --deliver-chat-id '...' \
     --secret 'SUBSCRIPTION_SECRET' --deliver-only
   ```
   `--deliver-only` renders the template verbatim and pushes to the target
   chat (WeChat DM in this deployment) — zero LLM cost.
3. Register the webhook URL on any session:
   `http://127.0.0.1:8644/webhooks/rdev-events?secret=SUBSCRIPTION_SECRET`

Verified end-to-end: host join → rdev gateway POST → Hermes HMAC validation
(X-Gitlab-Token) → `direct-deliver event=hello route=rdev-events target=weixin`
→ WeChat iLink send (rate-limited by the platform at test time, delivery path
confirmed in gateway logs).

Notes:
- Secret must match between `hermes webhook subscribe --secret` and the
  `?secret=` parameter.
- Loopback HTTP suffices when rdev gateway and Hermes share a host. For a
  remote rdev gateway (production hz-web), expose the Hermes webhook through a
  tunnel (cloudflared) and use the HTTPS URL.
- WeChat iLink enforces a 30s+ cooldown per chat; bursts may be rejected —
  expect occasional delivery delay, not loss (add rdev retry when delivery
  guarantees are required).

## OpenClaw integration (planned, not implemented)

OpenClaw (clawdb) is a personal agent runtime. Plan:

1. **Receive**: run a small receiver — either OpenClaw's built-in HTTP
   endpoint if available, or a minimal adapter (10-20 LOC) that exposes
   `POST /rdev-events`, validates the same `X-Gitlab-Token` secret, and calls
   the OpenClaw CLI/API to inject a message into the user's active chat.
   Reuse the exact `rdev.notification.v1` payload — no rdev-side changes.
2. **Send user notifications**: `--deliver-only` template + OpenClaw's chat
   adapter, mirroring the Hermes setup; no agent loop needed for simple pings.
3. **React**: for actionable events (task.result, status=offline), register a
   second Hermes-style subscription without `--deliver-only` so OpenClaw can
   run a follow-up task (e.g. retry, cleanup) via its agent loop.
4. **Security**: same secret model; keep the receiver loopback-only or behind
   the operator-managed tunnel; never log the secret.

Status: deferred until a concrete OpenClaw deployment exists on a host with
rdev access.
