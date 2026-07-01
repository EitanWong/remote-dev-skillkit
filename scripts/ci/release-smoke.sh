#!/usr/bin/env bash
set -euo pipefail

repo="${1:-example/remote-dev-skillkit}"
version="${RDEV_SMOKE_VERSION:-v0.1.0-ci-smoke}"
work_dir="$(mktemp -d /tmp/rdev-release-smoke.XXXXXX)"

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

scripts/audit-public-surface.sh > "$work_dir/public-surface-audit.txt"

go test ./internal/cli \
  -run 'TestGatewayDevMTLSHealthzRequiresClientCertificate|TestHostServeRegistersWithLocalMTLSGateway|TestHostServeMTLSGatewayRejectsMissingClientCertificate|TestHostServePollsAndCompletesDevJobWithLocalMTLSGateway' \
  -count=1 \
  > "$work_dir/dev-mtls-host-smoke.txt"

go test ./internal/model ./internal/cli ./internal/httpapi \
  -run 'TestHostEnrollmentRevocationListAllowsEmptyBaseline|TestEnrollmentInitRevocationsWritesEmptyVerifiedList|TestEnrollmentRevocationsEndpointReturnsEmptyBaseline|TestEnrollmentRevocationsEndpointRequiresIssuerToken' \
  -count=1 \
  > "$work_dir/enrollment-revocation-baseline-smoke.txt"

go test ./internal/cli \
  -run 'TestHostServeFetchesEnrollmentRevocationsBeforeRegistration|TestHostServeReportsFetchedEnrollmentRevocations|TestHostServeSendsIssuerTokenWhenFetchingEnrollmentRevocations|TestHostServeRequiresExplicitEnrollmentRevocationFetch|TestEnrollmentFetchRevocationsSendsIssuerTokenFromFile' \
  -count=1 \
  > "$work_dir/enrollment-host-revocation-refresh-smoke.txt"

go test ./internal/cli ./internal/gateway \
  -run 'TestHostServeRenewsEnrollmentCertificateBeforeRegistration|TestHostServeSkipsEnrollmentRenewalWhenCertificateIsFresh|TestMemoryGatewayEnrollmentRevocationsRejectRenewal' \
  -count=1 \
  > "$work_dir/enrollment-host-renewal-smoke.txt"

go test ./internal/model ./internal/gateway ./internal/httpapi ./internal/cli \
  -run 'TestHostEnrollmentCertificateRenewal|TestMemoryGatewayRenewsEnrollmentCertificate|TestEnrollmentCertificatesRenewEndpoint|TestEnrollmentRenewCertificate' \
  -count=1 \
  > "$work_dir/enrollment-renewal-smoke.txt"

go test ./internal/gateway ./internal/httpapi ./internal/cli \
  -run 'TestMemoryGatewayIssue.*EnrollmentCertificate|TestEnrollmentCertificatesEndpoint|TestEnrollmentIssueCertificate' \
  -count=1 \
  > "$work_dir/enrollment-issuance-smoke.txt"

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

scripts/github/audit-project-readiness.sh \
  --repo "$repo" \
  --out "$work_dir/github-project-readiness.json" \
  > "$work_dir/github-project-readiness-output.json"

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
github_project_readiness = json.loads((root / "github-project-readiness.json").read_text())
post_release_output = json.loads((root / "post-release-output.json").read_text())
post_release_plan = json.loads(pathlib.Path(post_release_output["plan"]).read_text())
post_release_verification = json.loads((root / "post-release-verification.json").read_text())
post_release_tampered = json.loads((root / "post-release-tampered-verification.json").read_text())
skillkit_install_plan_output = json.loads((root / "skillkit-install-plan-output.json").read_text())
skillkit_install_plan_verification = json.loads((root / "skillkit-install-plan-verification.json").read_text())
skillkit_install_dry_run = json.loads((root / "skillkit-install-dry-run.json").read_text())
skillkit_install_execute = json.loads((root / "skillkit-install-execute.json").read_text())
skillkit_manifest = json.loads((pathlib.Path(skillkit_install_plan_output["bundle"]) / "manifest.json").read_text())
commands = pathlib.Path(plan_output["commands"]).read_text()

