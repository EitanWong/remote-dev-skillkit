#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
policy_script="$repo_root/scripts/ci/git-policy.sh"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/git-policy-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

setup_repo() {
  local name="$1"
  local dest="$tmp_dir/$name"
  mkdir -p "$dest"
  python3 - "$repo_root" "$dest" <<'PY'
import os
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
  if output="$(
    cd "$repo"
    env "$@" bash "$policy_script" 2>&1
  )"; then
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

main_repo="$(setup_repo main)"
assert_success "$main_repo" \
  GITHUB_EVENT_NAME=push \
  GITHUB_REF_NAME='main'

valid_repo="$(setup_repo valid)"
git -C "$valid_repo" checkout -b 'feat/123-valid-name' >/dev/null
assert_success "$valid_repo" \
  GITHUB_EVENT_NAME=push \
  GITHUB_REF_NAME='feat/123-valid-name'

legacy_repo="$(setup_repo legacy)"
git -C "$legacy_repo" checkout -b 'codex/123-old-name' >/dev/null
assert_failure "$legacy_repo" 'legacy_codex_branch_forbidden' \
  GITHUB_EVENT_NAME=push \
  GITHUB_REF_NAME='codex/123-old-name'

pr_base_repo="$(setup_repo pr-base)"
git -C "$pr_base_repo" checkout -b 'feat/123-valid-name' >/dev/null
assert_failure "$pr_base_repo" 'pull requests must target main' \
  GITHUB_EVENT_NAME=pull_request \
  GITHUB_HEAD_REF='feat/123-valid-name' \
  GITHUB_BASE_REF='release/1.0'

pr_repo="$(setup_repo pr-valid)"
git -C "$pr_repo" checkout -b 'feat/123-valid-name' >/dev/null
assert_success "$pr_repo" \
  GITHUB_EVENT_NAME=pull_request \
  GITHUB_HEAD_REF='feat/123-valid-name' \
  GITHUB_BASE_REF='main' \
  PR_TITLE='feat: add git policy workflow' \
  PR_BODY=$'## Summary\n- add a git policy workflow\n\nCloses #123'

printf 'git policy wrapper tests passed\n'
