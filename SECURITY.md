# Security Policy

## Supported Versions

The project is pre-1.0. Security fixes target `main` until the first tagged release.

## Security Model

Remote Dev Skillkit is intended for explicit, visible, consent-based remote support and remote development.

Temporary third-party sessions must be:

- visible to the remote user;
- time-limited;
- revocable;
- auditable;
- user-level by default.

Managed service mode is only for operator-owned or formally managed devices.

## Prohibited Features

Reports or pull requests that add the following behavior will be rejected:

- hidden persistence;
- UAC or sudo bypass;
- credential dumping;
- disabling local security controls;
- unrestricted shell access without policy enforcement;
- public inbound listeners on target hosts by default;
- silent installation of long-lived agents on third-party devices.

## Reporting

Until a public security contact is configured, report issues privately to the repository owner.
