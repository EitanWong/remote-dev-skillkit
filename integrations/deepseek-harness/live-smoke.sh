#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BUNDLE="${ROOT}/integrations/deepseek-harness"
TMP="$(mktemp -d)"
DSH_BIN="${DSH_BIN:-dsh}"
DSH_PID=""
GWPID=""

cleanup() {
  if [[ -n "${DSH_PID}" ]]; then
    kill "${DSH_PID}" 2>/dev/null || true
    wait "${DSH_PID}" 2>/dev/null || true
  fi
  if [[ -n "${GWPID}" ]]; then
    kill "${GWPID}" 2>/dev/null || true
    wait "${GWPID}" 2>/dev/null || true
  fi
  rm -rf "${TMP}"
}
trap cleanup EXIT

command -v "${DSH_BIN}" >/dev/null 2>&1 || { printf 'live smoke: dsh not found; set DSH_BIN\n' >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { printf 'live smoke: pnpm not found on PATH\n' >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { printf 'live smoke: npm not found on PATH\n' >&2; exit 1; }
command -v node >/dev/null 2>&1 || { printf 'live smoke: node not found on PATH\n' >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { printf 'live smoke: curl not found on PATH\n' >&2; exit 1; }

go build -o "${TMP}/rdev" ./cmd/rdev
(cd "${BUNDLE}" && npm pack --dry-run --json >"${TMP}/pack.json")

GW_PORT="$(node -e "const net=require('node:net');const s=net.createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})")"
GW="http://127.0.0.1:${GW_PORT}"
"${TMP}/rdev" gateway serve --dev --addr "127.0.0.1:${GW_PORT}" >"${TMP}/gateway.json" 2>"${TMP}/gateway.err" &
GWPID="$!"
for _ in $(seq 1 40); do
  if curl -fsS "${GW}/v1/hosts" >/dev/null 2>&1; then break; fi
  sleep 0.25
done
curl -fsS "${GW}/v1/hosts" >/dev/null || { printf 'live smoke: gateway API did not become ready\n' >&2; sed -n '1,120p' "${TMP}/gateway.err" >&2; exit 1; }

printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"dsh-live-smoke","version":"1"}}}' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | "${TMP}/rdev" mcp serve >"${TMP}/mcp.jsonl"
node - "${TMP}/mcp.jsonl" <<'NODE'
const fs = require('node:fs')
const lines = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(JSON.parse)
const tools = lines.find(line => line.id === 2)?.result?.tools
if (!Array.isArray(tools) || tools.length !== 11) throw new Error(`expected 11 rdev MCP tools, got ${tools?.length}`)
NODE

printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"dsh-live-smoke-remote","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rdev.hosts.list","arguments":{}}}' \
  | "${TMP}/rdev" mcp serve --gateway-url "${GW}" >"${TMP}/mcp-remote.jsonl"
node - "${TMP}/mcp-remote.jsonl" <<'NODE'
const fs = require('node:fs')
const lines = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(JSON.parse)
const hosts = lines.find(line => line.id === 3)?.result?.structuredContent?.hosts
if (!Array.isArray(hosts)) throw new Error('remote rdev.hosts.list did not return a hosts array')
NODE

export DSH_HOME="${TMP}/dsh-home"
"${DSH_BIN}" plugin --profile web add "${BUNDLE}"
RDEV_BIN="${TMP}/rdev" RDEV_GATEWAY_URL="${GW}" "${DSH_BIN}" --profile web --dump-config >"${TMP}/config.yml"
[[ "$(grep -c 'id: rdev' "${TMP}/config.yml")" == 1 ]] || { printf 'live smoke: composed config does not contain exactly one rdev row\n' >&2; exit 1; }

PORT="$(node -e "const net=require('node:net');const s=net.createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})")"
RDEV_BIN="${TMP}/rdev" RDEV_GATEWAY_URL="${GW}" "${DSH_BIN}" web --no-open --port "${PORT}" >"${TMP}/dsh.log" 2>&1 &
DSH_PID="$!"

for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${PORT}/" >/dev/null 2>&1; then
    printf 'deepseek-harness live smoke: ok (loopback gateway, remote MCP call, 11 tools, Profile ready)\n'
    exit 0
  fi
  if ! kill -0 "${DSH_PID}" 2>/dev/null; then
    printf 'live smoke: dsh exited before readiness\n' >&2
    sed -n '1,200p' "${TMP}/dsh.log" >&2
    exit 1
  fi
  sleep 0.25
done

printf 'live smoke: timed out waiting for dsh web\n' >&2
sed -n '1,200p' "${TMP}/dsh.log" >&2
exit 1