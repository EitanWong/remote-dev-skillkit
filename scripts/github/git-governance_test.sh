#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/git-governance-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

repo_root="$(cd "$script_dir/../.." && pwd)"
test_repo_root="$tmp_dir/repo"
mkdir -p "$test_repo_root"
cp -R "$repo_root/.github" "$test_repo_root/"
mkdir -p "$test_repo_root/scripts"
cp -R "$repo_root/scripts/github" "$test_repo_root/scripts/"

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"
gh_log="$tmp_dir/gh.log"

cat >"$fake_bin/gh" <<'GH'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" != "api" ]]; then
  echo "unexpected gh command: ${1:-}" >&2
  exit 64
fi

shift
method="GET"
path=""
input=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --method|-X)
      method="${2:-}"
      shift 2
      ;;
    --input)
      input="${2:-}"
      shift 2
      ;;
    -H|-f|--field|--raw-field)
      shift 2
      ;;
    --*)
      shift
      ;;
    *)
      if [[ -z "$path" ]]; then
        path="$1"
      fi
      shift
      ;;
  esac
done

body_json=""
if [[ -n "$input" ]]; then
  body_json="$(python3 - "$input" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
print(json.dumps(json.loads(path.read_text(encoding='utf-8')), separators=(',', ':')))
PY
)"
fi

printf '%s\t%s\t%s\n' "$method" "$path" "$body_json" >> "${GH_LOG:?}"

assert_rule_body() {
  local body="$1"
  python3 - "$body" <<'PY'
import json
import sys

body = json.loads(sys.argv[1])
name = body.get("name")
conditions = body.get("conditions", {})
rules = body.get("rules", [])

if conditions.get("ref_name", {}).get("include") != ["refs/heads/main"]:
    raise SystemExit("ruleset must target refs/heads/main")

if name == "main-branch-governance":
    if body.get("target") != "branch":
        raise SystemExit("branch ruleset must target branches")
    if body.get("enforcement") != "active":
        raise SystemExit("branch ruleset must be active")
    rule_types = {rule.get("type") for rule in rules if isinstance(rule, dict)}
    required = {"pull_request", "required_linear_history", "required_status_checks", "non_fast_forward", "deletion"}
    missing = required - rule_types
    if missing:
        raise SystemExit(f"missing required rules: {sorted(missing)}")

    pull_request = next(rule for rule in rules if rule.get("type") == "pull_request")
    pr_params = pull_request.get("parameters", {})
    if pr_params.get("allowed_merge_methods") != ["squash"]:
        raise SystemExit("squash-only merge enforcement missing")
    if pr_params.get("required_approving_review_count") != 1:
        raise SystemExit("approval count must be 1")
    if pr_params.get("required_review_thread_resolution") is not True:
        raise SystemExit("conversation resolution required")
    if pr_params.get("require_code_owner_review") is not True:
        raise SystemExit("code owner review required")

    status_checks = next(rule for rule in rules if rule.get("type") == "required_status_checks")
    status_params = status_checks.get("parameters", {})
    if status_params.get("strict_required_status_checks_policy") is not True:
        raise SystemExit("up-to-date branch policy missing")
    contexts = {check.get("context") for check in status_params.get("required_status_checks", []) if isinstance(check, dict)}
    if contexts != {"git-policy", "go-checks"}:
        raise SystemExit(f"unexpected status check contexts: {sorted(contexts)}")
elif name == "main-commit-policy":
    commit_rule = next(rule for rule in rules if rule.get("type") == "commit_message_pattern")
    pattern = commit_rule.get("parameters", {}).get("pattern", "")
    if "conventional commits" not in commit_rule.get("parameters", {}).get("name", ""):
        raise SystemExit("commit policy must enforce conventional commits")
    if not pattern.startswith("^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)"):
        raise SystemExit("commit policy regex missing")
else:
    raise SystemExit(f"unexpected ruleset name: {name}")
PY
}

