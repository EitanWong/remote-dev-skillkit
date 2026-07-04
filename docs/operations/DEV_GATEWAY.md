# Development Gateway

`rdev gateway serve --dev` starts a local HTTP or HTTPS gateway backed by the same in-memory state machine used by `rdev mcp serve`.

This surface is suitable for local production-style validation and binds to
`127.0.0.1` by default. Plain HTTP does not authenticate requests. When
`--tls-cert` and `--tls-key` are set, the gateway serves HTTPS. When
`--client-ca` is also set, the gateway requires and verifies client
certificates signed by that CA. Host job transport supports short polling,
long polling, and WSS; WSS reuses the same TLS/mTLS client settings as the
HTTPS API.

For Connection Entry bootstraps, the dev gateway defaults
`--auto-build-rdev-assets=true`. When no explicit `--rdev-assets-dir` or
platform-specific helper flag is set, and the command is run from a valid
checkout with Go available, it builds the Windows/macOS/Linux `rdev` helpers
and serves them from `/assets`. This keeps accidental low-level
`gateway serve` plus `invite create` flows from giving clean targets a
bootstrap that fails with "rdev is required". Operators can disable the
behavior with `--auto-build-rdev-assets=false` or override it with reviewed
explicit asset paths.

## Operator Auth

For local production-like control-plane preflight, initialize hashed operator
tokens and start the dev gateway with `--operator-auth`:

```bash
rdev operator-auth init \
  --out .rdev/operator-auth/operators.json \
  --token-dir .rdev/operator-auth/tokens

rdev operator-auth verify \
  --auth .rdev/operator-auth/operators.json

rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --operator-auth .rdev/operator-auth/operators.json
```

The auth file uses schema `rdev.operator-auth.v1` and stores only
`sha256:<hex>` token hashes. Generated token files are written separately with
`0600` permissions and are never printed in command output. When operator auth
is enabled, control-plane mutations require an `admin` or `operator` token;
enrollment issuance, hosted renewal, and hosted revocation fetch require an
`admin` or `issuer` token; audit/read-only operator views require an `admin`,
`operator`, or `auditor` token.

Hosted operator auth can be configured with an EdDSA JWT issuer file:

```bash
rdev operator-auth verify-hosted \
  --auth .rdev/operator-auth/hosted-operators.json

rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --hosted-operator-auth .rdev/operator-auth/hosted-operators.json
```

The hosted auth file uses schema `rdev.hosted-operator-auth.v1` and validates
issuer, audience, key id, `exp`, `nbf`, and role claims. It is provider-neutral:
the project does not hardcode Okta, Auth0, cloud domains, private hosts, or
operator paths.

## Storage

Gateway state persistence is routed through a state-store provider boundary.
The built-in providers are `file` and `postgres`; future hosted providers
should implement the same load/save contract instead of changing HTTP
handlers. The Postgres provider uses `psql`/libpq, stores the current
`rdev.gateway-snapshot.v1` as JSONB in `rdev_gateway_snapshots`, and refuses
inline passwords in `--storage-path`. Use a libpq service file, `.pgpass`,
environment injection, or an operator-approved secret manager instead of
placing database passwords in process arguments.

```bash
rdev gateway storage verify \
  --provider file \
  --path .rdev/gateway/state.json

rdev gateway storage verify \
  --provider postgres \
  --path service=rdev_gateway

rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --storage-provider file \
  --storage-path .rdev/gateway/state.json

rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --storage-provider postgres \
  --storage-path service=rdev_gateway
```

`--state` remains a convenience alias for the file provider path.

Hosted provider packages are the reviewable deployment surface for gateway
storage/auth combinations. They contain provider metadata, gateway argument
templates, environment variable names, runbook text, checksums, and
`rdev.hosted-provider-runtime-contract.v1` evidence requirements, but no
credentials or private endpoints.

```bash
rdev hosted-provider package \
  --out dist/hosted-provider \
  --storage-provider postgres \
  --auth-provider hosted-ed25519-jwt

rdev hosted-provider verify \
  --package dist/hosted-provider
```

The built-in package proves the provider contract for single-node file storage
and provider-neutral EdDSA JWT auth. External packages for Postgres,
S3-compatible storage, Redis streams, OIDC/JWKS, and SAML emit
`runtime-contract.json` and `HOSTED_PROVIDER_RUNTIME.md` so operators and
Agents know which verification, backup, restore, retention, role-mapping,
failure-mode, and audit evidence must be collected. Durable third-party hosted
providers still need real deployed gateway evidence before production claims.

## Start

```bash
rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --audit-log .rdev/audit/events.jsonl \
  --state .rdev/gateway/state.json \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --manifest-signing-key .rdev/keys/manifest-root-key.json
```

For local TLS or mTLS transport preflight, add server certificate material and, when mutual TLS is required, a client CA:

```bash
rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --tls-cert .rdev/tls/gateway-server.pem \
  --tls-key .rdev/tls/gateway-server-key.pem \
  --client-ca .rdev/tls/client-ca.pem
```

`--tls-cert` and `--tls-key` must be provided together. `--client-ca` requires both of them and changes the listener from server-authenticated HTTPS to mTLS by setting the gateway to require and verify client certificates. This does not replace signed job envelopes, enrollment certificates, host-local policy, or approval gates; transport authentication is not job authorization.

Hosts can connect to the local HTTPS or mTLS gateway with an explicit gateway CA and, when the gateway was started with `--client-ca`, a client certificate/key pair:

```bash
rdev host serve \
  --mode temporary \
  --gateway https://127.0.0.1:8787 \
  --gateway-ca .rdev/tls/gateway-ca.pem \
  --gateway-client-cert .rdev/tls/host-client.pem \
  --gateway-client-key .rdev/tls/host-client-key.pem \
  --ticket-code ABCD-1234 \
  --once=false
```

`--gateway-client-cert` and `--gateway-client-key` must be provided together. The host uses the same gateway HTTP client for join-manifest fetches, registration, host approval waits, trust-bundle fetches, trust-store refreshes, job polling, job cancellation checks, completion, failure, and artifact appends. Direct `--gateway --ticket-code` registration remains local-dev only. For LAN/private or public gateway registration, use a signed `--manifest-url` plus pinned `--manifest-root-public-key`; that lets private HTTP gateway URLs and HTTPS gateways work without hand-copying ticket or trust fields.

Use WSS for the production host job channel:

```bash
rdev host serve \
  --mode managed \
  --gateway https://127.0.0.1:8787 \
  --transport wss \
  --gateway-ca .rdev/tls/gateway-ca.pem \
  --gateway-client-cert .rdev/tls/host-client.pem \
  --gateway-client-key .rdev/tls/host-client-key.pem \
  --ticket-code ABCD-1234 \
  --once=false
```

WSS job messages carry signed job envelopes exactly like polling. The transport
does not grant job authority; host-side envelope verification, trust pins,
nonce replay protection, enrollment certificates, approval tokens, workspace
locks, and adapter policy still decide whether work can run.

When `--signing-key` is set, the dev gateway creates or reuses an Ed25519 signing key file with `0600` permissions and prints its public-key fingerprint:

```text
rdev gateway signing key id=gateway-dev fingerprint=sha256:<hex>
rdev gateway manifest root id=manifest-dev public_key=manifest-dev:<base64url_ed25519_public_key>
```

When `--state` is set, the dev gateway writes `rdev.gateway-snapshot.v1` after
successful state mutations and reloads it on restart. The snapshot preserves
tickets, hosts, jobs, artifacts, audit events, and the signed trust bundle. It
requires `--signing-key`; restoring a snapshot with a different signing key is
rejected because old job envelopes and approval tokens must keep the same trust
root.

Hosts can pin that key during local development:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --identity-store .rdev/host/identity.json \
  --nonce-store .rdev/host/nonces.json \
  --trust-store .rdev/host/trust-bundle.json \
  --trust-pin sha256:<hex>
```

For the same host path over local mTLS, switch the gateway URL to `https://127.0.0.1:<port>` and add `--gateway-ca`, `--gateway-client-cert`, and `--gateway-client-key`.

Agents should normally create an invite first. This asks the gateway for a
ticket and returns a universal `connection_entry`, `connection_entry_plan`,
machine-readable fallback commands, and the MCP next actions:

```bash
rdev invite create \
  --gateway http://127.0.0.1:8787 \
  --reason "repair target development environment" \
  --capabilities shell.user,codex.run,git.diff \
  --transport auto
```

`auto` is the recommended field setting. The host first tries outbound WSS, then
HTTPS long-poll, then short polling. That keeps the connection path useful
across NAT, restrictive firewalls, proxies that block WebSocket upgrades, and
networks that interrupt long-held requests. If all outbound HTTPS variants fail,
the Agent should ask about proxy configuration, DNS, captive portal, VPN, TLS
inspection, or use already configured relay/mesh/SSH paths. If the required
route, credential, or endpoint is ambiguous, the Agent should ask a concise
question rather than guessing.

When the Agent server and target host share a LAN, use a gateway URL that is
actually routable from the target, such as `http://192.0.2.10:8787` in
documentation examples or the discovered private address in real use. Do not
use `127.0.0.1` for a different target machine. The invite `connection_plan`
marks LAN, relay, mesh, and SSH paths separately so agents can probe local
interfaces, route tables, SSH config, proxy settings, and mesh tooling before
asking follow-up questions.

The target host command in the invite consumes the signed join manifest, which
carries the ticket code, gateway URL, trust bundle, and trust fingerprint:

```bash
rdev host serve \
  --manifest-url http://127.0.0.1:8787/v1/tickets/<ticket_code>/manifest \
  --manifest-root-public-key manifest-dev:<base64url_ed25519_public_key> \
  --transport auto
```

Agents should treat that command as implementation detail. The human-facing
path is `connection_entry.entry_url` or a signed connection entry package. The
Agent decides whether the entry should become an attended temporary session for
a third-party or one-off repair machine, or a managed session for an
operator-owned workstation that needs durable development access.

## Endpoints

- `GET /healthz`
- `GET /v1/trust`
- `GET /v1/trust-bundle`
- `GET /v1/enrollment/revocations`
- `POST /v1/trust-bundle`
- `POST /v1/tickets`
- `GET /v1/tickets/{ticket_code}/manifest`
- `GET /v1/hosts`
- `POST /v1/hosts/register`
- `POST /v1/hosts/{host_id}/approve`
- `POST /v1/hosts/{host_id}/revoke`
- `GET /v1/hosts/{host_id}/trust-bundle/update?current_sequence=<n>&current_hash=<sha256:...>`
- `GET /v1/ws/hosts/{host_id}`
- `POST /v1/jobs`
- `GET /v1/jobs/{job_id}`
- `GET /v1/jobs/{job_id}/artifacts`
- `GET /v1/jobs/{job_id}/evidence-bundle?out=<directory>`
- `GET /v1/hosts/{host_id}/jobs/next`
- `GET /v1/hosts/{host_id}/jobs/next?wait_seconds=<1-60>`
- `POST /v1/jobs/{job_id}/complete`
- `POST /v1/jobs/{job_id}/fail`
- `GET /v1/artifacts/{artifact_id}`
- `GET /v1/audit`

## Example

```bash
curl -s http://127.0.0.1:8787/healthz

curl -s -X POST http://127.0.0.1:8787/v1/tickets \
  -H 'content-type: application/json' \
  -d '{"mode":"attended-temporary","ttl_seconds":600,"reason":"local dev"}'

curl -s http://127.0.0.1:8787/v1/tickets/<ticket_code>/manifest
curl -s http://127.0.0.1:8787/v1/audit
```

Export and verify an audit hash chain from a JSONL audit log:

```bash
rdev audit export \
  --input .rdev/audit/events.jsonl \
  --out .rdev/audit/audit-chain.json

rdev audit verify \
  --input .rdev/audit/audit-chain.json
```

The exported file uses schema `rdev.audit-chain.v1`. Each entry stores the canonical event hash, previous chain hash, and current chain hash. Verification recomputes every event hash and the final root hash, rejecting tampered events or broken chain links.

Read or update the development signed trust bundle:

```bash
curl -s http://127.0.0.1:8787/v1/trust-bundle
curl -s -X POST http://127.0.0.1:8787/v1/trust-bundle \
  -H 'content-type: application/json' \
  -d '{"trust_bundle":{...}}'
```

Trust bundle updates must use schema `rdev.trust-bundle.v1`, verify against the current active signing key, increase the sequence, and include the current bundle hash as `previous_bundle_hash`.

Managed-host trust refresh can use the host-bound update check:

```bash
curl -s 'http://127.0.0.1:8787/v1/hosts/<host_id>/trust-bundle/update?current_sequence=1&current_hash=sha256:...'
```

The response uses schema `rdev.trust-bundle-update.v1`. If the host is current, the response status is `current` and omits a bundle. If the gateway has a newer bundle, the response status is `update_available` and includes the candidate `rdev.trust-bundle.v1`; the host still verifies sequence, `previous_bundle_hash`, signature, and validity locally before persisting it.

Create and maintain operator-side trust bundles locally:

```bash
rdev trust init \
  --out .rdev/trust/trust-bundle.json \
  --root-key .rdev/keys/trust-root.json \
  --gateway-key .rdev/keys/gateway-prod.json

rdev trust rotate \
  --current .rdev/trust/trust-bundle.json \
  --out .rdev/trust/trust-bundle-next.json \
  --root-key .rdev/keys/trust-root.json \
  --gateway-key .rdev/keys/gateway-next.json \
  --gateway-key-id gateway-next \
  --retire-key gateway-prod

rdev trust revoke \
  --current .rdev/trust/trust-bundle-next.json \
  --out .rdev/trust/trust-bundle-revoked.json \
  --root-key .rdev/keys/trust-root.json \
  --key-id gateway-next \
  --reason "key compromise drill"

rdev trust verify \
  --bundle .rdev/trust/trust-bundle-revoked.json \
  --root-public-key trust-root:...
```

The trust CLI writes local `rdev.trust-bundle.v1` files only. It does not push
updates to a gateway. Use the dev `POST /v1/trust-bundle` endpoint or future
production trust distribution after the bundle has been reviewed and verified.

## Enrollment Lifecycle

Production enrollment authority operations produce reviewable evidence:

```bash
rdev enrollment lifecycle key-custody \
  --root-public-key enrollment-root:... \
  --custodian platform-security \
  --provider kms \
  --rotation-days 90 \
  --out .rdev/enrollment/key-custody.json

rdev enrollment lifecycle fleet-renewal-plan \
  --certificates .rdev/enrollment/fleet-certificates.json \
  --revocations .rdev/enrollment/revocations.json \
  --root-public-key enrollment-root:... \
  --renew-before 24h \
  --renew-valid-for 24h \
  --out .rdev/enrollment/fleet-renewal-plan.json

rdev enrollment lifecycle emergency-drill \
  --name enrollment-root-compromise-drill \
  --scenario enrollment-root-compromise \
  --operator-role admin \
  --root-public-key enrollment-root:... \
  --revocations .rdev/enrollment/revocations.json \
  --out .rdev/enrollment/emergency-drill.json
```

The schemas are `rdev.enrollment-key-custody.v1`,
`rdev.enrollment-fleet-renewal-plan.v1`, and
`rdev.enrollment-emergency-drill.v1`. Drill evidence stores a hashed reference
for local revocation paths so generated public artifacts do not expose private
machine paths.

Register a foreground temporary host:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --identity-store .rdev/host/identity.json
```

Create and process one dev job:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{"host_id":"hst_...","adapter":"shell","intent":"local demo","policy":{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"],"max_duration_seconds":30,"max_output_bytes":65536}}'

rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --transport long-poll \
  --max-jobs=1 \
  --approval-timeout=30s
```

Preflight the same shell job policy before creating it:

```bash
rdev policy explain-shell \
  --policy-json '{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"],"max_duration_seconds":30,"max_output_bytes":65536}'
```

Agents can call the same policy engine through MCP tool `rdev.policy.explain_shell`.

`rdev host serve` generates an Ed25519 host identity for the session. When `--identity-store <path>` is set, the identity is persisted to a local `0600` JSON file using schema `rdev.host-identity.v1`; the parent directory is created with `0700` permissions. On macOS, `--identity-store keychain:<service>/<account>` stores the same `rdev.host-identity.v1` payload in the user's Keychain instead of a JSON file. On Windows, `--identity-store dpapi:<service>/<account>` stores the same payload through a CurrentUser DPAPI-protected local envelope. On Linux, `--identity-store libsecret:<service>/<account>` stores the same payload through `secret-tool` and the user's Secret Service when available; `--identity-store keyctl:<service>/<account>` stores the same payload in the user's Linux keyring for headless hosts where Secret Service is unavailable. The host registration includes the identity key id, public key, fingerprint, and signed `rdev.host-registration-proof.v1` proof. The gateway verifies that proof before preserving the identity fingerprint. Signed job envelopes include the registered host identity fingerprint, and a host with a local identity rejects jobs bound to a different fingerprint.

For a stricter development enrollment path, issue a signed host enrollment
certificate and configure the dev gateway to require it:

```bash
rdev enrollment sign-certificate \
  --out .rdev/enrollment/host-enrollment.json \
  --key .rdev/keys/enrollment-root.json \
  --key-id enrollment-root \
  --ticket-code ABCD-1234 \
  --mode managed \
  --name managed-mac \
  --os darwin \
  --arch arm64 \
  --identity-key-id host \
  --identity-public-key <base64url_ed25519_public_key> \
  --identity-fingerprint sha256:<hex> \
  --capabilities codex.run,git.diff \
  --valid-minutes 60

rdev enrollment verify-certificate \
  --certificate .rdev/enrollment/host-enrollment.json \
  --root-public-key enrollment-root:<base64url_ed25519_public_key>

rdev enrollment init-revocations \
  --out .rdev/enrollment/revocations.json \
  --key .rdev/keys/enrollment-root.json

rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --enrollment-key .rdev/keys/enrollment-root.json \
  --operator-auth .rdev/operator-auth/operators.json \
  --enrollment-revocations .rdev/enrollment/revocations.json

rdev enrollment issue-certificate \
  --gateway http://127.0.0.1:8787 \
  --out .rdev/enrollment/host-enrollment-issued.json \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --ticket-code ABCD-1234 \
  --name managed-mac \
  --os darwin \
  --arch arm64 \
  --identity-key-id host \
  --identity-public-key <base64url_ed25519_public_key> \
  --identity-fingerprint sha256:<hex> \
  --capabilities codex.run,git.diff \
  --operator-token-file .rdev/operator-auth/tokens/issuer.token

rdev enrollment verify-certificate \
  --certificate .rdev/enrollment/host-enrollment-issued.json \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --revocations .rdev/enrollment/revocations.json

rdev enrollment renew-certificate \
  --certificate .rdev/enrollment/host-enrollment-issued.json \
  --out .rdev/enrollment/host-enrollment-renewed.json \
  --gateway http://127.0.0.1:8787 \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --operator-token-file .rdev/operator-auth/tokens/issuer.token \
  --valid-minutes 120

rdev enrollment renew-certificate \
  --certificate .rdev/enrollment/host-enrollment-issued.json \
  --out .rdev/enrollment/host-enrollment-local-renewed.json \
  --key .rdev/keys/enrollment-root.json \
  --revocations .rdev/enrollment/revocations.json \
  --valid-minutes 120

rdev enrollment verify-certificate \
  --certificate .rdev/enrollment/host-enrollment-renewed.json \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --revocations .rdev/enrollment/revocations.json

rdev enrollment fetch-revocations \
  --gateway http://127.0.0.1:8787 \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --operator-token-file .rdev/operator-auth/tokens/issuer.token \
  --out .rdev/enrollment/fetched-revocations.json \
  --force

rdev host serve \
  --mode managed \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --identity-store .rdev/host/identity.json \
  --enrollment-certificate .rdev/enrollment/host-enrollment-renewed.json \
  --renew-enrollment-certificate \
  --operator-token-file .rdev/operator-auth/tokens/issuer.token \
  --enrollment-renew-before 24h \
  --enrollment-renew-valid-minutes 120 \
  --fetch-enrollment-revocations \
  --enrollment-root-public-key enrollment-root:<base64url_ed25519_public_key>
```

