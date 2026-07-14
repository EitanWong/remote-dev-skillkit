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

validate_repo() {
  python3 - "$1" <<'PY'
import re
import sys

repo = sys.argv[1]
if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
    raise SystemExit("invalid --repo value; expected OWNER/REPO")
print(repo)
PY
}

redact_file() {
  python3 - "$1" <<'PY'
import os
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
for name in (
    "GH_TOKEN",
    "GITHUB_TOKEN",
    "GH_ENTERPRISE_TOKEN",
    "GITHUB_ENTERPRISE_TOKEN",
    "FAKE_GH_TOKEN",
    "FAKE_GH_PASSWORD",
    "FAKE_GH_SECRET",
):
    value = os.environ.get(name)
    if value:
        text = text.replace(value, "[REDACTED]")
sys.stdout.write(text)
PY
}

run_gh() {
  local stdout_file stderr_file status
  stdout_file="$(mktemp "${TMPDIR:-/tmp}/git-governance-gh-stdout.XXXXXX")"
  stderr_file="$(mktemp "${TMPDIR:-/tmp}/git-governance-gh-stderr.XXXXXX")"
  if gh "$@" >"$stdout_file" 2>"$stderr_file"; then
    status=0
  else
    status=$?
  fi
  redact_file "$stdout_file"
  redact_file "$stderr_file" >&2
  rm -f "$stdout_file" "$stderr_file"
  return "$status"
}

validate_json_array() {
  python3 - "$1" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
if not isinstance(payload, list):
    raise SystemExit("expected gh api JSON array response")
PY
}

extract_ruleset_id() {
  python3 - "$1" "$2" <<'PY'
import json
import re
import sys

payload = json.loads(sys.argv[1])
if not isinstance(payload, list):
    raise SystemExit("expected gh api JSON array response")

name = sys.argv[2]
for ruleset in payload:
    if not isinstance(ruleset, dict):
        continue
    if ruleset.get("name") != name:
        continue
    ruleset_id = ruleset.get("id")
    if isinstance(ruleset_id, int) and ruleset_id > 0:
        print(ruleset_id)
        raise SystemExit(0)
    if isinstance(ruleset_id, str) and re.fullmatch(r"[1-9][0-9]*", ruleset_id):
        print(ruleset_id)
        raise SystemExit(0)
    raise SystemExit("expected valid ruleset id")
PY
}

validate_json_object() {
  python3 - "$1" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
if not isinstance(payload, dict):
    raise SystemExit("expected gh api JSON object response")
PY
}

write_plan() {
  python3 - "$repo" "$branch_ruleset_path" "$commit_policy_path" <<'PY'
import json
import pathlib
import sys

repo = sys.argv[1]
branch_ruleset_path = pathlib.Path(sys.argv[2])
commit_policy_path = pathlib.Path(sys.argv[3])

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
print(json.dumps(plan, indent=2))
PY
}

main() {
  repo=""
  execute=0

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

  repo="$(validate_repo "$repo")"

  if [[ "$execute" -ne 1 ]]; then
    echo "refusing to apply governance without --execute" >&2
    exit 2
  fi

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "$script_dir/../.." && pwd)"
  branch_ruleset_path="$repo_root/.github/governance/branch-ruleset.json"
  commit_policy_path="$repo_root/.github/governance/commit-policy.json"
  repo_settings_tmp="$(mktemp "${TMPDIR:-/tmp}/git-governance-repo-settings.XXXXXX")"
  trap 'rm -f "$repo_settings_tmp"' EXIT

  if ! command -v gh >/dev/null 2>&1; then
    echo "gh is required to apply governance" >&2
    exit 1
  fi

  plan_json="$(write_plan)"

  apply_ruleset() {
    local ruleset_name="$1"
    local ruleset_path="$2"
    local rulesets_json ruleset_id response

    rulesets_json="$(run_gh api "repos/$repo/rulesets?per_page=100&targets=branch")"
    validate_json_array "$rulesets_json"
    ruleset_id="$(extract_ruleset_id "$rulesets_json" "$ruleset_name")"

    if [[ -n "$ruleset_id" ]]; then
      response="$(run_gh api --method PATCH "repos/$repo/rulesets/$ruleset_id" --input "$ruleset_path")"
    else
      response="$(run_gh api --method POST "repos/$repo/rulesets" --input "$ruleset_path")"
    fi
    validate_json_object "$response"
  }

  apply_ruleset "main-branch-governance" "$branch_ruleset_path"
  apply_ruleset "main-commit-policy" "$commit_policy_path"

  cat >"$repo_settings_tmp" <<'JSON'
{"allow_squash_merge":true,"allow_merge_commit":false,"allow_rebase_merge":false,"delete_branch_on_merge":true,"allow_auto_merge":false}
JSON
  response="$(run_gh api --method PATCH "repos/$repo" --input "$repo_settings_tmp")"
  validate_json_object "$response"

  printf 'applied governance plan JSON:\n%s\n' "$plan_json"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