assert_repo_settings_body() {
  local body="$1"
  python3 - "$body" <<'PY'
import json
import sys

body = json.loads(sys.argv[1])
expected = {
    "allow_squash_merge": True,
    "allow_merge_commit": False,
    "allow_rebase_merge": False,
    "delete_branch_on_merge": True,
    "allow_auto_merge": False,
}
if body != expected:
    raise SystemExit(f"unexpected repo settings body: {body!r}")
PY
}

case "${FAKE_GH_MODE:-happy}" in
  redact)
    printf '{"ok":true,"path":"%s"}\n' "$path"
    printf 'stdout:%s:%s\n' "${GH_TOKEN:-}" "${GITHUB_TOKEN:-}"
    printf 'stdout:%s:%s\n' "${GH_ENTERPRISE_TOKEN:-}" "${GITHUB_ENTERPRISE_TOKEN:-}"
    printf 'stderr:%s:%s\n' "${GH_TOKEN:-}" "${GITHUB_TOKEN:-}" >&2
    printf 'stderr:%s:%s\n' "${GH_ENTERPRISE_TOKEN:-}" "${GITHUB_ENTERPRISE_TOKEN:-}" >&2
    ;;
  bad-get-json)
    if [[ "$method" == "GET" && "$path" == repos/*/rulesets* ]]; then
      printf 'not-json\n'
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
    fi
    ;;
  bad-get-type)
    if [[ "$method" == "GET" && "$path" == repos/*/rulesets* ]]; then
      printf '{"not":"an array"}\n'
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
    fi
    ;;
  bad-ruleset-id)
    if [[ "$method" == "GET" && "$path" == repos/*/rulesets* ]]; then
      printf '[{"name":"main-branch-governance","id":"abc"},{"name":"main-commit-policy","id":202}]\n'
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
    fi
    ;;
  bad-post-json)
    if [[ "$method" == "GET" && "$path" == repos/*/rulesets* ]]; then
      printf '[]\n'
    elif [[ "$method" == "POST" && "$path" == repos/*/rulesets ]]; then
      printf 'not-json\n'
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
    fi
    ;;
  bad-patch-json)
    if [[ "$method" == "GET" && "$path" == repos/*/rulesets* ]]; then
      printf '[{"name":"main-branch-governance","id":101}]\n'
    elif [[ "$method" == "PATCH" && "$path" == repos/*/rulesets/* ]]; then
      printf 'not-json\n'
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
    fi
    ;;
  happy|*)
    if [[ "$method" == "GET" && "$path" == repos/*/rulesets* ]]; then
      printf '[{"name":"main-branch-governance","id":101},{"name":"main-commit-policy","id":"202"}]\n'
    elif [[ "$method" == "POST" && "$path" == repos/*/rulesets ]]; then
      assert_rule_body "$body_json"
      printf '{"ok":true,"kind":"create","path":"%s"}\n' "$path"
    elif [[ "$method" == "PATCH" && "$path" == repos/*/rulesets/* ]]; then
      assert_rule_body "$body_json"
      printf '{"ok":true,"kind":"update","path":"%s"}\n' "$path"
    elif [[ "$method" == "PATCH" && "$path" == repos/* ]]; then
      assert_repo_settings_body "$body_json"
      printf '{"ok":true,"kind":"settings","path":"%s"}\n' "$path"
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
    fi
    ;;
esac
GH
chmod +x "$fake_bin/gh"

export GH_LOG="$gh_log"
export PATH="$fake_bin:$PATH"
export GH_TOKEN="gh-token-$(python3 - <<'PY'
import secrets
print(secrets.token_hex(8))
PY
)"
export GITHUB_TOKEN="github-token-$(python3 - <<'PY'
import secrets
print(secrets.token_hex(8))
PY
)"
export GH_ENTERPRISE_TOKEN="gh-enterprise-token-$(python3 - <<'PY'
import secrets
print(secrets.token_hex(8))
PY
)"
export GITHUB_ENTERPRISE_TOKEN="github-enterprise-token-$(python3 - <<'PY'
import secrets
print(secrets.token_hex(8))
PY
)"

plan_script="$test_repo_root/scripts/github/plan-git-governance.sh"
apply_script="$test_repo_root/scripts/github/apply-git-governance.sh"

assert_executable() {
  local path="$1"
  if [[ ! -x "$path" ]]; then
    echo "expected executable bit on $path" >&2
    exit 1
  fi
}

assert_plan_json() {
  local path="$1"
  python3 - "$path" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
plan = json.loads(path.read_text(encoding='utf-8'))
if plan.get("schema_version") != "rdev.github-governance-plan.v1":
    raise SystemExit("unexpected plan schema")
if plan.get("repo") != "example-org/example-repo":
    raise SystemExit("unexpected repo in plan")
if plan.get("external_mutation") is not False:
    raise SystemExit("plan must remain read-only")
if plan.get("repo_settings", {}).get("delete_branch_on_merge") is not True:
    raise SystemExit("plan must include repo settings")
PY
}

expect_failure() {
  local description="$1"
  shift
  set +e
  "$@" >/dev/null 2>&1
  status=$?
  set -e
  if [[ "$status" -eq 0 ]]; then
    echo "$description unexpectedly succeeded" >&2
    exit 1
  fi
}

assert_executable "$plan_script"
assert_executable "$apply_script"
assert_executable "$test_repo_root/scripts/github/git-governance_test.sh"

: >"$gh_log"
plan_output="$tmp_dir/plan.json"
"$plan_script" --repo example-org/example-repo >"$plan_output"
assert_plan_json "$plan_output"
if [[ -s "$gh_log" ]]; then
  echo "plan script should not call gh" >&2
  cat "$gh_log" >&2
  exit 1
fi

expect_failure "plan script missing repo" "$plan_script"
expect_failure "plan script invalid repo" "$plan_script" --repo bad/repo/extra
expect_failure "apply script missing execute" "$apply_script" --repo example-org/example-repo
expect_failure "apply script invalid repo" "$apply_script" --repo bad/repo/extra --execute

: >"$gh_log"
apply_stdout="$tmp_dir/apply.out"
apply_stderr="$tmp_dir/apply.err"
FAKE_GH_MODE=happy "$apply_script" --repo example-org/example-repo --execute >"$apply_stdout" 2>"$apply_stderr"

python3 - "$gh_log" <<'PY'
import json
import sys
from pathlib import Path

entries = []
for raw_line in Path(sys.argv[1]).read_text(encoding='utf-8').splitlines():
    if not raw_line.strip():
        continue
    method, path, body = raw_line.split('\t', 2)
    entries.append((method, path, body))

if not any(method == 'GET' and path == 'repos/example-org/example-repo/rulesets?per_page=100&targets=branch' for method, path, _ in entries):
    raise SystemExit('missing ruleset list GET request')

rule_bodies = []
settings_bodies = []
for method, path, body in entries:
    if method in {'POST', 'PATCH'} and '/rulesets' in path:
        data = json.loads(body)
        rule_bodies.append((method, path, data))
    if method == 'PATCH' and path == 'repos/example-org/example-repo':
        settings_bodies.append(json.loads(body))

if len(rule_bodies) != 2:
    raise SystemExit(f'expected two ruleset mutations, got {len(rule_bodies)}')
if len(settings_bodies) != 1:
    raise SystemExit('expected one repo settings mutation')

branch_ruleset = next(data for _, _, data in rule_bodies if data.get('name') == 'main-branch-governance')
commit_policy = next(data for _, _, data in rule_bodies if data.get('name') == 'main-commit-policy')
settings = settings_bodies[0]

if branch_ruleset['conditions']['ref_name']['include'] != ['refs/heads/main']:
    raise SystemExit('branch ruleset must target main')
pull_request = next(rule for rule in branch_ruleset['rules'] if rule['type'] == 'pull_request')
if pull_request['parameters']['allowed_merge_methods'] != ['squash']:
    raise SystemExit('branch ruleset must be squash-only')
if pull_request['parameters']['required_approving_review_count'] != 1:
    raise SystemExit('branch ruleset must require one approval')
if pull_request['parameters']['required_review_thread_resolution'] is not True:
    raise SystemExit('branch ruleset must require resolved conversations')
if pull_request['parameters']['require_code_owner_review'] is not True:
    raise SystemExit('branch ruleset must require code owner review')
status_checks = next(rule for rule in branch_ruleset['rules'] if rule['type'] == 'required_status_checks')
if status_checks['parameters']['strict_required_status_checks_policy'] is not True:
    raise SystemExit('branch ruleset must require up-to-date branches')
contexts = {item['context'] for item in status_checks['parameters']['required_status_checks']}
if contexts != {'git-policy', 'go-checks'}:
    raise SystemExit(f'unexpected required status checks: {sorted(contexts)}')
required_types = {rule['type'] for rule in branch_ruleset['rules']}
for required in {'required_linear_history', 'non_fast_forward', 'deletion'}:
    if required not in required_types:
        raise SystemExit(f'missing branch rule: {required}')
if branch_ruleset['target'] != 'branch' or branch_ruleset['conditions']['ref_name']['include'] != ['refs/heads/main']:
    raise SystemExit('branch ruleset target/main mismatch')

commit_types = {rule['type'] for rule in commit_policy['rules']}
if 'commit_message_pattern' not in commit_types:
    raise SystemExit('commit policy must require conventional commits')
if commit_policy['conditions']['ref_name']['include'] != ['refs/heads/main']:
    raise SystemExit('commit policy must target main')

expected_settings = {
    'allow_squash_merge': True,
    'allow_merge_commit': False,
    'allow_rebase_merge': False,
    'delete_branch_on_merge': True,
    'allow_auto_merge': False,
}
if settings != expected_settings:
    raise SystemExit(f'unexpected repo settings: {settings!r}')
PY

redacted_stdout="$tmp_dir/redacted.out"
redacted_stderr="$tmp_dir/redacted.err"
set +e
env FAKE_GH_MODE=redact bash -c "source '$apply_script'; run_gh api repos/example-org/example-repo/redaction-check" >"$redacted_stdout" 2>"$redacted_stderr"
status=$?
set -e
if [[ "$status" -ne 0 ]]; then
  echo "run_gh redaction check failed" >&2
  cat "$redacted_stdout" >&2
  cat "$redacted_stderr" >&2
  exit 1
fi
combined_output="$(cat "$redacted_stdout" "$redacted_stderr")"
if grep -qE "${GH_TOKEN}|${GITHUB_TOKEN}|${GH_ENTERPRISE_TOKEN}|${GITHUB_ENTERPRISE_TOKEN}" <<<"$combined_output"; then
  echo "secret leaked in apply output" >&2
  printf '%s\n' "$combined_output" >&2
  exit 1
fi
if ! grep -q '\[REDACTED\]' <<<"$combined_output"; then
  echo "redaction marker missing in apply output" >&2
  printf '%s\n' "$combined_output" >&2
  exit 1
fi

: >"$gh_log"
expect_failure "bad GET JSON should fail" env FAKE_GH_MODE=bad-get-json "$apply_script" --repo example-org/example-repo --execute
if grep -qE 'POST|PATCH' "$gh_log"; then
  echo "malformed GET JSON should stop before mutation" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
expect_failure "bad GET type should fail" env FAKE_GH_MODE=bad-get-type "$apply_script" --repo example-org/example-repo --execute
if grep -qE 'POST|PATCH' "$gh_log"; then
  echo "non-array GET JSON should stop before mutation" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
expect_failure "bad ruleset id should fail" env FAKE_GH_MODE=bad-ruleset-id "$apply_script" --repo example-org/example-repo --execute
if grep -qE 'POST|PATCH' "$gh_log"; then
  echo "invalid ruleset id should stop before mutation" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
expect_failure "bad POST JSON should fail" env FAKE_GH_MODE=bad-post-json "$apply_script" --repo example-org/example-repo --execute
if ! grep -q 'POST' "$gh_log"; then
  echo "bad POST JSON did not reach create path" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
expect_failure "bad PATCH JSON should fail" env FAKE_GH_MODE=bad-patch-json "$apply_script" --repo example-org/example-repo --execute
if ! grep -q 'PATCH' "$gh_log"; then
  echo "bad PATCH JSON did not reach update path" >&2
  cat "$gh_log" >&2
  exit 1
fi

printf 'governance tests passed\n'
