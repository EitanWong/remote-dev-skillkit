#!/usr/bin/env bash
set -euo pipefail

repo="${1:-EitanWong/remote-dev-skillkit}"
version="${RDEV_SMOKE_VERSION:-v0.1.0-ci-smoke}"
work_dir="$(mktemp -d /tmp/rdev-release-smoke.XXXXXX)"

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

scripts/release/build-artifacts.sh \
  --out "$work_dir/artifacts" \
  --version "$version" \
  --targets windows/amd64 \
  --commands rdev,rdev-host,rdev-verify \
  > "$work_dir/build.json"

candidate_dir="$work_dir/candidate"
plan_dir="$work_dir/github-release-plan"
artifact_dir="$work_dir/artifacts/windows-amd64"

go run ./cmd/rdev release prepare-candidate \
  --source-root . \
  --out "$candidate_dir" \
  --version "$version" \
  --gateway-url https://api.example.com/v1 \
  --artifacts "$artifact_dir/rdev.exe,$artifact_dir/rdev-host.exe,$artifact_dir/rdev-verify.exe" \
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
build = json.loads((root / "build.json").read_text())
prepare = json.loads((root / "prepare.json").read_text())
verify = json.loads((root / "verify.json").read_text())
plan_output = json.loads((root / "plan-output.json").read_text())
plan = json.loads(pathlib.Path(plan_output["plan"]).read_text())
commands = pathlib.Path(plan_output["commands"]).read_text()

assert build["ok"] is True, build
build_manifest = json.loads(pathlib.Path(build["manifest"]).read_text())
assert build_manifest["schema_version"] == "rdev.build-artifacts.v1", build_manifest
assert len(build_manifest["artifacts"]) == 3, build_manifest["artifacts"]
assert all(artifact["size_bytes"] > 0 for artifact in build_manifest["artifacts"]), build_manifest["artifacts"]
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
    "build_schema": build_manifest["schema_version"],
    "built_artifacts": len(build_manifest["artifacts"]),
    "candidate_schema": prepare["schema"],
    "verification_schema": verify["schema"],
    "plan_schema": plan["schema_version"],
    "asset_count": len(plan["assets"]),
    "external_mutation": plan["external_mutation"],
}, indent=2))
PY
