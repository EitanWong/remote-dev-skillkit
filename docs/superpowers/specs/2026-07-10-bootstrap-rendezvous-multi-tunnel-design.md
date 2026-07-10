# Bootstrap Rendezvous and Multi-Tunnel Availability Design

## Goal

Eliminate the bootstrap-before-fallback DNS single point in attended temporary
support sessions while preserving a short, visible Windows command, outbound-only
connectivity, policy enforcement, auditability, and support for operators who do
not own a public domain.

This design extends the existing
`2026-07-09-connection-reliability-design.md`. That design makes the full helper
candidate-aware after the signed join manifest is available. This design closes
the earlier gap: selecting and reaching an entry before the bootstrap script and
manifest can be downloaded.

## User Experience

When a stable rendezvous is available, the Windows handoff remains short:

```powershell
powershell -NoProfile -Command "irm 'https://connect.example.com/j/<opaque-id>.ps1' -UseBasicParsing | iex"
```

The rendezvous can be:

- an operator-owned domain;
- a self-hosted `rdev rendezvous serve` deployment on a public server;
- an optional project-operated shared service;
- a compatible third-party deployment implementing the same protocol.

When no rendezvous is configured, `rdev` may still produce a direct public-tunnel
command. The payload must label this mode `degraded_single_entry=true`; it must
not claim automatic pre-bootstrap recovery.

## Root Cause

The current first-connect path has four coupled problems:

1. `startBestAvailableTunnel` returns after the first provider prints a URL, so
   the gateway owns only one ephemeral public path.
2. Windows `windowsBootstrapCommand` uses only `urls[0]`; macOS/Linux already use
   a bounded URL loop.
3. Signed join-manifest gateway candidates become available only after the target
   downloads the bootstrap and manifest. They cannot recover a DNS failure for
   the bootstrap hostname itself.
4. The bootstrap's preconnect, helper assets, checksum, and manifest URL all use
   the request's single base URL. If that hostname is not resolvable, none of the
   existing retry or verification logic runs.

The observed Windows failure was therefore an architectural failure, not merely
a Cloudflare outage: Cloudflare Tunnel registered successfully, while the target
resolver returned a name-resolution failure for the random
`trycloudflare.com` hostname.

## Provider Research

Research was restricted to official documentation, official repositories, and
official terms as of 2026-07-10.

### Default anonymous provider pool

#### Cloudflare Quick Tunnels

- Official documentation:
  <https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/>
- Command shape: `cloudflared tunnel --url http://localhost:<port>`.
- No account or token is required; an HTTPS `*.trycloudflare.com` endpoint is
  assigned.
- Intended only for development/testing. Cloudflare provides no SLA or uptime
  guarantee. The documented limit is 200 in-flight requests, and SSE is not
  supported.
- Eligible as a default provider, but never as the only reliable first entry.

#### Pinggy Free

- Official documentation: <https://pinggy.io/docs/>
- Terms: <https://pinggy.io/terms_of_service/>
- Command shape: `ssh -p 443 -R0:localhost:<port> free.pinggy.io`.
- No account is required for the free endpoint. It returns random HTTP and HTTPS
  URLs.
- Free tunnels rotate and are time-limited. The provider documents a browser
  screening page for free HTTP tunnels; non-browser clients reach the origin
  directly. `rdev` must still test the exact PowerShell/helper request behavior.
- Eligible as the preferred independent-network fallback to Cloudflare.

#### localhost.run

- Official documentation: <https://localhost.run/docs/>
- Command shape: `ssh -R 80:localhost:<port> localhost.run`.
- No proprietary client or account is required for an anonymous HTTPS endpoint.
- Free domains can rotate, are speed-limited, and have no published SLA.
- Eligible as a default hot spare after strict assigned-URL parsing and public
  health validation.

### Optional providers

#### Serveo

- Official documentation: <https://serveo.net/docs/>
- Terms and abuse policy: <https://serveo.net/terms/> and
  <https://serveo.net/abuse/>.
- Command shape: `ssh -R 80:localhost:<port> serveo.net`.
- Anonymous HTTP tunnels are available. Some capabilities require registration.
- Anonymous browser traffic can receive a warning/interstitial, and official
  policy permits additional metadata exposure for anonymous users.
- Eligible only as an explicitly allowed emergency provider after exact API and
  PowerShell bootstrap probes pass.