assert build["ok"] is True, build
build_manifest = json.loads(pathlib.Path(build["manifest"]).read_text())
assert build_manifest["schema_version"] == "rdev.build-artifacts.v1", build_manifest
assert build_manifest["out_dir"] == ".", build_manifest
assert len(build_manifest["artifacts"]) == 6, build_manifest["artifacts"]
assert all(artifact["size_bytes"] > 0 for artifact in build_manifest["artifacts"]), build_manifest["artifacts"]
assert all(artifact["cgo_enabled"] is False for artifact in build_manifest["artifacts"]), build_manifest["artifacts"]
build_sbom_path = pathlib.Path(build["sbom"])
assert build_manifest["sbom_path"] == "sbom.spdx.json", build_manifest
assert build_sbom_path.is_file(), build
build_sbom = json.loads(build_sbom_path.read_text())
assert build_sbom["spdxVersion"] == "SPDX-2.3", build_sbom
assert len(build_sbom["files"]) == len(build_manifest["artifacts"]), build_sbom
build_provenance_path = pathlib.Path(build["provenance"])
assert build_manifest["provenance_path"] == "provenance.json", build_manifest
assert build_provenance_path.is_file(), build
build_provenance = json.loads(build_provenance_path.read_text())
assert build_provenance["schema_version"] == "rdev.release-provenance.v1", build_provenance
assert build_provenance["external_mutation"] is False, build_provenance
assert len(build_provenance["subjects"]) == len(build_manifest["artifacts"]) + 1, build_provenance
build_provenance_subjects = {subject["path"]: subject for subject in build_provenance["subjects"]}
assert "sbom.spdx.json" in build_provenance_subjects, build_provenance
assert all(artifact["path"] in build_provenance_subjects for artifact in build_manifest["artifacts"]), build_provenance
build_checksums = pathlib.Path(build["checksums"]).read_text()
assert "sbom.spdx.json" in build_checksums, build
assert "provenance.json" in build_checksums, build
assert platform_output["ok"] is True, platform_output
assert platform_manifest["schema_version"] == "rdev.platform-release-candidates.v1", platform_manifest
assert platform_manifest["external_mutation"] is False, platform_manifest
assert len(platform_manifest["candidates"]) == 2, platform_manifest["candidates"]
assert {candidate["target"] for candidate in platform_manifest["candidates"]} == {"linux/amd64", "windows/amd64"}, platform_manifest["candidates"]
assert all(candidate["candidate_schema"] == "rdev.release-candidate.v1" for candidate in platform_manifest["candidates"]), platform_manifest["candidates"]
assert all(candidate["verification_schema"] == "rdev.release-candidate-verification.v1" for candidate in platform_manifest["candidates"]), platform_manifest["candidates"]
for candidate in platform_manifest["candidates"]:
    candidate_json = json.loads((pathlib.Path(candidate["candidate_dir"]) / "release-candidate.json").read_text())
    assert candidate_json["out_dir"] == ".", candidate_json
    assert candidate_json["provenance_path"] == "provenance.json", candidate_json
    assert any(file["path"] == "provenance.json" and file["kind"] == "provenance" for file in candidate_json["files"]), candidate_json["files"]
    assert any(file["path"] == "sbom.spdx.json" and file["kind"] == "sbom" for file in candidate_json["files"]), candidate_json["files"]
    sbom = json.loads((pathlib.Path(candidate["candidate_dir"]) / "sbom.spdx.json").read_text())
    assert sbom["spdxVersion"] == "SPDX-2.3", sbom
    artifact_names = {artifact["name"] for artifact in candidate_json["artifacts"]}
    sbom_names = {pathlib.Path(file["fileName"]).name for file in sbom["files"]}
    assert artifact_names == sbom_names, (artifact_names, sbom_names)
    provenance = json.loads((pathlib.Path(candidate["candidate_dir"]) / "provenance.json").read_text())
    assert provenance["schema_version"] == "rdev.release-provenance.v1", provenance
    assert provenance["external_mutation"] is False, provenance
    provenance_artifacts = {subject["path"] for subject in provenance["subjects"] if subject["kind"] == "artifact"}
    assert provenance_artifacts == {artifact["artifact_path"] for artifact in candidate_json["artifacts"]}, provenance
    provenance_support = {subject["path"] for subject in provenance["subjects"] if subject["kind"] != "artifact"}
    assert {"release-bundle.json", "sbom.spdx.json"}.issubset(provenance_support), provenance
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
assert github_project_readiness["schema_version"] == "rdev.github-project-readiness.v1", github_project_readiness
assert github_project_readiness["ok"] is True, github_project_readiness
assert github_project_readiness["external_mutation"] is False, github_project_readiness
assert github_project_readiness["bootstrap_dry_run"]["labels"] >= 19, github_project_readiness
assert github_project_readiness["bootstrap_dry_run"]["milestones"] >= 5, github_project_readiness
assert github_project_readiness["bootstrap_dry_run"]["seed_issues"] >= 9, github_project_readiness
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
assert skillkit_manifest["adaptive_configuration"]["schema_version"] == "rdev.adaptive-configuration-contract.v1", skillkit_manifest
assert skillkit_manifest["adaptive_configuration"]["required"] is True, skillkit_manifest
assert "rdev doctor" in skillkit_manifest["adaptive_configuration"]["probe_before_acting"], skillkit_manifest
assert "rdev mcp tools" in skillkit_manifest["adaptive_configuration"]["probe_before_acting"], skillkit_manifest
assert "framework install path" in skillkit_manifest["adaptive_configuration"]["ask_if_unclear"], skillkit_manifest
assert "https://api.example.com/v1" in skillkit_manifest["adaptive_configuration"]["placeholders"], skillkit_manifest
assert skillkit_install_plan_output["adaptive_configuration_schema"] == "rdev.adaptive-configuration-contract.v1", skillkit_install_plan_output
assert skillkit_install_plan_verification["schema"] == "rdev.skillkit-install-plan-verification.v1", skillkit_install_plan_verification
assert skillkit_install_plan_verification["ok"] is True, skillkit_install_plan_verification
assert skillkit_install_plan_verification["bundle_verify_ok"] is True, skillkit_install_plan_verification
assert skillkit_install_plan_verification["frameworks_verified"] == 6, skillkit_install_plan_verification
assert skillkit_install_plan_verification["adaptive_configuration_verified"] is True, skillkit_install_plan_verification
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
    "build_sbom": True,
    "build_provenance": True,
    "platform_candidates_schema": platform_manifest["schema_version"],
    "platform_candidate_count": len(platform_manifest["candidates"]),
    "plan_schema": plan["schema_version"],
    "github_project_readiness_schema": github_project_readiness["schema_version"],
    "post_release_schema": post_release_plan["schema_version"],
    "post_release_verification_schema": post_release_verification["schema_version"],
    "skillkit_install_plan_schema": skillkit_install_plan_output["schema"],
    "skillkit_install_plan_verification_schema": skillkit_install_plan_verification["schema"],
    "skillkit_install_report_schema": skillkit_install_execute["schema"],
    "skillkit_adaptive_configuration": True,
    "planned_platforms": plan_output["platform_count"],
    "github_project_seed_issues": github_project_readiness["bootstrap_dry_run"]["seed_issues"],
    "post_release_platforms": post_release_output["platform_count"],
    "skillkit_install_frameworks": skillkit_install_plan_output["framework_count"],
    "skillkit_install_executed": skillkit_install_execute["executed"],
    "public_surface_audit": True,
    "dev_mtls_host_smoke": True,
    "enrollment_revocation_baseline_smoke": True,
    "enrollment_host_revocation_refresh_smoke": True,
    "enrollment_revocation_auth_smoke": True,
    "enrollment_host_renewal_smoke": True,
    "enrollment_renewal_smoke": True,
    "enrollment_hosted_renewal_smoke": True,
    "enrollment_issuance_smoke": True,
    "enrollment_operator_auth_smoke": True,
    "asset_count": len(plan["assets"]),
    "candidate_sbom": True,
    "candidate_provenance": True,
    "external_mutation": plan["external_mutation"],
}, indent=2))
PY