The certificate uses schema `rdev.host-enrollment-certificate.v1` and binds the
ticket code, host mode, host name, OS/arch, capabilities, validity window, and
host identity fingerprint to the enrollment root signature. Operators can sign
it locally with `sign-certificate` or, when the dev gateway is started with
`--enrollment-key`, request it from `POST /v1/enrollment/certificates` through
`issue-certificate`; the CLI verifies the returned certificate against the
pinned `--root-public-key` before writing it. Hosted issuance cannot grant
capabilities outside the ticket capability set. When `--operator-auth` is
enabled, hosted issuance, hosted renewal, and hosted revocation fetch require an
operator auth token with the `issuer` role, passed through
`--operator-token-file`. The CLI reads the bearer token from disk, sends it as
`Authorization: Bearer ...`, and does not include it in JSON output. Local
`renew-certificate --key ...` first verifies the current
certificate, optionally checks signed revocations, then emits a refreshed
certificate with the same authorized scope and a new validity window. Hosted
`renew-certificate --gateway ... --root-public-key ...` calls
`POST /v1/enrollment/certificates/renew`, verifies the returned enrollment root,
previous certificate fingerprint, renewed certificate signature, and renewed
fingerprint before writing the certificate. Signed registration proof still
proves private-key possession; the enrollment certificate proves the operator
authorized this host to join under the stated scope.
Hosts can also set `--renew-enrollment-certificate --enrollment-root-public-key`
with their `--enrollment-certificate`; before registration, the host verifies
the current certificate against the pinned root, renews only when the certificate
expires within `--enrollment-renew-before`, verifies the hosted renewal response,
and writes the replacement back to the same certificate path. Use
`--operator-token-file` when the gateway protects hosted renewal with
the operator auth token.

The revocation list uses schema `rdev.host-enrollment-revocations.v1` and binds
revoked enrollment certificate fingerprints to the same enrollment root. When
`--enrollment-revocations` is configured, the dev gateway verifies the list
signature and freshness before registration and rejects revoked certificates.
Hosts can also set `--fetch-enrollment-revocations --enrollment-root-public-key`
with their `--enrollment-certificate`; before registration, the host fetches
`GET /v1/enrollment/revocations`, optionally authenticates with
`--operator-token-file`, verifies the signed list and local certificate
against the pinned enrollment root, then refuses a locally revoked certificate
without sending the registration payload.
Use `rdev enrollment init-revocations` to publish a signed empty baseline before
any certificate has been revoked, pass that baseline to `renew-certificate`
before extending a certificate, then append retired or compromised certificates
with `revoke-certificate --current`. This is a local signed revocation-list,
operator-auth-protected dev hosted issuance, renewal, and revocation
distribution primitive, host-side near-expiry renewal primitive, plus a dev
distribution path; it is not the full production enrollment authority lifecycle
with fleet renewal policy, emergency drills, hardware-backed custody, or
production hosted storage.

To smoke the revocation path, revoke a certificate and then expect
`verify-certificate --revocations` or gateway registration with that same
certificate to fail:

```bash
rdev enrollment init-revocations \
  --out .rdev/enrollment/revocations.json \
  --key .rdev/keys/enrollment-root.json

rdev enrollment revoke-certificate \
  --out .rdev/enrollment/revocations.json \
  --current .rdev/enrollment/revocations.json \
  --key .rdev/keys/enrollment-root.json \
  --certificate .rdev/enrollment/host-enrollment.json \
  --reason "host retired" \
  --force

rdev enrollment verify-revocations \
  --revocations .rdev/enrollment/revocations.json \
  --root-public-key enrollment-root:<base64url_ed25519_public_key>

curl -s http://127.0.0.1:8787/v1/enrollment/revocations

rdev enrollment fetch-revocations \
  --gateway http://127.0.0.1:8787 \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --operator-token-file .rdev/operator-auth/tokens/issuer.token \
  --out .rdev/enrollment/fetched-revocations.json

rdev enrollment verify-certificate \
  --certificate .rdev/enrollment/host-enrollment.json \
  --root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --revocations .rdev/enrollment/revocations.json

rdev gateway serve \
  --dev \
  --addr 127.0.0.1:8787 \
  --signing-key .rdev/keys/gateway-signing-key.json \
  --enrollment-root-public-key enrollment-root:<base64url_ed25519_public_key> \
  --enrollment-revocations .rdev/enrollment/revocations.json
```

`rdev host serve` keeps a nonce replay cache for signed job envelopes. By default, this cache is in-memory for the process. When `--nonce-store <path>` is set, used nonces are persisted to a local `0600` JSON file using schema `rdev.host-nonce-store.v1`; the parent directory is created with `0700` permissions. Expired nonce entries are pruned before new entries are written.

