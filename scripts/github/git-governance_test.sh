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

printf '%q ' "$0" "$@" >> "${GH_LOG:?}"
printf '\n' >> "${GH_LOG:?}"

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

emit_secret_streams() {
  printf 'stdout:%s:%s\n' "${GH_TOKEN:-}" "${GITHUB_TOKEN:-}"
  printf 'stdout:%s:%s\n' "${GH_ENTERPRISE_TOKEN:-}" "${GITHUB_ENTERPRISE_TOKEN:-}"
  printf 'stderr:%s:%s\n' "${GH_TOKEN:-}" "${GITHUB_TOKEN:-}" >&2
  printf 'stderr:%s:%s\n' "${GH_ENTERPRISE_TOKEN:-}" "${GITHUB_ENTERPRISE_TOKEN:-}" >&2
}

emit_secret_stderr() {
  printf 'stderr:%s:%s\n' "${GH_TOKEN:-}" "${GITHUB_TOKEN:-}" >&2
  printf 'stderr:%s:%s\n' "${GH_ENTERPRISE_TOKEN:-}" "${GITHUB_ENTERPRISE_TOKEN:-}" >&2
}

case "${FAKE_GH_MODE:-happy}" in
  redact)
    printf '{"ok":true,"path":"%s"}\n' "$path"
    emit_secret_streams
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
      printf '{"ok":true,"kind":"create","path":"%s"}\n' "$path"
      emit_secret_stderr
    elif [[ "$method" == "PATCH" && "$path" == repos/*/rulesets/* ]]; then
      printf '{"ok":true,"kind":"update","path":"%s"}\n' "$path"
      emit_secret_stderr
    elif [[ "$method" == "PATCH" && "$path" == repos/* ]]; then
      printf '{"ok":true,"kind":"settings","path":"%s"}\n' "$path"
      emit_secret_stderr
    else
      printf '{"ok":true,"path":"%s"}\n' "$path"
      emit_secret_stderr
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

source "$apply_script"

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

count_gh_calls() {
  if [[ -s "$gh_log" ]]; then
    grep -cve '^[[:space:]]*$' "$gh_log"
  else
    echo 0
  fi
}

if ! "$plan_script" --repo example-org/example-repo >/dev/null; then
  echo "plan script failed" >&2
  exit 1
fi

export FAKE_GH_MODE=happy
env FAKE_GH_MODE=happy bash -c "source '$apply_script'; main --repo example-org/example-repo --execute" >/tmp/git-governance-apply.out 2>/tmp/git-governance-apply.err
combined_output="$(cat /tmp/git-governance-apply.out /tmp/git-governance-apply.err)"
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
if [[ "$(count_gh_calls)" -lt 3 ]]; then
  echo "expected multiple gh calls during apply" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
export FAKE_GH_MODE=bad-get-json
set +e
env FAKE_GH_MODE=bad-get-json bash -c "source '$apply_script'; main --repo example-org/example-repo --execute" >/tmp/git-governance-bad-get-json.out 2>/tmp/git-governance-bad-get-json.err
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  echo "malformed GET JSON unexpectedly succeeded" >&2
  exit 1
fi
if [[ "$(count_gh_calls)" -ne 1 ]]; then
  echo "malformed GET JSON should stop after the first gh call" >&2
  cat "$gh_log" >&2
  exit 1
fi
if grep -q 'POST\|PATCH' "$gh_log"; then
  echo "malformed GET JSON triggered mutation" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
export FAKE_GH_MODE=bad-get-type
set +e
env FAKE_GH_MODE=bad-get-type bash -c "source '$apply_script'; main --repo example-org/example-repo --execute" >/tmp/git-governance-bad-get-type.out 2>/tmp/git-governance-bad-get-type.err
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  echo "non-array GET JSON unexpectedly succeeded" >&2
  exit 1
fi
if grep -q 'POST\|PATCH' "$gh_log"; then
  echo "non-array GET JSON triggered mutation" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
export FAKE_GH_MODE=bad-ruleset-id
set +e
env FAKE_GH_MODE=bad-ruleset-id bash -c "source '$apply_script'; main --repo example-org/example-repo --execute" >/tmp/git-governance-bad-ruleset-id.out 2>/tmp/git-governance-bad-ruleset-id.err
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  echo "invalid ruleset id unexpectedly succeeded" >&2
  exit 1
fi
if grep -q 'POST\|PATCH' "$gh_log"; then
  echo "invalid ruleset id triggered mutation" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
export FAKE_GH_MODE=bad-post-json
set +e
env FAKE_GH_MODE=bad-post-json bash -c "source '$apply_script'; main --repo example-org/example-repo --execute" >/tmp/git-governance-bad-post-json.out 2>/tmp/git-governance-bad-post-json.err
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  echo "malformed POST JSON unexpectedly succeeded" >&2
  exit 1
fi
if ! grep -q 'POST' "$gh_log"; then
  echo "malformed POST JSON did not reach create path" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
export FAKE_GH_MODE=bad-patch-json
set +e
env FAKE_GH_MODE=bad-patch-json bash -c "source '$apply_script'; main --repo example-org/example-repo --execute" >/tmp/git-governance-bad-patch-json.out 2>/tmp/git-governance-bad-patch-json.err
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  echo "malformed PATCH JSON unexpectedly succeeded" >&2
  exit 1
fi
if ! grep -q 'PATCH' "$gh_log"; then
  echo "malformed PATCH JSON did not reach update path" >&2
  cat "$gh_log" >&2
  exit 1
fi

: >"$gh_log"
unset FAKE_GH_MODE
set +e
env FAKE_GH_MODE=happy bash -c "source '$apply_script'; main --repo 'bad/repo/extra' --execute" >/tmp/git-governance-bad-repo.out 2>/tmp/git-governance-bad-repo.err
status=$?
set -e
if [[ "$status" -eq 0 ]]; then
  echo "invalid repo unexpectedly succeeded" >&2
  exit 1
fi
if [[ -s "$gh_log" ]]; then
  echo "invalid repo should fail before gh is called" >&2
  cat "$gh_log" >&2
  exit 1
fi

set +e
export FAKE_GH_MODE=redact
run_gh api repos/example-org/example-repo/redaction-check >/tmp/git-governance-run-gh.out 2>/tmp/git-governance-run-gh.err
status=$?
set -e
if [[ "$status" -ne 0 ]]; then
  echo "run_gh direct call failed" >&2
  exit 1
fi
if grep -qE "${GH_TOKEN}|${GITHUB_TOKEN}|${GH_ENTERPRISE_TOKEN}|${GITHUB_ENTERPRISE_TOKEN}" /tmp/git-governance-run-gh.out /tmp/git-governance-run-gh.err; then
  echo "run_gh leaked a secret" >&2
  cat /tmp/git-governance-run-gh.out >&2
  cat /tmp/git-governance-run-gh.err >&2
  exit 1
fi
if ! grep -q '\[REDACTED\]' /tmp/git-governance-run-gh.out; then
  echo "stdout redaction marker missing" >&2
  cat /tmp/git-governance-run-gh.out >&2
  exit 1
fi
if ! grep -q '\[REDACTED\]' /tmp/git-governance-run-gh.err; then
  echo "stderr redaction marker missing" >&2
  cat /tmp/git-governance-run-gh.err >&2
  exit 1
fi

printf 'governance tests passed\n'
