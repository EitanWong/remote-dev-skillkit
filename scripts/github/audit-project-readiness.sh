#!/usr/bin/env bash
set -euo pipefail

repo="EitanWong/remote-dev-skillkit"
out=""

usage() {
  cat >&2 <<'USAGE'
usage: scripts/github/audit-project-readiness.sh [--repo OWNER/REPO] [--out PATH]

Audits local GitHub project-management readiness without mutating GitHub. The
report checks docs, issue/PR templates, workflow files, release planning
scripts, and the bootstrap-project dry-run backlog shape.

Options:
  --repo OWNER/REPO  Repository name to use in dry-run previews.
  --out PATH         Optional JSON output path.
  -h, --help         Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --out)
      out="${2:-}"
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

if [[ -z "$repo" ]]; then
  usage
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
dry_run_output="$(mktemp /tmp/rdev-github-project-dry-run.XXXXXX)"
trap 'rm -f "$dry_run_output"' EXIT

(
  cd "$repo_root"
  scripts/github/bootstrap-project.sh --dry-run "$repo" > "$dry_run_output"
)

python3 - "$repo_root" "$repo" "$dry_run_output" "$out" <<'PY'
import datetime
import hashlib
import json
import pathlib
import subprocess
import sys

repo_root = pathlib.Path(sys.argv[1])
repo = sys.argv[2]
dry_run_path = pathlib.Path(sys.argv[3])
out_arg = sys.argv[4]

def run_git(*args):
    try:
        return subprocess.check_output(["git", *args], cwd=repo_root, text=True).rstrip("\n")
    except Exception:
        return ""