When `--once=false`, `rdev host serve` fetches the signed gateway trust bundle from `GET /v1/trust-bundle`, waits until the registered host is approved, refreshes the local trust store through `GET /v1/hosts/{host_id}/trust-bundle/update` when `--trust-store` is configured, polls `GET /v1/hosts/{host_id}/jobs/next`, verifies the signed job envelope against the active key in the trust bundle, the local host identity fingerprint, and the nonce replay cache, runs the development host runner, and reports completion to `POST /v1/jobs/{job_id}/complete`. For older dev gateways, the host falls back to legacy `GET /v1/trust`.

For a more realistic outbound host channel during local development, use HTTPS long-poll:

```bash
rdev host serve \
  --mode temporary \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --transport long-poll \
  --long-poll-timeout=25s
```

Long-poll uses the same safety path as short polling but holds the host's outbound `GET /v1/hosts/{host_id}/jobs/next?wait_seconds=<n>` request open until a job is available or the wait timeout elapses. This is a development bridge toward WSS/mTLS transport; it is not a production transport by itself.

When `--trust-store <path>` is set, the host persists verified signed trust bundles to a local `0600` JSON file. On macOS, `--trust-store keychain:<service>/<account>` stores the same `rdev.host-trust-store.v1` payload in Keychain. On Windows, `--trust-store dpapi:<service>/<account>` stores the same payload through a CurrentUser DPAPI-protected local envelope. On Linux, `--trust-store libsecret:<service>/<account>` stores the same payload through `secret-tool` and the user's Secret Service when available; `--trust-store keyctl:<service>/<account>` stores the same payload in the user's Linux keyring for headless hosts where Secret Service is unavailable. Future updates must verify against the stored bundle's active signing key, increase sequence, and match `previous_bundle_hash`. If the gateway trust-bundle endpoint is unavailable, the host can use the stored bundle for job verification.

For a managed Mac, a protected local identity/trust pair can be configured without changing the rest of the host command:

```bash
rdev host serve \
  --mode managed \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --identity-store keychain:remote-dev-skillkit/managed-mac-identity \
  --trust-store keychain:remote-dev-skillkit/managed-mac-trust \
  --once=false \
  --transport long-poll
```

For a managed Windows host, use the same command shape with Windows DPAPI refs:

```powershell
rdev host serve `
  --mode managed `
  --gateway https://api.example.com/v1 `
  --ticket-code ABCD-1234 `
  --identity-store dpapi:remote-dev-skillkit/managed-windows-identity `
  --trust-store dpapi:remote-dev-skillkit/managed-windows-trust `
  --once=false `
  --transport long-poll
```

For a managed Linux host with a reachable Secret Service, use libsecret refs:

```bash
rdev host serve \
  --mode managed \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --identity-store libsecret:remote-dev-skillkit/managed-linux-identity \
  --trust-store libsecret:remote-dev-skillkit/managed-linux-trust \
  --once=false \
  --transport long-poll
```

For a headless managed Linux host with a user keyring, use keyctl refs:

```bash
rdev host serve \
  --mode managed \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --identity-store keyctl:remote-dev-skillkit/managed-linux-identity \
  --trust-store keyctl:remote-dev-skillkit/managed-linux-trust \
  --once=false \
  --transport long-poll
```

`keychain:` references are macOS-only. `dpapi:` references are Windows-only and
use the current Windows user context. `libsecret:` references are Linux-only and
require `secret-tool` plus a reachable Secret Service. `keyctl:` references are
Linux-only and require the `keyctl` command plus an available user keyring.
File-backed stores remain available for development and explicitly documented
degraded paths.

If the host runner rejects a job, the host reports the failure to `POST /v1/jobs/{job_id}/fail`. The gateway marks the job `failed`, stores `failure_reason`, and writes a `job.fail` audit event.

The development shell adapter executes `policy.argv` directly without shell interpolation. The first argv item must match `policy.allow_commands`, the workspace root must exist, write scopes must remain inside the workspace, and output is capped by the signed envelope limit. Before execution, the host also runs the shared implicit approval preflight. Shell jobs that request package installation, elevation, GUI control, service management, push, merge, deploy, publish, or credential changes return `rdev.approval-required.v1` unless a matching signed approval token is present. Completion and failure artifacts use schema `rdev.shell-result.v1` and include argv, canonical workspace, exit code, redacted stdout/stderr excerpts, timeout state, cancellation state, truncation state, duration, redaction rules, and redaction counts.

When the gateway job status becomes `canceled`, `rdev host serve` cancels the local job context. Built-in shell, PowerShell, Codex, Claude Code, and acpx adapters receive that context and stop the running child process cooperatively. The host may append cancellation evidence through `POST /v1/jobs/{job_id}/artifact` while preserving the gateway job's `canceled` terminal state.

The development PowerShell adapter uses the same host safety path. Create an `adapter=powershell` job with `powershell.user` capability:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "host_id": "hst_...",
    "adapter": "powershell",
    "intent": "diagnose Windows user environment",
    "policy": {
      "workspace_root": ".",
      "capabilities": ["powershell.user"],
      "command": "Get-ChildItem Env:",
      "allow_commands": ["pwsh", "powershell", "powershell.exe"],
      "max_duration_seconds": 120,
      "max_output_bytes": 65536
    }
  }'
```

The adapter runs the selected executable as `-NoProfile -NonInteractive -Command <command>`. It does not add `-ExecutionPolicy Bypass` or install persistence. For deterministic tests or custom hosts, signed payloads may override `powershell_command`; if that value contains a path separator, `allow_commands` must contain the exact same path. Result artifacts use schema `rdev.powershell-result.v1` and include redacted command, argv, stdout/stderr, timeout state, cancellation state, truncation state, duration, redaction rules, and redaction counts.

PowerShell jobs also run the shared implicit approval preflight before workspace lock acquisition and adapter execution. Commands that request service management, package installation, elevation, GUI control, push, merge, deploy, publish, credential changes, or execution-policy changes return `rdev.approval-required.v1` unless a matching approval token is present.

The development Codex adapter uses the same host safety path. Create an `adapter=codex` job with `codex.run` and `git.diff` capabilities:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "host_id": "hst_...",
    "adapter": "codex",
    "intent": "update README",
    "policy": {
      "workspace_root": ".",
      "capabilities": ["codex.run", "git.diff"],
      "prompt": "Update README and keep the change scoped to this repository.",
      "verification_commands": [["git", "status", "--short"]],
      "allow_verification_commands": ["git"],
      "max_duration_seconds": 1800,
      "max_output_bytes": 1048576
    }
  }'
