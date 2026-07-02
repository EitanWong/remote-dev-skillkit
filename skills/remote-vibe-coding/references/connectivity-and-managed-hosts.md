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
- If NAT, firewall, CGNAT, missing public DNS, or restrictive egress blocks
  direct LAN/hosted reachability, evaluate already-configured tunnel or mesh
  paths automatically. Prefer open-source/free options before paid relays:
  frp for reverse proxy/NAT traversal, Chisel for HTTP(S)-based TCP/UDP
  tunneling, headscale for a self-hosted Tailscale-compatible control plane,
  and WireGuard for direct VPN tunnels.
- Before installing or enabling tunnel/mesh components, verify source
  provenance, prefer temporary/user-scoped configuration, and ask before
  privileged, persistent, paid, firewall, DNS, cloud, or security-policy
  changes.
- Relay, mesh/VPN, and SSH tunnel paths are connectivity only. They never
  replace target consent, host approval, signed jobs, local policy checks, or
  evidence.
- After choosing any connectivity path, return to the Connection Entry flow.
  The target-side human receives a link, visible script, or signed package from
  `connection_entry` / `entry_package_plan`; raw ticket, root, gateway,
  transport, release, and checksum values stay in Agent metadata.

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
