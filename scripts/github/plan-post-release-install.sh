#!/usr/bin/env bash
set -euo pipefail

release_plan=""
out_dir=""
base_url="https://github.com"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/plan-post-release-install.sh --release-plan PATH [options]

Creates a local post-release download and install verification plan from
rdev.github-platform-release-plan.v1. The script is read-only with respect to
GitHub: it never creates releases, uploads assets, pushes commits, or mutates
external state.

Options:
  --release-plan PATH  plan.json from scripts/github/plan-platform-release.sh
  --out DIR            Output directory; defaults to <release-plan>/post-release-install
  --base-url URL       GitHub base URL; defaults to https://github.com
  -h, --help           Show this help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release-plan)
      release_plan="${2:-}"
      shift 2
      ;;
    --out)
      out_dir="${2:-}"
      shift 2
      ;;
    --base-url)
      base_url="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      echo "unexpected argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$release_plan" ]]; then
  usage
  exit 2
fi

release_plan_abs="$(python3 - "$release_plan" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"
if [[ ! -f "$release_plan_abs" ]]; then
  echo "release plan JSON not found: $release_plan_abs" >&2
  exit 1
fi

if [[ -z "$out_dir" ]]; then
  out_dir="$(dirname "$release_plan_abs")/post-release-install"
fi
mkdir -p "$out_dir"
out_dir="$(python3 - "$out_dir" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"

python3 - "$release_plan_abs" "$out_dir" "$base_url" <<'PY'
import datetime
import json
import pathlib
import re
import shlex
import sys
import urllib.parse

release_plan_path = pathlib.Path(sys.argv[1])
out_dir = pathlib.Path(sys.argv[2])
base_url = sys.argv[3].rstrip("/")

plan = json.loads(release_plan_path.read_text())
if plan.get("schema_version") != "rdev.github-platform-release-plan.v1":
    raise SystemExit(f"unsupported release plan schema: {plan.get('schema_version')}")

repo = plan.get("repo") or ""
tag = plan.get("tag") or ""
if not repo or not tag:
    raise SystemExit("release plan must include repo and tag")

release_index_path = pathlib.Path(plan.get("release_index") or "")
if not release_index_path.is_file():
    raise SystemExit(f"release index not found: {release_index_path}")
release_index = json.loads(release_index_path.read_text())
if release_index.get("schema_version") != "rdev.platform-release-index.v1":
    raise SystemExit(f"unsupported release index schema: {release_index.get('schema_version')}")

assets = list(plan.get("assets") or [])
if not assets:
    raise SystemExit("release plan has no assets")

assets_by_name = {}
for asset in assets:
    name = asset.get("name") or ""
    if not name:
        raise SystemExit(f"asset missing name: {asset}")
    if name in assets_by_name:
        raise SystemExit(f"duplicate asset name in release plan: {name}")
    assets_by_name[name] = asset

generated_at = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
download_base = f"{base_url}/{repo}/releases/download/{urllib.parse.quote(tag, safe='')}"

def slug(value):
    return re.sub(r"[^A-Za-z0-9._-]+", "-", value).strip("-") or "target"

def sha_hex(value):
    if value.startswith("sha256:"):
        return value.split(":", 1)[1]
    return value

def release_url(name):
    return f"{download_base}/{urllib.parse.quote(name, safe='')}"

def shell_quote(value):
    return shlex.quote(value)

def markdown_code(value):
    return value.replace("|", "\\|")

def strip_archive_suffix(name):
    if name.endswith(".tar.gz"):
        return name[:-7]
    if name.endswith(".zip"):
        return name[:-4]
    return pathlib.Path(name).stem

def asset_entry(asset):
    name = asset["name"]
    return {
        "name": name,
        "kind": asset.get("kind", ""),
        "download_url": release_url(name),
        "sha256": asset.get("sha256", ""),
        "sha256_hex": sha_hex(asset.get("sha256", "")),
        "size_bytes": asset.get("size_bytes", 0),
    }

