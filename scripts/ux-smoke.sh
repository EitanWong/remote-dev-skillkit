#!/usr/bin/env bash
# UX smoke: drive the complete first-run user journey on loopback and assert
# that every step produces the outputs a human or agent can actually use.
#
# Steps exercised (in the order a new user would hit them):
#   1. gateway serve --dev on a free port -> machine-readable ready JSON
#   2. create session -> parseable response containing join_code
#   3. host serve --join-code --once -> human card on stderr, pure JSON on stdout
#   4. events after join -> hello event present
#   5. submit a bounded read-only task -> succeeded task.result event
#   6. close session -> close event
#
# Exits non-zero on any deviation so CI can gate on it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
PORT=$((18000 + RANDOM % 1000))
GW="http://127.0.0.1:${PORT}"
PASS=0
FAIL=0

cleanup() {
  if [[ -n "${GWPID:-}" ]]; then kill "$GWPID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT

step() { printf '[ux-smoke] %s\n' "$*"; }
ok()   { printf '[ux-smoke]   ok: %s\n' "$*"; PASS=$((PASS + 1)); }
bad()  { printf '[ux-smoke]   FAIL: %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }

# 0. build binaries the way a user would (go build, no ldflags needed)
step "building rdev and rdev-host"
go build -o "$WORK/rdev" "$ROOT/cmd/rdev"
go build -o "$WORK/rdev-host" "$ROOT/cmd/rdev-host"
ok "binaries built"

# 1. gateway
step "starting gateway on ${GW}"
"$WORK/rdev" gateway serve --dev --addr "127.0.0.1:${PORT}" >"$WORK/gw.json" 2>"$WORK/gw.err" &
GWPID=$!
for _ in $(seq 1 40); do
  curl -sf "${GW}/health" >/dev/null 2>&1 && break
  sleep 0.25
done
if python3 -c "import json,sys; d=json.load(open('$WORK/gw.json')); assert d.get('schema_version')=='rdev.gateway-ready.v2', d; assert d.get('url')=='$GW', d" 2>"$WORK/gwcheck.err"; then
  ok "gateway ready JSON parseable (rdev.gateway-ready.v2)"
else
  bad "gateway ready JSON: $(head -c 200 "$WORK/gw.err")"
fi

# 2. create session
SESSION_JSON="$(curl -sf -X POST "${GW}/v1/sessions" -H 'Content-Type: application/json' -d '{"reason":"ux smoke"}' 2>"$WORK/sess.err" || true)"
if SID="$(printf '%s' "$SESSION_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['session']['id'])" 2>/dev/null)"; then
  ok "session created ($SID)"
else
  bad "session create: $SESSION_JSON"
fi
JOIN="$(printf '%s' "$SESSION_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['session']['join_code'])" 2>/dev/null || true)"

# 3. host join (human card on stderr, JSON on stdout) -- --once proves the
#    output contract; the resident phase below proves task execution.
step "joining host (--once, output contract)"
timeout 10 "$WORK/rdev-host" serve --join-code "$JOIN" --gateway "$GW" --name ux-smoke-host \
  >"$WORK/host.json" 2>"$WORK/host.err" || true
if grep -q 'Control Plane session connector is ready' "$WORK/host.err"; then
  ok "host printed human-readable session card on stderr"
else
  bad "host stderr card missing: $(head -c 200 "$WORK/host.err")"
fi
if python3 -c "import json; d=json.load(open('$WORK/host.json')); assert d.get('status')=='session-joined', d" 2>/dev/null; then
  ok "host stdout is pure machine-readable JSON"
else
  bad "host stdout not JSON: $(head -c 200 "$WORK/host.json")"
fi

# 4. resident host (long-poll) for the task round trip -- a second session,
#    because the --once host already owns the first one (single-target).
step "creating second session for resident host"
SID2_JSON="$(curl -sf -X POST "${GW}/v1/sessions" -H 'Content-Type: application/json' -d '{"reason":"ux smoke resident"}' 2>/dev/null || true)"
SID2="$(printf '%s' "$SID2_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['session']['id'])" 2>/dev/null || true)"
JOIN2="$(printf '%s' "$SID2_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['session']['join_code'])" 2>/dev/null || true)"
step "starting resident host for task execution"
"$WORK/rdev-host" serve --join-code "$JOIN2" --gateway "$GW" --name ux-smoke-host --once=false \
  >"$WORK/host2.json" 2>"$WORK/host2.err" &