```

By default the adapter runs:

```bash
codex exec -C <workspace_root> --sandbox workspace-write --json <prompt>
```

For deterministic local tests, signed job payloads may override `codex_command` and `codex_args`. The result artifact uses schema `rdev.codex-result.v1` and includes Codex command output, Git status, Git diff/stat, optional verification command results, output truncation flags, duration, redaction rules, and redaction counts. Verification commands must be allowlisted through `allow_verification_commands`. When a verification command is `go test -json ...`, the command result also includes a `rdev.test-report.v1` summary with package/test pass, fail, skip counts and parsed test cases.

Codex jobs also run the same implicit approval preflight before workspace lock acquisition and before adapter execution. If `intent`, `prompt`, `codex_args`, `verification_commands`, `external_actions`, `dangerous_actions`, `approval_actions`, or `requested_approvals` request high-risk external consequences, the host returns `rdev.approval-required.v1` unless a matching approval token is present. Current operations include `git.push`, `git.merge`, `deploy.run`, `publish.run`, `credential.change`, `service.manage`, `package.install`, `elevation.request`, and `gui.control`.

The development Claude Code adapter uses the same host safety path. Create an
`adapter=claude-code` job with `claude-code.run` and `git.diff` capabilities:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "host_id": "hst_...",
    "adapter": "claude-code",
    "intent": "update README",
    "policy": {
      "workspace_root": ".",
      "capabilities": ["claude-code.run", "git.diff"],
      "prompt": "Update README and keep the change scoped to this repository.",
      "verification_commands": [["git", "status", "--short"]],
      "allow_verification_commands": ["git"],
      "max_duration_seconds": 1800,
      "max_output_bytes": 1048576
    }
  }'
```

By default the adapter runs:

```bash
claude -p <prompt>
```

For deterministic local tests or custom hosts, signed job payloads may override
`claude_code_command` and `claude_code_args`. The result artifact uses schema
`rdev.claude-code-result.v1` and includes Claude Code command output, Git
status, Git diff/stat, optional verification command results, output
truncation flags, duration, redaction rules, and redaction counts. Verification
commands must be allowlisted through `allow_verification_commands`. When a
verification command is `go test -json ...`, the command result also includes a
`rdev.test-report.v1` summary with package/test pass, fail, skip counts and
parsed test cases.

Claude Code jobs run the same implicit approval preflight as Codex before
workspace lock acquisition and before adapter execution. If `intent`, `prompt`,
`claude_code_args`, `verification_commands`, `external_actions`,
`dangerous_actions`, `approval_actions`, or `requested_approvals` request
high-risk external consequences, the host returns `rdev.approval-required.v1`
unless a matching approval token is present.

The development ACP/acpx adapter uses the same host safety path. acpx is a
headless Agent Client Protocol CLI and its upstream CLI/runtime are currently
alpha, so the adapter keeps signed payload overrides as the compatibility
valve. Create an `adapter=acpx` job with `acpx.run` and `git.diff`
capabilities:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/jobs \
  -H 'content-type: application/json' \
  -d '{
    "host_id": "hst_...",
    "adapter": "acpx",
    "intent": "update README through ACP",
    "policy": {
      "workspace_root": ".",
      "capabilities": ["acpx.run", "git.diff"],
      "prompt": "Update README and keep the change scoped to this repository.",
      "acpx_agent": "codex",
      "verification_commands": [["git", "status", "--short"]],
      "allow_verification_commands": ["git"],
      "max_duration_seconds": 1800,
      "max_output_bytes": 1048576
    }
  }'
```

By default the adapter runs:

```bash
acpx --cwd <workspace_root> codex exec <prompt>
```

For deterministic local tests, custom ACP agents, or upstream acpx CLI changes,
signed job payloads may override `acpx_command`, `acpx_agent`, and `acpx_args`.
The result artifact uses schema `rdev.acpx-result.v1` and includes acpx command
output, Git status, Git diff/stat, optional verification command results,
output truncation flags, duration, redaction rules, and redaction counts.
Verification commands must be allowlisted through `allow_verification_commands`.
When a verification command is `go test -json ...`, the command result also
includes a `rdev.test-report.v1` summary with package/test pass, fail, skip
counts and parsed test cases.

acpx jobs run the same implicit approval preflight as Codex and Claude Code
before workspace lock acquisition and before adapter execution. If `intent`,
`prompt`, `acpx_agent`, `acpx_args`, `verification_commands`,
`external_actions`, `dangerous_actions`, `approval_actions`, or
`requested_approvals` request high-risk external consequences, the host returns
`rdev.approval-required.v1` unless a matching approval token is present.

## Workspace Locks And Git Worktrees

Managed coding jobs should not mutate the primary checkout directly. The development CLI now includes the workspace foundation that Codex, Claude Code, acpx, future ACP, and Git adapters share.

Acquire a one-writer lock for a repository:

```bash
rdev workspace lock \
  --repo . \
  --host-id hst_... \
  --job-id job_... \
  --adapter codex
```

The lock file uses schema `rdev.workspace-lock.v1`, is stored under `<repo>/.rdev/workspace-locks` by default, and is written with `0600` permissions. A second writer for the same canonical repo root is rejected until the lock expires or is released by the owning job.

Inspect or release the lock:

```bash
rdev workspace status --repo .
rdev workspace unlock --repo . --job-id job_...
```

Prepare a Git worktree for a coding adapter:

```bash
rdev workspace prepare-worktree \
  --repo . \
  --host-id hst_... \
  --job-id job_... \
  --adapter codex \
  --base-ref HEAD
```

The response uses schema `rdev.git-worktree-plan.v1`, records the lock, branch, worktree path, and command evidence for `git rev-parse` and `git worktree add`. If `git worktree add` fails, the CLI releases the lock so another job is not blocked by a failed preparation step.

To enforce the same lock during host job execution, start the host with a lock store:

```bash
rdev host serve \
  --mode managed \
  --gateway http://127.0.0.1:8787 \
  --ticket-code ABCD-1234 \
  --once=false \
  --workspace-lock-store .rdev/workspace-locks