def unix_platform_commands(entry):
    archive = entry["archive_name"]
    root = strip_archive_suffix(archive)
    archive_sha = sha_hex(entry["archive_sha256"])
    required = ",".join(entry.get("required_artifacts") or [])
    verify_candidate = ["./rdev", "release", "verify-candidate", "--candidate", "."]
    verify_bundle = ["./rdev-verify", "--bundle", "release-bundle.json", "--root-public-key", entry.get("root_public_key", "")]
    if required:
        verify_candidate += ["--require-artifacts", required]
        verify_bundle += ["--require-artifacts", required]
    lines = [
        "set -euo pipefail",
        f"mkdir -p {shell_quote(root)}-download",
        f"cd {shell_quote(root)}-download",
        f"curl -fL -o {shell_quote(archive)} {shell_quote(release_url(archive))}",
        f"printf '%s  %s\\n' {shell_quote(archive_sha)} {shell_quote(archive)} | shasum -a 256 -c -",
        f"tar -xzf {shell_quote(archive)}",
        f"cd {shell_quote(root)}",
        "chmod +x ./rdev ./rdev-host ./rdev-verify 2>/dev/null || true",
        "chmod +x ./*.sh 2>/dev/null || true",
        " ".join(shell_quote(part) for part in verify_candidate),
        " ".join(shell_quote(part) for part in verify_bundle),
    ]
    return lines

def windows_platform_commands(entry):
    archive = entry["archive_name"]
    root = strip_archive_suffix(archive)
    archive_sha = sha_hex(entry["archive_sha256"]).upper()
    required = ",".join(entry.get("required_artifacts") or [])
    verify_candidate = [".\\rdev.exe", "release", "verify-candidate", "--candidate", "."]
    verify_bundle = [".\\rdev-verify.exe", "--bundle", "release-bundle.json", "--root-public-key", entry.get("root_public_key", "")]
    if required:
        verify_candidate += ["--require-artifacts", required]
        verify_bundle += ["--require-artifacts", required]
    def ps_quote(value):
        return "'" + value.replace("'", "''") + "'"
    def ps_arg(value):
        if value.startswith(".\\") or value.startswith("--"):
            return value
        return ps_quote(value)
    lines = [
        "$ErrorActionPreference = 'Stop'",
        f"$Root = {ps_quote(root + '-download')}",
        "New-Item -ItemType Directory -Force -Path $Root | Out-Null",
        "Set-Location $Root",
        f"Invoke-WebRequest -Uri {ps_quote(release_url(archive))} -OutFile {ps_quote(archive)}",
        f"$Expected = {ps_quote(archive_sha)}",
        f"$Actual = (Get-FileHash -Algorithm SHA256 {ps_quote(archive)}).Hash.ToUpperInvariant()",
        "if ($Actual -ne $Expected) { throw \"SHA256 mismatch: expected $Expected got $Actual\" }",
        f"Expand-Archive -Force {ps_quote(archive)} .",
        f"Set-Location {ps_quote(root)}",
        " ".join(ps_arg(part) for part in verify_candidate),
        " ".join(ps_arg(part) for part in verify_bundle),
    ]
    return lines

def skillkit_commands(skillkit):
    name = skillkit["name"]
    root = strip_archive_suffix(name)
    sha = sha_hex(skillkit["sha256"])
    return [
        "set -euo pipefail",
        f"mkdir -p {shell_quote(root)}-download",
        f"cd {shell_quote(root)}-download",
        f"curl -fL -o {shell_quote(name)} {shell_quote(release_url(name))}",
        f"printf '%s  %s\\n' {shell_quote(sha)} {shell_quote(name)} | shasum -a 256 -c -",
        f"tar -xzf {shell_quote(name)}",
        "rdev skillkit verify --bundle skillkit",
    ]

platforms = []
for entry in release_index.get("platforms") or []:
    archive_name = entry.get("archive_name") or ""
    asset = assets_by_name.get(archive_name)
    if not asset:
        raise SystemExit(f"platform archive asset missing from release plan: {archive_name}")
    target = entry.get("target") or ""
    if target.startswith("windows/"):
        commands = windows_platform_commands(entry)
        command_file = out_dir / f"verify-{slug(target)}.ps1"
        command_language = "powershell"
    else:
        commands = unix_platform_commands(entry)
        command_file = out_dir / f"verify-{slug(target)}.sh"
        command_language = "bash"
    command_file.write_text("\n".join(commands) + "\n", encoding="utf-8")
    command_file.chmod(0o755 if command_language == "bash" else 0o644)
    platforms.append({
        "target": target,
        "archive": asset_entry(asset),
        "root_public_key": entry.get("root_public_key", ""),
        "required_artifacts": entry.get("required_artifacts") or [],
        "verification_script": command_file.name,
        "verification_language": command_language,
        "commands": commands,
    })

asset_kinds = {asset.get("kind", ""): asset for asset in assets}
required_kinds = ["platform-release-index", "platform-release-verification", "install-guide", "skillkit-archive"]
missing_kinds = [kind for kind in required_kinds if kind not in asset_kinds]
if missing_kinds:
    raise SystemExit(f"release plan missing required assets: {', '.join(missing_kinds)}")

