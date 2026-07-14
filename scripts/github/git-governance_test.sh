#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/git-governance-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"

gh_log="$tmp_dir/gh.log"
plan_out="$tmp_dir/plan.json"
apply_out="$tmp_dir/apply.out"
apply_err="$tmp_dir/apply.err"
gh_token_value="$(python3 - <<'PY'
import secrets

print("fake-gh-token-" + secrets.token_hex(8))
PY
)"

cat > "$fake_bin/gh" <<'GH'
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

if [[ "$method" == "GET" && "$path" == repos/*/rulesets* && "$path" == *"targets=branch"* ]]; then
  printf '[]\n'
elif [[ "$method" == "POST" && "$path" == repos/*/rulesets ]]; then
  printf '{"ok":true,"credential":"%s","path":"%s"}\n' "${FAKE_GH_TOKEN:?}" "$path"
elif [[ "$method" == "PATCH" && "$path" == repos/*/rulesets/* ]]; then
  printf '{"ok":true,"credential":"%s","path":"%s"}\n' "${FAKE_GH_TOKEN:?}" "$path"
elif [[ "$method" == "PATCH" && "$path" == repos/* ]]; then
  printf '{"ok":true,"credential":"%s","path":"%s"}\n' "${FAKE_GH_TOKEN:?}" "$path"
else
  printf '{"ok":true,"credential":"%s","path":"%s"}\n' "${FAKE_GH_TOKEN:?}" "$path"
fi
GH
chmod +x "$fake_bin/gh"

export GH_LOG="$gh_log"
export FAKE_GH_TOKEN="$gh_token_value"
export PATH="$fake_bin:$PATH"

plan_script="$repo_root/scripts/github/plan-git-governance.sh"
apply_script="$repo_root/scripts/github/apply-git-governance.sh"

plan_stdout="$tmp_dir/plan.out"
"$plan_script" --repo example-org/example-repo >"$plan_stdout"
python3 - "$plan_stdout" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
assert payload["schema_version"] == "rdev.github-governance-plan.v1"
assert payload["repo"] == "example-org/example-repo"
assert payload["external_mutation"] is False
assert payload["governance"]["branch_ruleset"]["rules"][0]["type"] == "pull_request"
assert payload["governance"]["commit_policy"]["rules"][0]["type"] == "commit_message_pattern"
assert payload["repo_settings"]["delete_branch_on_merge"] is True
PY

if "$apply_script" --repo example-org/example-repo >"$apply_out" 2>"$apply_err"; then
  echo "apply without --execute unexpectedly succeeded" >&2
  exit 1
fi
if [[ -s "$gh_log" ]]; then
  echo "apply without --execute invoked gh" >&2
  cat "$gh_log" >&2
  exit 1
fi

: > "$gh_log"
"$apply_script" --repo example-org/example-repo --execute >"$apply_out" 2>"$apply_err"
combined_output="$(cat "$apply_out" "$apply_err")"
if grep -q "$gh_token_value" <<<"$combined_output"; then
  echo "secret leaked in apply output" >&2
  printf '%s\n' "$combined_output" >&2
  exit 1
fi
if ! grep -q '\[REDACTED\]' <<<"$combined_output"; then
  echo "redaction marker not present in apply output" >&2
  printf '%s\n' "$combined_output" >&2
  exit 1
fi
python3 - "$gh_log" <<'PY'
import pathlib
import sys

lines = pathlib.Path(sys.argv[1]).read_text().strip().splitlines()
assert lines, "expected gh invocations"
normalized = [line.replace("\\", "") for line in lines]
assert any("repos/example-org/example-repo/rulesets?per_page=100&targets=branch" in line for line in normalized)
assert any("repos/example-org/example-repo/rulesets" in line for line in normalized)
assert any("repos/example-org/example-repo" in line for line in normalized)
PY

printf 'governance tests passed\n'
