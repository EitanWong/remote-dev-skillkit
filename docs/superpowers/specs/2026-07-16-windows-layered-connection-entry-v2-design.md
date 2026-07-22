# Windows Layered Connection Entry V2 Design

## Status

Approved requirements, recorded on 2026-07-16. This design supersedes the
entry-selection and bootstrap-size portions of the 2026-07-15 design while
retaining its signed layered manifest, verified cache, and explicit archive
recovery model.

## Goals

- Prefer a visible PowerShell layered path on Windows 10/11 amd64.
- Keep `Start-ConnectionEntry.cmd` as a visible native fallback, not the only
  entry path.
- When the command broker is used, try PowerShell normally, retry it with a
  process-scoped execution-policy override, and then use the native CMD path.
- Permit automatic path changes only before the core runtime starts, so one
  Connection Entry attempt can establish at most one host connection.
- Keep the single core's connection pool dynamic after startup: continuously
  probe health and switch away from degraded WSS, long-poll, short-poll, relay,
  or other verified routes without launching a second core.
- Reject a delivered handoff whose final compressed size, including the
  executable bootstrap, exceeds 1,048,576 bytes.
- Register the minimal core before downloading optional components.
- Route every Connection Entry in the project through `rdev-bootstrap`; no
  launcher may download or execute the old full `rdev` helper directly.
- Keep a verified archive profile as an explicit recovery path after all
  layered paths fail, but make `rdev-bootstrap` verify, download, and launch
  that recovery profile too.

## Universal Bootstrap Invariant

`rdev-bootstrap` is the only connection bootstrap for every generated package,
join page, launcher, and supported platform. A Connection Entry launcher may
prepare ACLs, choose a visible shell, and obtain the platform bootstrap, but it
must not:

- test for or prefer an installed full `rdev` helper;
- download `rdev-<os>-<arch>` as a connection helper;
- execute `rdev host serve` or `rdev-host` directly;
- keep a separate legacy helper-download implementation;
- use the old `rdev-bootstrap upgrade` path to fetch and execute a full helper.

Release builds therefore publish `rdev-bootstrap` for every supported target,
and each platform handoff contains or obtains that bootstrap before connection
startup. The bootstrap verifies the signed platform manifest, downloads the
minimal `rdev-host` core, and owns the foreground connection lifecycle. The
Windows delivery alone has the additional 1 MiB final-ZIP limit.

## Measured Baseline

The release build uses Windows amd64, `CGO_ENABLED=0`, `-trimpath`, and
`-ldflags "-s -w"`.

| Artifact | PE bytes | gzip -9 bytes |
| --- | ---: | ---: |
| Current `rdev-bootstrap.exe` | 7,010,304 | 2,875,395 |
| Current `rdev-host.exe` | 7,915,520 | 3,199,332 |
| Empty stripped Go PE | 1,197,568 | 550,600 |
| Ed25519/JSON/SHA verifier with inlining disabled | 2,378,240 | 947,433 |
| Verifier plus foreground core execution | 2,553,344 | 1,018,787 |
| Verifier plus current `assetdownload.Download` | 6,014,976 | 2,346,939 |

Compiler flags alone cannot close the current 1.8 MiB compressed gap. The
first-contact executable must stop linking the legacy upgrade command and the
standard `net/http`/TLS transport graph.

## Scope Decomposition

This design contains three independently reviewable subprojects:

1. **Entry delivery:** deterministic handoff archive, hard size gate, focused
   bootstrap, PowerShell-first broker, native CMD fallback, and single-attempt
   state. This is the first implementation plan.
2. **Post-registration components:** signed component catalog, lazy component
   resolver, restricted helper process protocol, cancellation, retry, and
   cleanup. This gets a separate plan after entry delivery passes its gate.
3. **Windows acceptance:** real Windows 10/11 evidence, physical interruption,
   Defender/file-lock behavior, ACL/reparse checks, cleanup, and performance
   measurements. This gets a separate plan because it depends on real hosts.

## Delivered Handoff

The controller publishes a private deterministic ZIP named
`Windows-ConnectionEntry.zip`. Its complete contents are:

- `Start-ConnectionEntry.ps1`, the preferred visible layered entry;
- `Start-ConnectionEntry.cmd`, the visible broker and native fallback;
- `rdev-bootstrap.exe`, the focused first-contact executable;
- `rdev-bootstrap.exe.rdev-release.json`, its Ed25519 release manifest;
- `rdev-bootstrap.exe.sha256`;
- `windows-layered-verification.json`;
- an explicit archive-recovery instruction that invokes the same bootstrap
  against the separately signed archive profile without automatically running
  it.

The ZIP is written to a temporary sibling, closed, measured, and published by
atomic rename only when its size is at most 1,048,576 bytes. The unpacked
private handoff remains available to existing local workflows, but the ZIP is
the measured delivery unit. Because the ZIP is private session material, its
runnable launchers may carry the signed join-manifest URL and root needed to
start without manual flag assembly. Archive reports, package plan details,
logs, public release assets, and public evidence must not expose ticket codes,
gateway URLs, bearer tokens, credentials, or controller-local absolute paths.
ZIP entry names also follow Windows basename rules: no reserved device name,
control character, forbidden punctuation, trailing dot/space, separator,
drive/ADS colon, superscript device-number alias, or case/normalization
collision. On Windows the temporary archive is created atomically with a
protected DACL limited to the current user, SYSTEM, and Administrators before
any sensitive byte is written;
on other controllers it uses mode `0600`. Extracted launchers reapply and
validate the target-side DACL before reading or executing bootstrap material.

## Bootstrap Boundary

`rdev-bootstrap.exe` has one release command: `layered-run`. It performs only:

1. strict HTTPS manifest fetch with a 1 MiB response bound;
2. Ed25519 release-root verification, validity-window and expected-version
   checks;
3. exact `windows/amd64` core selection and same-origin relative URL
   resolution;
4. resumable, bounded core download through `assetdownload.Download`;
5. SHA-256 and signed-size verification;
6. private cache validation and final executable lock/recheck;
7. an atomic `core_started` attempt transition immediately before foreground
   execution.

The legacy `upgrade` command and the generated full-helper download paths are
removed from the connection surface. Recovery, cold start, warm start, and
normal reconnect all enter through the same bootstrap verification boundary.

To preserve `assetdownload.Download` without linking `net/http`, its transport
is separated from its cache, Range, retry, SHA-256, size, and atomic-promotion
logic. The standard HTTP adapter remains the default for full binaries. The
focused Windows bootstrap injects a bounded command transport backed by the
Windows system `curl.exe`; it forbids redirects, credentials, query strings,
fragments, non-HTTPS URLs, and non-system executable lookup. The downloader,
not the scripts or transport, continues to own partial-file offsets, retry
decisions, byte bounds, digest checks, cache promotion, and final placement.

The final signed ZIP size gate decides release eligibility. A build that
exceeds the limit stops before materialization or publication; no reported
size substitutes for the actual final ZIP size.

## Entry Selection, Process State, And Dynamic Routes

Both launchers are visible and documented:

- `Start-ConnectionEntry.ps1` is the preferred direct entry.
- `Start-ConnectionEntry.cmd` is the broker and native fallback.

When CMD is launched without a mode, it performs these foreground attempts in
order:

1. signed-verification PowerShell path under the current policy;
2. the same PowerShell path with a process-scoped
   `-ExecutionPolicy Bypass` argument; this changes no registry or machine
   policy and still yields to enforced Group Policy;
3. native CMD preparation followed by the focused bootstrap.

PowerShell catches only failures that occur before the core starts and invokes
the native CMD mode with the same attempt directory. If PowerShell is blocked
before its body runs, the broker observes its nonzero exit and advances. A
direct PowerShell launch that is blocked before execution leaves CMD as the
visible operator fallback.

Each broker invocation creates a unique private attempt directory by an atomic
directory-create loop. The state file uses this schema:

```json
{
  "schema_version": "rdev.windows-layered-attempt.v1",
  "attempt_id": "opaque local id",
  "stage": "pre_core|core_started|core_exited",
  "launcher": "powershell|powershell-bypass|cmd",
  "updated_at": "RFC3339 timestamp"
}
```

