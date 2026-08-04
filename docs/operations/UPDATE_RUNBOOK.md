# Host & Gateway Update Runbook

This runbook covers updating the Remote Dev Skillkit control plane (gateway)
and enrolled managed Windows hosts. Design goals: updates are explicit,
verifiable, idempotent, and never leave a host without a working connector.

## Version layout

- Gateway releases on the pilot host are immutable per-commit directories:
  `/opt/rdev-gateway/releases/<sha>-<slug>/` containing `rdev-gateway`,
  `rdev-host.exe`, `SHA256SUMS`, `COMMIT`, and optionally `MCP_TOOLS.json`.
- The active service command is chosen by a numbered systemd drop-in chain
  (`/etc/systemd/system/rdev-gateway-pilot.service.d/NN-<slug>.conf`). The
  highest-numbered drop-in wins; older ones are kept for rollback.
- Gateway state and signing keys live in `/var/lib/rdev-gateway/managed-v2/`
  and are **never overwritten** by a rollout.
- Host connectors on a managed Windows host are digest-keyed release copies
  under the protected service state root (`releases/<sha256>/rdev-host.exe`);
  the running image is never overwritten in place.

## Gateway cutover (operator)

1. Build the release bundle from a verified commit (see below).
2. Stage it on the pilot host: `/opt/rdev-gateway/releases/<sha>-<slug>/`
   with `COMMIT` and `SHA256SUMS`. Keep the old release directory intact.
3. Write the next drop-in (highest number) pointing at the new release:
   `ExecStart=` (clear) followed by the new `ExecStart=` with the same
   `--state-file`, `--signing-key-file`, `--signing-key-id`,
   `--operator-auth-file`, `--public-base-url`, and
   `--windows-amd64-host-binary` flags.
4. `systemctl daemon-reload && systemctl restart rdev-gateway-pilot`.
   Long-poll connections blip; enrolled hosts reconnect within their
   reconnect-grace window without operator action.
5. Verify, in order:
   - `systemctl is-active` + MainPID changed;
   - `rdev-gateway version` / commit of the new binary;
   - `/healthz` and trust-bundle freshness;
   - state file unchanged (mtime/hash) — persistence intact;
   - served artifact digest == `SHA256SUMS` entry
     (`sha256sum /opt/rdev-gateway/releases/<sha>-<slug>/rdev-host.exe`);
   - authenticated lifecycle probe: `create -> close` via the configured MCP
     launcher (a `403` is a credential-binding gate, not a reason to weaken
     auth);
   - hosts still listed with fresh `last_seen_at`.
6. Rollback: remove/point below the new drop-in and restart the service with
   the previous release. State and signing identity are untouched, so the
   previous binary resumes with the same sessions.

## Host connector update (control-plane path, no human needed)

`host-update` is a session task adapter:

- Requires the `host.update` capability in the session ceiling and in the
  task capabilities.
- The host downloads the artifact the gateway currently serves from
  `GET /v1/sessions/{id}/artifacts/host-update` under its endpoint lease,
  verifies SHA-256, stages a digest-keyed release, then runs a **detached**
  updater (`rdev-host service update --release <dir>`).
- The updater re-verifies the staged digest, switches the SCM binary path
  (never overwriting the running image), starts the new service, waits for it
  to run, and **auto-rolls-back** to the previous release if the replacement
  stops during the health window.
- The task result is posted before the service restarts; verify the outcome
  from the reconnected endpoint's `host_version`/`host_commit` and the
  `UPDATE_RESULT.json` marker in the staged release directory.
- Idempotent: an update to the digest the host already runs reports
  `up-to-date` and changes nothing.
- Ordering: the host applies whatever the gateway serves, so **cut the
  gateway over first**; pinning `expected_sha256` in the payload makes a
  not-yet-cut-over gateway fail with a clear hint instead of applying a stale
  build.

## Re-enrolling an existing host into a new session

A host service is bound to the join code in its service config. To move it to
a new session (e.g. to raise the capability ceiling with `host.update`), issue
a browser handoff for the new session and open it on the target Windows host:
the bootstrap performs `service install --replace-existing`, which preserves
identity/trust state and stages the gateway-served (current) connector.

## Building a release bundle

```sh
git checkout <sha>            # verified commit, must be an ancestor of origin/main
./scripts/check.sh            # full gate
mkdir -p dist/<sha>-<slug> && cd dist/<sha>-<slug>
CGO_ENABLED=0 go build -o rdev-gateway ../../cmd/rdev-gateway
CGO_ENABLED=0 go build -o rdev          ../../cmd/rdev
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o rdev-host.exe ../../cmd/rdev-host
printf '%s\n' "$(sha256sum rdev-gateway | cut -d' ' -f1)  rdev-gateway" > SHA256SUMS
# ... one line per artifact; plus COMMIT file with the full sha
```

Build metadata (`internal/buildinfo`) is injected by ldflags in operator
builds; verify with `rdev-gateway version` after staging.

## Corner cases handled

- Host offline at update time: the task stays queued; the host picks it up on
  reconnect (standard task semantics).
- Corrupt download / digest mismatch: aborts before staging; the old service
  is untouched.
- Staging race or corruption: the detached updater re-verifies the digest
  sidecar before activation.
- New binary fails to boot: SCM health window expires/stopped → automatic
  rollback to the previous release; the outcome is in `UPDATE_RESULT.json`.
- Concurrent updates: the update is a single task; the SCM replace path is
  serialized by the service handle.
- Gateway update while hosts are joined: long-poll blips; hosts reconnect
  under the reconnect grace; verify with fresh `last_seen_at` + a bounded task
  receipt (a stale `online` snapshot is not evidence).
