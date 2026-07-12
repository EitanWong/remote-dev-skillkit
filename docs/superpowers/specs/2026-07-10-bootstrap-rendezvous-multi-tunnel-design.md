# Bootstrap Rendezvous and Multi-Tunnel Availability Design

## Goal

Eliminate the bootstrap-before-fallback DNS single point in attended temporary
support sessions while preserving a short, visible Windows command, outbound-only
connectivity, policy enforcement, auditability, and support for operators who do
not own a public domain. Mainland China is a first-class deployment profile with
its own provider eligibility, rendezvous placement, and release acceptance gate.

This design extends the existing
`2026-07-09-connection-reliability-design.md`. That design makes the full helper
candidate-aware after the signed join manifest is available. This design closes
the earlier gap: selecting and reaching an entry before the bootstrap script and
manifest can be downloaded.

## User Experience

The long-term recommended Windows handoff uses a previously installed,
Authenticode-signed static connector and remains short. The current Phase 1
foreground compatibility path is documented separately below:

```powershell
rdev-connect 7K3M-Q9TV-2R6P-X8HD-4WCN-FA9B-2Z
```

The connector is installed once from a signed release, a trusted package
manager, or a download-and-inspect flow. Connector installation is separate from
each temporary session and must not add a service, scheduled task, auto-start, or
unattended access. This preserves the short recurring command without making an
ephemeral HTTPS origin the code-signing authority.

This intentionally changes the clean-machine contract. A stock Windows host
cannot simultaneously have zero prior trust/install, a very short command,
multiple pre-HTTP fallback domains, and execution-time publisher verification.
The supported secure paths are therefore:

- when an approved package manager/source is present, run its short connector
  install command, then the short join command;
- otherwise, download and inspect the signed connector through a normal browser
  or administrator-provided media, then run the short join command;
- after that one-time user-scoped setup, every temporary session uses only
  `rdev-connect <code>` and still creates no persistent remote-access service.

A combined one-line install-and-join command may be offered only for a package
manager/platform combination that passes real clean-machine tests, including
PATH/alias refresh and non-interactive publisher verification. The design does
not promise that capability on Windows editions without such a trust channel.

The rendezvous can be:

- an operator-owned domain;
- a self-hosted `rdev rendezvous serve` deployment on a public server;
- an optional project-operated shared service;
- a compatible third-party deployment implementing the same protocol.

For a target in mainland China, the Agent selects the `cn-mainland` profile. The
signed connector probes multiple mainland-verified rendezvous domains embedded
in its signed trust/config bundle. It must not silently substitute a globally
preferred hostname that has no current mainland network evidence.

When the signed connector is unavailable, `rdev` may offer the existing
download-to-file plus Authenticode verification flow from
`docs/architecture/FINAL_SYSTEM_DESIGN.md`. A dynamic `irm | iex` command is not
the recommended path. Any zero-install, single-origin compatibility flow must be
explicitly labeled `degraded_single_entry=true` and must not claim automatic
pre-bootstrap recovery or equivalent supply-chain safety.

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

## Current Phase 1 Foreground Compatibility Path

As implemented on 2026-07-12, public fallback happens on the Agent/gateway side;
the Windows human does not select providers. The current order is:

1. configured stable operator gateway or named tunnel;
2. Cloudflare Quick Tunnel, priority 10;
3. managed checksum-pinned tunn3l v0.5.1, priority 20;
4. localhost.run with the reviewed official host key, priority 30;
5. Pinggy at priority 40, or another SSH provider, only after an explicit
   operator allowlist and a reviewed exact host pin.

`rdev` chooses one recommended public hostname and emits one short generated
PowerShell command in `target_handoff_envelope.full_text`. A target report of
`trycloudflare.com` DNS failure or NXDOMAIN is evidence to stop selecting
Cloudflare for that recovery attempt, not a reason to restart it. The Agent
creates a protected policy containing only
`{"disabled_provider_ids":["cloudflare-quick"]}` inside the protected absolute
work directory and reruns foreground `support-session connect --start` with
`--region global --provider-policy <path>`. Managed tunn3l or localhost.run then
owns fallback, and only the newly generated handoff is sent. The Agent does not
manually start providers, background the supervisor, expose a LAN-primary
handoff, construct a multi-URL PowerShell loop, or change DNS, hosts, proxy, or
firewall state.