```

When `--workspace-lock-store` is set, the hostrunner acquires `rdev.workspace-lock.v1` after signature, nonce, identity, capability, and approval checks, and before adapter execution. The lock is released after success or adapter denial. If another active job holds the canonical repo root, the job fails with structured denial code `workspace_locked`.

The host redacts common secret patterns before artifact upload:

- `sk-...` API keys
- GitHub `ghp_...` and `github_pat_...` tokens
- `Authorization: Bearer ...`
- `password=...`, `token=...`, `api_key=...`, and JSON equivalents
- AWS access key IDs
- PEM private key blocks

Read execution evidence:

```bash
curl -s http://127.0.0.1:8787/v1/jobs/<job_id>/artifacts
curl -s http://127.0.0.1:8787/v1/artifacts/<artifact_id>
```

Export a complete job evidence bundle from gateway state:

```bash
rdev evidence export \
  --gateway http://127.0.0.1:8787 \
  --job-id <job_id> \
  --out .rdev/evidence/<job_id>
```

The CLI fetches the job, artifacts, and audit events from the gateway API and writes the same `rdev.evidence-bundle.v1` directory shape as file-based export: `manifest.json`, `checksums.txt`, `job.json`, `envelope.json`, `policy-decision.json`, `artifacts.json`, artifact files, `audit-slice.jsonl`, and `audit-chain.json`.

The dev gateway also exposes server-side export for local diagnostics:

```bash
curl -s "http://127.0.0.1:8787/v1/jobs/<job_id>/evidence-bundle?out=$(pwd)/.rdev/evidence/<job_id>"
```

Revoke a host and cancel its pending/running jobs:

```bash
curl -s -X POST http://127.0.0.1:8787/v1/hosts/<host_id>/revoke \
  -H 'content-type: application/json' \
  -d '{"reason":"support session complete"}'
```

## Managed Service Files

Managed service commands write inspectable service files first. They do not
start a background service unless `rdev host service-control --execute` is used.

macOS uses a LaunchAgent plist:

```bash
rdev host install-service \
  --platform macos \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --workspace-lock-store ~/.rdev/host/workspace-locks \
  --plist-out ./com.remote-dev-skillkit.host.plist

rdev host service-status \
  --platform macos \
  --plist ./com.remote-dev-skillkit.host.plist

rdev host service-control \
  --platform macos \
  --action start \
  --plist ./com.remote-dev-skillkit.host.plist

rdev host uninstall-service \
  --platform macos \
  --plist ./com.remote-dev-skillkit.host.plist
```

Linux uses a systemd user unit:

```bash
rdev host install-service \
  --platform linux \
  --label rdev-host.service \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --workspace-lock-store ~/.rdev/host/workspace-locks \
  --unit-out ./rdev-host.service

rdev host service-status \
  --platform linux \
  --label rdev-host.service \
  --unit ./rdev-host.service

rdev host service-control \
  --platform linux \
  --action start \
  --label rdev-host.service \
  --unit ./rdev-host.service

rdev host uninstall-service \
  --platform linux \
  --label rdev-host.service \
  --unit ./rdev-host.service
```

For macOS and Linux, `service-control` is a dry-run by default. It prints the
planned `launchctl` or `systemctl --user` commands and requires `--execute`
before invoking the OS service manager.

For release-evidence planning, generate and verify a Linux managed-service
acceptance plan before running the reviewed commands on a real Linux host:

```bash
rdev acceptance linux-managed-service \
  --out .rdev/acceptance/linux-managed-service \
  --binary /opt/rdev/rdev \
  --gateway https://api.example.com/v1 \
  --ticket-code ABCD-1234 \
  --release-bundle /opt/rdev/release-bundle.json \
  --release-root-public-key release-root:... \
  --release-require-artifacts rdev,rdev-host,rdev-verify

rdev acceptance verify-linux-managed-service \
  --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json
```

These acceptance commands write a reviewed systemd user unit and plan only.
They do not execute `systemctl`, enable the unit, start it, stop it, or uninstall
it. Real Linux managed support still requires a Linux host transcript proving
start, status, release-gated registration, reconnect after logout or reboot,
managed job evidence, stop, and uninstall.

After the real host run, package the collected evidence:

```bash
rdev acceptance package-linux-managed-service \
  --plan .rdev/acceptance/linux-managed-service/linux-managed-service-plan.json \
  --out .rdev/acceptance/linux-managed-service-evidence \
  --start-transcript start.txt \
  --status-transcript status.txt \
  --logs journal.txt \
  --release-gate release-gate.json \
  --audit audit.jsonl \
  --reconnect reconnect.txt \
  --job-evidence-dir job-evidence \
  --stop-transcript stop.txt \
  --uninstall-transcript uninstall.txt
```

The package command redacts copied evidence, writes `package.json` and
`checksums.txt`, requires release-gate output with `ok=true`, and requires
job evidence containing an approval-required artifact. It still does not run
`systemctl`.

Windows uses reviewed `sc.exe` command plans:

```powershell
rdev host install-service `
  --platform windows `
  --label RemoteDevSkillkitHost `
  --binary 'C:\Program Files\rdev\rdev.exe' `
  --gateway https://api.example.com/v1 `
  --ticket-code ABCD-1234 `
  --workspace-lock-store 'C:\ProgramData\rdev\workspace-locks' `
  --release-bundle 'C:\Program Files\rdev\release-bundle.json' `
  --release-root-public-key release-root:... `
  --release-require-artifacts rdev-host.exe,rdev-verify.exe

rdev host service-status `
  --platform windows `
  --label RemoteDevSkillkitHost

rdev host service-control `
  --platform windows `
  --action start `
  --label RemoteDevSkillkitHost

rdev host uninstall-service `
  --platform windows `
  --label RemoteDevSkillkitHost
```

`install-service --platform windows` does not create the service itself. It
prints machine-readable `sc.exe create` and `sc.exe description` command plans
so the operator can review and run them from an elevated PowerShell session.
`service-status`, `service-control`, and `uninstall-service` are dry-run by
default; `service-control --execute` is required before `sc.exe start`, `stop`,
or `query/qc` is invoked. This is a managed-host planning/control surface, not
real Windows Service acceptance evidence.

For release-evidence planning, generate and verify a Windows managed-service
acceptance plan before running the reviewed commands on a real Windows host:

```powershell
rdev acceptance windows-managed-service `
  --out .rdev\acceptance\windows-managed-service `
  --binary 'C:\Program Files\rdev\rdev.exe' `
  --gateway https://api.example.com/v1 `
  --ticket-code ABCD-1234 `
  --release-bundle 'C:\Program Files\rdev\release-bundle.json' `
  --release-root-public-key release-root:... `
  --release-require-artifacts rdev.exe,rdev-host.exe,rdev-verify.exe

rdev acceptance verify-windows-managed-service `
  --plan .rdev\acceptance\windows-managed-service\windows-managed-service-plan.json
