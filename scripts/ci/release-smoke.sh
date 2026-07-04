#!/usr/bin/env bash
set -euo pipefail

repo="${1:-example/remote-dev-skillkit}"
version="${RDEV_SMOKE_VERSION:-v0.1.0-ci-smoke}"
work_dir="$(mktemp -d /tmp/rdev-release-smoke.XXXXXX)"

cleanup() {
  if [ -n "${runner_gateway_pid:-}" ]; then
    kill "$runner_gateway_pid" 2>/dev/null || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

scripts/audit-public-surface.sh > "$work_dir/public-surface-audit.txt"

go run ./cmd/rdev acceptance fresh-agent-support-session \
  --out "$work_dir/fresh-agent-support-session" \
  > "$work_dir/fresh-agent-support-session-output.json"

go test ./internal/cli \
  -run 'TestGatewayDevMTLSHealthzRequiresClientCertificate|TestHostServeRegistersWithLocalMTLSGateway|TestHostServeMTLSGatewayRejectsMissingClientCertificate|TestHostServePollsAndCompletesDevJobWithLocalMTLSGateway' \
  -count=1 \
  > "$work_dir/dev-mtls-host-smoke.txt"

go test ./internal/wsproto ./internal/cli \
  -run 'TestHTTPToWebSocketURL|TestUpgradeAndDialExchangeJSON|TestHostServeWSSCompletesDevJob|TestHostServeWSSCompletesDevJobWithLocalMTLSGateway' \
  -count=1 \
  > "$work_dir/wss-mtls-host-smoke.txt"

go test ./internal/gateway ./internal/httpapi ./internal/operatorauth ./internal/cli \
  -run 'TestFileStateStoreRoundTrip|TestPostgresStateStoreRoundTripThroughPSQL|TestPostgresStateStoreVerifyRuntime|TestRedisStreamStateStoreRoundTripThroughRedisCLI|TestRedisStreamStateStoreVerifyRuntime|TestS3CompatibleStateStoreRoundTripThroughAWSCLI|TestS3CompatibleStateStoreVerifyRuntime|TestServerStateStorePersistsGatewayMutations|TestGatewayStorageVerifyFileProvider|TestGatewayStorageVerifyPostgresRejectsInlinePassword|TestGatewayStorageVerifyRedisRejectsInlineCredentials|TestGatewayStorageVerifyS3CompatibleRejectsUnsafeLocation|TestHostedIssuerAuthorizesSignedJWTByRole|TestCombinedAuthorizerAcceptsLocalAndHostedSources|TestOIDCJWKSVerifierAuthorizesRS256TokenByRole|TestOIDCJWKSVerifierRejectsUnsafeJWKSURL|TestOperatorAuthVerifyHosted|TestOperatorAuthVerifyOIDCJWKSWithToken' \
  -count=1 \
  > "$work_dir/hosted-storage-auth-smoke.txt"

go test ./internal/hostedprovider ./internal/cli \
	-run 'TestBuildAndVerifyHostedProviderPackage|TestBuildPostgresHostedJWTProviderUsesBuiltInGatewayRuntime|TestBuildRedisHostedJWTProviderUsesBuiltInGatewayRuntime|TestBuildS3CompatibleHostedJWTProviderUsesBuiltInGatewayRuntime|TestBuildPostgresOIDCJWKSProviderUsesBuiltInGatewayRuntime|TestVerifyHostedProviderPackageDetectsTamperedFile|TestHostedProviderPackageAndVerify|TestHostedProviderRedisHostedJWTUsesBuiltInGatewayRuntime|TestHostedProviderS3CompatibleHostedJWTUsesBuiltInGatewayRuntime' \
	-count=1 \
	> "$work_dir/hosted-provider-package-smoke.txt"

go test ./internal/relayadapter ./internal/cli \
	-run 'TestBuildAndVerifyRelayAdapterPackage|TestBuildAndVerifyFRPCRelayAdapterPackage|TestBuildAndVerifyAdditionalConnectivityAdapterPackages|TestVerifyRelayAdapterPackageDetectsUnsafeHelperSurface|TestRelayAdapterPackageAndVerify|TestRelayAdapterPackageSupportsMeshSSHAndVPNKinds' \
	-count=1 \
	> "$work_dir/relay-adapter-package-smoke.txt"

go test ./internal/acceptance ./internal/cli \
	-run 'TestPackageAndVerifyRelayAdapterEvidence|TestVerifyRelayAdapterEvidenceRejectsMissingConnection|TestAcceptancePackageRelayAdapter' \
	-count=1 \
	> "$work_dir/relay-adapter-acceptance-package-smoke.txt"

go test ./internal/acceptance ./internal/cli \
	-run 'TestPackageAndVerifyHostedProviderRuntimeEvidence|TestVerifyHostedProviderRuntimeRejectsMissingDurabilityEvidence|TestAcceptancePackageHostedProviderRuntime' \
	-count=1 \
	> "$work_dir/hosted-provider-runtime-acceptance-package-smoke.txt"

hosted_provider_dir="$work_dir/hosted-provider"
go run ./cmd/rdev hosted-provider package \
	--out "$hosted_provider_dir" \
	--storage-provider file \
	--auth-provider hosted-ed25519-jwt \
  > "$work_dir/hosted-provider-package.json"
go run ./cmd/rdev hosted-provider verify \
	--package "$hosted_provider_dir" \
	> "$work_dir/hosted-provider-verification.json"

postgres_hosted_provider_dir="$work_dir/hosted-provider-postgres-jwt"
go run ./cmd/rdev hosted-provider package \
	--out "$postgres_hosted_provider_dir" \
	--storage-provider postgres \
	--auth-provider hosted-ed25519-jwt \
	> "$work_dir/hosted-provider-postgres-jwt-package.json"
go run ./cmd/rdev hosted-provider verify \
	--package "$postgres_hosted_provider_dir" \
	> "$work_dir/hosted-provider-postgres-jwt-verification.json"

redis_hosted_provider_dir="$work_dir/hosted-provider-redis-jwt"
go run ./cmd/rdev hosted-provider package \
	--out "$redis_hosted_provider_dir" \
	--storage-provider redis-stream \
	--auth-provider hosted-ed25519-jwt \
	> "$work_dir/hosted-provider-redis-jwt-package.json"
go run ./cmd/rdev hosted-provider verify \
	--package "$redis_hosted_provider_dir" \
	> "$work_dir/hosted-provider-redis-jwt-verification.json"

s3_hosted_provider_dir="$work_dir/hosted-provider-s3-jwt"
go run ./cmd/rdev hosted-provider package \
	--out "$s3_hosted_provider_dir" \
	--storage-provider s3-compatible \
	--auth-provider hosted-ed25519-jwt \
	> "$work_dir/hosted-provider-s3-jwt-package.json"
go run ./cmd/rdev hosted-provider verify \
	--package "$s3_hosted_provider_dir" \
	> "$work_dir/hosted-provider-s3-jwt-verification.json"

external_hosted_provider_dir="$work_dir/hosted-provider-postgres-oidc"
go run ./cmd/rdev hosted-provider package \
	--out "$external_hosted_provider_dir" \
	--storage-provider postgres \
	--auth-provider oidc-jwks \
	> "$work_dir/hosted-provider-postgres-oidc-package.json"
go run ./cmd/rdev hosted-provider verify \
	--package "$external_hosted_provider_dir" \
	> "$work_dir/hosted-provider-postgres-oidc-verification.json"

saml_hosted_provider_dir="$work_dir/hosted-provider-s3-saml"
go run ./cmd/rdev hosted-provider package \
	--out "$saml_hosted_provider_dir" \
	--storage-provider s3-compatible \
	--auth-provider saml-assertion \
	> "$work_dir/hosted-provider-s3-saml-package.json"
go run ./cmd/rdev hosted-provider verify \
	--package "$saml_hosted_provider_dir" \
	> "$work_dir/hosted-provider-s3-saml-verification.json"

hosted_runtime_input="$work_dir/hosted-runtime-input"
mkdir -p "$hosted_runtime_input"
printf '%s\n' 'gateway started with hosted provider package' > "$hosted_runtime_input/gateway-startup.txt"
printf '%s\n' '{"ok":true,"provider":"file"}' > "$hosted_runtime_input/storage-verification.json"
printf '%s\n' '{"ok":true,"provider":"hosted-ed25519-jwt"}' > "$hosted_runtime_input/auth-verification.json"
printf '%s\n' 'snapshot copied to reviewed backup location' > "$hosted_runtime_input/backup-evidence.txt"
printf '%s\n' 'restored snapshot and verified audit chain' > "$hosted_runtime_input/restore-evidence.txt"
printf '%s\n' 'retention policy reviewed for release smoke' > "$hosted_runtime_input/retention-evidence.txt"
printf '%s\n' '{"probes":[{"role":"operator","authorized":true},{"role":"viewer","authorized":false}]}' > "$hosted_runtime_input/role-mapping-evidence.json"
printf '%s\n' '{"ok":true,"failure_mode_tested":true,"mode":"invalid auth rejected"}' > "$hosted_runtime_input/failure-mode-evidence.json"
printf '%s\n' 'gateway_start' 'storage_verify' 'auth_verify' 'role_probe' 'failure_probe' 'cleanup' > "$hosted_runtime_input/audit.txt"
hosted_runtime_dir="$work_dir/hosted-runtime-acceptance"
go run ./cmd/rdev acceptance package-hosted-provider-runtime \
	--hosted-provider-package "$hosted_provider_dir" \
	--out "$hosted_runtime_dir" \
	--gateway-startup "$hosted_runtime_input/gateway-startup.txt" \
	--storage-verification "$hosted_runtime_input/storage-verification.json" \
	--auth-verification "$hosted_runtime_input/auth-verification.json" \
	--backup-evidence "$hosted_runtime_input/backup-evidence.txt" \
	--restore-evidence "$hosted_runtime_input/restore-evidence.txt" \
	--retention-evidence "$hosted_runtime_input/retention-evidence.txt" \
	--role-mapping-evidence "$hosted_runtime_input/role-mapping-evidence.json" \
	--failure-mode-evidence "$hosted_runtime_input/failure-mode-evidence.json" \
	--audit "$hosted_runtime_input/audit.txt" \
	> "$work_dir/hosted-provider-runtime-acceptance-package.json"
go run ./cmd/rdev acceptance verify-hosted-provider-runtime-package \
	--package "$hosted_runtime_dir" \
	> "$work_dir/hosted-provider-runtime-acceptance-verification.json"

relay_adapter_dir="$work_dir/relay-adapter"
go run ./cmd/rdev relay-adapter package \
	--out "$relay_adapter_dir" \
	--adapter chisel \
	> "$work_dir/relay-adapter-package.json"
go run ./cmd/rdev relay-adapter verify \
	--package "$relay_adapter_dir" \
	> "$work_dir/relay-adapter-verification.json"

for adapter in ssh-tunnel headscale-tailscale wireguard; do
	adapter_dir="$work_dir/connectivity-adapter-$adapter"
	go run ./cmd/rdev relay-adapter package \
		--out "$adapter_dir" \
		--adapter "$adapter" \
		> "$work_dir/connectivity-adapter-$adapter-package.json"
	go run ./cmd/rdev relay-adapter verify \
		--package "$adapter_dir" \
		> "$work_dir/connectivity-adapter-$adapter-verification.json"
done

relay_acceptance_input="$work_dir/relay-acceptance-input"
mkdir -p "$relay_acceptance_input"
runner_gateway_dir="$work_dir/runner-gateway"
mkdir -p "$runner_gateway_dir"
cat > "$runner_gateway_dir/server.py" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok\n")
    def log_message(self, fmt, *args):
        return
server = HTTPServer(("127.0.0.1", 0), Handler)
with open(sys.argv[1], "w", encoding="utf-8") as f:
    f.write(str(server.server_address[1]))
server.serve_forever()
PY
python3 "$runner_gateway_dir/server.py" "$runner_gateway_dir/port" > "$runner_gateway_dir/server.log" 2>&1 &
runner_gateway_pid=$!
for _ in $(seq 1 50); do
	if [ -s "$runner_gateway_dir/port" ]; then
		runner_gateway_url="http://127.0.0.1:$(cat "$runner_gateway_dir/port")"
		break
	fi
	sleep 0.1
done
if [ -z "${runner_gateway_url:-}" ]; then
	echo "failed to discover runner gateway test server port" >&2
	exit 1
fi
cat > "$relay_acceptance_input/invite.json" <<JSON
{
  "schema_version": "rdev.agent-invite.v1",
  "gateway_url": "https://gateway.example.invalid/v1",
  "join_url": "https://gateway.example.invalid/join/TEST-CODE",
  "manifest_url": "https://gateway.example.invalid/v1/tickets/TEST-CODE/manifest",
  "manifest_root_public_key": "manifest-root:ddddddddddddddddddddddddddddddddddddddddddd",
  "ticket": {
    "id": "tkt_ci_smoke",
    "code": "TEST-CODE",
    "mode": "attended-temporary",
    "status": "active",
    "ttl_seconds": 600,
    "capabilities": ["shell.user"],
    "reason": "release smoke",
    "created_at": "2026-07-02T12:00:00Z",
    "expires_at": "2026-07-02T12:10:00Z"
  },
  "transport": "auto",
  "created_at": "2026-07-02T12:00:00Z"
}
JSON
go run ./cmd/rdev connection-entry plan \
	--invite "$relay_acceptance_input/invite.json" \
	--out "$relay_acceptance_input/connection-entry" \
	--target-os linux \
	--ownership third-party \
	> "$relay_acceptance_input/connection-entry-plan.json"
fake_bin="$relay_acceptance_input/fake-bin"
mkdir -p "$fake_bin"
cat > "$fake_bin/wg" <<'SH'
#!/bin/sh
echo "fake wg helper started" >&2
sleep 1
SH
chmod +x "$fake_bin/wg"
cat > "$fake_bin/rdev-host-smoke" <<'SH'
#!/bin/sh
if [ -n "${RDEV_FAKE_RDEV_TRANSCRIPT:-}" ]; then
  printf 'fake rdev host serve %s\n' "$*" >> "$RDEV_FAKE_RDEV_TRANSCRIPT"
fi
exit 0
SH
chmod +x "$fake_bin/rdev-host-smoke"
PATH="$fake_bin:$PATH" \
HTTP_PROXY= \
HTTPS_PROXY= \
ALL_PROXY= \
NO_PROXY= \
http_proxy= \
https_proxy= \
all_proxy= \
no_proxy= \
RDEV_VPN_GATEWAY_URL="$runner_gateway_url" \
RDEV_VPN_START_ARGV_JSON='["wg","show"]' \
RDEV_FAKE_RDEV_TRANSCRIPT="$relay_acceptance_input/fake-rdev-transcript.txt" \
go run ./cmd/rdev connection-entry run \
	--runner-manifest "$relay_acceptance_input/connection-entry/connection-entry-runner/connection-entry-runner.json" \
	--rdev-command "$fake_bin/rdev-host-smoke" \
	--probe-timeout 1s \
	--result-out "$relay_acceptance_input/runner-result.json" \
	> "$relay_acceptance_input/runner-output.json"
kill "$runner_gateway_pid" 2>/dev/null || true
wait "$runner_gateway_pid" 2>/dev/null || true
runner_gateway_pid=
printf '%s\n' 'started reviewed relay helper' > "$relay_acceptance_input/helper-transcript.txt"
printf '%s\n' '{"ok":true,"status":"healthy"}' > "$relay_acceptance_input/gateway-status.json"
printf '%s\n' '{"ok":true,"host_status":"active"}' > "$relay_acceptance_input/host-status.json"
printf '%s\n' '{"ok":true,"connected":true}' > "$relay_acceptance_input/connection-status.json"
printf '%s\n' 'helper_start' 'host_registered' 'cleanup' > "$relay_acceptance_input/audit.txt"
relay_acceptance_dir="$work_dir/relay-acceptance"
go run ./cmd/rdev acceptance package-relay-adapter \
	--relay-package "$relay_adapter_dir" \
	--out "$relay_acceptance_dir" \
	--runner-result "$relay_acceptance_input/runner-result.json" \
	--helper-transcript "$relay_acceptance_input/helper-transcript.txt" \
	--gateway-status "$relay_acceptance_input/gateway-status.json" \
	--host-status "$relay_acceptance_input/host-status.json" \
	--connection-status "$relay_acceptance_input/connection-status.json" \
	--audit "$relay_acceptance_input/audit.txt" \
	> "$work_dir/relay-adapter-acceptance-package.json"
go run ./cmd/rdev acceptance verify-relay-adapter-package \
	--package "$relay_acceptance_dir" \
	> "$work_dir/relay-adapter-acceptance-verification.json"

go test ./internal/enrollmentlifecycle ./internal/cli \
	-run 'TestBuildFleetRenewalPlan|TestEnrollmentLifecycleKeyCustodyWritesRecord|TestEnrollmentLifecycleFleetRenewalPlanRequiresRevocations|TestEnrollmentLifecycleEmergencyDrillWritesEvidence' \
	-count=1 \
  > "$work_dir/enrollment-lifecycle-smoke.txt"

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

post_release_evidence_dir="$work_dir/post-release-download-evidence-input"
post_release_skillkit_evidence_dir="$work_dir/post-release-download-skillkit-input"
mkdir -p "$post_release_evidence_dir" "$post_release_skillkit_evidence_dir"
printf '%s\n' 'downloaded linux/amd64 release archive and verified checksum' > "$post_release_evidence_dir/linux-amd64-transcript.txt"
printf '%s\n' '{"ok":true,"schema_version":"rdev.release-candidate-verification.v1"}' > "$post_release_evidence_dir/linux-amd64-candidate-verify.json"
printf '%s\n' '{"ok":true,"schema_version":"rdev.release-bundle-verification.v1"}' > "$post_release_evidence_dir/linux-amd64-bundle-verify.json"
printf '%s\n' 'downloaded windows/amd64 release archive and verified checksum' > "$post_release_evidence_dir/windows-amd64-transcript.txt"
printf '%s\n' '{"ok":true,"schema_version":"rdev.release-candidate-verification.v1"}' > "$post_release_evidence_dir/windows-amd64-candidate-verify.json"
printf '%s\n' '{"ok":true,"schema_version":"rdev.release-bundle-verification.v1"}' > "$post_release_evidence_dir/windows-amd64-bundle-verify.json"
printf '%s\n' 'downloaded and verified Skillkit archive' > "$post_release_skillkit_evidence_dir/skillkit-transcript.txt"
printf '%s\n' '{"ok":true,"schema_version":"rdev.skillkit-bundle-verification.v1"}' > "$post_release_skillkit_evidence_dir/skillkit-verify.json"
post_release_download_package_dir="$work_dir/post-release-download-acceptance"
go run ./cmd/rdev acceptance package-post-release-download \
  --plan "$post_release_dir/post-release-install-plan.json" \
  --plan-verification "$work_dir/post-release-verification.json" \
  --out "$post_release_download_package_dir" \
  --evidence-dir "$post_release_evidence_dir" \
  --skillkit-evidence-dir "$post_release_skillkit_evidence_dir" \
  > "$work_dir/post-release-download-acceptance-package.json"
go run ./cmd/rdev acceptance verify-post-release-download-package \
  --package "$post_release_download_package_dir" \
  > "$work_dir/post-release-download-acceptance-verification.json"

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
go run ./cmd/rdev skillkit verify \
  --bundle "$first_skillkit_dir" \
  > "$work_dir/skillkit-verification.json"
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

python3 - "$work_dir/plan-output.json" "$work_dir/update-api" <<'PY'
import json
import pathlib
import sys

plan_output = json.loads(pathlib.Path(sys.argv[1]).read_text())
plan = json.loads(pathlib.Path(plan_output["plan"]).read_text())
api_root = pathlib.Path(sys.argv[2])
release_dir = api_root / "repos" / plan["repo"] / "releases"
release_dir.mkdir(parents=True, exist_ok=True)
release = {
    "tag_name": plan["tag"],
    "name": plan["title"],
    "html_url": f"https://github.com/{plan['repo']}/releases/tag/{plan['tag']}",
    "draft": False,
    "prerelease": False,
    "published_at": "2026-07-02T00:00:00Z",
    "assets": [
        {
            "name": asset["name"],
            "browser_download_url": f"https://github.com/{plan['repo']}/releases/download/{plan['tag']}/{asset['name']}",
            "digest": asset["sha256"],
            "size": asset["size_bytes"],
            "content_type": "application/octet-stream",
        }
        for asset in plan["assets"]
    ],
}
(release_dir / "latest").write_text(json.dumps(release, indent=2) + "\n", encoding="utf-8")
PY
cat > "$work_dir/update-api-server.py" <<'PY'
import functools
import http.server
import pathlib
import socketserver
import sys

root = pathlib.Path(sys.argv[1])
port_file = pathlib.Path(sys.argv[2])
handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(root))
with socketserver.TCPServer(("127.0.0.1", 0), handler) as server:
    port_file.write_text(str(server.server_address[1]), encoding="utf-8")
    server.serve_forever()
