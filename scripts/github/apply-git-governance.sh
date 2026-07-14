#!/usr/bin/env bash
set -euo pipefail

repo=""
execute=0

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/apply-git-governance.sh --repo OWNER/REPO --execute

Applies the governance configuration to GitHub using gh api. The script
rejects runs without --execute before invoking gh.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --execute)
      execute=1
      shift
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

if [[ "$execute" -ne 1 ]]; then
  echo "refusing to apply governance without --execute" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
branch_ruleset_path="$repo_root/.github/governance/branch-ruleset.json"
commit_policy_path="$repo_root/.github/governance/commit-policy.json"
repo_settings_tmp="$(mktemp "${TMPDIR:-/tmp}/git-governance-repo-settings.XXXXXX.json")"
plan_json="$(mktemp "${TMPDIR:-/tmp}/git-governance-plan.XXXXXX.json")"
trap 'rm -f "$repo_settings_tmp" "$plan_json"' EXIT

if ! command -v gh >/dev/null 2>&1; then
  echo "gh is required to apply governance" >&2
  exit 1
fi

redact_stream() {
  python3 -c 'import os,sys
text=sys.stdin.read()
for name in ("GH_TOKEN","GITHUB_TOKEN","FAKE_GH_TOKEN","FAKE_GH_PASSWORD","FAKE_GH_SECRET"):
    value=os.environ.get(name)
    if value:
        text=text.replace(value,"[REDACTED]")
sys.stdout.write(text)'
}

run_gh() {
  local output status
  if output="$(gh "$@" 2>&1)"; then
    status=0
  else
    status=$?
  fi
  printf '%s' "$output" | redact_stream
  printf '\n'
  return "$status"
}

write_plan() {
  python3 - "$repo" "$branch_ruleset_path" "$commit_policy_path" "$plan_json" <<'PY'
import json
import pathlib
import sys

repo = sys.argv[1]
branch_ruleset_path = pathlib.Path(sys.argv[2])
commit_policy_path = pathlib.Path(sys.argv[3])
plan_path = pathlib.Path(sys.argv[4])

branch_ruleset = json.loads(branch_ruleset_path.read_text(encoding="utf-8"))
commit_policy = json.loads(commit_policy_path.read_text(encoding="utf-8"))
plan = {
    "repo": repo,
    "branch_ruleset": branch_ruleset,
    "commit_policy": commit_policy,
    "repo_settings": {
        "allow_squash_merge": True,
        "allow_merge_commit": False,
        "allow_rebase_merge": False,
        "delete_branch_on_merge": True,
        "allow_auto_merge": False,
    },
}
plan_path.write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")
print(plan_path)
PY
}

apply_ruleset() {
  local ruleset_name="$1"
  local ruleset_path="$2"
  local rulesets_json ruleset_id

  rulesets_json="$(run_gh api "repos/$repo/rulesets?per_page=100&targets=branch")"
  ruleset_id="$(python3 - "$rulesets_json" "$ruleset_name" <<'PY'
import json
import sys

raw = sys.argv[1].strip() or "[]"
name = sys.argv[2]
try:
    rulesets = json.loads(raw)
except Exception:
    rulesets = []
for ruleset in rulesets:
    if ruleset.get("name") == name:
        print(ruleset.get("id", ""))
        break
PY
)"

  if [[ -n "$ruleset_id" ]]; then
    run_gh api --method PATCH "repos/$repo/rulesets/$ruleset_id" --input "$ruleset_path"
  else
    run_gh api --method POST "repos/$repo/rulesets" --input "$ruleset_path"
  fi
}

plan_path="$(write_plan)"

apply_ruleset "main-branch-governance" "$branch_ruleset_path"
apply_ruleset "main-commit-policy" "$commit_policy_path"

cat >"$repo_settings_tmp" <<'JSON'
{
  "allow_squash_merge": true,
  "allow_merge_commit": false,
  "allow_rebase_merge": false,
  "delete_branch_on_merge": true,
  "allow_auto_merge": false
}
JSON
run_gh api --method PATCH "repos/$repo" --input "$repo_settings_tmp"

printf 'applied governance plan: %s\n' "$plan_path"
