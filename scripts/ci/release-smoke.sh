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

scripts/github/plan-platform-release.sh \
  --platform-candidates "$work_dir/platform-candidates/platform-candidates.json" \
  --repo "$repo" \
  --out "$plan_dir" \
  > "$work_dir/plan-output.json"

post_release_dir="$work_dir/post-release-install"
scripts/github/plan-post-release-install.sh \
  --release-plan "$plan_dir/plan.json" \
  --out "$post_release_dir" \
  > "$work_dir/post-release-output.json"

scripts/github/verify-post-release-install-plan.sh \
  --plan "$post_release_dir/post-release-install-plan.json" \
  > "$work_dir/post-release-verification.json"

first_skillkit_dir="$(python3 - "$platform_candidates_dir/platform-candidates.json" <<'PY'
import json
import pathlib
import sys

manifest = json.loads(pathlib.Path(sys.argv[1]).read_text())
candidates = manifest.get("candidates") or []
if not candidates:
    raise SystemExit("no platform candidates")
print(pathlib.Path(candidates[0]["candidate_dir"]) / "skillkit")
PY
)"
skillkit_install_dir="$work_dir/skillkit-install-plan"
go run ./cmd/rdev skillkit plan-install \
  --bundle "$first_skillkit_dir" \
  --out "$skillkit_install_dir" \
  --frameworks codex,claude-code,hermes,openclaw,opencode,generic-mcp-agent \
  > "$work_dir/skillkit-install-plan-output.json"
go run ./cmd/rdev skillkit verify-install-plan \
  --plan "$skillkit_install_dir/install-plan.json" \
  > "$work_dir/skillkit-install-plan-verification.json"
skillkit_install_target="$work_dir/skillkit-install-target"
go run ./cmd/rdev skillkit install \
  --bundle "$first_skillkit_dir" \
  --framework codex \
  --target "$skillkit_install_target" \
  > "$work_dir/skillkit-install-dry-run.json"
go run ./cmd/rdev skillkit install \
  --bundle "$first_skillkit_dir" \
  --framework codex \
  --target "$skillkit_install_target" \
  --execute \
  > "$work_dir/skillkit-install-execute.json"

cp -R "$post_release_dir" "$work_dir/post-release-install-tampered"
printf '\nNew-Service rdev\n' >> "$work_dir/post-release-install-tampered/verify-windows-amd64.ps1"
if scripts/github/verify-post-release-install-plan.sh \
  --plan "$work_dir/post-release-install-tampered/post-release-install-plan.json" \
  > "$work_dir/post-release-tampered-verification.json"; then
  echo "tampered post-release install plan unexpectedly verified" >&2
  exit 1