Temporary anonymous/account-free services are candidates, not guaranteed
mainland-China services. A successful Agent-side tunn3l sample, including the
opt-in live readiness test, is not China Telecom/Unicom/Mobile evidence.
`cn-mainland` remains fail-closed until fresh verified evidence exists for all
three carriers under the evidence contract below. The generated Windows command
must not use `EncodedCommand`, Base64 bootstrap, or `ExecutionPolicy Bypass`.
Smoke-test completion does not authorize disconnecting a live session; the
foreground supervisor remains alive until the operator explicitly disconnects.

For tunn3l v0.5.1, `Anonymous=true` means no account or registration is
required; it is not a privacy or no-telemetry guarantee. The pinned upstream
source creates a `dv_` plus 24-hex device ID
([source](https://github.com/bdecrem/tunn3l/blob/2025a09e880bb6df4395ea8c65a6949a97265e44/cli/bore.js#L35-L42))
and sends that ID, the Agent hostname, and Agent OS in relay registration
metadata
([source](https://github.com/bdecrem/tunn3l/blob/2025a09e880bb6df4395ea8c65a6949a97265e44/cli/bore.js#L163-L169)).
`rdev` supplies a fresh empty session `HOME`/`USERPROFILE`/XDG config and clears
tunn3l token/subdomain/password plus runtime preload variables. It therefore
does not reuse the user's real `~/.tunn3l` and generates a new session-scoped
device ID. The relay still observes normal network and HTTP tunnel traffic.
These claims are pinned to commit
`2025a09e880bb6df4395ea8c65a6949a97265e44` and must not be generalized beyond
v0.5.1.

## Provider Research

Research was restricted to official documentation, official repositories, and
official terms as of 2026-07-10.

### Mainland China requirement

“Publicly reachable” and “reachable from mainland China” are different
properties. No anonymous global tunnel provider is considered mainland-ready
from documentation or marketing alone. The default pool for `cn-mainland` is
assembled only from target-side evidence covering China Telecom, China Unicom,
and China Mobile. Evidence expires and must be refreshed; an untested provider
has `cn_mainland_status=unknown`, not `verified`.

The observed `trycloudflare.com` NXDOMAIN is sufficient to remove Cloudflare
Quick Tunnels from assumed-first position for this profile. Cloudflare's official
China Network is a separate JD Cloud-based product, not evidence that anonymous
Quick Tunnel hostnames are reachable in mainland China:
<https://developers.cloudflare.com/china-network/>.

Regional selection applies hard eligibility gates first: credentials are already
configured, provider terms and client integrity are accepted by policy, legal
requirements are satisfied by the operator, and regional evidence is unexpired.
Eligible candidates are then ranked by measured success rate, latency, capacity,
and independent failure domains. Ownership or a `CN`/`HK` label does not by
itself outrank better evidence. Explicit degraded single-entry mode is last and
is clearly reported as unreliable.

The software does not modify DNS servers, enable DNS-over-HTTPS, edit the hosts
file, change proxies, or attempt to evade network controls. Deployments in
mainland China must satisfy applicable registration, filing, content, and service
requirements; `rdev` reports this as an operator responsibility and does not
automate legal acceptance or identity verification. A project-operated shared
service also requires a separate legal/privacy release gate covering applicable
filing or licensing, data roles, retention and deletion, cross-border transfer,
subprocessors, incident response, and user notice.

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

#### tunn3l.sh v0.5.1

- Official repository and pinned release:
  <https://github.com/bdecrem/tunn3l/tree/2025a09e880bb6df4395ea8c65a6949a97265e44>
  and <https://github.com/bdecrem/tunn3l/releases/tag/v0.5.1>.
- `rdev` downloads only the platform asset selected by GOOS/GOARCH, verifies its
  embedded compressed SHA-256, expands it into protected session storage, and
  invokes only foreground `tunn3l http <port> --json`.
- No account is required. The privacy and session-scoped state limits are the
  exact v0.5.1 limits stated in the Phase 1 section above.
- Eligible as the priority-20 global fallback; it is not mainland-verified.

#### Pinggy Free

- Official documentation: <https://pinggy.io/docs/>
- Terms: <https://pinggy.io/terms_of_service/>
- Command shape: `ssh -p 443 -R0:localhost:<port> free.pinggy.io`.
- No account is required for the free endpoint. It returns random HTTP and HTTPS
  URLs.
- Free tunnels rotate and are time-limited. The provider documents a browser
  screening page for free HTTP tunnels; non-browser clients reach the origin
  directly. `rdev` must still test the exact PowerShell/helper request behavior.
- Eligible as a fallback candidate when measured failure-domain metadata shows
  sufficient independence from the other selected route.

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

For `cn-mainland`, the following domestic services are candidate plugins, not
anonymous defaults:

- **cpolar** — official Windows guidance requires free account registration and
  login. Its official tutorial documents random free addresses, approximately
  24-hour rotation, 1 Mbit/s free bandwidth, and selectable `CN`, `HK`, `US`,
  `TW`, and `EUR` regions. Official guide:
  <https://www.cpolar.com/blog/cpolar-quick-start-tutorial-windows-series>.
- **NATAPP** — the official quick-start requires account registration, selecting
  a free or paid tunnel, obtaining an `authtoken`, and running the client with
  that token. Official guide: <https://natapp.cn/article/natapp_newbie>.
- **SakuraFrp** — requires an account/access key; its official documentation has
  an identity-verification workflow and states that mainland HTTP(S) “website”
  nodes require ICP filing verification. Official account and filing references:
  <https://doc.natfrp.com/faq/account.html> and
  <https://doc.natfrp.com/faq/beian.html>.
- **OpenFrp** — the official site describes a free service with 20+ worldwide
  nodes, but use begins through login or registration. It is eligible only after
  exact node, credential, client-integrity, and mainland-network probes succeed:
  <https://www.openfrp.net/>.

Credentials for these plugins are referenced from an operator-controlled secret
store or environment; they never appear in commands, descriptors, logs, or
ticket metadata. The Agent may explain configuration and validate an existing
account, but must not register, perform identity verification, purchase service,
or accept provider terms on the user's behalf.

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
- whether default automatic use is permitted;
- supported deployment regions and selectable provider nodes;
- per-region status: `unknown`, `candidate`, `verified`, `degraded`, or `blocked`;
- timestamp and expiry of regional evidence;
- authoritative-DNS operator, edge/CDN network, origin ASN/network, tunnel
  control plane, and certificate dependency as separate failure-domain fields;
- sampled carrier/province, DNS/TCP/TLS/HTTP success, latency, throughput, and
  continuous-session duration, stored without target-identifying IP addresses.

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

For the global profile, Cloudflare, tunn3l, and localhost.run are the default
automatic candidates, not assumed-independent brands. Pinggy is non-default and
requires an explicit restrictive allowlist plus a reviewed exact pin. Automatic
racing requires evidence that the selected pair does not share the same relevant
DNS, edge, origin, and control-plane failure domains. For `cn-mainland`, there is
no hard-coded provider winner: the race uses the highest-ranked eligible
candidates with unexpired mainland evidence. A configured domestic provider may
outrank every anonymous provider. Serveo and LocalTunnel require explicit policy
eligibility in every profile.

SSH-backed providers may be automatic only when an official host key or SSH CA
can be pinned. The registry stores a reviewed pinset, its provenance, expiry, and
rotation procedure. A provider without stable verifiable host identity requires
explicit per-session approval and cannot be a default automatic provider.

Provider processes stay in the visible foreground supervisor. Context
cancellation, gateway exit, provider exit, or session cleanup must stop and reap
all child processes. No provider process may be detached with `nohup`, `&`, a
service, or a scheduled task.

### 3. Rendezvous protocol

The rendezvous is not a task gateway and never receives task payloads or signing
private keys. In the recommended signed-connector flow it is an untrusted
availability and metadata service: it can deny, replay, or substitute data, but
it cannot make the connector execute unsigned code or silently change embedded
trust roots. A single-origin script compatibility flow makes that HTTPS origin a
code-delivery authority and therefore cannot claim equivalent security or
reliability.

It stores an opaque, short-lived descriptor under a cryptographically random
128-bit-or-stronger identifier. The descriptor contains:

- schema version and generation number;
- issue and expiry times;
- session/ticket reference;
- session authority public key and expected ticket mode;
- ordered public bootstrap candidates;
- provider kind and a bounded non-secret request-header profile;
- health evidence timestamp;
- signed release-manifest reference and expected helper hash/size;
- session-authority signature over the canonical descriptor.

The signed connector embeds maintainer release roots and a signed default
rendezvous set. Operator-managed deployments can add rendezvous and operator
trust roots only through an explicit signed policy installation. The session
authority is bound as follows:

- operator-owned mode chains it to an already configured operator trust root;
- shared mode treats the human code only as a locator and requires an attended
  cross-channel fingerprint comparison before host activation;
- no shared rendezvous mode may silently auto-activate a first target before that
  binding is confirmed.

The human code encodes at least 128 bits of randomness in a grouped, readable
form. It remains short enough for `rdev-connect <code>` but is not an 8-character
low-entropy password.

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

Publication fans the exact same opaque code and canonical signed descriptor
bytes to every rendezvous in the selected set. Reliable readiness requires the
exact generation to be read back and signature-verified through at least two
independent rendezvous domains from the applicable probe region. Partial publish
does not advance the advertised generation: the previous quorum-readable
generation remains active while retries continue. If no quorum can be restored,
the session is degraded and the CLI must not claim rendezvous failover. Rotation
stops old tunnels only after both descriptor quorum and gateway-candidate health
quorum are proven.

Request-header profiles are schema-defined and allow only reviewed, non-secret
headers. `Authorization`, `Cookie`, `Proxy-Authorization`, `Host` overrides,
control characters, CR/LF, and unbounded names or values are forbidden. Provider
credentials are used only on the operator side to start a tunnel and are never
sent to the target.

For `cn-mainland`, rendezvous is also the regional availability anchor. A
deployment claiming mainland readiness must provide:

- at least two stable, mainland-verified hostnames rather than an ephemeral
  tunnel hostname;
- rendezvous hostnames whose authoritative DNS and origins do not share one
  failure domain;
- target-side DNS, TLS, script, descriptor, and small-asset probes;
- a mainland-compliant origin when hosted in mainland China, or measured Hong
  Kong/nearby origins when mainland hosting is not available;
- a documented failover and certificate-renewal runbook;
- no dependency on a blocked third-party script, font, analytics, or CDN asset.

The short command contains no hostname. The signed connector probes its signed
rendezvous set, so one domain-specific NXDOMAIN, negative cache, certificate
failure, or blocked route can fall through to another independent domain. Users
without their own domain can use an optional project-operated regional
rendezvous set; operators can instead install a signed self-hosted policy.

All rendezvous and tunnel operators can cause denial of service. Because a
one-command bearer handoff necessarily passes session metadata through the entry
service, a shared rendezvous is also a sensitive metadata boundary. Shared-mode
deployments must use short ticket TTLs, redacted logs, strict access controls, and
must not silently broaden auto-activation. Shared mode is not ready until the
attended fingerprint pairing succeeds.

### 4. Short Windows connector flow

The human command remains `rdev-connect <code>`. The static connector is
Authenticode-signed and its release is verified before first use. No Base64,
EncodedCommand, `Invoke-Expression`, `ExecutionPolicy Bypass`, DNS-over-HTTPS
workaround, hosts-file edit, proxy edit, or firewall mutation is permitted.

“Valid Authenticode” alone is insufficient because it accepts any Windows-trusted
publisher. Package installation must bind all of: approved package source,
canonical package ID, installer digest from that source, expected maintainer
publisher identity, and the current publisher certificate/SPKI allowlist. Manual
installation uses an external verifier or an administrator-inspected signed
release manifest before the connector executes. Publisher rotation requires a
maintainer-signed trust-bundle update with an overlap window and an explicit
revocation path; the new connector cannot authorize its own signer after launch.

The connector:

1. probes the signed regional rendezvous set with bounded DNS/TCP/TLS/HTTP
   timeouts;
2. obtains the descriptor and verifies schema, expiry, ticket binding,
   generation, and session-authority signature;
3. performs attended fingerprint binding when shared rendezvous is used;
4. probes eligible gateway candidates with bounded DNS/TCP/TLS/HTTP timeouts;
5. selects the first candidate that serves the expected ticket/bootstrap
   contract;
6. verifies the maintainer-signed release manifest using an embedded release
   root, then obtains the helper from any candidate or approved release mirror;
7. verifies the helper hash, size, platform, version, and Authenticode policy
   from that signed manifest before execution; a candidate-supplied `.sha256`
   file is never a trust anchor;
8. passes the signed descriptor/session authority to the full helper;
9. reports selected rendezvous, provider, and fallback reason after a gateway is
   reachable.

The descriptor is the live candidate authority. The join manifest binds the same
session-authority key and minimum descriptor generation. The helper accepts only
monotonically newer descriptors signed by that key, which makes tunnel rotation
an atomic authority update instead of an unsigned URL replacement.

When the signed connector or rendezvous set is unavailable, the existing direct
download flow remains available only as explicitly degraded compatibility mode.

### 5. Readiness contract

Availability and authorization use separate states:

- `ready_to_send=true` means the handoff can be given to the human because the
  exact descriptor generation is readable through rendezvous quorum and at least
  two independent signed gateway candidates are healthy;
- `ready_to_activate=true` means the target has run the verified connector, the
  session authority is bound, required shared-mode fingerprint pairing has
  succeeded, the target is registered, and normal policy/approval gates allow
  activation;
- `ready_to_execute=true` is later and still requires task-specific policy,
  capability, and approval decisions.

`ready_to_send=true` requires one of:

- a healthy operator-owned rendezvous set plus at least two independent healthy
  signed gateway candidates;
- a healthy approved shared rendezvous plus at least two independent healthy
  public candidates; `ready_to_activate` remains false until attended fingerprint
  pairing succeeds;
- an explicit operator override accepting degraded direct mode.

Under `cn-mainland`, the first two cases additionally require an unexpired
representative mainland rendezvous evidence and two mainland-verified gateway
candidates with independent failure domains. Global health checks alone never
satisfy this condition. Current-target evidence can improve or demote selection
only after the signed connector begins running; it cannot be a prerequisite
falsely claimed before the first command.

Without an override, a single random tunnel yields:

```json
{
  "ready_to_send": false,
  "ready_to_activate": false,
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
- The Authenticode-signed static connector and embedded maintainer release roots
  are the initial code-execution trust anchor. Rendezvous and tunnels cannot
  replace them.
- A compatibility rendezvous serving executable PowerShell is a full bootstrap
  code-delivery authority. It is degraded, off by default, never uses
  `Invoke-Expression`, and must follow the existing download-to-file plus
  Authenticode verification design. It still must never hold gateway, host,
  release, or task signing private keys.
- Gateway, manifest, release, and task signing keys remain separate.
- No fallback may weaken descriptor/manifest signature, ticket expiry,
  maintainer-signed helper hash and size, Authenticode policy, capability, or task
  authorization checks. A checksum fetched beside its artifact is evidence only,
  never an authority.
- SSH providers use reviewed host-key policy. `StrictHostKeyChecking=no` is
  forbidden. Unknown or changed host keys fail closed unless an operator approves
  a pinned update.
- Provider install actions require an official download URL, pinned SHA-256,
  user/workspace scope, and explicit approval when a new dependency is needed.
- No service installation, hidden persistence, inbound router port, UAC/sudo
  bypass, Defender bypass, DNS mutation, hosts-file mutation, or firewall change.
- Protected raw audit and shareable export are separate views. Shareable logs
  redact provider URLs, ticket codes, hostnames, IPs, and descriptors while
  retaining `provider_id`, a one-way `candidate_id`, descriptor generation, and
  error class so fallback remains auditable.

## CLI and Contract Changes

Add:

- `rdev tunnel providers` — read-only registry and eligibility report;
- `rdev tunnel probe` — read-only executable/config/terms readiness;
- `rdev rendezvous serve` — self-hostable descriptor service;
- `rdev rendezvous publish` — authenticated descriptor publication;
- `rdev support-session connect --provider-policy <policy-file>`;
- `rdev support-session connect --region cn-mainland`;
- `rdev tunnel probe --region cn-mainland`;
- `rdev-connect <code>` — signed target-side connector entry;
- signed rendezvous-set policy plus a protected publication credential reference;
- typed `availability_set`, `bootstrap_candidates`, `provider_attempts`,
  `rendezvous`, `region_profile`, `regional_evidence`, `readiness`, and
  `degraded_reason` output fields.

Region selection is explicit configuration or an Agent decision based on the
operator's stated target location. It is not inferred by sending the target IP
to a third-party geolocation service. If the target region is unknown, the CLI
reports that uncertainty rather than claiming mainland readiness.

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
- confirm no shell wrappers or unsafe SSH options are generated;
- validate regional evidence expiry and ensure `unknown` never sorts as
  `verified`;
- ensure credentials and target-identifying network data are redacted.

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
- first rendezvous NXDOMAIN and second independent rendezvous succeeds;
- reliable readiness requires exact-generation read-back quorum; partial publish
  keeps the previous generation and cannot claim failover;
- shared rendezvous substitution cannot activate before fingerprint pairing;
- descriptor and join manifest bind the same session authority and generation;
- forbidden/secret request headers and CR/LF injection fail schema validation;
- descriptor is removed after TTL;
- logs and errors do not expose ticket or credentials.

### Windows tests

- Windows 10/11, Windows PowerShell 5.1 and PowerShell 7;
- signed connector install/inspection and Authenticode failure behavior;
- a different but Windows-trusted publisher, wrong package source/ID, stale
  signer pin, and unauthorized signer rotation all fail before first execution;
- first rendezvous domain NXDOMAIN, TLS failure, HTTP 530, 429, and timeout;
- second independent rendezvous domain succeeds without
  DNS/hosts/proxy/firewall mutation;
- first gateway candidate fails and the second candidate succeeds;
- helper hash mismatch, size mismatch, unsigned manifest, wrong release root, and
  a candidate replacing both helper and `.sha256` all fail before execution;
- locked desktop remains fail-closed for desktop capabilities;
- temporary connector leaves no service, scheduled task, Run key, Startup item,
  or firewall rule.

### Mainland China matrix

Pre-release representative mainland verification must include, at minimum:

- China Telecom, China Unicom, and China Mobile from at least two geographically
  distinct mainland locations per carrier;
- no VPN, overseas proxy, or cloud-only probe counted as mainland evidence;
- Windows native DNS/TLS plus signed-connector descriptor read, helper download,
  registration, and smoke test; the optional Authenticode installation flow is
  also tested under Windows PowerShell 5.1;
- DNS/TLS/HTTP success rate, p50/p95 latency, small-asset throughput, and a
  continuous 60-minute session including at least one candidate rotation;
- deliberate failure of the preferred tunnel while the stable rendezvous stays
  reachable and selects an independent candidate;
- evidence timestamp and expiry, with automatic demotion when stale.

Each result is a signed probe attestation containing the accepted issuer,
connector/probe version, coarse carrier and region, resolver type, endpoint/node
identity, timestamp, sample count, metrics, and outcome. Raw target IP addresses
are not placed in the attestation. Trust policy defines accepted evidence
issuers. Evidence expires after at most seven days and is invalidated immediately
by endpoint/node identity, DNS root, certificate dependency, or connector version
changes; a fresh hard failure demotes the route without waiting for expiry.

This representative evidence is used for initial ranking and release claims. It
does not claim that the current target was tested before the connector ran.
Current-session target evidence begins only after `rdev-connect` starts and may
adjust selection for that session. A provider is `verified` only for the measured
profile and evidence window. Release notes must name the tested carrier/region
matrix and must not claim universal mainland coverage.

### Real acceptance gate

A release cannot claim reliable Windows session handoff until evidence shows:

1. the handoff command remains short and visible;
2. the first public provider is deliberately made unresolvable on the target;
3. the signed connector selects a second independent rendezvous and provider;
4. the helper and manifest verify successfully;
5. `support-session smoke-test --remote-control` completes;
6. audit evidence records provider attempts and fallback without secrets;
7. explicit stop reaps all providers and leaves no persistence;
8. `ready_to_send`, `ready_to_activate`, and `ready_to_execute` transition
   independently and fail closed.

A separate clean-machine claim additionally requires successful connector setup
through every advertised package/manual path, including publisher pinning,
revocation, PATH/alias behavior, reboot requirements, and uninstall cleanup.

For a release advertised as mainland-ready, all items in the mainland matrix
must also pass on real mainland Windows hosts. A failure on any carrier does not
necessarily block the global release, but it blocks the mainland-ready claim and
demotes the affected route until replacement evidence passes.

## Delivery Phases

### Phase 1 — Provider registry and availability set

- Introduce typed providers and lifecycle handles.
- Implement Cloudflare, managed pinned tunn3l, localhost.run, and explicit-only
  Pinggy adapters with priorities 10, 20, 30, and 40.
- Keep Serveo and LocalTunnel policy-disabled by default.
- Refactor foreground startup so the local gateway is healthy before provider
  probes and the ticket is created after the availability set is known.
- Add readiness and cleanup invariants.
- Add region profiles and expiring regional-evidence records; ship
  `cn-mainland` initially as evidence-required, with no presumed provider.
- Keep all generated direct handoffs degraded until the signed connector and
  rendezvous-set path is complete.

### Phase 2 — Signed connector and self-hostable rendezvous set

- Publish the small Authenticode-signed `rdev-connect` asset through multiple
  stable release mirrors and document trusted package/manual installation.
- Pin approved package source, package ID, installer digest, maintainer publisher,
  and signer rotation/revocation policy.
- Embed maintainer release roots and a signed default rendezvous set.
- Implement the signed descriptor/session-authority schema and
  `rdev rendezvous serve/publish`.
- Generate `rdev-connect <128-bit-code>` as the recurring short handoff.
- Implement attended fingerprint pairing for shared rendezvous mode.
- Verify the signed release manifest and helper hash/size before execution.
- Add descriptor generation rotation and supervision.
- Keep direct tunnel mode as an explicit degraded fallback.
- Document a mainland/Hong Kong deployment profile and optional project-operated
  regional endpoint without making that service mandatory.

### Phase 2b — Credentialed mainland provider adapters

- Add adapters only for providers whose official client integrity, credential
  handling, terms, and non-interactive lifecycle can be verified.
- Start with the smallest provider set that passes the real mainland matrix;
  cpolar, NATAPP, SakuraFrp, and OpenFrp are candidates, not promised defaults.
- Never auto-register accounts, submit identity data, or purchase/upgrade plans.

### Phase 3 — Optional project regional rendezvous service

- Complete the independent legal/privacy/security release gates before operating
  a shared service.
- Deploy at least two mainland-verified rendezvous domains with independent DNS
  and origin failure domains.
- Publish signed regional evidence and automatically demote stale or failing
  routes.
- Keep self-hosted and operator-owned modes fully supported; the shared service
  remains optional.

## Success Criteria

- A random DNS failure for one rendezvous or tunnel provider is no longer fatal
  when the signed connector has an independent healthy candidate.
- Mainland-ready claims are based on current, signed, expiring representative
  China Telecom/Unicom/Mobile evidence, not global probes or provider marketing.
- After one-time verified connector setup, every Windows session handoff remains
  one short, readable command; clean machines use an honest verified setup step
  rather than a long or unsafe bootstrap command.
- Operators can use a project service, self-hosted domain, or private server;
  users without a rendezvous receive an honest degraded direct mode.
- Provider additions are isolated, typed, testable, policy-controlled, and
  safely supervised.
- No provider or fallback weakens the project's trust, authorization, audit, or
  operating-system safety boundaries.
