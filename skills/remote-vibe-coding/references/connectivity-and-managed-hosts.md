# Connectivity and Managed Hosts

Read this only for connection reliability, LAN/relay/mesh/SSH choices,
max-control discovery, or long-running owned workstations.

## Connectivity

- Prefer `--transport auto` for unknown or restrictive networks. It attempts
  WSS, then HTTPS long-poll, then short polling, all as outbound target-host
  connections.
- If the host does not appear, check proxy requirements, TLS interception,
  blocked outbound 443, DNS failure, captive portals, VPN requirements, and
  configured relay/mesh/SSH paths before asking the human.
- When the target host and Agent gateway may be on the same LAN, derive or ask
  for a LAN-reachable gateway URL.
- Agents may inspect local interfaces, routes, DNS/mDNS, proxy settings, SSH
  config, and installed mesh tooling, and may run scoped private-network
  reachability probes.
- Relay, mesh/VPN, and SSH tunnel paths are connectivity only. They never
  replace target consent, host approval, signed jobs, local policy checks, or
  evidence.

## Max-Control Profile

- When `authority_profile=max-control`, the approved remote host may act as the
  Agent's field workstation.
- It may discover reachable devices and control downstream authorized hosts or
  devices through configured SSH, mesh, relay, or management APIs only when job
  policy grants `downstream.control.scoped`.
- Capture evidence for every downstream action and keep the task intent bounded.

## Managed Owned Workstations

- For the operator's own Mac, Windows PC, or Linux workstation, prefer managed
  mode for recurring development work.
- Use reviewed LaunchAgent, systemd user-unit, Windows Service, or foreground
  managed smoke plans according to the detected OS and available service
  manager.
- Use `--once=false`, `--transport auto`, release gates, enrollment renewal,
  revocation refresh, workspace locks, Git worktrees, host-local context,
  reconnect evidence, and safe stop/uninstall instructions.
- Do not claim real Linux or Windows managed-service readiness until a clean
  host proves start/status/reconnect/job evidence/stop/uninstall acceptance.
