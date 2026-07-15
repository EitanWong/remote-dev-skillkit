#!/usr/bin/env bash
set -euo pipefail

build_manifest=""
source_root="."
out_dir="dist/release-candidates"
version=""
gateway_url=""
key_path=""
required_commands="rdev-host,rdev-verify"
targets=""
clean=false

usage() {
  cat <<'EOF'
Usage: scripts/release/prepare-platform-candidates.sh --build-manifest PATH --key PATH [options]

Prepare and verify one release candidate per build target from rdev.build-artifacts.v1.
The script is local-only: it does not publish to GitHub or mutate external services.

Options:
  --build-manifest PATH       Path to build-artifacts.json from build-artifacts.sh
  --key PATH                  Release root signing key file
  --source-root DIR           Source root for Skillkit export. Default: .
  --out DIR                   Output directory. Default: dist/release-candidates
  --version VERSION           Version override. Default: version from build manifest
  --gateway-url URL           Gateway URL embedded in Skillkit install docs
  --required-commands LIST    Commands required per target. Default: rdev-host,rdev-verify
  --targets LIST              Optional comma-separated GOOS/GOARCH targets to include
  --clean                     Remove output directory before preparing candidates
  -h, --help                  Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --build-manifest)
      build_manifest="${2:?missing value for --build-manifest}"
      shift 2
      ;;
    --key)
      key_path="${2:?missing value for --key}"
      shift 2
      ;;
    --source-root)
      source_root="${2:?missing value for --source-root}"
      shift 2
      ;;
    --out)
      out_dir="${2:?missing value for --out}"
      shift 2
      ;;
    --version)
      version="${2:?missing value for --version}"
      shift 2
      ;;
    --gateway-url)
      gateway_url="${2:?missing value for --gateway-url}"
      shift 2
      ;;
    --required-commands)
      required_commands="${2:?missing value for --required-commands}"
      shift 2
      ;;
    --targets)
      targets="${2:?missing value for --targets}"
      shift 2
      ;;
    --clean)
      clean=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$build_manifest" || -z "$key_path" ]]; then
  usage >&2
  exit 2
fi
if [[ ! -f "$build_manifest" ]]; then
  echo "build manifest not found: $build_manifest" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

build_manifest_abs="$(python3 - "$build_manifest" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"
out_dir="$(python3 - "$out_dir" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"
key_path="$(python3 - "$key_path" <<'PY'
import pathlib, sys
print(pathlib.Path(sys.argv[1]).expanduser().resolve())
PY
)"

if [[ "$clean" == true ]]; then
  rm -rf "$out_dir"
fi
mkdir -p "$out_dir"

selection_json="$out_dir/.platform-selection.json"
python3 - "$build_manifest_abs" "$required_commands" "$targets" "$version" > "$selection_json" <<'PY'
import json
import pathlib
import sys

manifest_path = pathlib.Path(sys.argv[1])
required_commands = [value.strip() for value in sys.argv[2].split(",") if value.strip()]
target_filter = [value.strip() for value in sys.argv[3].split(",") if value.strip()]
version_override = sys.argv[4].strip()

manifest = json.loads(manifest_path.read_text())
if manifest.get("schema_version") != "rdev.build-artifacts.v1":
    raise SystemExit(f"unsupported build manifest schema: {manifest.get('schema_version')}")

manifest_out_dir = pathlib.Path(manifest.get("out_dir") or ".").expanduser()
if manifest_out_dir.is_absolute():
    build_root = manifest_out_dir.resolve()
else:
    build_root = (manifest_path.parent / manifest_out_dir).resolve()
groups = {}
target_order = []
for artifact in manifest.get("artifacts", []):
    target = artifact.get("target")
    if not target:
        raise SystemExit("artifact missing target")
    if target_filter and target not in target_filter:
        continue
    if target not in groups:
        groups[target] = []
        target_order.append(target)
    path = build_root / artifact["path"]
    item = dict(artifact)
    item["abs_path"] = str(path)
    groups[target].append(item)

if not target_order:
    raise SystemExit("no build targets selected")

candidates = []
for target in target_order:
    artifacts = groups[target]
    command_to_name = {artifact["command"]: artifact["name"] for artifact in artifacts}
    missing_commands = [command for command in required_commands if command not in command_to_name]
    if missing_commands:
        raise SystemExit(f"{target} missing required commands: {','.join(missing_commands)}")
    for artifact in artifacts:
        path = pathlib.Path(artifact["abs_path"])
        if not path.is_file():
            raise SystemExit(f"artifact missing for {target}: {path}")
    candidates.append({
        "target": target,
        "goos": artifacts[0].get("goos", ""),
        "goarch": artifacts[0].get("goarch", ""),
        "artifact_paths": [artifact["abs_path"] for artifact in artifacts],
        "required_artifacts": [command_to_name[command] for command in required_commands],
        "artifact_count": len(artifacts),
    })

print(json.dumps({
    "schema_version": "rdev.platform-candidate-selection.v1",
    "version": version_override or manifest.get("version", ""),
    "build_manifest": str(manifest_path),
    "build_schema": manifest["schema_version"],
    "build_generated_at": manifest.get("generated_at", ""),
    "required_commands": required_commands,
    "candidates": candidates,
}, indent=2))
PY

