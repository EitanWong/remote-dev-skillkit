## Summary

- 

## Issue Linkage

- [ ] Linked issue recorded below
- Closes #

## Branch and Worktree Evidence

Paste the exact local command output:

```text
$ git branch --show-current

$ git worktree list --porcelain | sed -n '1,8p'
```

## Acceptance Evidence

- [ ] Tests or checks run locally
- [ ] Evidence bundle, audit export, or acceptance transcript attached when relevant
- [ ] Docs or Skillkit instructions updated when behavior changed

## Tests

- [ ] `./scripts/check.sh`
- [ ] Additional focused tests documented below
- Focused test notes:

## Security Review

- [ ] No hardcoded secrets or credentials
- [ ] No hidden persistence or inbound temporary-host exposure
- [ ] Dangerous actions remain approval-gated before side effects
- [ ] Host-side validation, workspace boundaries, redaction, and audit are preserved

## Release Impact

- User-visible change:
- Migration or compatibility impact:
- Release-smoke impact:
