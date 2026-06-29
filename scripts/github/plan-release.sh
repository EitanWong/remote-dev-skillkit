#!/usr/bin/env bash
set -euo pipefail

candidate=""
repo=""
tag=""
title=""
out_dir=""
required_artifacts=""
notes_file=""

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/plan-release.sh --candidate DIR_OR_JSON --repo OWNER/REPO [options]

Creates a local GitHub Release plan from a verified release candidate. This
script is read-only with respect to GitHub: it never creates releases, uploads
assets, pushes commits, or mutates external state.

Options:
  --candidate PATH            Release candidate directory or release-candidate.json
  --repo OWNER/REPO           GitHub repository name for the planned release
  --tag TAG                   Release tag; defaults to candidate version
  --title TITLE               Release title; defaults to tag
  --out DIR                   Output directory; defaults to <candidate>/github-release-plan
  --require-artifacts LIST    Comma-separated artifact ids required by verification
  --notes-file PATH           Existing release notes to reference instead of generating one
  -h, --help                  Show this help

Environment:
  RDEV_BIN=/path/to/rdev      rdev binary to use. Defaults to ./rdev when present,
                              otherwise "go run ./cmd/rdev" from the repository.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate)
      candidate="${2:-}"
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
    --require-artifacts)
      required_artifacts="${2:-}"
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

if [[ -z "$candidate" || -z "$repo" ]]; then
  usage
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

candidate_abs="$(python3 - "$candidate" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"

if [[ -d "$candidate_abs" ]]; then
  candidate_json="$candidate_abs/release-candidate.json"
  candidate_dir="$candidate_abs"
else
  candidate_json="$candidate_abs"
  candidate_dir="$(dirname "$candidate_abs")"
fi

if [[ ! -f "$candidate_json" ]]; then
  echo "release candidate JSON not found: $candidate_json" >&2
  exit 1
fi

if [[ -z "$out_dir" ]]; then
  out_dir="$candidate_dir/github-release-plan"
fi
mkdir -p "$out_dir"
out_dir="$(python3 - "$out_dir" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"

if [[ -n "${RDEV_BIN:-}" ]]; then
  rdev_cmd=("$RDEV_BIN")
elif [[ -x "$repo_root/rdev" ]]; then
  rdev_cmd=("$repo_root/rdev")
else
  rdev_cmd=(go run ./cmd/rdev)
fi

verification_json="$out_dir/verification.json"
verify_args=(release verify-candidate --candidate "$candidate_json")
if [[ -n "$required_artifacts" ]]; then
  verify_args+=(--require-artifacts "$required_artifacts")
fi

(
  cd "$repo_root"
  "${rdev_cmd[@]}" "${verify_args[@]}"
) > "$verification_json"

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

python3 - "$candidate_json" "$verification_json" "$repo" "$tag" "$title" "$out_dir" "$notes_file" <<'PY'
import datetime
import hashlib
import json
import pathlib
import re
import shlex
import sys
import tarfile

candidate_json = pathlib.Path(sys.argv[1])
verification_json = pathlib.Path(sys.argv[2])
repo = sys.argv[3]
tag = sys.argv[4]
title = sys.argv[5]
out_dir = pathlib.Path(sys.argv[6])
provided_notes = pathlib.Path(sys.argv[7]) if sys.argv[7] else None

candidate = json.loads(candidate_json.read_text())
verification = json.loads(verification_json.read_text())
candidate_dir = candidate_json.parent

version = candidate.get("version") or ""
if not tag:
    tag = version
if not title:
    title = tag
if not tag:
    raise SystemExit("release tag is required because candidate version is empty")

def sha256_file(path):
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()

def asset(path, kind, name=None):
    path = pathlib.Path(path)
    return {
        "name": name or path.name,
        "path": str(path.resolve()),
        "kind": kind,
        "sha256": sha256_file(path),
        "size_bytes": path.stat().st_size,
    }

