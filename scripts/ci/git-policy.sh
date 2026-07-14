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
  local event_name="${GIT_POLICY_EVENT_NAME:-${GITHUB_EVENT_NAME:-push}}"
  case "$event_name" in
    pull_request|pull_request_target)
      printf 'pull_request\n'
      ;;
    *)
      printf 'push\n'
      ;;
  esac
}

resolve_branch_name() {
  local branch_name="${GIT_POLICY_BRANCH_NAME:-${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}}"
  if [[ -z "$branch_name" ]]; then
    branch_name="$(git branch --show-current 2>/dev/null || true)"
  fi
  printf '%s\n' "$branch_name"
}

validate_git_branch_ref() {
  local branch_name="$1"
  [[ -n "$branch_name" ]] || fail 'branch name is required'
  git check-ref-format --branch "$branch_name" >/dev/null 2>&1 || \
    fail "branch name is not a valid git ref: $branch_name"
}

prepare_branch_context() {
  local branch_name="$1"
  validate_git_branch_ref "$branch_name"

  local current_branch
  current_branch="$(git branch --show-current 2>/dev/null || true)"
  if [[ "$current_branch" == "$branch_name" ]]; then
    return 0
  fi

  git branch --force "$branch_name" HEAD >/dev/null
  git switch --quiet "$branch_name" >/dev/null
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
  [[ -n "$pr_title" ]] || fail 'GIT_POLICY_PR_TITLE is required for pull_request validation'
  [[ -n "$pr_body" ]] || fail 'GIT_POLICY_PR_BODY is required for pull_request validation'

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

  local branch_name
  branch_name="$(resolve_branch_name)"

  if [[ "$event_name" == 'push' && "$branch_name" == 'main' ]]; then
    printf 'git policy skipped for protected branch %s\n' "$branch_name"
    return 0
  fi

  prepare_branch_context "$branch_name"
  run_policy_check 'origin/main'

  if [[ "$event_name" == 'pull_request' ]]; then
    run_pr_validation \
      "${GIT_POLICY_BASE_REF:-${GITHUB_BASE_REF:-}}" \
      "${GIT_POLICY_PR_TITLE:-${PR_TITLE:-${GITHUB_PR_TITLE:-}}}" \
      "${GIT_POLICY_PR_BODY:-${PR_BODY:-${GITHUB_PR_BODY:-}}}"
  fi
}

main "$@"
