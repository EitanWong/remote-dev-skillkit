# Enrollment Lifecycle

Read this only for enrollment certificates, hosted renewal, revocation refresh,
operator auth, key custody, fleet renewal, or emergency drills.

## Certificate Checks

- Before trusting a managed or temporary host registration that includes an
  enrollment certificate, verify it with MCP tool
  `rdev.enrollment.verify_certificate` or CLI
  `rdev enrollment verify-certificate`.
- Certificate and revocation verification are read-only and never grant host
  access by themselves.
- When requesting or renewing certificates from a configured gateway, use
  pinned-root verification before local write and include operator auth when
  the gateway requires it.

## Renewal and Revocation

- Initialize signed empty revocation baselines before relying on revocation
  refresh flows.
- Renew expiring local certificates with signed revocation checks when
  revocations are available.
- Fetch gateway-published signed revocations with pinned-root verification and
  include returned revocation artifacts or JSON in host startup when required.
- Hosts that register with enrollment certificates should refresh near-expiry
  certificates and revocations before registration, then refuse revoked
  certificates locally.

## Authority Evidence

- For enrollment authority operations, produce machine-readable evidence for:
  key custody, fleet renewal planning, and emergency drills.
- Do not store private keys, private hostnames, local machine paths, private
  ticket codes, or operator tokens in public evidence.
- Real organization-specific custody authorizations and emergency procedures remain
  operator-owned; the project records evidence structure and drills, not the
  operator's secrets.