```

These acceptance commands also do not execute PowerShell or `sc.exe`; they only
write and verify the plan and required evidence checklist.

## Release Artifact Verification

Development release artifacts can be signed and verified before bootstrap:

```bash
rdev release sign \
  --artifact ./rdev-host.exe \
  --key .rdev/keys/release-root.json \
  --out ./rdev-host.exe.rdev-release.json

rdev release verify \
  --artifact ./rdev-host.exe \
  --manifest ./rdev-host.exe.rdev-release.json \
  --root-public-key release-root:<base64url_ed25519_public_key>

rdev release create-bundle \
  --dir ./dist \
  --artifacts rdev-host.exe,rdev-verify.exe \
  --require-artifacts rdev-host.exe,rdev-verify.exe \
  --key .rdev/keys/release-root.json

rdev release verify-bundle \
  --bundle ./dist/release-bundle.json \
  --root-public-key release-root:<base64url_ed25519_public_key>

rdev-verify \
  --bundle ./dist/release-bundle.json \
  --root-public-key release-root:<base64url_ed25519_public_key> \
  --require-artifacts rdev-host.exe,rdev-verify.exe
```

The release manifest is a signed `rdev.release-artifact.v1` JSON document.
The release bundle is a signed `rdev.release-bundle.v1` index that verifies the
bundle signature, every listed artifact manifest, artifact and manifest
SHA-256/size, and required artifact presence before publishing. The standalone
verifier supports the same bundle check for target-host or bootstrap contexts
after the verifier binary itself has been hash-pinned.

For Windows bootstrap, publish a tiny verifier binary alongside the host binary:

```powershell
.\windows-temporary.ps1 `
  -GatewayUrl https://agent.example.com `
  -TicketCode ABCD-1234 `
  -DownloadUrl https://example/rdev-host.exe `
  -ExpectedSha256 <host_sha256> `
  -ReleaseBundleUrl https://example/release-bundle.json `
  -ReleaseRootPublicKey release-root:<base64url_ed25519_public_key> `
  -VerifierDownloadUrl https://example/rdev-verify.exe `
  -VerifierExpectedSha256 <verifier_sha256>
```

The script hash-pins `rdev-verify.exe` before using it to verify the signed release bundle. Single-artifact `-ReleaseManifestUrl` remains supported for compatibility.

## Limitations

- In-memory state by default. `--state` / `--storage-provider file` adds a restart-safe JSON snapshot. `--storage-provider postgres` adds a built-in `psql`/libpq JSONB snapshot store, but production claims still require real backup, restore, retention, role-mapping, failure-mode, audit, and redaction evidence.
- WSS/mTLS host job transport is implemented for local release validation through `--transport wss`, `--tls-cert`, `--tls-key`, `--client-ca`, `--gateway-ca`, `--gateway-client-cert`, and `--gateway-client-key`. Real NAT, proxy, and platform-service acceptance still require target-environment evidence.
- Hosted operator auth is provider-neutral EdDSA JWT verification. Hosted provider packages can describe OIDC/JWKS and SAML runtime evidence requirements, but this is not a bundled SSO admin console, organization membership sync, audit-retention service, or hosted multi-tenant control plane.
- TLS lifecycle automation such as ACME issuance, certificate rotation, and public gateway deployment remains operator-managed. The gateway and host load explicit PEM material and enforce client certificates when `--client-ca` is configured.
- Without `--signing-key`, signed job envelopes use an in-memory development Ed25519 key.
- With `--signing-key`, the dev gateway persists one Ed25519 key file and host `--trust-pin` can reject unexpected gateway public keys.
- If `--manifest-signing-key` is omitted, the dev join manifest is signed by the same gateway key it advertises.
- If `--manifest-signing-key` is provided, the dev join manifest is signed by a separate root key; hosts should pass `--manifest-root-public-key <key_id>:<base64url_ed25519_public_key>` before trusting the embedded gateway job-signing bundle. Production still needs release-key lifecycle policy, revocation, managed trust bundle updates, and platform-native Windows code signing policy.
- `GET /v1/trust-bundle` and `POST /v1/trust-bundle` are development endpoints for exercising signed trust bundle rotation. They are not authenticated in dev mode. They are durable across process restarts only when `--state` is enabled with the matching persistent `--signing-key`.
- `--trust-store` supports local `0600` JSON files, macOS `keychain:<service>/<account>` references, Windows `dpapi:<service>/<account>` references, Linux `libsecret:<service>/<account>` references when `secret-tool` and Secret Service are available, and Linux `keyctl:<service>/<account>` references when `keyctl` and a user keyring are available.
- `--identity-store` supports local `0600` JSON files, macOS `keychain:<service>/<account>` references, Windows `dpapi:<service>/<account>` references, Linux `libsecret:<service>/<account>` references when `secret-tool` and Secret Service are available, and Linux `keyctl:<service>/<account>` references when `keyctl` and a user keyring are available. Registrations with identity keys must include `rdev.host-registration-proof.v1`; when `--enrollment-root-public-key` is configured, registrations must also include `rdev.host-enrollment-certificate.v1`; when `--enrollment-revocations` is configured, the gateway accepts signed empty revocation baselines, rejects certificates listed in non-empty signed `rdev.host-enrollment-revocations.v1` files, rejects hosted renewal for revoked current certificates, and can require the operator auth token before serving revocation refreshes. Enrollment authority lifecycle evidence is available through `rdev enrollment lifecycle ...`; real organization-specific custody approvals and emergency runbooks remain operator-owned.
- `--nonce-store` is a file-backed development nonce replay cache. Production managed hosts should use durable local storage with pruning and crash-safe writes.
- Linux systemd support currently writes and controls user units, `rdev acceptance linux-managed-service` can verify a release-evidence plan, and `rdev acceptance package-linux-managed-service` can archive real run evidence. Real managed Linux acceptance still needs reboot/reconnect evidence from a Linux host.
- The dev shell adapter is intentionally narrow: allowlisted argv only, no shell interpolation, host-side artifact redaction for common secret patterns, and no OS-specific sandboxing beyond workspace boundary checks.
