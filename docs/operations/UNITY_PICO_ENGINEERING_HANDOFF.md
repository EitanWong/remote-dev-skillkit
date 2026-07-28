# Unity / PICO Scoped Engineering Handoff

This handoff is for a **joined managed Windows session**. It uses the existing
`rdev.sessions.task` engineering-task contract; it does not create an additional
control plane, persistent remote shell, or inbound listener.

## Non-negotiable boundary

| Surface | Rule |
| --- | --- |
| Human main worktree | Read-only to the AI. It supplies the immutable `base_sha`; the AI must not use it as its execution checkout. |
| AI worktree | A fresh `git-worktree` checkout on an `rdev/<task>` branch. The host runner remaps `workspace.root`, read scope, write scope, and verification paths to this checkout. |
| Lock | The host runner acquires the repository/task workspace lock before creating the worktree and releases it during finalization. The lock serializes rdev task ownership; it does not make human edits disappear. |
| Network | `default-deny` unless a later, explicit task grants a narrowly required network capability. |
| Generated Unity output | `Library/`, `Temp/`, `Logs/`, `obj/`, `Build/`, and `UserSettings/` are outside write scope. Do not commit or transfer them as task artifacts. |

A clean human main worktree is required for a delivery task. If it is dirty,
stop before task submission rather than silently folding human changes into the
AI branch.

## Scope selection

Start from the narrowest feature directory. For a PICO/Unity change, the normal
write scope is:

- `Assets/<FEATURE_DIR>` — required project source, prefabs, scenes, and `.meta`
  files for the feature only;
- `Packages/manifest.json` and `Packages/packages-lock.json` — only when the
  accepted change explicitly adds or changes a package;
- one named `ProjectSettings/*.asset` file — only when the accepted change needs
  a project setting.

Do **not** use `Assets/` or `ProjectSettings/` as a blanket write scope. Split a
cross-cutting change into separately reviewed tasks when its scope cannot be
expressed narrowly.

## Submission template

Replace every uppercase placeholder with the joined endpoint's actual values.
`BASE_SHA` must come from the human main worktree immediately before submission;
`BRANCH` must be unique for the task. The outer and inner capabilities and
idempotency key intentionally match.

```json
{
  "session_id": "SESSION_ID",
  "target_endpoint_id": "WINDOWS_ENDPOINT_ID",
  "adapter": "codex",
  "capabilities": ["codex.run", "git.diff"],
  "idempotency_key": "unity-pico-FEATURE-TASK-ID",
  "engineering_task": {
    "schema_version": "rdev.engineering-task.v1",
    "goal": "Implement the accepted Unity/PICO FEATURE change without editing the human main worktree.",
    "workspace": {
      "root": "C:\\src\\UNITY_PROJECT",
      "base_sha": "BASE_SHA",
      "branch": "rdev/unity-pico-FEATURE-TASK-ID",
      "isolation": "git-worktree",
      "dirty_policy": "require-clean",
      "cleanup": "preserve",
      "read_scope": ["Assets", "Packages", "ProjectSettings"],
      "write_scope": [
        "Assets/FEATURE_DIR",
        "Packages/manifest.json",
        "Packages/packages-lock.json",
        "ProjectSettings/EXPLICIT_SETTING.asset"
      ]
    },
    "plan": [
      "Inspect the scoped project files and current Unity/PICO configuration.",
      "Implement only the accepted feature change in the isolated worktree.",
      "Run the allowlisted checks and record the diff/evidence for review."
    ],
    "acceptance": [
      "The human main worktree branch and HEAD remain unchanged.",
      "The isolated diff touches only declared write_scope paths.",
      "Unity batch-mode compilation or the project-specific PICO validation succeeds.",
      "git diff --check succeeds and the reviewable diff is attached to task evidence."
    ],
    "verification": {
      "commands": [
        ["git", "diff", "--check"],
        ["powershell", "-NoProfile", "-NonInteractive", "-File", "Scripts/ci/unity-pico-validate.ps1"]
      ],
      "allow_commands": ["git", "powershell"]
    },
    "limits": {
      "max_duration_seconds": 7200,
      "max_output_bytes": 1048576,
      "max_attempts": 2
    },
    "network_policy": "default-deny",
    "required_capabilities": ["codex.run", "git.diff"],
    "interrupts_required": ["interrupt.human", "interrupt.network"],
    "idempotency_key": "unity-pico-FEATURE-TASK-ID"
  },
  "payload": {
    "prompt": "Work only in the isolated worktree. Stop and report if the accepted change needs a path outside write_scope, a new network permission, or a change to human-owned main worktree state."
  }
}
```

Before submitting, remove optional package/settings paths that the accepted
change does not need. A path remains forbidden unless it appears in
`write_scope`, even if it is present in `read_scope`.

## Verification and review gate

1. Read the task result and its `rdev.git-worktree-evidence.v1` evidence.
2. Confirm the evidence base SHA matches `BASE_SHA`, the branch has the expected
   `rdev/` name, and the recorded write-scope diff has no out-of-scope path.
3. Run the declared Unity/PICO validation from the isolated worktree. A real
   project may replace `Scripts/ci/unity-pico-validate.ps1` with its existing
   batch-mode command, but the command and executable must remain allowlisted.
4. Review the diff in the isolated worktree. Only a human accepts/cherry-picks
   the branch into the human main worktree.

## Preserve versus rollback

`cleanup: "preserve"` is the delivery default: it retains the isolated worktree
for human diff review while releasing the rdev task lock. It does not merge,
commit, or modify the human main worktree.

For a disposable spike, set `cleanup: "rollback"` before submission. The host
runner then resets/removes the isolated worktree during finalization and records
the rollback evidence. Do not use `rollback` for a change that must be reviewed.

If a preserved delivery task is rejected after review, perform rollback as a
new, explicit scoped operation: reset/remove only the recorded AI worktree,
run `git worktree prune`, verify the human main worktree HEAD is still
`BASE_SHA`, and attach the cleanup evidence. Never reset or clean the human main
worktree as part of AI rollback.

## Why this maps to the implementation

- `internal/hostrunner/isolation.go` creates the linked worktree, remaps the
  envelope root/scopes to it, and finalizes evidence/lock release.
- `internal/contracts/engineering_task.go` validates `base_sha`, isolation,
  scopes, cleanup policy, verification allowlist, limits, and exact adapter
  capability requirements.
- `internal/workspace/worktree_finalize.go` records worktree terminal state and
  supports `preserve`, `remove`, and `rollback` cleanup policies.
