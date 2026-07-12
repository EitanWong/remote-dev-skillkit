# Resilient Public Tunnel Fallback Design

**Date:** 2026-07-11

## Goal

Make the foreground support-session flow survive a non-resolving Cloudflare
Quick Tunnel while preserving one short generated Windows command, strict
provider identity verification, and no unattended background access.

## Evidence

The original failure has two independent causes:

1. Cloudflare can assign a syntactically valid `trycloudflare.com` hostname that
   remains NXDOMAIN on the current network.
2. The SSH fallbacks correctly refuse to start without reviewed host identity,
   but the project previously shipped no provider trust anchor.

Current official-source research found:

- localhost.run publishes its exact SSH RSA host key in the official
  `localhost-run/client-service` repository. The reviewed fingerprint is
  `SHA256:FV8IMJ4IYjYUTnd6on7PqbRjaZf4c1EhhEBgeUdE94I`.
- Pinggy and Serveo do not publish an official SSH host key, SSH CA, or
  DNSSEC-authenticated SSHFP record. They must remain operator-pin-only.
- `tunn3l.sh` is an open-source, no-registration, Agent-oriented WebSocket
  tunnel on standard HTTPS/WSS port 443. Its official v0.5.1 release publishes
  per-asset SHA-256 digests and its foreground `http <port> --json` mode does
  not install a daemon.
- The current China network path successfully established a pinned
  `tunn3l.sh` client, received an HTTPS candidate, completed Web-PKI TLS, and
  forwarded a real request to a local origin.
- Tunnelmole established its control channel but its assigned public endpoint
  did not forward the test request; LocalTunnel and bore have unsuitable
  plaintext data paths; other surveyed anonymous services lacked a verifiable
  identity, required registration, or failed TLS.

This is a point-in-time reachability sample, not a claim of nationwide mainland
availability. `cn-mainland` remains evidence-required.

## Decision

Use a layered default pool:

1. Operator-configured stable public gateway or named tunnel.
2. Cloudflare Quick Tunnel.
3. Managed `tunn3l.sh` v0.5.1 foreground client.
4. localhost.run with the release-reviewed official host key.
5. Pinggy or Serveo only when the operator supplies a reviewed exact-host pin.

Cloudflare and tunn3l should occupy the first two automatic worker slots. The
registry therefore gains an explicit automatic priority rather than relying on
provider-ID alphabetical order. Regional eligibility remains a hard gate before
priority is considered.

Rejected approaches:

- `StrictHostKeyChecking=no`, `accept-new`, runtime `ssh-keyscan`, or first-use
  prompts: unauthenticated TOFU.
- Pinggy's native/standalone client as a trust workaround: the official source
  does not provide enough evidence of complete server certificate and hostname
  verification.
- Tunnelmole as the immediate default: its current public endpoint failed the
  end-to-end forwarding probe and its mutable unsigned binaries need a separate
  supply-chain review.
- LocalTunnel or bore: the tunneled data path is not authenticated end to end.

## Managed tunn3l Release Manifest