PY
python3 "$work_dir/update-api-server.py" "$work_dir/update-api" "$work_dir/update-api-port" > "$work_dir/update-api-server.log" 2>&1 &
update_api_pid=$!
for _ in $(seq 1 50); do
  update_api_port="$(cat "$work_dir/update-api-port" 2>/dev/null || true)"
  if [[ -n "$update_api_port" ]]; then
    break
  fi
  sleep 0.1
done
if [[ -z "${update_api_port:-}" ]]; then
  echo "update API smoke server did not start" >&2
  exit 1
fi
go run ./cmd/rdev update plan \
  --repo "$repo" \
  --api-base-url "http://127.0.0.1:$update_api_port" \
  --current-version "v0.0.0" \
  --platform linux/amd64 \
  > "$work_dir/update-plan.json"
kill "$update_api_pid"
wait "$update_api_pid" 2>/dev/null || true

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
import zipfile

root = pathlib.Path(sys.argv[1])
fresh_agent_output = json.loads((root / "fresh-agent-support-session-output.json").read_text())
build = json.loads((root / "build.json").read_text())
platform_output = json.loads((root / "platform-candidates-output.json").read_text())
platform_manifest = json.loads(pathlib.Path(platform_output["manifest"]).read_text())
plan_output = json.loads((root / "plan-output.json").read_text())
plan = json.loads(pathlib.Path(plan_output["plan"]).read_text())
github_project_readiness = json.loads((root / "github-project-readiness.json").read_text())
hosted_provider_package = json.loads((root / "hosted-provider-package.json").read_text())
hosted_provider_verification = json.loads((root / "hosted-provider-verification.json").read_text())
postgres_hosted_provider_package = json.loads((root / "hosted-provider-postgres-jwt-package.json").read_text())
postgres_hosted_provider_manifest = json.loads((root / "hosted-provider-postgres-jwt" / "hosted-provider.json").read_text())
postgres_hosted_provider_verification = json.loads((root / "hosted-provider-postgres-jwt-verification.json").read_text())
redis_hosted_provider_package = json.loads((root / "hosted-provider-redis-jwt-package.json").read_text())
redis_hosted_provider_manifest = json.loads((root / "hosted-provider-redis-jwt" / "hosted-provider.json").read_text())
redis_hosted_provider_verification = json.loads((root / "hosted-provider-redis-jwt-verification.json").read_text())
s3_hosted_provider_package = json.loads((root / "hosted-provider-s3-jwt-package.json").read_text())
s3_hosted_provider_manifest = json.loads((root / "hosted-provider-s3-jwt" / "hosted-provider.json").read_text())
s3_hosted_provider_verification = json.loads((root / "hosted-provider-s3-jwt-verification.json").read_text())
external_hosted_provider_package = json.loads((root / "hosted-provider-postgres-oidc-package.json").read_text())
external_hosted_provider_verification = json.loads((root / "hosted-provider-postgres-oidc-verification.json").read_text())
external_hosted_provider_manifest = json.loads((root / "hosted-provider-postgres-oidc" / "hosted-provider.json").read_text())
external_hosted_runtime_contract = json.loads((root / "hosted-provider-postgres-oidc" / "runtime-contract.json").read_text())
saml_hosted_provider_package = json.loads((root / "hosted-provider-s3-saml-package.json").read_text())
saml_hosted_provider_verification = json.loads((root / "hosted-provider-s3-saml-verification.json").read_text())
saml_hosted_provider_manifest = json.loads((root / "hosted-provider-s3-saml" / "hosted-provider.json").read_text())
saml_hosted_runtime_contract = json.loads((root / "hosted-provider-s3-saml" / "runtime-contract.json").read_text())
hosted_provider_runtime_package = json.loads((root / "hosted-provider-runtime-acceptance-package.json").read_text())
hosted_provider_runtime_verification = json.loads((root / "hosted-provider-runtime-acceptance-verification.json").read_text())
relay_adapter_package = json.loads((root / "relay-adapter-package.json").read_text())
relay_adapter_verification = json.loads((root / "relay-adapter-verification.json").read_text())
connectivity_adapter_packages = {
    adapter: json.loads((root / f"connectivity-adapter-{adapter}-package.json").read_text())
    for adapter in ["ssh-tunnel", "headscale-tailscale", "wireguard"]
}
connectivity_adapter_manifests = {
    adapter: json.loads((root / f"connectivity-adapter-{adapter}" / "relay-adapter.json").read_text())
    for adapter in ["ssh-tunnel", "headscale-tailscale", "wireguard"]
}
connectivity_adapter_verifications = {
    adapter: json.loads((root / f"connectivity-adapter-{adapter}-verification.json").read_text())
    for adapter in ["ssh-tunnel", "headscale-tailscale", "wireguard"]
}
relay_adapter_acceptance_package = json.loads((root / "relay-adapter-acceptance-package.json").read_text())
relay_adapter_acceptance_verification = json.loads((root / "relay-adapter-acceptance-verification.json").read_text())
post_release_output = json.loads((root / "post-release-output.json").read_text())
post_release_plan = json.loads(pathlib.Path(post_release_output["plan"]).read_text())
post_release_verification = json.loads((root / "post-release-verification.json").read_text())
post_release_download_package = json.loads((root / "post-release-download-acceptance-package.json").read_text())
post_release_download_verification = json.loads((root / "post-release-download-acceptance-verification.json").read_text())
post_release_tampered = json.loads((root / "post-release-tampered-verification.json").read_text())
skillkit_install_plan_output = json.loads((root / "skillkit-install-plan-output.json").read_text())
skillkit_verification = json.loads((root / "skillkit-verification.json").read_text())
skillkit_install_plan_verification = json.loads((root / "skillkit-install-plan-verification.json").read_text())
skillkit_install_dry_run = json.loads((root / "skillkit-install-dry-run.json").read_text())
skillkit_install_execute = json.loads((root / "skillkit-install-execute.json").read_text())
update_plan = json.loads((root / "update-plan.json").read_text())
skillkit_manifest = json.loads((pathlib.Path(skillkit_install_plan_output["bundle"]) / "manifest.json").read_text())
skillkit_mcp_tools = json.loads((pathlib.Path(skillkit_install_plan_output["bundle"]) / "mcp" / "tools.json").read_text())
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
    assert candidate_json["connection_entry_path"] == "connection-entry-release.zip", candidate_json
    assert any(file["path"] == "provenance.json" and file["kind"] == "provenance" for file in candidate_json["files"]), candidate_json["files"]
    assert any(file["path"] == "sbom.spdx.json" and file["kind"] == "sbom" for file in candidate_json["files"]), candidate_json["files"]
    assert any(file["path"] == "connection-entry-release.zip" and file["kind"] == "connection-entry-release-archive" for file in candidate_json["files"]), candidate_json["files"]
    entry_archive_path = pathlib.Path(candidate["candidate_dir"]) / "connection-entry-release.zip"
    assert entry_archive_path.is_file(), candidate_json
    with zipfile.ZipFile(entry_archive_path) as archive:
        names = set(archive.namelist())
        required_archive_entries = {
            "CONNECTION_ENTRY_RELEASE.md",
            "connection-entry-release.json",
            "connection-entry-runner.template.json",
            "connection-entry-checksums.txt",
            "release/release-bundle.json",
            "release/sbom.spdx.json",
            "release/provenance.json",
        }
        artifact_basenames = {pathlib.Path(artifact["artifact_path"]).name for artifact in candidate_json["artifacts"]}
        if "rdev.exe" in artifact_basenames:
            required_archive_entries.add("launchers/Start-ConnectionEntry.ps1")
        else:
            required_archive_entries.add("launchers/start-connection-entry.sh")
        assert required_archive_entries.issubset(names), names
        entry_manifest = json.loads(archive.read("connection-entry-release.json"))
        assert entry_manifest["schema_version"] == "rdev.connection-entry-release-package.v1", entry_manifest
        assert entry_manifest["no_private_parameters"] is True, entry_manifest
        assert entry_manifest["execution_mode"] == "runtime-invite-required", entry_manifest
        required_artifacts = ",".join(artifact["name"] for artifact in candidate_json["artifacts"])
        assert entry_manifest["required_release_artifacts"] == [artifact["name"] for artifact in candidate_json["artifacts"]], entry_manifest
        assert len(entry_manifest["launchers"]) >= 1, entry_manifest
        launcher_paths = [path for path in names if path.startswith("launchers/")]
        launcher_text = "\n".join(archive.read(path).decode() for path in launcher_paths)
        assert "rdev-verify" in launcher_text, launcher_text
        assert "--bundle" in launcher_text and ("release/release-bundle.json" in launcher_text or "release\\release-bundle.json" in launcher_text), launcher_text
        assert "--root-public-key" in launcher_text and candidate_json["root_public_key"] in launcher_text, launcher_text
        assert "--require-artifacts" in launcher_text and required_artifacts in launcher_text, launcher_text
        for artifact in candidate_json["artifacts"]:
            assert "bin/" + pathlib.Path(artifact["artifact_path"]).name in names, names
            assert "bin/" + pathlib.Path(artifact["manifest_path"]).name in names, names
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
assert hosted_provider_package["schema"] == "rdev.hosted-provider-package.v1", hosted_provider_package
assert hosted_provider_package["ok"] is True, hosted_provider_package
assert hosted_provider_package["external_mutation"] is False, hosted_provider_package
assert hosted_provider_package["storage_provider"] == "file", hosted_provider_package
assert hosted_provider_package["auth_provider"] == "hosted-ed25519-jwt", hosted_provider_package
assert hosted_provider_verification["schema"] == "rdev.hosted-provider-package-verification.v1", hosted_provider_verification
assert hosted_provider_verification["ok"] is True, hosted_provider_verification
assert hosted_provider_verification["storage_provider"] == "file", hosted_provider_verification
assert hosted_provider_verification["auth_provider"] == "hosted-ed25519-jwt", hosted_provider_verification
assert postgres_hosted_provider_package["schema"] == "rdev.hosted-provider-package.v1", postgres_hosted_provider_package
assert postgres_hosted_provider_package["ok"] is True, postgres_hosted_provider_package
assert postgres_hosted_provider_package["storage_provider"] == "postgres", postgres_hosted_provider_package
assert postgres_hosted_provider_package["auth_provider"] == "hosted-ed25519-jwt", postgres_hosted_provider_package
assert postgres_hosted_provider_verification["schema"] == "rdev.hosted-provider-package-verification.v1", postgres_hosted_provider_verification
assert postgres_hosted_provider_verification["ok"] is True, postgres_hosted_provider_verification
assert postgres_hosted_provider_manifest["gateway_args"][:6] == ["rdev", "gateway", "serve", "--storage-provider", "postgres", "--storage-path"], postgres_hosted_provider_manifest["gateway_args"]
assert "operator-reviewed-hosted-gateway-launcher" not in postgres_hosted_provider_manifest["gateway_args"], postgres_hosted_provider_manifest["gateway_args"]
assert redis_hosted_provider_package["schema"] == "rdev.hosted-provider-package.v1", redis_hosted_provider_package
assert redis_hosted_provider_package["ok"] is True, redis_hosted_provider_package
assert redis_hosted_provider_package["storage_provider"] == "redis-stream", redis_hosted_provider_package
assert redis_hosted_provider_package["auth_provider"] == "hosted-ed25519-jwt", redis_hosted_provider_package
assert redis_hosted_provider_verification["schema"] == "rdev.hosted-provider-package-verification.v1", redis_hosted_provider_verification
assert redis_hosted_provider_verification["ok"] is True, redis_hosted_provider_verification
assert redis_hosted_provider_manifest["gateway_args"][:6] == ["rdev", "gateway", "serve", "--storage-provider", "redis-stream", "--storage-path"], redis_hosted_provider_manifest["gateway_args"]
assert "operator-reviewed-hosted-gateway-launcher" not in redis_hosted_provider_manifest["gateway_args"], redis_hosted_provider_manifest["gateway_args"]
assert s3_hosted_provider_package["schema"] == "rdev.hosted-provider-package.v1", s3_hosted_provider_package
assert s3_hosted_provider_package["ok"] is True, s3_hosted_provider_package
assert s3_hosted_provider_package["storage_provider"] == "s3-compatible", s3_hosted_provider_package
assert s3_hosted_provider_package["auth_provider"] == "hosted-ed25519-jwt", s3_hosted_provider_package
assert s3_hosted_provider_verification["schema"] == "rdev.hosted-provider-package-verification.v1", s3_hosted_provider_verification
assert s3_hosted_provider_verification["ok"] is True, s3_hosted_provider_verification
assert s3_hosted_provider_manifest["gateway_args"][:6] == ["rdev", "gateway", "serve", "--storage-provider", "s3-compatible", "--storage-path"], s3_hosted_provider_manifest["gateway_args"]
assert "operator-reviewed-hosted-gateway-launcher" not in s3_hosted_provider_manifest["gateway_args"], s3_hosted_provider_manifest["gateway_args"]
assert external_hosted_provider_package["schema"] == "rdev.hosted-provider-package.v1", external_hosted_provider_package
assert external_hosted_provider_package["ok"] is True, external_hosted_provider_package
assert external_hosted_provider_package["storage_provider"] == "postgres", external_hosted_provider_package
assert external_hosted_provider_package["auth_provider"] == "oidc-jwks", external_hosted_provider_package
assert external_hosted_provider_verification["schema"] == "rdev.hosted-provider-package-verification.v1", external_hosted_provider_verification
assert external_hosted_provider_verification["ok"] is True, external_hosted_provider_verification
assert external_hosted_provider_manifest["gateway_args"][:6] == ["rdev", "gateway", "serve", "--storage-provider", "postgres", "--storage-path"], external_hosted_provider_manifest["gateway_args"]
assert "--oidc-jwks-operator-auth" in external_hosted_provider_manifest["gateway_args"], external_hosted_provider_manifest["gateway_args"]
assert "operator-reviewed-hosted-gateway-launcher" not in external_hosted_provider_manifest["gateway_args"], external_hosted_provider_manifest["gateway_args"]
assert external_hosted_runtime_contract["schema_version"] == "rdev.hosted-provider-runtime-contract.v1", external_hosted_runtime_contract
assert external_hosted_runtime_contract["runtime_status"] == "durable-runtime-evidence-required", external_hosted_runtime_contract
assert len(external_hosted_runtime_contract["required_evidence"]) >= 9, external_hosted_runtime_contract
assert saml_hosted_provider_package["schema"] == "rdev.hosted-provider-package.v1", saml_hosted_provider_package
assert saml_hosted_provider_package["ok"] is True, saml_hosted_provider_package
assert saml_hosted_provider_package["storage_provider"] == "s3-compatible", saml_hosted_provider_package
assert saml_hosted_provider_package["auth_provider"] == "saml-assertion", saml_hosted_provider_package
assert saml_hosted_provider_verification["schema"] == "rdev.hosted-provider-package-verification.v1", saml_hosted_provider_verification
assert saml_hosted_provider_verification["ok"] is True, saml_hosted_provider_verification
assert saml_hosted_provider_manifest["gateway_args"][:6] == ["rdev", "gateway", "serve", "--storage-provider", "s3-compatible", "--storage-path"], saml_hosted_provider_manifest["gateway_args"]
assert "--saml-operator-auth" in saml_hosted_provider_manifest["gateway_args"], saml_hosted_provider_manifest["gateway_args"]
assert "operator-reviewed-hosted-gateway-launcher" not in saml_hosted_provider_manifest["gateway_args"], saml_hosted_provider_manifest["gateway_args"]
assert saml_hosted_runtime_contract["schema_version"] == "rdev.hosted-provider-runtime-contract.v1", saml_hosted_runtime_contract
assert saml_hosted_runtime_contract["runtime_status"] == "durable-runtime-evidence-required", saml_hosted_runtime_contract
assert any(item["example_command"].startswith("rdev operator-auth verify-saml") for item in saml_hosted_runtime_contract["required_evidence"] if item["name"] == "auth-verification"), saml_hosted_runtime_contract
assert hosted_provider_runtime_package["schema"] == "rdev.acceptance-package.hosted-provider-runtime.v1", hosted_provider_runtime_package
assert hosted_provider_runtime_package["ok"] is True, hosted_provider_runtime_package
assert hosted_provider_runtime_package["storage_provider"] == "file", hosted_provider_runtime_package
assert hosted_provider_runtime_package["auth_provider"] == "hosted-ed25519-jwt", hosted_provider_runtime_package
assert hosted_provider_runtime_package["runtime_claim"] == "single-node-hosted-smoke", hosted_provider_runtime_package
assert hosted_provider_runtime_verification["schema"] == "rdev.acceptance-verification.hosted-provider-runtime-package.v1", hosted_provider_runtime_verification
assert hosted_provider_runtime_verification["ok"] is True, hosted_provider_runtime_verification
assert hosted_provider_runtime_verification["runtime_claim"] == "single-node-hosted-smoke", hosted_provider_runtime_verification
assert relay_adapter_package["schema"] == "rdev.relay-adapter-package.v1", relay_adapter_package
assert relay_adapter_package["ok"] is True, relay_adapter_package
assert relay_adapter_package["external_mutation"] is False, relay_adapter_package
assert relay_adapter_package["adapter_kind"] == "chisel", relay_adapter_package
assert relay_adapter_package["runner_env"]["gateway_url_var"] == "RDEV_RELAY_GATEWAY_URL", relay_adapter_package
assert relay_adapter_verification["schema"] == "rdev.relay-adapter-package-verification.v1", relay_adapter_verification
assert relay_adapter_verification["ok"] is True, relay_adapter_verification
assert relay_adapter_verification["adapter_kind"] == "chisel", relay_adapter_verification
expected_connectivity = {
    "ssh-tunnel": ("ssh-tunnel", "RDEV_SSH_GATEWAY_URL", "manual-review-required"),
    "headscale-tailscale": ("headscale-tailscale", "RDEV_MESH_GATEWAY_URL", "RDEV_MESH_DOWNLOAD_URL"),
    "wireguard": ("wireguard", "RDEV_VPN_GATEWAY_URL", "RDEV_VPN_DOWNLOAD_URL"),
}
for adapter, (kind, gateway_var, install_marker) in expected_connectivity.items():
    package = connectivity_adapter_packages[adapter]
    manifest = connectivity_adapter_manifests[adapter]
    verification = connectivity_adapter_verifications[adapter]
    assert package["schema"] == "rdev.relay-adapter-package.v1", package
    assert package["ok"] is True, package
    assert package["adapter_kind"] == kind, package
    assert package["runner_env"]["gateway_url_var"] == gateway_var, package
    assert manifest["adapter_kind"] == kind, manifest
    install_argv = " ".join(manifest["install_action_template"]["argv"])
    assert install_marker in install_argv, manifest
    if adapter != "ssh-tunnel":
        assert "rdev deps install" in install_argv, manifest
        assert manifest["install_action_template"]["requires_elevation"] is False, manifest
    assert verification["schema"] == "rdev.relay-adapter-package-verification.v1", verification
    assert verification["ok"] is True, verification
    assert verification["adapter_kind"] == kind, verification