def sha256(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()

checks = []

def add(name, ok, detail=""):
    checks.append({"name": name, "ok": bool(ok), "detail": detail})
    return bool(ok)

required_files = [
    "README.md",
    "LICENSE",
    "CHANGELOG.md",
    "SECURITY.md",
    "CONTRIBUTING.md",
    "CODE_OF_CONDUCT.md",
    "TASKS.md",
    "docs/project/PROJECT_STRUCTURE.md",
    "docs/project/GITHUB_PROJECT_MANAGEMENT.md",
    "docs/project/ROADMAP.md",
    "docs/project/ACCEPTANCE_TESTS.md",
    "docs/project/RELEASE_CHECKLIST.md",
    "docs/project/VERSIONING.md",
    ".github/PULL_REQUEST_TEMPLATE.md",
    ".github/ISSUE_TEMPLATE/config.yml",
    ".github/ISSUE_TEMPLATE/engineering-task.yml",
    ".github/ISSUE_TEMPLATE/acceptance-run.yml",
    ".github/ISSUE_TEMPLATE/security-hardening.yml",
    ".github/workflows/ci.yml",
    "scripts/github/bootstrap-project.sh",
    "scripts/github/plan-release.sh",
    "scripts/github/plan-platform-release.sh",
    "scripts/github/plan-post-release-install.sh",
    "scripts/github/verify-post-release-install-plan.sh",
]

required_i18n_files = [
    "docs/i18n/README.md",
    "docs/i18n/README.zh-CN.md",
    "docs/i18n/README.es.md",
    "docs/i18n/README.fr.md",
    "docs/i18n/README.de.md",
    "docs/i18n/README.ja.md",
    "docs/i18n/README.ko.md",
    "docs/i18n/README.pt-BR.md",
    "docs/i18n/README.hi.md",
    "docs/i18n/README.ar.md",
    "docs/i18n/README.ru.md",
]

file_entries = []
for rel in required_files + required_i18n_files:
    path = repo_root / rel
    exists = path.is_file()
    add(f"file:{rel}", exists, "present" if exists else "missing")
    file_entries.append({
        "path": rel,
        "exists": exists,
        "sha256": sha256(path) if exists else "",
        "size_bytes": path.stat().st_size if exists else 0,
    })

dry_run = dry_run_path.read_text()
label_count = sum(1 for line in dry_run.splitlines() if line.startswith("label: "))
milestone_count = sum(1 for line in dry_run.splitlines() if line.startswith("milestone: "))
issue_count = sum(1 for line in dry_run.splitlines() if line.startswith("issue: "))
add("bootstrap_dry_run_labels", label_count >= 19, f"labels={label_count}")
add("bootstrap_dry_run_milestones", milestone_count >= 5, f"milestones={milestone_count}")
add("bootstrap_dry_run_seed_issues", issue_count >= 9, f"issues={issue_count}")
add("bootstrap_dry_run_no_gh_commands", "gh " not in dry_run, "dry-run output is command-free preview")
add("bootstrap_dry_run_completed", "GitHub project bootstrap dry-run completed" in dry_run, "completion marker")

workflow = (repo_root / ".github/workflows/ci.yml").read_text() if (repo_root / ".github/workflows/ci.yml").is_file() else ""
add("ci_runs_check_script", "./scripts/check.sh" in workflow, "")
add("ci_runs_release_smoke", "./scripts/ci/release-smoke.sh" in workflow, "")

readme = (repo_root / "README.md").read_text() if (repo_root / "README.md").is_file() else ""
for phrase in [
    "Multilingual quick starts",
    "Project Structure",
    "Apache-2.0",
    "Codex",
    "Claude Code",
    "Hermes",
    "OpenClaw/OpenCode",
]:
    add("readme:" + phrase, phrase in readme, phrase)

i18n_index = (repo_root / "docs/i18n/README.md").read_text() if (repo_root / "docs/i18n/README.md").is_file() else ""
add("i18n_language_count", sum(1 for rel in required_i18n_files if (repo_root / rel).is_file()) >= 10, f"files={sum(1 for rel in required_i18n_files if (repo_root / rel).is_file())}")
for rel in required_i18n_files[1:]:
    add("i18n_index:" + rel, pathlib.Path(rel).name in i18n_index, rel)

project_structure = (repo_root / "docs/project/PROJECT_STRUCTURE.md").read_text() if (repo_root / "docs/project/PROJECT_STRUCTURE.md").is_file() else ""
for phrase in ["cmd/", "internal/", "pkg/adapterkit/", "skills/", "Public Repository Hygiene"]:
    add("project_structure:" + phrase, phrase in project_structure, phrase)

project_doc = (repo_root / "docs/project/GITHUB_PROJECT_MANAGEMENT.md").read_text() if (repo_root / "docs/project/GITHUB_PROJECT_MANAGEMENT.md").is_file() else ""
for phrase in [
    "No GitHub repository, issues, labels, milestones, project board, release, or push",
    "Local Release Evidence Before External GitHub Mutation",
    "Seed Issues To Create After Approval",
    "Recommended sequence after explicit approval",
]:
    add("project_doc:" + phrase[:48], phrase in project_doc, phrase)

dirty = run_git("status", "--short")
head = run_git("rev-parse", "--short", "HEAD")
branch = run_git("branch", "--show-current")
remote = run_git("remote", "-v")

ok = all(check["ok"] for check in checks)
report = {
    "schema_version": "rdev.github-project-readiness.v1",
    "ok": ok,
    "external_mutation": False,
    "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
    "repo": repo,
    "git": {
        "head": head,
        "branch": branch,
        "dirty": bool(dirty),
        "dirty_files": dirty.splitlines() if dirty else [],
        "remotes": remote.splitlines() if remote else [],
    },
    "bootstrap_dry_run": {
        "labels": label_count,
        "milestones": milestone_count,
        "seed_issues": issue_count,
        "sha256": sha256(dry_run_path),
    },
    "files": file_entries,
    "checks": checks,
    "failed_checks": [check for check in checks if not check["ok"]],
}

content = json.dumps(report, indent=2) + "\n"
if out_arg:
    out_path = pathlib.Path(out_arg).expanduser()
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(content)
print(content, end="")
if not ok:
    raise SystemExit(1)
PY