The bootstrap writes state through a private temporary file and atomic rename.
Broker fallback is allowed only while `stage=pre_core`. It writes
`core_started` before creating the core process. This state prevents a second
core process; it does not pin the core to one network route. After startup the
core's bounded runtime route pool continues health probes, races eligible
routes, and changes the selected route when the active route becomes slow or
unavailable. HTTPS long-poll, short-poll, and configured verified gateway,
relay, or mesh candidates remain runtime choices under signed policy. A route
change reuses the registered session, endpoint, lease, event cursor, and core
process; it does not create a duplicate host registration.

The current sequential `gatewayCandidateSet` is only a starting point. The V2
pool must probe candidates concurrently within a fixed worker bound, record
latency and consecutive failures, switch immediately on active-route failure,
and use hysteresis for latency-only changes to avoid flapping. Event polling
adapters expose long-poll and short-poll under one cursor contract. A transport
that has no maintained standard-library implementation is excluded rather than
implemented with an ad hoc protocol stack.

If the core process itself later exits, the broker prints an explicit
`rdev-bootstrap` archive-profile command and exits without starting a second
layered or recovery process in that attempt. Closing the visible launcher
cancels the context, terminates its child
process tree, waits for cleanup, and leaves no service, scheduled task, Run
key, firewall rule, or background updater.

## Manifest And Cache Invariants

All release and layered manifests fail closed on unknown fields, invalid
schema, missing version, invalid validity window, Ed25519 signature mismatch,
wrong platform, unknown kind, duplicate ID, invalid SHA-256, nonpositive size,
or unsafe path. Asset URLs are same-origin HTTPS relative paths only.

The user cache remains under `%LOCALAPPDATA%\RemoteDevSkillkit\cache` with
private directory/file permissions corresponding to `0700`/`0600`. UNC paths,
reparse points in every ancestor, non-regular files, oversized partial files,
and identity changes between verification and execution are rejected. Partial
files are untrusted and never executed. Final promotion is atomic; a valid old
file is never removed before its replacement is durable.

## Post-Registration Components

The core manifest may describe optional helpers for these capabilities:

- desktop control;
- file transfer;
- screenshot capture;
- screen recording;
- input control.

Availability in a release never triggers a pre-registration download. The
core starts the component manager only after signed join-manifest verification
and successful host registration. A component request selects exactly one
platform/capability asset, re-verifies the signed manifest and expected
version, calls `assetdownload.Download`, validates the private cache, and
starts a restricted child only for the lifetime of the requesting session.
Cancellation propagates to download and child execution. Helper failure does
not terminate an otherwise usable core connection.

## Acceptance Contract

Unit and cross-compiled Windows tests must prove:

- final deterministic ZIP at or below 1,048,576 bytes;
- preferred PowerShell success;
- current-policy failure, process-scoped retry, and native CMD fallback;
- exactly one core launch for each broker attempt;
- dynamic route change inside that one core when the active pool member fails;
- no duplicate registration or second core process during a route change;
- no optional component request before registration;
- one requested component verifies, caches, cancels, and retries;
- physical interruption resumes with Range;
- private NTFS ACLs, UNC/reparse rejection, and locked executable launch;
- window/session close leaves no child, service, task, Run key, or updater;
- layered exhaustion prints, but does not execute, the bootstrap archive
  profile;
- repository contract tests find no generated direct full-helper download or
  direct host connection outside `rdev-bootstrap`;
- cold/warm registration time, actual network bytes, and cache hit rate.

Real Windows 10 and Windows 11 amd64 reports remain mandatory release evidence.
Synthetic tests establish contracts but do not satisfy that gate.

## Security And Privacy

The design adds no hidden persistence, inbound public listener, unrestricted
shell, policy mutation, or privilege escalation. Public release assets and
public reports exclude tickets, gateway values, tokens, credentials, and
private paths. Plan check details and logs use presence/status values, stage
names, sizes, durations, cache booleans, and redacted error classes only.