assert relay_adapter_acceptance_package["schema"] == "rdev.acceptance-package.relay-adapter.v1", relay_adapter_acceptance_package
assert relay_adapter_acceptance_package["ok"] is True, relay_adapter_acceptance_package
assert relay_adapter_acceptance_package["selected_path"] == "existing-wireguard-vpn", relay_adapter_acceptance_package
assert set(relay_adapter_acceptance_package["accepted_paths"]) >= {
    "existing-frp-or-chisel-relay",
    "existing-ssh-tunnel",
    "existing-headscale-tailscale-mesh",
    "existing-wireguard-vpn",
}, relay_adapter_acceptance_package
assert relay_adapter_acceptance_verification["schema"] == "rdev.acceptance-verification.relay-adapter-package.v1", relay_adapter_acceptance_verification
assert relay_adapter_acceptance_verification["ok"] is True, relay_adapter_acceptance_verification
assert relay_adapter_acceptance_verification["selected_path"] == "existing-wireguard-vpn", relay_adapter_acceptance_verification
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
assert post_release_download_package["schema"] == "rdev.acceptance-package.post-release-download.v1", post_release_download_package
assert post_release_download_package["ok"] is True, post_release_download_package
assert set(post_release_download_package["platform_targets"]) == {"linux/amd64", "windows/amd64"}, post_release_download_package
assert post_release_download_package["skillkit_included"] is True, post_release_download_package
assert post_release_download_verification["schema"] == "rdev.acceptance-verification.post-release-download-package.v1", post_release_download_verification
assert post_release_download_verification["ok"] is True, post_release_download_verification
assert post_release_tampered["schema_version"] == "rdev.post-release-install-verification.v1", post_release_tampered
assert post_release_tampered["ok"] is False, post_release_tampered
assert any(check["name"].endswith("script_matches_commands") for check in post_release_tampered["failed_checks"]), post_release_tampered
assert any(check["name"].endswith("script_no_forbidden_side_effects") for check in post_release_tampered["failed_checks"]), post_release_tampered
assert skillkit_install_plan_output["schema"] == "rdev.skillkit-install-plan.v1", skillkit_install_plan_output
assert skillkit_install_plan_output["ok"] is True, skillkit_install_plan_output
assert skillkit_install_plan_output["external_mutation"] is False, skillkit_install_plan_output
assert skillkit_install_plan_output["framework_count"] == 6, skillkit_install_plan_output
assert skillkit_verification["schema"] == "rdev.skillkit-bundle-verification.v1", skillkit_verification
assert skillkit_verification["ok"] is True, skillkit_verification
assert any(check["name"] == "skill_agents_metadata" and check["passed"] is True for check in skillkit_verification["checks"]), skillkit_verification
assert skillkit_manifest["adaptive_configuration"]["schema_version"] == "rdev.adaptive-configuration-contract.v1", skillkit_manifest
assert skillkit_manifest["adaptive_configuration"]["required"] is True, skillkit_manifest
assert "rdev doctor" in skillkit_manifest["adaptive_configuration"]["probe_before_acting"], skillkit_manifest
assert "rdev mcp tools" in skillkit_manifest["adaptive_configuration"]["probe_before_acting"], skillkit_manifest
assert "available connection modes" in skillkit_manifest["adaptive_configuration"]["probe_before_acting"], skillkit_manifest
assert "framework install path" in skillkit_manifest["adaptive_configuration"]["ask_if_unclear"], skillkit_manifest
assert "https://api.example.com/v1" in skillkit_manifest["adaptive_configuration"]["placeholders"], skillkit_manifest
assert skillkit_mcp_tools["tools"][0]["name"] == "rdev.support_session.connect", skillkit_mcp_tools["tools"][:3]
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
assert update_plan["schema_version"] == "rdev.update-plan.v1", update_plan
assert update_plan["update_available"] is True, update_plan
assert update_plan["platform"] == "linux/amd64", update_plan
assert update_plan["selected_archive"]["name"].endswith("linux-amd64.tar.gz"), update_plan
assert any("rdev release verify-bundle" in step for step in update_plan["verification_steps"]), update_plan
assert any(check["name"] == "plan_is_dry_run" and check["passed"] is True for check in update_plan["checks"]), update_plan
assert fresh_agent_output["ok"] is True, fresh_agent_output
assert fresh_agent_output["schema"] == "rdev.acceptance.fresh-agent-support-session.v1", fresh_agent_output
fresh_agent_checks = {check["name"]: check for check in fresh_agent_output["checks"]}
bootstrap_self_repair_checks = [
    "bootstrap_self_repair_join_page_available",
    "bootstrap_self_repair_windows_downloads_verified_helper",
    "bootstrap_self_repair_shell_downloads_verified_helper",
    "bootstrap_self_repair_pins_manifest_root",
    "bootstrap_self_repair_starts_visible_host",
    "bootstrap_self_repair_assets_have_hashes",
    "bootstrap_self_repair_no_manual_rdev_requirement",
]
assert all(
    name in fresh_agent_checks and fresh_agent_checks[name]["passed"] is True
    for name in bootstrap_self_repair_checks
), fresh_agent_checks
stable_fallback_checks = [
    "stable_fallback_handoff_uses_configured_gateway",
    "stable_fallback_created_uses_relay_candidate",
    "stable_fallback_continuity_is_durable",
    "stable_fallback_supervision_does_not_request_upgrade",
    "stable_fallback_runbook_reports_stable_candidate",
]
assert all(
    name in fresh_agent_checks and fresh_agent_checks[name]["passed"] is True
    for name in stable_fallback_checks
), fresh_agent_checks

