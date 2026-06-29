#!/usr/bin/env bash
set -euo pipefail

repo="${1:-EitanWong/remote-dev-skillkit}"
version="${RDEV_SMOKE_VERSION:-v0.1.0-ci-smoke}"
work_dir="$(mktemp -d /tmp/rdev-release-smoke.XXXXXX)"

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$work_dir/artifacts"
printf 'cli-binary\n' > "$work_dir/artifacts/rdev"
printf 'host-binary\n' > "$work_dir/artifacts/rdev-host.exe"
printf 'verify-binary\n' > "$work_dir/artifacts/rdev-verify.exe"

candidate_dir="$work_dir/candidate"
plan_dir="$work_dir/github-release-plan"

go run ./cmd/rdev release prepare-candidate \
  --source-root . \
  --out "$candidate_dir" \
  --version "$version" \
  --gateway-url https://api.example.com/v1 \
  --artifacts "$work_dir/artifacts/rdev,$work_dir/artifacts/rdev-host.exe,$work_dir/artifacts/rdev-verify.exe" \
  --require-artifacts rdev-host.exe,rdev-verify.exe \
  --key "$work_dir/release-root.json" \
  > "$work_dir/prepare.json"

go run ./cmd/rdev release verify-candidate \
  --candidate "$candidate_dir" \
  --require-artifacts rdev-host.exe,rdev-verify.exe \
  > "$work_dir/verify.json"

scripts/github/plan-release.sh \
  --candidate "$candidate_dir" \
  --repo "$repo" \
  --require-artifacts rdev-host.exe,rdev-verify.exe \
  --out "$plan_dir" \
  > "$work_dir/plan-output.json"

python3 - "$work_dir" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
prepare = json.loads((root / "prepare.json").read_text())
verify = json.loads((root / "verify.json").read_text())
plan_output = json.loads((root / "plan-output.json").read_text())
plan = json.loads(pathlib.Path(plan_output["plan"]).read_text())
commands = pathlib.Path(plan_output["commands"]).read_text()

assert prepare["ok"] is True, prepare
assert verify["ok"] is True, verify
assert plan_output["ok"] is True, plan_output
assert plan["schema_version"] == "rdev.github-release-plan.v1", plan
assert plan["external_mutation"] is False, plan
assert any(asset["kind"] == "skillkit-archive" for asset in plan["assets"]), plan["assets"]
assert len({asset["name"] for asset in plan["assets"]}) == len(plan["assets"]), plan["assets"]
assert "gh release create" in commands, commands
assert "gh release upload" in commands, commands

print(json.dumps({
    "ok": True,
    "candidate_schema": prepare["schema"],
    "verification_schema": verify["schema"],
    "plan_schema": plan["schema_version"],
    "asset_count": len(plan["assets"]),
    "external_mutation": plan["external_mutation"],
}, indent=2))
PY