fi

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
post_release_output = json.loads((root / "post-release-output.json").read_text())
post_release_plan = json.loads(pathlib.Path(post_release_output["plan"]).read_text())
post_release_verification = json.loads((root / "post-release-verification.json").read_text())
post_release_tampered = json.loads((root / "post-release-tampered-verification.json").read_text())
skillkit_install_plan_output = json.loads((root / "skillkit-install-plan-output.json").read_text())
skillkit_install_plan_verification = json.loads((root / "skillkit-install-plan-verification.json").read_text())
skillkit_install_dry_run = json.loads((root / "skillkit-install-dry-run.json").read_text())
skillkit_install_execute = json.loads((root / "skillkit-install-execute.json").read_text())
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
assert plan["schema_version"] == "rdev.github-platform-release-plan.v1", plan
assert plan["external_mutation"] is False, plan
assert plan_output["platform_count"] == 2, plan_output
assert any(asset["kind"] == "platform-candidate-archive" and "linux-amd64" in asset["name"] for asset in plan["assets"]), plan["assets"]
assert any(asset["kind"] == "platform-candidate-archive" and "windows-amd64" in asset["name"] for asset in plan["assets"]), plan["assets"]
assert any(asset["kind"] == "platform-release-index" for asset in plan["assets"]), plan["assets"]
assert any(asset["kind"] == "install-guide" for asset in plan["assets"]), plan["assets"]
assert any(asset["kind"] == "skillkit-archive" for asset in plan["assets"]), plan["assets"]
assert len({asset["name"] for asset in plan["assets"]}) == len(plan["assets"]), plan["assets"]
assert "gh release create" in commands, commands
assert "gh release upload" in commands, commands
assert post_release_output["ok"] is True, post_release_output
assert post_release_plan["schema_version"] == "rdev.post-release-install-plan.v1", post_release_plan
assert post_release_plan["external_mutation"] is False, post_release_plan
assert post_release_output["platform_count"] == 2, post_release_output
assert {platform["target"] for platform in post_release_plan["platforms"]} == {"linux/amd64", "windows/amd64"}, post_release_plan["platforms"]
assert all(platform["archive"]["download_url"].startswith("https://github.com/") for platform in post_release_plan["platforms"]), post_release_plan["platforms"]
assert any("rdev release verify-candidate" in "\n".join(platform["commands"]) for platform in post_release_plan["platforms"]), post_release_plan["platforms"]
assert any("rdev-verify" in "\n".join(platform["commands"]) for platform in post_release_plan["platforms"]), post_release_plan["platforms"]
post_release_plan_dir = pathlib.Path(post_release_output["plan"]).parent
assert (post_release_plan_dir / post_release_plan["documents"]["verify_install"]).is_file(), post_release_plan["documents"]
assert (post_release_plan_dir / post_release_plan["skillkit"]["verification_script"]).is_file(), post_release_plan["skillkit"]
assert post_release_verification["schema_version"] == "rdev.post-release-install-verification.v1", post_release_verification
assert post_release_verification["ok"] is True, post_release_verification
assert post_release_verification["external_mutation"] is False, post_release_verification
assert post_release_tampered["schema_version"] == "rdev.post-release-install-verification.v1", post_release_tampered
assert post_release_tampered["ok"] is False, post_release_tampered
assert any(check["name"].endswith("script_matches_commands") for check in post_release_tampered["failed_checks"]), post_release_tampered
assert any(check["name"].endswith("script_no_forbidden_side_effects") for check in post_release_tampered["failed_checks"]), post_release_tampered
assert skillkit_install_plan_output["schema"] == "rdev.skillkit-install-plan.v1", skillkit_install_plan_output
assert skillkit_install_plan_output["ok"] is True, skillkit_install_plan_output
assert skillkit_install_plan_output["external_mutation"] is False, skillkit_install_plan_output
assert skillkit_install_plan_output["framework_count"] == 6, skillkit_install_plan_output
assert skillkit_install_plan_verification["schema"] == "rdev.skillkit-install-plan-verification.v1", skillkit_install_plan_verification
assert skillkit_install_plan_verification["ok"] is True, skillkit_install_plan_verification
assert skillkit_install_plan_verification["bundle_verify_ok"] is True, skillkit_install_plan_verification
assert skillkit_install_plan_verification["frameworks_verified"] == 6, skillkit_install_plan_verification
assert skillkit_install_dry_run["schema"] == "rdev.skillkit-install-report.v1", skillkit_install_dry_run
assert skillkit_install_dry_run["ok"] is True, skillkit_install_dry_run
assert skillkit_install_dry_run["execute"] is False, skillkit_install_dry_run
assert skillkit_install_dry_run["executed"] is False, skillkit_install_dry_run
assert skillkit_install_dry_run["local_mutation"] is False, skillkit_install_dry_run
assert skillkit_install_dry_run["external_mutation"] is False, skillkit_install_dry_run
assert skillkit_install_execute["schema"] == "rdev.skillkit-install-report.v1", skillkit_install_execute
assert skillkit_install_execute["ok"] is True, skillkit_install_execute
assert skillkit_install_execute["execute"] is True, skillkit_install_execute
assert skillkit_install_execute["executed"] is True, skillkit_install_execute
assert skillkit_install_execute["local_mutation"] is True, skillkit_install_execute
assert skillkit_install_execute["external_mutation"] is False, skillkit_install_execute
install_target = pathlib.Path(skillkit_install_execute["target"])
assert (install_target / "remote-vibe-coding" / "SKILL.md").is_file(), skillkit_install_execute
assert (install_target / ".remote-dev-skillkit" / "mcp" / "tools.json").is_file(), skillkit_install_execute

print(json.dumps({
    "ok": True,
    "build_schema": build_manifest["schema_version"],
    "built_artifacts": len(build_manifest["artifacts"]),
    "platform_candidates_schema": platform_manifest["schema_version"],
    "platform_candidate_count": len(platform_manifest["candidates"]),
    "plan_schema": plan["schema_version"],
    "post_release_schema": post_release_plan["schema_version"],
    "post_release_verification_schema": post_release_verification["schema_version"],
    "skillkit_install_plan_schema": skillkit_install_plan_output["schema"],
    "skillkit_install_plan_verification_schema": skillkit_install_plan_verification["schema"],
    "skillkit_install_report_schema": skillkit_install_execute["schema"],
    "planned_platforms": plan_output["platform_count"],
    "post_release_platforms": post_release_output["platform_count"],
    "skillkit_install_frameworks": skillkit_install_plan_output["framework_count"],
    "skillkit_install_executed": skillkit_install_execute["executed"],
    "asset_count": len(plan["assets"]),
    "external_mutation": plan["external_mutation"],
}, indent=2))
PY
