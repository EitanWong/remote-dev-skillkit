#!/usr/bin/env bash
set -euo pipefail

out_dir="dist/artifacts"
version="${RDEV_BUILD_VERSION:-0.0.1-dev}"
targets="${RDEV_BUILD_TARGETS:-$(go env GOOS)/$(go env GOARCH)}"
commands="rdev,rdev-host,rdev-gateway,rdev-mcp,rdev-verify"
clean=false

usage() {
  cat <<'EOF'
Usage: scripts/release/build-artifacts.sh [options]

Build release artifacts without signing or publishing them.

Options:
  --out DIR             Output directory. Default: dist/artifacts
  --version VERSION     Version embedded in binaries. Default: RDEV_BUILD_VERSION or 0.0.1-dev
  --targets LIST        Comma-separated GOOS/GOARCH list. Default: current Go target
  --commands LIST       Comma-separated command list. Default: rdev,rdev-host,rdev-gateway,rdev-mcp,rdev-verify
  --clean               Remove output directory before building
  -h, --help            Show this help

Environment:
  RDEV_CGO_ENABLED      Override CGO_ENABLED for all targets. By default, native darwin builds use cgo so macOS Keychain support is compiled in; cross-target builds use CGO_ENABLED=0.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out)
      out_dir="${2:?missing value for --out}"
      shift 2
      ;;
    --version)
      version="${2:?missing value for --version}"
      shift 2
      ;;
    --targets)
      targets="${2:?missing value for --targets}"
      shift 2
      ;;
    --commands)
      commands="${2:?missing value for --commands}"
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

if [[ "$clean" == true ]]; then
  rm -rf "$out_dir"
fi
mkdir -p "$out_dir"

generated_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
checksums_path="$out_dir/checksums.txt"
tsv_path="$out_dir/.build-artifacts.tsv"
: > "$checksums_path"
: > "$tsv_path"
source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty=false
if [[ -n "$source_commit" ]] && [[ -n "$(git status --short 2>/dev/null || true)" ]]; then
  source_dirty=true
fi

IFS=',' read -r -a target_list <<< "$targets"
IFS=',' read -r -a command_list <<< "$commands"
host_goos="$(go env GOOS)"
host_goarch="$(go env GOARCH)"

for target in "${target_list[@]}"; do
  target="${target//[[:space:]]/}"
  [[ -n "$target" ]] || continue
  goos="${target%%/*}"
  goarch="${target##*/}"
  if [[ "$goos" == "$target" || -z "$goos" || -z "$goarch" ]]; then
    echo "target must be formatted as GOOS/GOARCH: $target" >&2
    exit 2
  fi
  cgo_enabled="${RDEV_CGO_ENABLED:-0}"
  if [[ -z "${RDEV_CGO_ENABLED:-}" && "$goos" == "darwin" && "$goos" == "$host_goos" && "$goarch" == "$host_goarch" ]]; then
    cgo_enabled=1
  fi
  suffix=""
  if [[ "$goos" == "windows" ]]; then
    suffix=".exe"
  fi
  target_dir="$out_dir/${goos}-${goarch}"
  mkdir -p "$target_dir"
  for command in "${command_list[@]}"; do
    command="${command//[[:space:]]/}"
    [[ -n "$command" ]] || continue
    package="./cmd/$command"
    if [[ ! -d "$package" ]]; then
      echo "command package does not exist: $package" >&2
      exit 2
    fi
    artifact="$target_dir/$command$suffix"
    echo "building $command for $goos/$goarch cgo=$cgo_enabled -> $artifact" >&2
    CGO_ENABLED="$cgo_enabled" GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "-s -w -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.Name=$command -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.Version=$version -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.Commit=$source_commit -X github.com/EitanWong/remote-dev-skillkit/internal/buildinfo.BuildTime=$generated_at" \
      -o "$artifact" "$package"
    sha="$(shasum -a 256 "$artifact" | awk '{print $1}')"
    size="$(wc -c < "$artifact" | tr -d ' ')"
    rel="${artifact#"$out_dir"/}"
    printf '%s  %s\n' "$sha" "$rel" >> "$checksums_path"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$command" "$target" "$goos" "$goarch" "$rel" "$sha" "$size" "$cgo_enabled" >> "$tsv_path"
  done
done

python3 - "$out_dir" "$version" "$generated_at" "$targets" "$commands" "$tsv_path" "$source_commit" "$source_dirty" <<'PY'
import json
import os
import pathlib
import sys

out_dir, version, generated_at, targets, commands, tsv_path, source_commit, source_dirty = sys.argv[1:]
artifacts = []
for line in pathlib.Path(tsv_path).read_text().splitlines():
    command, target, goos, goarch, rel, sha, size, cgo_enabled = line.split("\t")
    artifacts.append({
        "name": pathlib.Path(rel).name,
        "command": command,
        "target": target,
        "goos": goos,
        "goarch": goarch,
        "path": rel,
        "sha256": sha,
        "size_bytes": int(size),
        "cgo_enabled": cgo_enabled == "1",
    })

