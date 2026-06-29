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
  --targets linux/amd64,windows/amd64 \
  --commands rdev,rdev-host,rdev-verify \
  > "$work_dir/build.json"

platform_candidates_dir="$work_dir/platform-candidates"
plan_dir="$work_dir/github-release-plan"

scripts/release/prepare-platform-candidates.sh \
  --build-manifest "$work_dir/artifacts/build-artifacts.json" \
  --out "$platform_candidates_dir" \
  --source-root . \
  --gateway-url https://api.example.com/v1 \
  --key "$work_dir/release-root.json" \
  > "$work_dir/platform-candidates-output.json"

windows_candidate="$(python3 - "$work_dir/platform-candidates/platform-candidates.json" <<'PY'
import json
import pathlib
import sys

manifest = json.loads(pathlib.Path(sys.argv[1]).read_text())
for candidate in manifest["candidates"]:
    if candidate["target"] == "windows/amd64":
        print(candidate["candidate_dir"])
        break
else:
    raise SystemExit("windows/amd64 candidate missing")
PY
)"

scripts/github/plan-release.sh \
  --candidate "$windows_candidate" \
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
platform_output = json.loads((root / "platform-candidates-output.json").read_text())
platform_manifest = json.loads(pathlib.Path(platform_output["manifest"]).read_text())
plan_output = json.loads((root / "plan-output.json").read_text())
plan = json.loads(pathlib.Path(plan_output["plan"]).read_text())
commands = pathlib.Path(plan_output["commands"]).read_text()

assert build["ok"] is True, build
build_manifest = json.loads(pathlib.Path(build["manifest"]).read_text())
assert build_manifest["schema_version"] == "rdev.build-artifacts.v1", build_manifest
assert len(build_manifest["artifacts"]) == 6, build_manifest["artifacts"]
assert all(artifact["size_bytes"] > 0 for artifact in build_manifest["artifacts"]), build_manifest["artifacts"]
assert platform_output["ok"] is True, platform_output
assert platform_manifest["schema_version"] == "rdev.platform-release-candidates.v1", platform_manifest
assert platform_manifest["external_mutation"] is False, platform_manifest
assert len(platform_manifest["candidates"]) == 2, platform_manifest["candidates"]
assert {candidate["target"] for candidate in platform_manifest["candidates"]} == {"linux/amd64", "windows/amd64"}, platform_manifest["candidates"]
assert all(candidate["candidate_schema"] == "rdev.release-candidate.v1" for candidate in platform_manifest["candidates"]), platform_manifest["candidates"]
assert all(candidate["verification_schema"] == "rdev.release-candidate-verification.v1" for candidate in platform_manifest["candidates"]), platform_manifest["candidates"]
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
    "platform_candidates_schema": platform_manifest["schema_version"],
    "platform_candidate_count": len(platform_manifest["candidates"]),
    "plan_schema": plan["schema_version"],
    "asset_count": len(plan["assets"]),
    "external_mutation": plan["external_mutation"],
}, indent=2))
PY
