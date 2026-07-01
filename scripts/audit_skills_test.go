package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditSkillsAcceptsValidSkillTree(t *testing.T) {
	root := writeSkillTree(t)

	output, err := runAuditSkills(t, root)
	if err != nil {
		t.Fatalf("expected audit to pass, got %v\n%s", err, output)
	}
	if !strings.Contains(output, "skills_audit_ok=true skills=4 required=4") {
		t.Fatalf("unexpected audit output: %s", output)
	}
}

func TestAuditSkillsRejectsInvalidSkillTrees(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *testing.T, root string)
		wantErr string
	}{
		{
			name: "missing agents metadata",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				removeFile(t, filepath.Join(root, "remote-vibe-coding", "agents", "openai.yaml"))
			},
			wantErr: "remote-vibe-coding: missing agents/openai.yaml",
		},
		{
			name: "missing linked reference",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				removeFile(t, filepath.Join(root, "remote-vibe-coding", "references", "runtime-memory.md"))
			},
			wantErr: "remote-vibe-coding: missing linked reference references/runtime-memory.md",
		},
		{
			name: "hidden skill file",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "host-triage", ".DS_Store"), "junk\n")
			},
			wantErr: "hidden skill file is not allowed",
		},
		{
			name: "long reference without contents",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				var lines []string
				lines = append(lines, "# Runtime Memory")
				for i := 0; i < 101; i++ {
					lines = append(lines, "line")
				}
				writeFile(t, filepath.Join(root, "remote-vibe-coding", "references", "runtime-memory.md"), strings.Join(lines, "\n"))
			},
			wantErr: "remote-vibe-coding: long reference needs ## Contents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeSkillTree(t)
			tt.mutate(t, root)

			output, err := runAuditSkills(t, root)
			if err == nil {
				t.Fatalf("expected audit to fail\n%s", output)
			}
			if !strings.Contains(output, tt.wantErr) {
				t.Fatalf("expected %q in output:\n%s", tt.wantErr, output)
			}
		})
	}
}

func runAuditSkills(t *testing.T, skillsRoot string) (string, error) {
	t.Helper()
	repoRoot := filepath.Dir(mustGetwd(t))
	cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "audit-skills.sh"))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "SKILLS_ROOT="+skillsRoot)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeSkillTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"host-triage", "remote-job-review", "remote-vibe-coding", "safe-remote-support"} {
		writeSkill(t, root, name)
	}
	writeFile(t, filepath.Join(root, "remote-vibe-coding", "references", "runtime-memory.md"), strings.Join([]string{
		"# Runtime Memory",
		"",
		"## Contents",
		"",
		"- Scope",
		"",
		"## Scope",
		"",
		"Keep safe, redacted, scoped discoveries outside the public repo.",
	}, "\n"))
	return root
}

func writeSkill(t *testing.T, root string, name string) {
	t.Helper()
	referenceLink := ""
	if name == "remote-vibe-coding" {
		referenceLink = "\nRead [runtime memory](references/runtime-memory.md) when useful.\n"
	}
	writeFile(t, filepath.Join(root, name, "SKILL.md"), strings.Join([]string{
		"---",
		"name: " + name,
		"description: Test skill for " + name + ".",
		"---",
		"",
		"# " + name,
		referenceLink,
	}, "\n"))
	writeFile(t, filepath.Join(root, name, "agents", "openai.yaml"), strings.Join([]string{
		"interface:",
		`  display_name: "Test Skill"`,
		`  short_description: "Test skill metadata"`,
		`  default_prompt: "Use $` + name + ` to complete the remote workflow."`,
		"policy:",
		"  allow_implicit_invocation: true",
	}, "\n"))
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