def spdx_id(value):
    chars = []
    for char in value:
        if char.isalnum() or char in ".-":
            chars.append("-" if char == "." else char)
        else:
            chars.append("-")
    out = "".join(chars).strip("-")
    return out or "artifact"

sbom = {
    "SPDXID": "SPDXRef-DOCUMENT",
    "spdxVersion": "SPDX-2.3",
    "name": f"remote-dev-skillkit-{version}",
    "dataLicense": "CC0-1.0",
    "documentNamespace": f"https://example.com/remote-dev-skillkit/sbom/{spdx_id(version)}",
    "creationInfo": {
        "created": generated_at,
        "creators": ["Tool: scripts/release/build-artifacts.sh"],
    },
    "packages": [{
        "SPDXID": "SPDXRef-Package-remote-dev-skillkit",
        "name": "remote-dev-skillkit",
        "versionInfo": version,
        "downloadLocation": "NOASSERTION",
        "filesAnalyzed": True,
        "licenseConcluded": "Apache-2.0",
        "licenseDeclared": "Apache-2.0",
        "copyrightText": "NOASSERTION",
    }],
    "files": [{
        "SPDXID": f"SPDXRef-File-{spdx_id(item['target'] + '-' + item['name'])}",
        "fileName": "./" + item["path"],
        "fileTypes": ["BINARY"],
        "checksums": [{"algorithm": "SHA256", "checksumValue": item["sha256"]}],
        "licenseConcluded": "NOASSERTION",
        "copyrightText": "NOASSERTION",
        "comment": f"{item['command']} for {item['target']} ({item['size_bytes']} bytes)",
    } for item in artifacts],
}
sbom_path = pathlib.Path(out_dir) / "sbom.spdx.json"
sbom_path.write_text(json.dumps(sbom, indent=2) + "\n")
import hashlib
sbom_sha = hashlib.sha256(sbom_path.read_bytes()).hexdigest()
with (pathlib.Path(out_dir) / "checksums.txt").open("a", encoding="utf-8") as handle:
    handle.write(f"{sbom_sha}  sbom.spdx.json\n")

provenance = {
    "schema_version": "rdev.release-provenance.v1",
    "version": version,
    "generated_at": generated_at,
    "builder": {
        "id": "remote-dev-skillkit/scripts",
        "command": "scripts/release/build-artifacts.sh",
    },
    "invocation": {
        "command": "scripts/release/build-artifacts.sh",
        "args": [
            "--out", "<out-dir>",
            "--version", version,
            "--targets", targets,
            "--commands", commands,
        ],
    },
    "source": {
        "repository": "remote-dev-skillkit",
        "commit": source_commit,
        "dirty": source_dirty.lower() == "true",
    },
    "external_mutation": False,
    "subjects": [{
        "name": item["name"],
        "path": item["path"],
        "sha256": item["sha256"],
        "size_bytes": item["size_bytes"],
        "kind": "artifact",
    } for item in artifacts] + [{
        "name": "sbom.spdx.json",
        "path": "sbom.spdx.json",
        "sha256": sbom_sha,
        "size_bytes": sbom_path.stat().st_size,
        "kind": "sbom",
    }],
}
provenance_path = pathlib.Path(out_dir) / "provenance.json"
provenance_path.write_text(json.dumps(provenance, indent=2) + "\n")
provenance_sha = hashlib.sha256(provenance_path.read_bytes()).hexdigest()
with (pathlib.Path(out_dir) / "checksums.txt").open("a", encoding="utf-8") as handle:
    handle.write(f"{provenance_sha}  provenance.json\n")

payload = {
    "schema_version": "rdev.build-artifacts.v1",
    "version": version,
    "generated_at": generated_at,
    "out_dir": ".",
    "checksums_path": "checksums.txt",
    "sbom_path": "sbom.spdx.json",
    "provenance_path": "provenance.json",
    "targets": [value.strip() for value in targets.split(",") if value.strip()],
    "commands": [value.strip() for value in commands.split(",") if value.strip()],
    "artifacts": artifacts,
}
manifest = pathlib.Path(out_dir) / "build-artifacts.json"
manifest.write_text(json.dumps(payload, indent=2) + "\n")
print(json.dumps({
    "ok": True,
    "schema": payload["schema_version"],
    "manifest": str(manifest),
    "checksums": str(pathlib.Path(out_dir) / "checksums.txt"),
    "sbom": str(sbom_path),
    "provenance": str(provenance_path),
    "artifact_count": len(artifacts),
}, indent=2))
PY

rm -f "$tsv_path"