print(json.dumps({
    "ok": True,
    "fresh_agent_support_session_contract": True,
    "fresh_agent_bootstrap_self_repair_contract": True,
    "fresh_agent_stable_fallback_contract": True,
    "fresh_agent_support_session_schema": fresh_agent_output["schema"],
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
    "skillkit_bundle_verification_schema": skillkit_verification["schema"],
    "skillkit_agents_metadata": True,
    "skillkit_install_plan_verification_schema": skillkit_install_plan_verification["schema"],
    "skillkit_install_report_schema": skillkit_install_execute["schema"],
    "skillkit_adaptive_configuration": True,
    "skillkit_mcp_connect_first": True,
    "planned_platforms": plan_output["platform_count"],
    "github_project_seed_issues": github_project_readiness["bootstrap_dry_run"]["seed_issues"],
    "post_release_platforms": post_release_output["platform_count"],
    "skillkit_install_frameworks": skillkit_install_plan_output["framework_count"],
    "skillkit_install_executed": skillkit_install_execute["executed"],
    "update_plan_smoke": True,
    "public_surface_audit": True,
    "dev_mtls_host_smoke": True,
    "wss_mtls_host_smoke": True,
    "hosted_storage_auth_smoke": True,
    "hosted_provider_package_schema": hosted_provider_package["schema"],
    "hosted_provider_package_verification_schema": hosted_provider_verification["schema"],
    "postgres_hosted_provider_package_schema": postgres_hosted_provider_package["schema"],
    "postgres_hosted_provider_runtime_gateway_args": True,
    "redis_hosted_provider_package_schema": redis_hosted_provider_package["schema"],
    "redis_hosted_provider_runtime_gateway_args": True,
    "s3_hosted_provider_package_schema": s3_hosted_provider_package["schema"],
    "s3_hosted_provider_runtime_gateway_args": True,
    "external_hosted_provider_package_schema": external_hosted_provider_package["schema"],
    "oidc_jwks_hosted_provider_runtime_gateway_args": True,
    "external_hosted_provider_runtime_contract_schema": external_hosted_runtime_contract["schema_version"],
    "saml_hosted_provider_package_schema": saml_hosted_provider_package["schema"],
    "saml_hosted_provider_runtime_gateway_args": True,
    "saml_hosted_provider_runtime_contract_schema": saml_hosted_runtime_contract["schema_version"],
    "hosted_provider_runtime_acceptance_package_schema": hosted_provider_runtime_package["schema"],
    "hosted_provider_runtime_acceptance_verification_schema": hosted_provider_runtime_verification["schema"],
    "relay_adapter_package_schema": relay_adapter_package["schema"],
    "relay_adapter_package_verification_schema": relay_adapter_verification["schema"],
    "connectivity_adapter_package_kinds": sorted(package["adapter_kind"] for package in connectivity_adapter_packages.values()),
    "relay_adapter_acceptance_package_schema": relay_adapter_acceptance_package["schema"],
    "relay_adapter_acceptance_verification_schema": relay_adapter_acceptance_verification["schema"],
    "post_release_download_acceptance_package_schema": post_release_download_package["schema"],
    "post_release_download_acceptance_verification_schema": post_release_download_verification["schema"],
    "enrollment_lifecycle_smoke": True,
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
    "connection_entry_release_archive": True,
    "external_mutation": plan["external_mutation"],
}, indent=2))
PY
