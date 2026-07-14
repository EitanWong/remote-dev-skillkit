#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
policy_script="$repo_root/scripts/ci/git-policy.sh"
workflow_file="$repo_root/.github/workflows/git-policy.yml"
ci_workflow_file="$repo_root/.github/workflows/ci.yml"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/git-policy-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local file="$1"
  local needle="$2"
  grep -F "$needle" "$file" >/dev/null || die "expected $file to contain: $needle"
}

assert_not_contains() {
  local file="$1"
  local needle="$2"
  if grep -F "$needle" "$file" >/dev/null; then
    die "did not expect $file to contain: $needle"
  fi
}

assert_not_line() {
  local file="$1"
  local line="$2"
  if grep -Fx "$line" "$file" >/dev/null; then
    die "did not expect $file to contain exact line: $line"
  fi
}

setup_repo() {
  local name="$1"
  local dest="$tmp_dir/$name"
  mkdir -p "$dest"
  python3 - "$repo_root" "$dest" <<'PY'
import shutil
import sys
from pathlib import Path

src = Path(sys.argv[1])
dest = Path(sys.argv[2])
for entry in src.iterdir():
    if entry.name in {'.git', '.superpowers'}:
        continue
    target = dest / entry.name
    if entry.is_dir():
        shutil.copytree(entry, target, symlinks=True)
    else:
        shutil.copy2(entry, target, follow_symlinks=False)
PY
  git -C "$dest" init >/dev/null
  git -C "$dest" config user.name 'Task Seven Test'
  git -C "$dest" config user.email 'task7@example.com'
  git -C "$dest" config core.hooksPath /dev/null
  git -C "$dest" add .
  git -C "$dest" commit -m 'test: seed repo' >/dev/null
  printf '%s\n' "$dest"
}

assert_success() {
  local repo="$1"
  shift
  if ! (
    cd "$repo"
    env "$@" bash "$policy_script"
  ); then
    die "expected success for repo $repo"
  fi
}

assert_failure() {
  local repo="$1"
  local want="$2"
  shift 2
  local output
  if output="$({
    cd "$repo"
    env "$@" bash "$policy_script" 2>&1
  })"; then
    printf '%s\n' "$output" >&2
    die "expected failure for repo $repo"
  fi
  if [[ "$output" != *"$want"* ]]; then
    printf 'missing expected text: %s\nfull output:\n%s\n' "$want" "$output" >&2
    exit 1
  fi
}

if [[ ! -f "$policy_script" ]]; then
  die "missing policy script at $policy_script"
fi
if [[ ! -f "$workflow_file" ]]; then
  die "missing workflow file at $workflow_file"
fi
if [[ ! -f "$ci_workflow_file" ]]; then
  die "missing CI workflow file at $ci_workflow_file"
fi

assert_contains "$workflow_file" 'pull_request_target:'
assert_contains "$workflow_file" '    branches:'
assert_contains "$workflow_file" "      - '**'"
assert_not_line "$workflow_file" '  pull_request:'
assert_contains "$workflow_file" '  git-policy:'
assert_contains "$workflow_file" '    name: git-policy'
assert_contains "$workflow_file" 'persist-credentials: false'
assert_contains "$workflow_file" "ref: \${{ github.event_name == 'pull_request_target' && github.event.pull_request.base.ref || github.ref_name }}"
assert_not_contains "$workflow_file" 'ref: ${{ github.head_ref || github.ref_name }}'
assert_not_contains "$workflow_file" 'ref: ${{ github.event.pull_request.head.ref }}'

assert_contains "$ci_workflow_file" 'pull_request:'
assert_contains "$ci_workflow_file" '  go-checks:'
assert_contains "$ci_workflow_file" '    name: go-checks'
assert_contains "$ci_workflow_file" '  release-smoke:'

main_repo="$(setup_repo main)"
assert_success "$main_repo" \
  GIT_POLICY_EVENT_NAME=push \
  GIT_POLICY_BRANCH_NAME='main'

valid_repo="$(setup_repo valid)"
assert_success "$valid_repo" \
  GIT_POLICY_EVENT_NAME=push \
  GIT_POLICY_BRANCH_NAME='feat/123-valid-name'

legacy_repo="$(setup_repo legacy)"
assert_failure "$legacy_repo" 'legacy_codex_branch_forbidden' \
  GIT_POLICY_EVENT_NAME=push \
  GIT_POLICY_BRANCH_NAME='codex/123-old-name'

pr_base_repo="$(setup_repo pr-base)"
assert_failure "$pr_base_repo" 'pull requests must target main' \
  GIT_POLICY_EVENT_NAME=pull_request_target \
  GIT_POLICY_BRANCH_NAME='feat/123-valid-name' \
  GIT_POLICY_BASE_REF='release/1.0' \
  GIT_POLICY_PR_TITLE='feat: add trusted policy workflow' \
  GIT_POLICY_PR_BODY=$'## Summary\n- validate trusted base checkout\n\nCloses #123'

pr_repo="$(setup_repo pr-valid)"
head_before="$(git -C "$pr_repo" rev-parse HEAD)"
assert_success "$pr_repo" \
  GIT_POLICY_EVENT_NAME=pull_request_target \
  GIT_POLICY_BRANCH_NAME='feat/123-valid-name' \
  GIT_POLICY_HEAD_SHA='deadbeef1234' \
  GIT_POLICY_BASE_REF='main' \
  GIT_POLICY_PR_TITLE='feat: add trusted git policy workflow' \
  GIT_POLICY_PR_BODY=$'## Summary\n- validate trusted base checkout\n\nCloses #123'
head_after="$(git -C "$pr_repo" rev-parse HEAD)"
[[ "$head_before" == "$head_after" ]] || die 'trusted PR validation must not change HEAD commit'
current_branch="$(git -C "$pr_repo" branch --show-current)"
[[ "$current_branch" == 'feat/123-valid-name' ]] || die "expected local metadata branch context, got $current_branch"

printf 'git policy wrapper tests passed\n'