selected_version="$(python3 - "$selection_json" <<'PY'
import json, pathlib, sys
print(json.loads(pathlib.Path(sys.argv[1]).read_text()).get("version", ""))
PY
)"
if [[ -z "$selected_version" ]]; then
  echo "version is required because the build manifest version is empty" >&2
  exit 2
fi

summary_tsv="$out_dir/.platform-candidates.tsv"
: > "$summary_tsv"

candidate_count="$(python3 - "$selection_json" <<'PY'
import json, pathlib, sys
print(len(json.loads(pathlib.Path(sys.argv[1]).read_text())["candidates"]))
PY
)"

for ((idx=0; idx<candidate_count; idx++)); do
  target="$(python3 - "$selection_json" "$idx" <<'PY'
import json, pathlib, sys
selection = json.loads(pathlib.Path(sys.argv[1]).read_text())
print(selection["candidates"][int(sys.argv[2])]["target"])
PY
)"
  target_slug="${target//\//-}"
  candidate_dir="$out_dir/$target_slug"
  if [[ -e "$candidate_dir" ]]; then
    entries="$(find "$candidate_dir" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')"
    if [[ "$entries" != "0" ]]; then
      echo "candidate directory must be empty: $candidate_dir" >&2
      exit 1
    fi
  fi
  artifacts_csv="$(python3 - "$selection_json" "$idx" <<'PY'
import json, pathlib, sys
selection = json.loads(pathlib.Path(sys.argv[1]).read_text())
print(",".join(selection["candidates"][int(sys.argv[2])]["artifact_paths"]))
PY
)"
  required_csv="$(python3 - "$selection_json" "$idx" <<'PY'
import json, pathlib, sys
selection = json.loads(pathlib.Path(sys.argv[1]).read_text())
print(",".join(selection["candidates"][int(sys.argv[2])]["required_artifacts"]))
PY
)"

  prepare_json="$out_dir/$target_slug.prepare.json"
  verify_json="$out_dir/$target_slug.verify.json"
  (
    cd "$repo_root"
    go run ./cmd/rdev release prepare-candidate \
      --source-root "$source_root" \
      --out "$candidate_dir" \
      --version "$selected_version" \
      --target-platform "$target" \
      --gateway-url "$gateway_url" \
      --artifacts "$artifacts_csv" \
      --require-artifacts "$required_csv" \
      --key "$key_path"
  ) > "$prepare_json"
  (
    cd "$repo_root"
    go run ./cmd/rdev release verify-candidate \
      --candidate "$candidate_dir" \
      --require-artifacts "$required_csv"
  ) > "$verify_json"

  python3 - "$prepare_json" "$verify_json" "$target" "$candidate_dir" "$required_csv" "$summary_tsv" <<'PY'
import json
import pathlib
import sys

prepare_path, verify_path, target, candidate_dir, required_csv, summary_tsv = sys.argv[1:]
prepare = json.loads(pathlib.Path(prepare_path).read_text())
verify = json.loads(pathlib.Path(verify_path).read_text())
if prepare.get("ok") is not True:
    raise SystemExit(f"prepare failed for {target}: {prepare}")
if verify.get("ok") is not True:
    raise SystemExit(f"verification failed for {target}: {verify}")
fields = [
    target,
    candidate_dir,
    prepare["release_candidate"],
    verify_path,
    prepare["schema"],
    verify["schema"],
    str(prepare["artifact_count"]),
    required_csv,
]
with pathlib.Path(summary_tsv).open("a", encoding="utf-8") as handle:
    handle.write("\t".join(fields) + "\n")
PY
done

python3 - "$selection_json" "$summary_tsv" "$out_dir" "$selected_version" <<'PY'
import json
import os
import pathlib
import sys

selection = json.loads(pathlib.Path(sys.argv[1]).read_text())
summary_tsv = pathlib.Path(sys.argv[2])
out_dir = pathlib.Path(sys.argv[3])
version = sys.argv[4]
candidates = []
for line in summary_tsv.read_text().splitlines():
    target, candidate_dir, candidate_json, verification_json, candidate_schema, verification_schema, artifact_count, required_csv = line.split("\t")
    candidates.append({
        "target": target,
        "candidate_dir": candidate_dir,
        "candidate_json": candidate_json,
        "verification_json": verification_json,
        "candidate_schema": candidate_schema,
        "verification_schema": verification_schema,
        "artifact_count": int(artifact_count),
        "required_artifacts": [value for value in required_csv.split(",") if value],
    })

manifest = {
    "schema_version": "rdev.platform-release-candidates.v1",
    "version": version,
    "generated_at": __import__("datetime").datetime.now(__import__("datetime").timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "out_dir": str(out_dir.resolve()),
    "build_manifest": selection["build_manifest"],
    "build_schema": selection["build_schema"],
    "build_generated_at": selection.get("build_generated_at", ""),
    "required_commands": selection["required_commands"],
    "external_mutation": False,
    "candidates": candidates,
}
manifest_path = out_dir / "platform-candidates.json"
manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
print(json.dumps({
    "ok": True,
    "schema": manifest["schema_version"],
    "manifest": str(manifest_path),
    "candidate_count": len(candidates),
    "external_mutation": False,
}, indent=2))
PY

rm -f "$selection_json" "$summary_tsv"