#### LocalTunnel

- Official repository: <https://github.com/localtunnel/localtunnel>
- Command shape: `npx localtunnel --port <port>` or `lt --port <port>`.
- No account is required and HTTPS URLs are assigned.
- It requires Node/npm or an additional client. Automatic use is allowed only
  when a trusted runtime is already installed or a pinned, checksum-verified
  install action has been explicitly approved. `npx latest` is forbidden.

#### bore.pub

- Official repository: <https://github.com/ekzhang/bore>
- Command shape: `bore local <port> --to bore.pub`.
- The public instance requires no account, but provides raw TCP rather than an
  HTTPS entry. The project explicitly states that the optional bore secret does
  not encrypt forwarded traffic.
- Excluded from public bootstrap. It remains available for self-hosted or
  application-encrypted advanced TCP paths.

### Credentialed provider plugins

ngrok, zrok public service, Tailscale Funnel, Microsoft Dev Tunnels, named
Cloudflare tunnels, and similar services require an account, login, enrollment,
or token. They may be used when the operator already configured credentials, but
the Agent must not create accounts, accept paid resources, or weaken security
controls automatically.

### Excluded provider

Tunnelmole hosted service is excluded from Agent automation. Its official terms
grant unusually broad “hack back” rights, including scanning or attempting to
access systems suspected of misuse. Those terms conflict with this project's
safety boundaries even though its client is technically easy to automate.

## Architecture

### 1. Typed provider registry

Add a provider registry independent of CLI parsing:

```go
type Provider interface {
    ID() string
    Metadata() ProviderMetadata
    Start(context.Context, StartRequest) (Handle, error)
}

type Handle interface {
    Candidate() GatewayCandidate
    Wait() <-chan error
    Stop(context.Context) error
}
```

`ProviderMetadata` declares:

- supported protocols;
- anonymous/account/token requirements;
- required executable and aliases;
- official documentation and terms URLs;
- network root;
- maximum session duration when published;
- endpoint rotation behavior;
- browser interstitial behavior;
- privacy exposure notes;
- whether transport is HTTPS or raw TCP;
- whether default automatic use is permitted.

Provider output parsers must be pure, table-tested functions. Provider processes
must never be started through shell command strings.

### 2. Availability set, not first-success selection

The gateway lifecycle becomes:

1. Reserve the local listen address.
2. Prepare and verify platform helper assets.
3. Load gateway and manifest signing keys and state.
4. Start the local gateway and verify local `/healthz`.
5. Start eligible public providers concurrently with a bounded concurrency of
   two; a third provider may remain warming.
6. Validate each candidate through DNS, TCP, TLS, `/healthz`, bootstrap-script,
   and small asset probes.
7. Create the ticket only after the availability set is known.
8. Store typed candidates in ticket metadata and the signed join manifest.
9. Publish the signed descriptor to rendezvous when configured.
10. Generate the short handoff and begin supervision.

The default zero-registration race is Cloudflare plus Pinggy. localhost.run is
the first hot spare. Serveo and LocalTunnel require explicit policy eligibility.

Provider processes stay in the visible foreground supervisor. Context
cancellation, gateway exit, provider exit, or session cleanup must stop and reap
all child processes. No provider process may be detached with `nohup`, `&`, a
service, or a scheduled task.

### 3. Rendezvous protocol

The rendezvous is not a task gateway and never receives task payloads or signing
private keys. In script-bootstrap mode, however, its HTTPS origin is necessarily
a bootstrap code-delivery trust boundary: a compromised origin could replace the
PowerShell script before any in-script signature check runs. Operator-owned and
project-operated rendezvous deployments must therefore be treated as trusted
bootstrap infrastructure. Phase 3's separately distributed signed native
connector is what reduces rendezvous back to an untrusted availability layer.

It stores an opaque, short-lived descriptor under a cryptographically random
128-bit-or-stronger identifier. The descriptor contains:

- schema version and generation number;
- issue and expiry times;
- session/ticket reference;
- manifest root public key and expected ticket mode;
- ordered public bootstrap candidates;
- provider kind and required request headers;
- health evidence timestamp;
- release/helper verification requirements;
- gateway signature over the canonical descriptor.

The rendezvous must:

- reject listing and prefix search;
- enforce TTL and maximum descriptor size;
- use constant-shape not-found responses;
- rate-limit reads and writes;
- never store gateway signing private keys, host private keys, operator tokens,
  provider tokens, task payloads, artifacts, or audit transcripts;
- accept descriptor updates only from an authenticated operator/gateway client;
- preserve only the latest valid generation plus a bounded overlap generation
  for atomic tunnel rotation.

All rendezvous and tunnel operators can cause denial of service. Because a
one-command bearer handoff necessarily passes session metadata through the entry
service, a shared rendezvous is also a sensitive metadata boundary. Shared-mode
deployments must use short ticket TTLs, redacted logs, strict access controls, and
must not silently broaden auto-activation. A future host-bound pairing protocol
is required before claiming that a shared rendezvous is cryptographically unable
to impersonate a first target.

### 4. Short Windows bootstrap

The rendezvous serves a reviewed project-generated bootstrap script. The human
command remains one short `irm ... | iex` command. No Base64, EncodedCommand,
`ExecutionPolicy Bypass`, DNS-over-HTTPS workaround, hosts-file edit, proxy edit,
or firewall mutation is permitted.

The bootstrap:

1. obtains the signed descriptor from the same stable rendezvous entry;
2. verifies schema, expiry, ticket binding, and the gateway signature against the
   root embedded by the trusted bootstrap origin;
3. probes eligible bootstrap candidates with bounded DNS/TCP/TLS/HTTP timeouts;
4. selects the first candidate that serves the expected ticket/bootstrap
   contract;
5. downloads checksum metadata and the helper through the selected candidate;
6. verifies SHA-256 before execution;
7. passes the entire signed candidate set to the full helper;
8. reports selected provider and fallback reason after a gateway is reachable.

When no rendezvous exists, the existing direct short command remains available
but is explicitly degraded and cannot claim pre-bootstrap fallback.

### 5. Readiness contract

`ready_to_send_to_human=true` requires one of:

- a healthy operator-owned stable rendezvous plus at least one healthy signed
  gateway candidate;
- a healthy approved shared rendezvous plus at least two independent healthy
  public candidates for reliable mode;
- an explicit operator override accepting degraded direct mode.

Without an override, a single random tunnel yields:

```json
{
  "ready_to_send_to_human": false,
  "readiness": "degraded-single-entry",
  "degraded_reason": "bootstrap depends on one ephemeral DNS name",
  "standard_next_action": "start another eligible provider or configure rendezvous"
}
```

The CLI must never silently fall back from failed public providers to a LAN URL
and present that URL as a remote-ready handoff.

### 6. Rotation and supervision

The supervisor tracks each provider as `starting`, `healthy`, `degraded`,
`expired-soon`, `exited`, or `stopped`.

For time-limited providers such as Pinggy Free:

1. start a replacement before expiry;
2. complete all public health probes;
3. publish a new signed descriptor generation;
4. retain the old generation for a short overlap;
5. stop the old provider only after the new descriptor is readable.

Provider failure after target registration does not automatically interrupt the
session when another signed gateway candidate remains healthy. Session reports
must distinguish bootstrap availability, registered host connectivity, and task
transport state.

## Security Invariants

- Third-party tunnels are untrusted transports and availability dependencies.
- A rendezvous serving executable PowerShell is a trusted bootstrap delivery
  boundary until the separately distributed signed native connector replaces
  script-first execution. It still must never hold gateway, host, release, or
  task signing private keys.
- Gateway, manifest, release, and task signing keys remain separate.
- No fallback may weaken manifest signature, ticket expiry, helper checksum,
  policy, capability, or task authorization checks.
- SSH providers use reviewed host-key policy. `StrictHostKeyChecking=no` is
  forbidden. Unknown or changed host keys fail closed unless an operator approves
  a pinned update.
- Provider install actions require an official download URL, pinned SHA-256,
  user/workspace scope, and explicit approval when a new dependency is needed.
- No service installation, hidden persistence, inbound router port, UAC/sudo
  bypass, Defender bypass, DNS mutation, hosts-file mutation, or firewall change.
- Provider URLs, ticket codes, hostnames, IPs, and descriptors are redacted from
  shareable logs according to existing audit policy.

## CLI and Contract Changes

Add:

- `rdev tunnel providers` — read-only registry and eligibility report;
- `rdev tunnel probe` — read-only executable/config/terms readiness;
- `rdev rendezvous serve` — self-hostable descriptor service;
- `rdev rendezvous publish` — authenticated descriptor publication;
- `rdev support-session connect --provider-policy <policy-file>`;
- `RDEV_RENDEZVOUS_URL` and a protected operator credential reference;
- typed `availability_set`, `bootstrap_candidates`, `provider_attempts`,
  `rendezvous`, `readiness`, and `degraded_reason` output fields.

The existing `gateway_url_candidates` and `JoinManifest.GatewayCandidates`
remain compatible. Old clients use the primary candidate. New clients consume
the availability set and descriptor generation.

Do not use a single provider-specific environment variable such as
`RDEV_CLOUDFLARED_GATEWAY_URL` to store a localhost.run, Pinggy, or Serveo URL.
Provider identity and lifecycle must remain typed.

## Testing

### Pure/provider tests

- parse valid and misleading output for every provider;
- reject documentation, dashboard, warning, attacker-suffix, userinfo, and
  malformed URLs;
- validate metadata, eligibility, terms URLs, executable requirements, and
  provider ordering;
- confirm no shell wrappers or unsafe SSH options are generated.

### Lifecycle tests

- Cloudflare healthy and Pinggy healthy;
- first provider NXDOMAIN and second provider healthy;
- provider prints URL but public health fails;
- provider exits after readiness;
- one provider times out while another succeeds;
- context cancellation reaps every child;
- replacement tunnel rotates descriptor atomically;
- no public provider succeeds and LAN is not exposed as remote-ready.

### Rendezvous tests

- unguessable identifier and no listing;
- authenticated publish, signature verification, expiry, size bounds, and
  generation monotonicity;
- unauthorized update, replay, downgrade, wrong ticket, wrong root, and stale
  health evidence fail closed;
- descriptor is removed after TTL;
- logs and errors do not expose ticket or credentials.

### Windows tests

- Windows 10/11, Windows PowerShell 5.1 and PowerShell 7;
- first bootstrap candidate NXDOMAIN, TLS failure, HTTP 530, 429, and timeout;
- second candidate succeeds without DNS/hosts/proxy/firewall mutation;
- helper hash mismatch fails before execution;
- locked desktop remains fail-closed for desktop capabilities;
- temporary connector leaves no service, scheduled task, Run key, Startup item,
  or firewall rule.

### Real acceptance gate

A release cannot claim reliable Windows first-connect until evidence shows:

1. the handoff command remains short and visible;
2. the first public provider is deliberately made unresolvable on the target;
3. rendezvous bootstrap selects a second independent provider;
4. the helper and manifest verify successfully;
5. `support-session smoke-test --remote-control` completes;
6. audit evidence records provider attempts and fallback without secrets;
7. explicit stop reaps all providers and leaves no persistence.

## Delivery Phases

### Phase 1 — Provider registry and availability set

- Introduce typed providers and lifecycle handles.
- Implement Cloudflare, Pinggy, and localhost.run adapters.
- Keep Serveo and LocalTunnel policy-disabled by default.
- Refactor foreground startup so the local gateway is healthy before provider
  probes and the ticket is created after the availability set is known.
- Add readiness and cleanup invariants.

### Phase 2 — Self-hostable rendezvous and short bootstrap

- Implement the signed descriptor schema and `rdev rendezvous serve/publish`.
- Generate the stable short handoff when `RDEV_RENDEZVOUS_URL` is configured.
- Add descriptor generation rotation and supervision.
- Keep direct tunnel mode as an explicit degraded fallback.

### Phase 3 — Published signed native connector

- Publish a small `rdev-connect`/`rdev-bootstrap` release asset through multiple
  stable mirrors.
- Add release-root and platform-signature verification.
- Move candidate probing and helper download from scripts into the reviewed Go
  connector while keeping the short rendezvous command as the human entry.

## Success Criteria

- A random DNS failure for one tunnel provider is no longer fatal when a stable
  rendezvous and another healthy provider exist.
- The Windows handoff remains one short, readable command.
- Operators can use a project service, self-hosted domain, or private server;
  users without a rendezvous receive an honest degraded direct mode.
- Provider additions are isolated, typed, testable, policy-controlled, and
  safely supervised.
- No provider or fallback weakens the project's trust, authorization, audit, or
  operating-system safety boundaries.
