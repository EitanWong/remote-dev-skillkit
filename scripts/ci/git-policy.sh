#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$repo_root" ]]; then
  repo_root="$(cd "$script_dir/../.." && pwd)"
fi

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

normalize_event() {
  local event_name="${GITHUB_EVENT_NAME:-push}"
  case "$event_name" in
    pull_request|pull_request_target)
      printf 'pull_request\n'
      ;;
    *)
      printf 'push\n'
      ;;
  esac
}

ensure_local_branch() {
  local branch_name="$1"
  [[ -n "$branch_name" ]] || fail 'branch name is required'

  local current_branch
  current_branch="$(git branch --show-current 2>/dev/null || true)"
  if [[ "$current_branch" == "$branch_name" ]]; then
    return 0
  fi

  git update-ref "refs/heads/$branch_name" HEAD
  git switch --force-create "$branch_name" --quiet >/dev/null 2>&1 || git switch "$branch_name" --quiet >/dev/null
}

run_policy_check() {
  local base_ref="$1"
  GO111MODULE=on go run ./cmd/rdev git policy check --repo . --base "$base_ref"
}

run_pr_validation() {
  local pr_base="$1"
  local pr_title="$2"
  local pr_body="$3"

  [[ "$pr_base" == 'main' ]] || fail "pull requests must target main; got $pr_base"
  [[ -n "$pr_title" ]] || fail 'PR_TITLE is required for pull_request validation'
  [[ -n "$pr_body" ]] || fail 'PR_BODY is required for pull_request validation'

  GO111MODULE=on go run ./cmd/rdev git pr plan \
    --repo . \
    --base "$pr_base" \
    --title "$pr_title" \
    --body "$pr_body"
}

main() {
  require_command git
  require_command go

  cd "$repo_root"

  local event_name
  event_name="$(normalize_event)"
  local branch_name="${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}"
  if [[ -z "$branch_name" ]]; then
    branch_name="$(git branch --show-current 2>/dev/null || true)"
  fi

  if [[ "$event_name" == 'push' && "$branch_name" == 'main' ]]; then
    printf 'git policy skipped for protected branch %s\n' "$branch_name"
    return 0
  fi

  if [[ -n "$branch_name" ]]; then
    ensure_local_branch "$branch_name"
  fi

  run_policy_check 'origin/main'

  if [[ "$event_name" == 'pull_request' ]]; then
    run_pr_validation \
      "${GITHUB_BASE_REF:-}" \
      "${PR_TITLE:-${GITHUB_PR_TITLE:-}}" \
      "${PR_BODY:-${GITHUB_PR_BODY:-}}"
  fi
}

main "$@"