safe_tag = re.sub(r"[^A-Za-z0-9._-]+", "-", tag).strip("-") or "release"
skillkit_archive = out_dir / f"remote-dev-skillkit-{safe_tag}-skillkit.tar.gz"
skillkit_dir = candidate_dir / "skillkit"
with tarfile.open(skillkit_archive, "w:gz") as tar:
    for path in sorted(skillkit_dir.rglob("*")):
        rel = path.relative_to(candidate_dir)
        info = tar.gettarinfo(str(path), arcname=str(rel))
        info.uid = 0
        info.gid = 0
        info.uname = ""
        info.gname = ""
        info.mtime = 0
        if path.is_file():
            with path.open("rb") as handle:
                tar.addfile(info, handle)
        else:
            tar.addfile(info)

assets = []
seen_asset_names = set()
def add_asset(item):
    if item["name"] in seen_asset_names:
        raise SystemExit(f"duplicate release asset name: {item['name']}")
    seen_asset_names.add(item["name"])
    assets.append(item)

for artifact in candidate.get("artifacts", []):
    add_asset(asset(candidate_dir / artifact["artifact_path"], "artifact"))
    add_asset(asset(candidate_dir / artifact["manifest_path"], "release-manifest"))
for rel_path, kind in [
    ("release-bundle.json", "release-bundle"),
    ("checksums.txt", "checksums"),
]:
    add_asset(asset(candidate_dir / rel_path, kind))
add_asset(asset(candidate_json, "release-candidate"))
add_asset(asset(verification_json, "release-candidate-verification"))
add_asset(asset(skillkit_archive, "skillkit-archive"))
assets.sort(key=lambda item: (item["kind"], item["name"]))

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
            "## Verification",
            "",
            f"- Release candidate verification schema: `{verification.get('schema', '')}`",
            f"- Release candidate verification ok: `{str(verification.get('ok')).lower()}`",
            "- Review `verification.json` before publishing.",
            "",
            "## Installation",
            "",
            "- Download the release assets.",
            "- Verify the release candidate or signed bundle before installing.",
            "- Install the exported Skillkit into the target agent runtime.",
            "",
        ]) + "\n",
        encoding="utf-8",
    )

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
    "schema_version": "rdev.github-release-plan.v1",
    "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
    "repo": repo,
    "tag": tag,
    "title": title,
    "draft": True,
    "external_mutation": False,
    "candidate": {
        "path": str(candidate_json.resolve()),
        "dir": str(candidate_dir.resolve()),
        "version": version,
        "root_public_key": candidate.get("root_public_key", ""),
        "release_bundle_path": str((candidate_dir / "release-bundle.json").resolve()),
        "skillkit_path": str((candidate_dir / "skillkit").resolve()),
    },
    "verification": {
        "path": str(verification_json.resolve()),
        "ok": bool(verification.get("ok")),
        "schema": verification.get("schema", ""),
    },
    "candidate_file_count": len(candidate.get("files", [])),
    "assets": assets,
    "notes_file": str(notes_path.resolve()),
    "commands": {
        "create_release": " ".join(shlex.quote(part) for part in create_cmd),
        "upload_assets": " ".join(shlex.quote(part) for part in upload_cmd),
    },
    "recommended_actions": [
        "Confirm verification.ok is true before publishing.",
        "Review asset paths, hashes, release notes, and acceptance evidence.",
        "Get explicit operator approval before running any gh release command.",
        "Create the release as a draft first, then verify downloads before publishing.",
    ],
}

(out_dir / "plan.json").write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8")
(out_dir / "commands.txt").write_text(
    "\n".join([
        "# Dry-run release commands. Do not run without explicit operator approval.",
        "# This script did not mutate GitHub.",
        "",
        plan["commands"]["create_release"],
        plan["commands"]["upload_assets"],
        "",
        "# Optional after verifying downloads and acceptance evidence:",
        f"gh release edit {shlex.quote(tag)} --repo {shlex.quote(repo)} --draft=false",
        "",
    ]),
    encoding="utf-8",
)

print(json.dumps({
    "ok": bool(verification.get("ok")),
    "schema": plan["schema_version"],
    "plan": str((out_dir / "plan.json").resolve()),
    "commands": str((out_dir / "commands.txt").resolve()),
    "notes": str(notes_path.resolve()),
    "verification": str(verification_json.resolve()),
    "asset_count": len(assets),
    "external_mutation": False,
}, indent=2))
PY