The implementation pins the official
[`bdecrem/tunn3l` v0.5.1 release](https://github.com/bdecrem/tunn3l/releases/tag/v0.5.1):

| Agent platform | Asset | Compressed SHA-256 |
|---|---|---|
| darwin/arm64 | `tunn3l-darwin-arm64.gz` | `360669bd64595709cdc111e9bf430040c4608ad823582d035f955464fa1f45e4` |
| darwin/amd64 | `tunn3l-darwin-x64.gz` | `35d559e55cbd40afcaf3acbe806020b7cafd9d8559d3fb6db2c3d16844c10bd6` |
| linux/arm64 | `tunn3l-linux-arm64.gz` | `9df47cad6d1e09313e5b01f76c69e4cde0c901ee424fa7943269cd101db3b1e1` |
| linux/amd64 | `tunn3l-linux-x64.gz` | `902bc626033efb7bddde141542a145d95f55d256bd310e439bc71290a0ad6d58` |

Windows Agent hosts are unsupported by this managed asset version and must
continue to the next candidate. Windows target hosts are fully supported because
the public-tunnel client runs on the Agent/gateway machine, not the target.

The fixed GitHub release URL is only a transport. Bytes are executable only
after matching the embedded digest. Changing version, URL, or digest requires a
reviewed source change.

## Managed Tool Lifecycle

The support-session work directory owns all downloaded and generated state:

```text
<work-dir>/.rdev/tools/tunn3l/v0.5.1/
<work-dir>/.rdev/provider-state/tunn3l/home/
<work-dir>/.rdev/provider-trust/localhost-run/known_hosts
```

For tunn3l:

1. Resolve the asset only from `runtime.GOOS` and `runtime.GOARCH`.
2. Create and validate private directories below the protected session work
   directory.
3. Reuse a cached compressed asset only after streaming its SHA-256 from a
   protected regular-file handle.
4. Otherwise download the fixed HTTPS release URL to a unique file in the same
   directory, follow only HTTPS GitHub release-asset redirects, cap compressed
   bytes at 64 MiB, sync, and atomically rename after digest verification.
5. Decompress the verified gzip stream into a fresh private executable with a
   128 MiB expanded limit. Never execute partial, oversized, symlinked,
   reparse-point, or digest-mismatched content.
6. Invoke only `tunn3l http <local-port> --json`. Never invoke its `daemon`,
   custom subdomain, password, TCP, SSH, login, or token surfaces.
7. Scope `HOME`/`USERPROFILE` to the session provider-state directory, force
   `TUNN3L_RELAY=wss://tunn3l.sh/ws/connect`, clear token/subdomain overrides,
   and reject Node TLS-disabling environment variables.
8. Parse the first canonical `https://<label>.tunn3l.sh` candidate, then continue
   draining and discarding all later provider output.

The process remains a direct child of the visible foreground supervisor. Stop,
context cancellation, process exit, or gateway shutdown must cancel and reap it.

## localhost.run Trust Anchor

Embed a reviewed provider trust record containing:

- provider ID `localhost-run`;
- logical host `localhost.run` and port 22;
- the exact official RSA known-hosts line;
- fingerprint `SHA256:FV8IMJ4IYjYUTnd6on7PqbRjaZf4c1EhhEBgeUdE94I`;
- source commit `9f499be7ece07d59ed927edbcfa6860ee7bcb853` and immutable source URL;
- review date and rotation notes.

Each support session atomically materializes a private provider-specific
`known_hosts` snapshot. Existing strict SSH argv and exact host/port validation
remain unchanged. Operator policy may override the built-in pin with a protected
reviewed file. Key mismatch fails closed and moves to the next candidate; rdev
never keyscans or mutates the user's global SSH state.

## Eligibility and Diagnostics

- Add `AutomaticPriority` to provider metadata. Lower positive values start
  first after regional eligibility: Cloudflare 10, tunn3l 20, localhost.run 30,
  operator-pin-only providers 40 or later.
- Under the global profile, non-default providers are eligible only when an
  explicit provider policy allowlists them.
- Under `cn-mainland`, every provider still requires fresh verified evidence;
  this live Agent-side sample does not bypass that rule.
- Registry evaluation must retain fixed ineligibility reasons even when no
  provider is selected.
- If selection is empty, support-session writes a `provider-selection`
  diagnostic with skipped attempts such as `regional-evidence-missing` and
  returns an eligibility error. It must not misreport a static bootstrap probe
  that never ran.
- Managed-tool failures use fixed redacted classes such as `tool-unsupported`,
  `download-failed`, `integrity-failed`, `install-failed`, and `process-exited`.
  No cache path, release URL, public candidate URL, token, raw stderr, or key
  material appears in shareable status.

## Code Boundaries

- Add a small managed-tool installer under `internal/cli`; do not expand the
  already large provider file with download logic.
- Add provider trust-anchor materialization under a separate focused file.
- Extend `tunnel.StartRequest` with the protected provider tool/state/trust root.
- Add a `tunn3l` canonical provider and URL parser.
- Change support-session startup to pass the session root and to preserve
  registry evaluation when selection is empty.
- Keep stable gateway and generated handoff contracts unchanged.

## Tests

TDD coverage must include:

- all four asset mappings and unsupported-platform rejection before network;
- valid cache reuse, corrupt cache rejection, bounded TLS download, non-2xx,
  redirect downgrade, digest mismatch, oversize compressed/expanded content,
  cancellation, and concurrent installation;
- protected file/directory, symlink/reparse, and atomic-install behavior;
- exact tunn3l argv and sanitized environment with no persistence flags;
- canonical `tunn3l.sh` URL parsing and suffix-attack rejection;
- foreground process survival, exit observation, stop, and reaping;
- official localhost.run key fingerprint, provenance, private materialization,
  operator override, and host-key mismatch fallback;
- global priority ordering and explicit-only non-default providers;
- empty mainland selection producing `provider-selection` plus
  `regional-evidence-missing`, without a static-probe diagnostic;
- Cloudflare DNS failure followed by healthy tunn3l readiness;
- an opt-in live tunn3l readiness test against a local gateway;
- full tests, vet, race tests, Windows test cross-compilation, and Windows/Linux
  builds.

## Success Criteria

1. With no stable gateway and no operator SSH pin, a failed Cloudflare DNS probe
   can fall through to a verified foreground tunn3l HTTPS candidate.
2. The target human still receives exactly the generated short readable Windows
   PowerShell command.
3. localhost.run can start unattended only because its official key is pinned;
   Pinggy and Serveo remain fail-closed without operator-reviewed pins.
4. No downloaded client, SSH key, public URL, or provider process escapes the
   session trust and lifecycle boundaries.
5. The real Windows target completes bootstrap and the built-in support-session
   smoke test through a public candidate before the change is declared complete.

