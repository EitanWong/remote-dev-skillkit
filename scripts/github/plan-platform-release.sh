#!/usr/bin/env bash
set -euo pipefail

platform_candidates=""
repo=""
tag=""
title=""
out_dir=""
notes_file=""

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/plan-platform-release.sh --platform-candidates PATH --repo OWNER/REPO [options]

Creates a local GitHub Release plan from rdev.platform-release-candidates.v1.
This script is read-only with respect to GitHub: it never creates releases,
uploads assets, pushes commits, or mutates external state.

Options:
  --platform-candidates PATH  platform-candidates.json from prepare-platform-candidates.sh
  --repo OWNER/REPO           GitHub repository name for the planned release
  --tag TAG                   Release tag; defaults to platform candidate version
  --title TITLE               Release title; defaults to tag
  --out DIR                   Output directory; defaults to <platform-candidates>/github-platform-release-plan
  --notes-file PATH           Existing release notes to reference instead of generating one
  -h, --help                  Show this help

Environment:
  RDEV_BIN=/path/to/rdev      rdev binary to use. Defaults to ./rdev when present,
                              otherwise "go run ./cmd/rdev" from the repository.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform-candidates)
      platform_candidates="${2:-}"
      shift 2
      ;;
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --tag)
      tag="${2:-}"
      shift 2
      ;;
    --title)
      title="${2:-}"
      shift 2
      ;;
    --out)
      out_dir="${2:-}"
      shift 2
      ;;
    --notes-file)
      notes_file="${2:-}"
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

if [[ -z "$platform_candidates" || -z "$repo" ]]; then
  usage
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

platform_candidates_abs="$(python3 - "$platform_candidates" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"
if [[ ! -f "$platform_candidates_abs" ]]; then
  echo "platform candidates JSON not found: $platform_candidates_abs" >&2
  exit 1
fi

if [[ -z "$out_dir" ]]; then
  out_dir="$(dirname "$platform_candidates_abs")/github-platform-release-plan"
fi
mkdir -p "$out_dir"
out_dir="$(python3 - "$out_dir" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"

if [[ -n "$notes_file" ]]; then
  notes_file="$(python3 - "$notes_file" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"
  if [[ ! -f "$notes_file" ]]; then
    echo "release notes file not found: $notes_file" >&2
    exit 1
  fi
fi

if [[ -n "${RDEV_BIN:-}" ]]; then
  rdev_cmd_json="$(python3 - "$RDEV_BIN" <<'PY'
import json, sys
print(json.dumps([sys.argv[1]]))
PY
)"
elif [[ -x "$repo_root/rdev" ]]; then
  rdev_cmd_json="$(python3 - "$repo_root/rdev" <<'PY'
import json, sys
print(json.dumps([sys.argv[1]]))
PY
)"
else
  rdev_cmd_json='["go","run","./cmd/rdev"]'
fi

RDEV_CMD_JSON="$rdev_cmd_json" RDEV_REPO_ROOT="$repo_root" python3 - "$platform_candidates_abs" "$repo" "$tag" "$title" "$out_dir" "$notes_file" <<'PY'
import datetime
import hashlib
import json
import os
import pathlib
import re
import shlex
import subprocess
import sys
import tarfile
import zipfile

platform_candidates_path = pathlib.Path(sys.argv[1])
repo = sys.argv[2]
tag = sys.argv[3]
title = sys.argv[4]
out_dir = pathlib.Path(sys.argv[5])
provided_notes = pathlib.Path(sys.argv[6]) if sys.argv[6] else None
repo_root = pathlib.Path(os.environ["RDEV_REPO_ROOT"])
rdev_cmd = json.loads(os.environ["RDEV_CMD_JSON"])

manifest = json.loads(platform_candidates_path.read_text())
if manifest.get("schema_version") != "rdev.platform-release-candidates.v1":
    raise SystemExit(f"unsupported platform candidates schema: {manifest.get('schema_version')}")

version = manifest.get("version") or ""
if not tag:
    tag = version
if not title:
    title = tag
if not tag:
    raise SystemExit("release tag is required because platform candidate version is empty")

out_dir.mkdir(parents=True, exist_ok=True)
assets_dir = out_dir / "assets"
evidence_dir = out_dir / "verification"
assets_dir.mkdir(exist_ok=True)
evidence_dir.mkdir(exist_ok=True)

safe_tag = re.sub(r"[^A-Za-z0-9._-]+", "-", tag).strip("-") or "release"
generated_at = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

def slug(value):
    return re.sub(r"[^A-Za-z0-9._-]+", "-", value).strip("-") or "target"