skillkit_asset = asset_kinds["skillkit-archive"]
skillkit_script = out_dir / "verify-skillkit.sh"
skillkit_lines = skillkit_commands(skillkit_asset)
skillkit_script.write_text("\n".join(skillkit_lines) + "\n", encoding="utf-8")
skillkit_script.chmod(0o755)

global_assets = [asset_entry(asset_kinds[kind]) for kind in required_kinds]
all_assets = [asset_entry(asset) for asset in assets]

install_doc = out_dir / "VERIFY_INSTALL.md"
lines = [
    f"# Verify And Install {plan.get('title') or tag}",
    "",
    "This plan is generated from a local GitHub Release dry-run plan. It does not publish or mutate GitHub.",
    "",
    f"- Repository: `{repo}`",
    f"- Tag: `{tag}`",
    f"- Release URL: `{base_url}/{repo}/releases/tag/{urllib.parse.quote(tag, safe='')}`",
    "",
    "## Platform Archives",
    "",
    "| Target | Archive | SHA-256 | Verification script |",
    "|---|---|---|---|",
]
for item in platforms:
    lines.append(
        f"| `{item['target']}` | `{item['archive']['name']}` | `{item['archive']['sha256_hex']}` | `{pathlib.Path(item['verification_script']).name}` |"
    )
lines += [
    "",
    "Run the verification script for your platform after the release assets are published.",
    "Do not run `rdev-host` from an archive until both the archive checksum and signed `release-bundle.json` verify.",
    "",
    "## Skillkit",
    "",
    f"- Archive: `{skillkit_asset['name']}`",
    f"- SHA-256: `{sha_hex(skillkit_asset['sha256'])}`",
    f"- Verification script: `{skillkit_script.name}`",
    "",
    "The Skillkit verifier assumes `rdev` is already available on PATH. Alternatively, run the command with the `rdev` binary extracted from a verified platform archive.",
    "",
    "## Asset Index",
    "",
    "| Asset | Kind | SHA-256 | Download URL |",
    "|---|---|---|---|",
]
for asset in all_assets:
    lines.append(
        f"| `{asset['name']}` | `{asset['kind']}` | `{asset['sha256_hex']}` | `{markdown_code(asset['download_url'])}` |"
    )
lines += [
    "",
    "## Evidence To Archive",
    "",
    "- verification script stdout/stderr;",
    "- archive checksum result;",
    "- `rdev release verify-candidate` JSON output;",
    "- `rdev-verify --bundle` JSON output;",
    "- Skillkit verification output if installing agent skills;",
    "- platform and OS version used for the transcript.",
    "",
]
install_doc.write_text("\n".join(lines), encoding="utf-8")

commands_file = out_dir / "commands.txt"
commands_file.write_text(
    "# Post-release verification scripts. Download assets only after the GitHub Release exists.\n"
    + "\n".join(item["verification_script"] for item in platforms)
    + "\n"
    + skillkit_script.name
    + "\n",
    encoding="utf-8",
)

post_plan = {
    "schema_version": "rdev.post-release-install-plan.v1",
    "generated_at": generated_at,
    "external_mutation": False,
    "source_release_plan": str(release_plan_path.resolve()),
    "repo": repo,
    "tag": tag,
    "release_url": f"{base_url}/{repo}/releases/tag/{urllib.parse.quote(tag, safe='')}",
    "asset_download_base": download_base,
    "global_assets": global_assets,
    "platforms": platforms,
    "skillkit": {
        "archive": asset_entry(skillkit_asset),
        "verification_script": skillkit_script.name,
        "verification_language": "bash",
        "commands": skillkit_lines,
    },
    "documents": {
        "verify_install": install_doc.name,
        "commands": commands_file.name,
    },
    "release_plan_schema": plan.get("schema_version", ""),
    "release_index_schema": release_index.get("schema_version", ""),
}
post_plan_path = out_dir / "post-release-install-plan.json"
post_plan_path.write_text(json.dumps(post_plan, indent=2) + "\n", encoding="utf-8")

print(json.dumps({
    "ok": True,
    "schema": post_plan["schema_version"],
    "plan": str(post_plan_path),
    "verify_install": str(install_doc),
    "commands": str(commands_file),
    "platform_count": len(platforms),
    "asset_count": len(all_assets),
    "external_mutation": False,
}, indent=2))
PY
