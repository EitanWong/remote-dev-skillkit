#!/usr/bin/env bash
set -euo pipefail

python3 - <<'PY'
import re
import sys
from pathlib import Path

root = Path("skills")
failures = []
required_skills = {
    "host-triage",
    "remote-job-review",
    "remote-vibe-coding",
    "safe-remote-support",
}


def fail(message):
    failures.append(message)


def read_text(path):
    return path.read_text(encoding="utf-8")


def frontmatter(content):
    match = re.match(r"^---\n(.*?)\n---\n", content, re.S)
    if not match:
        return None
    values = {}
    for line in match.group(1).splitlines():
        if not line.strip():
            continue
        if ":" not in line:
            return None
        key, value = line.split(":", 1)
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


if not root.is_dir():
    fail("skills directory is missing")
else:
    for path in sorted(root.rglob("*")):
        if path.name.startswith("."):
            fail(f"hidden skill file is not allowed: {path}")

    skill_dirs = [path for path in sorted(root.iterdir()) if path.is_dir()]
    names = {path.name for path in skill_dirs}
    for name in sorted(required_skills - names):
        fail(f"required skill missing: {name}")

    for skill_dir in skill_dirs:
        name = skill_dir.name
        if not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", name):
            fail(f"skill directory name must be hyphen-case: {name}")

        skill_md = skill_dir / "SKILL.md"
        if not skill_md.is_file():
            fail(f"{name}: missing SKILL.md")
            continue

        content = read_text(skill_md)
        lines = content.splitlines()
        if len(lines) > 500:
            fail(f"{name}: SKILL.md exceeds 500 lines ({len(lines)})")

        fm = frontmatter(content)
        if fm is None:
            fail(f"{name}: invalid SKILL.md frontmatter")
        else:
            keys = set(fm)
            if keys != {"name", "description"}:
                fail(f"{name}: SKILL.md frontmatter must contain only name and description")
            if fm.get("name") != name:
                fail(f"{name}: frontmatter name does not match directory")
            description = fm.get("description", "")
            if not description:
                fail(f"{name}: missing description")
            if len(description) > 1024:
                fail(f"{name}: description exceeds 1024 characters")
            if "<" in description or ">" in description:
                fail(f"{name}: description must not contain angle brackets")

        agents = skill_dir / "agents" / "openai.yaml"
        if not agents.is_file():
            fail(f"{name}: missing agents/openai.yaml")
        else:
            agent_text = read_text(agents)
            for needle in [
                "interface:",
                "display_name:",
                "short_description:",
                "default_prompt:",
                f"${name}",
                "policy:",
                "allow_implicit_invocation: true",
            ]:
                if needle not in agent_text:
                    fail(f"{name}: agents/openai.yaml missing {needle}")

        for extra in ["README.md", "CHANGELOG.md", "INSTALLATION_GUIDE.md", "QUICK_REFERENCE.md"]:
            if (skill_dir / extra).exists():
                fail(f"{name}: unsupported auxiliary skill doc {extra}")

        for link in re.findall(r"\]\((references/[^)]+)\)", content):
            target = skill_dir / link
            if not target.is_file():
                fail(f"{name}: missing linked reference {link}")

        references = skill_dir / "references"
        if references.exists():
            if not references.is_dir():
                fail(f"{name}: references must be a directory")
            else:
                for ref in sorted(references.rglob("*")):
                    if ref.is_dir():
                        if ref != references:
                            fail(f"{name}: nested reference directories are not allowed: {ref}")
                        continue
                    if ref.suffix != ".md":
                        fail(f"{name}: reference files must be markdown: {ref}")
                        continue
                    ref_text = read_text(ref)
                    ref_lines = ref_text.splitlines()
                    if len(ref_lines) > 100 and "## Contents" not in ref_text:
                        fail(f"{name}: long reference needs ## Contents: {ref}")

if failures:
    print("skills_audit_ok=false", file=sys.stderr)
    for failure in failures:
        print(failure, file=sys.stderr)
    sys.exit(1)

print(f"skills_audit_ok=true skills={len(required_skills)}")
PY
