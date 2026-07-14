#!/usr/bin/env bash
set -euo pipefail

repo=""

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/plan-git-governance.sh --repo OWNER/REPO

Prints a read-only JSON plan for the repository governance configuration.
The script never calls gh and never mutates local or remote state.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      echo "unexpected argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$repo" ]]; then
  usage
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
branch_ruleset_path="$repo_root/.github/governance/branch-ruleset.json"
commit_policy_path="$repo_root/.github/governance/commit-policy.json"
codeowners_path="$repo_root/.github/CODEOWNERS"

python3 - "$repo" "$branch_ruleset_path" "$commit_policy_path" "$codeowners_path" <<'PY'
import datetime
import json
import pathlib
import sys

repo = sys.argv[1]
paths = {
    "branch_ruleset": pathlib.Path(sys.argv[2]),
    "commit_policy": pathlib.Path(sys.argv[3]),
    "codeowners": pathlib.Path(sys.argv[4]),
}

payload = {}
for key, path in paths.items():
    if not path.is_file():
        raise SystemExit(f"required governance file not found: {path}")
    if path.suffix == ".json":
        payload[key] = json.loads(path.read_text(encoding="utf-8"))
    else:
        payload[key] = {
            "path": str(path.resolve()),
            "content": path.read_text(encoding="utf-8"),
        }

plan = {
    "schema_version": "rdev.github-governance-plan.v1",
    "generated_at": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "repo": repo,
    "external_mutation": False,
    "source_files": {
        "branch_ruleset": str(paths["branch_ruleset"].resolve()),
        "commit_policy": str(paths["commit_policy"].resolve()),
        "codeowners": str(paths["codeowners"].resolve()),
    },
    "governance": {
        "branch_ruleset": payload["branch_ruleset"],
        "commit_policy": payload["commit_policy"],
        "codeowners": payload["codeowners"],
    },
    "repo_settings": {
        "allow_squash_merge": True,
        "allow_merge_commit": False,
        "allow_rebase_merge": False,
        "delete_branch_on_merge": True,
        "allow_auto_merge": False,
    },
    "guarantees": [
        "main-only",
        "pull-request-only",
        "one-approval",
        "required-checks: git-policy, go-checks",
        "conversation-resolution",
        "strict-up-to-date-branches",
        "no-force-push",
        "no-branch-deletion",
        "squash-only-merge",
        "auto-delete-head-branch",
    ],
}
print(json.dumps(plan, indent=2))
PY
