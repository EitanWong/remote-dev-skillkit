# Windows GUI Actions Without Task-Level Authorization

## Goal

Allow an attended Windows session with the corresponding signed GUI capability to execute desktop GUI tasks without adding `authorizations_required` to each task payload.

## Scope

The change applies to all native desktop actions: screenshots, recording, window focus and movement, keyboard and mouse input, application launch and close, URL open, and clipboard read/write. It does not change session capability ceilings, signed manifests, visible connector requirements, audit logging, persistence rules, UAC behavior, or non-GUI authorization policies.

## Design

The desktop action-to-authorization mapping will return no task-level authorization for every supported GUI action. The CLI desktop task builder and shared policy templates will therefore omit `authorizations_required` for GUI tasks. Existing capability checks remain authoritative: a task still requires its action-specific capability, and the host runner still fails closed when that capability is absent.

Tests will verify both sides of the contract: every supported GUI action produces no task-level authorization, while non-GUI actions and capability checks retain their existing behavior. After implementation, a fresh Windows helper session will provide real-device evidence for every supported GUI action that can be exercised safely, including screenshot, recording, window inspection, focus/move, keyboard/mouse input, app launch/close, URL open, and clipboard operations.

## Verification

Run focused Go tests for policy, task templates, and CLI task construction, then the broader test suite. Rebuild the Windows helper and execute the three scoped desktop tasks through the standard Control Plane session, reviewing task results and screenshot artifact validity before reporting success.