def sha256_file(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def sha256_prefixed(path):
    return "sha256:" + sha256_file(path)

assets = []
seen_asset_names = set()

def add_asset(path, kind, name=None):
    path = pathlib.Path(path)
    item = {
        "name": name or path.name,
        "path": str(path.resolve()),
        "kind": kind,
        "sha256": sha256_prefixed(path),
        "size_bytes": path.stat().st_size,
    }
    if item["name"] in seen_asset_names:
        raise SystemExit(f"duplicate release asset name: {item['name']}")
    seen_asset_names.add(item["name"])
    assets.append(item)
    return item

def normalize_tarinfo(info):
    info.uid = 0
    info.gid = 0
    info.uname = ""
    info.gname = ""
    info.mtime = 0
    return info

def archive_candidate(candidate, target_slug):
    target = candidate["target"]
    candidate_dir = pathlib.Path(candidate["candidate_dir"])
    if not candidate_dir.is_dir():
        raise SystemExit(f"candidate directory not found for {target}: {candidate_dir}")
    root_name = f"remote-dev-skillkit-{safe_tag}-{target_slug}"
    if target.startswith("windows/"):
        archive_path = assets_dir / f"{root_name}.zip"
        with zipfile.ZipFile(archive_path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            for path in sorted(candidate_dir.rglob("*")):
                rel = path.relative_to(candidate_dir).as_posix()
                arcname = f"{root_name}/{rel}"
                if path.is_dir():
                    info = zipfile.ZipInfo(arcname.rstrip("/") + "/", date_time=(1980, 1, 1, 0, 0, 0))
                    info.external_attr = 0o755 << 16
                    archive.writestr(info, b"")
                else:
                    info = zipfile.ZipInfo(arcname, date_time=(1980, 1, 1, 0, 0, 0))
                    mode = 0o755 if os.access(path, os.X_OK) else 0o644
                    info.external_attr = mode << 16
                    archive.writestr(info, path.read_bytes())
    else:
        archive_path = assets_dir / f"{root_name}.tar.gz"
        with tarfile.open(archive_path, "w:gz") as archive:
            for path in sorted(candidate_dir.rglob("*")):
                rel = path.relative_to(candidate_dir).as_posix()
                info = archive.gettarinfo(str(path), arcname=f"{root_name}/{rel}")
                normalize_tarinfo(info)
                if path.is_file():
                    with path.open("rb") as handle:
                        archive.addfile(info, handle)
                else:
                    archive.addfile(info)
    return archive_path

def verify_candidate(candidate, target_slug):
    target = candidate["target"]
    verification_path = evidence_dir / f"{target_slug}.verification.json"
    args = rdev_cmd + [
        "release",
        "verify-candidate",
        "--candidate",
        candidate["candidate_dir"],
    ]
    required = candidate.get("required_artifacts") or []
    if required:
        args += ["--require-artifacts", ",".join(required)]
    result = subprocess.run(args, cwd=repo_root, text=True, capture_output=True, check=False)
    verification_path.write_text(result.stdout, encoding="utf-8")
    if result.returncode != 0:
        raise SystemExit(
            f"candidate verification failed for {target} with exit={result.returncode}\n"
            f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
        )
    verification = json.loads(result.stdout)
    if verification.get("ok") is not True:
        raise SystemExit(f"candidate verification returned ok=false for {target}: {verification}")
    return verification_path, verification

platform_entries = []
for candidate in manifest.get("candidates", []):
    target = candidate["target"]
    target_slug = slug(target)
    candidate_json = pathlib.Path(candidate["candidate_json"])
    candidate_payload = json.loads(candidate_json.read_text())
    verification_path, verification = verify_candidate(candidate, target_slug)
    archive_path = archive_candidate(candidate, target_slug)
    archive_asset = add_asset(archive_path, "platform-candidate-archive")
    platform_entries.append({
        "target": target,
        "archive_name": archive_asset["name"],
        "archive_sha256": archive_asset["sha256"],
        "archive_size_bytes": archive_asset["size_bytes"],
        "candidate_schema": candidate_payload.get("schema_version", ""),
        "verification_schema": verification.get("schema", ""),
        "verification_ok": verification.get("ok") is True,
        "required_artifacts": candidate.get("required_artifacts") or [],
        "root_public_key": candidate_payload.get("root_public_key", ""),
    })

if not platform_entries:
    raise SystemExit("platform candidates manifest has no candidates")

first_candidate_dir = pathlib.Path(manifest["candidates"][0]["candidate_dir"])
skillkit_dir = first_candidate_dir / "skillkit"
skillkit_archive = assets_dir / f"remote-dev-skillkit-{safe_tag}-skillkit.tar.gz"
with tarfile.open(skillkit_archive, "w:gz") as archive:
    for path in sorted(skillkit_dir.rglob("*")):
        rel = path.relative_to(first_candidate_dir).as_posix()
        info = archive.gettarinfo(str(path), arcname=rel)
        normalize_tarinfo(info)
        if path.is_file():
            with path.open("rb") as handle:
                archive.addfile(info, handle)
        else:
            archive.addfile(info)
skillkit_asset = add_asset(skillkit_archive, "skillkit-archive")

release_index = {
    "schema_version": "rdev.platform-release-index.v1",
    "version": version or tag,
    "generated_at": generated_at,
    "tag": tag,
    "platforms": platform_entries,
    "skillkit_archive": {
        "name": skillkit_asset["name"],
        "sha256": skillkit_asset["sha256"],
        "size_bytes": skillkit_asset["size_bytes"],
    },
}
release_index_path = out_dir / "platform-release-index.json"
release_index_path.write_text(json.dumps(release_index, indent=2) + "\n", encoding="utf-8")
add_asset(release_index_path, "platform-release-index")

verification_summary = {
    "schema_version": "rdev.github-platform-release-verification.v1",
    "generated_at": generated_at,
    "platform_candidates_schema": manifest.get("schema_version", ""),
    "platform_candidate_count": len(platform_entries),
    "all_platform_candidates_verified": all(entry["verification_ok"] for entry in platform_entries),
    "platforms": [
        {
            "target": entry["target"],
            "candidate_schema": entry["candidate_schema"],
            "verification_schema": entry["verification_schema"],
            "verification_ok": entry["verification_ok"],
            "archive_name": entry["archive_name"],
            "archive_sha256": entry["archive_sha256"],
        }
        for entry in platform_entries
    ],
}
verification_summary_path = out_dir / "platform-release-verification.json"
verification_summary_path.write_text(json.dumps(verification_summary, indent=2) + "\n", encoding="utf-8")
add_asset(verification_summary_path, "platform-release-verification")

install_path = out_dir / "INSTALL_PLATFORMS.md"
install_lines = [
    f"# Install {title}",
    "",
    "Choose the archive matching your platform, extract it, then verify the signed bundle before running host code.",
    "",
    "| Target | Archive | Verify command |",
    "|---|---|---|",
]
for entry in platform_entries:
    required = ",".join(entry["required_artifacts"])
    if entry["target"].startswith("windows/"):
        verifier = ".\\rdev-verify.exe"
    else:
        verifier = "./rdev-verify"
    verify_cmd = f"{verifier} --bundle release-bundle.json --root-public-key {entry['root_public_key']}"
    if required:
        verify_cmd += f" --require-artifacts {required}"
    install_lines.append(f"| `{entry['target']}` | `{entry['archive_name']}` | `{verify_cmd}` |")
install_lines += [
    "",
    "The aggregate release index is `platform-release-index.json`; compare archive SHA-256 values before extraction when possible.",
    "Do not run `rdev-host` from an unverified archive.",
    "",
]
install_path.write_text("\n".join(install_lines), encoding="utf-8")
add_asset(install_path, "install-guide")

if provided_notes:
    notes_path = provided_notes
else:
    notes_path = out_dir / "RELEASE_NOTES.md"
    notes_path.write_text(
        "\n".join([
            f"# {title}",
            "",
            f"Version: `{version or tag}`",
            "",
            "## Release Assets",
            "",
            "- Download the archive for your platform.",
            "- Read `INSTALL_PLATFORMS.md` before running host binaries.",
            "- Verify `release-bundle.json` with `rdev-verify` after extraction.",
            "",
            "## Platforms",
            "",
        ] + [f"- `{entry['target']}`: `{entry['archive_name']}`" for entry in platform_entries]) + "\n",
        encoding="utf-8",
    )

assets.sort(key=lambda item: (item["kind"], item["name"]))
create_cmd = [
    "gh", "release", "create", tag,
    "--repo", repo,
    "--title", title,
    "--notes-file", str(notes_path),
    "--draft",
]
upload_cmd = [
    "gh", "release", "upload", tag,
    "--repo", repo,
] + [f'{item["path"]}#{item["name"]}' for item in assets] + ["--clobber"]

plan = {
    "schema_version": "rdev.github-platform-release-plan.v1",
    "generated_at": generated_at,
    "repo": repo,
    "tag": tag,
    "title": title,
    "draft": True,
    "external_mutation": False,
    "platform_candidates": {
        "schema": manifest.get("schema_version", ""),
        "path": str(platform_candidates_path.resolve()),
        "version": version,
        "candidate_count": len(platform_entries),
    },
    "assets": assets,
    "notes": str(notes_path.resolve()),
    "install_guide": str(install_path.resolve()),
    "release_index": str(release_index_path.resolve()),
    "verification": str(verification_summary_path.resolve()),
    "commands": {
        "release_create": create_cmd,
        "release_upload": upload_cmd,
    },
}
plan_path = out_dir / "plan.json"
plan_path.write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")

commands_path = out_dir / "commands.txt"
commands_path.write_text(
    "# Dry-run command preview only. Review plan.json before executing.\n"
    + " ".join(shlex.quote(part) for part in create_cmd)
    + "\n"
    + " ".join(shlex.quote(part) for part in upload_cmd)
    + "\n",
    encoding="utf-8",
)

print(json.dumps({
    "ok": True,
    "schema": plan["schema_version"],
    "plan": str(plan_path),
    "commands": str(commands_path),
    "notes": str(notes_path),
    "install_guide": str(install_path),
    "release_index": str(release_index_path),
    "verification": str(verification_summary_path),
    "asset_count": len(assets),
    "platform_count": len(platform_entries),
    "external_mutation": False,
}, indent=2))
PY