HOSTPID=$!
cleanup() {
  if [[ -n "${HOSTPID:-}" ]]; then kill "$HOSTPID" 2>/dev/null || true; fi
  if [[ -n "${GWPID:-}" ]]; then kill "$GWPID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT
SID="$SID2"

# 4. events contain hello
step "reading events"
EVENTS_OK=""
for _ in $(seq 1 20); do
  EVENTS="$(curl -sf "${GW}/v1/sessions/${SID}/events?after_seq=0" 2>/dev/null || true)"
  if printf '%s' "$EVENTS" | python3 -c "import json,sys; evs=json.load(sys.stdin)['events']; assert any(e['type']=='hello' for e in evs), evs" 2>/dev/null; then
    EVENTS_OK=1
    break
  fi
  sleep 1
done
if [[ -n "$EVENTS_OK" ]]; then
  ok "hello event observed after join"
else
  bad "no hello event: $(printf '%s' "${EVENTS:-}" | head -c 200)"
fi

# 5. task round trip
step "submitting read-only receipt task"
TASK_JSON="$(curl -sf -X POST "${GW}/v1/sessions/${SID}/tasks" -H 'Content-Type: application/json' \
  -d "{\"adapter\":\"shell\",\"idempotency_key\":\"ux-smoke-${PORT}\",\"intent\":\"bounded receipt\",\"capabilities\":[\"shell.user\"],\"limits\":{\"max_duration_seconds\":15,\"max_output_bytes\":1024},\"payload\":{\"argv\":[\"printf\",\"ux-smoke-ok\"],\"allow_commands\":[\"printf\"],\"workspace_root\":\"/tmp\"}}" 2>/dev/null || true)"
TASK_ID="$(printf '%s' "$TASK_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['task']['id'])" 2>/dev/null || true)"
if [[ -n "$TASK_ID" ]]; then
  ok "task offered ($TASK_ID)"
else
  bad "task submit failed: $(printf '%s' "$TASK_JSON" | head -c 200)"
fi
sleep 2
step "waiting for task.result"
RESULT_OK=""
for _ in $(seq 1 20); do
  RESULT="$(curl -sf "${GW}/v1/sessions/${SID}/events?after_seq=0" 2>/dev/null || true)"
  if printf '%s' "$RESULT" | python3 -c "
import json,sys
evs = json.load(sys.stdin)['events']
res = [e for e in evs if e.get('type')=='task.result' and e.get('task_id')=='$TASK_ID']
assert res, 'no task.result yet'
art = res[-1]['payload'].get('artifact_content','')
assert 'ux-smoke-ok' in art, art[:200]
" 2>/dev/null; then
    RESULT_OK=1
    break
  fi
  sleep 1
done
if [[ -n "$RESULT_OK" ]]; then
  ok "task.result succeeded with expected output"
else
  bad "task round trip failed: $(printf '%s' "${RESULT:-}" | head -c 300)"
fi

# 7. error paths a novice will hit -- assert human-readable guidance
step "exercising novice error paths"
OUT=""
OUT="$("$WORK/rdev" bogus 2>&1 || true)"
if [[ "$OUT" == *"available commands"* ]]; then
  ok "unknown command names available commands"
else
  bad "unknown command error lacks guidance: $(printf '%s' "$OUT" | head -c 120)"
fi
OUT="$("$WORK/rdev-host" serve 2>&1 || true)"
if [[ "$OUT" == *"--join-code is required"* ]]; then
  ok "missing --join-code explains what to do"
else
  bad "missing --join-code error not actionable: $(printf '%s' "$OUT" | head -c 120)"
fi
OUT="$(timeout 8 "$WORK/rdev-host" serve --join-code ABCD-1234 --gateway "$GW" 2>&1 || true)"
if [[ "$OUT" =~ invalid|no\ longer\ active ]]; then
  ok "bad join code error is human-readable"
else
  bad "bad join code error unclear: $(printf '%s' "$OUT" | head -c 120)"
fi
OUT="$(timeout 8 "$WORK/rdev-host" serve --join-code ABCD-1234 --gateway 'http://127.0.0.1:9' 2>&1 || true)"
if [[ "$OUT" == *"cannot reach the gateway"* ]]; then
  ok "unreachable gateway error is human-readable"
else
  bad "unreachable gateway error unclear: $(printf '%s' "$OUT" | head -c 120)"
fi
OUT="$("$WORK/rdev" gateway serve --dev --addr "127.0.0.1:${PORT}" 2>&1 || true)"
if [[ "$OUT" == *"already in use"* || "$OUT" == *"try a different port"* ]]; then
  ok "port-in-use error suggests an alternative"
else
  bad "port-in-use error lacks guidance: $(printf '%s' "$OUT" | head -c 120)"
fi

# 8. close
step "closing session"
if curl -sf -X POST "${GW}/v1/sessions/${SID}/close" -H 'Content-Type: application/json' -d '{"reason":"ux smoke complete"}' >/dev/null 2>&1; then
  ok "session closed"
else
  bad "session close failed"
fi

printf '\n[ux-smoke] PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[[ $FAIL -eq 0 ]]
