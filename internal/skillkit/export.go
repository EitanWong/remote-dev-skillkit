package skillkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ManifestSchemaVersion = "rdev.skillkit-bundle.v1"

var DefaultFrameworks = []string{"codex", "claude-code", "hermes", "openclaw", "opencode"}

type ExportOptions struct {
	SourceRoot  string
	OutDir      string
	GatewayURL  string
	GeneratedAt time.Time
}

type Manifest struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	GatewayURL    string       `json:"gateway_url,omitempty"`
	Skills        []SkillEntry `json:"skills"`
	Frameworks    []string     `json:"frameworks"`
	Files         []FileEntry  `json:"files"`
}

type SkillEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

type FileEntry struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int    `json:"size_bytes"`
	Kind      string `json:"kind"`
}

func Export(opts ExportOptions) (Manifest, error) {
	if opts.SourceRoot == "" {
		opts.SourceRoot = "."
	}
	if opts.OutDir == "" {
		return Manifest{}, fmt.Errorf("out is required")
	}
	if opts.GeneratedAt.IsZero() {
		opts.GeneratedAt = time.Now()
	}
	sourceRoot, err := filepath.Abs(opts.SourceRoot)
	if err != nil {
		return Manifest{}, err
	}
	if err := prepareOutputDir(opts.OutDir); err != nil {
		return Manifest{}, err
	}

	var files []FileEntry
	skills, skillFiles, err := copySkills(sourceRoot, opts.OutDir)
	if err != nil {
		return Manifest{}, err
	}
	files = append(files, skillFiles...)
	toolsEntry, err := copyFile(filepath.Join(sourceRoot, "mcp", "tools.json"), opts.OutDir, "mcp/tools.json", "mcp-tools")
	if err != nil {
		return Manifest{}, err
	}
	files = append(files, toolsEntry)
	generated, err := writeGeneratedDocs(opts.OutDir, opts.GatewayURL)
	if err != nil {
		return Manifest{}, err
	}
	files = append(files, generated...)

	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion,
		GeneratedAt:   opts.GeneratedAt.UTC(),
		GatewayURL:    opts.GatewayURL,
		Skills:        skills,
		Frameworks:    append([]string(nil), DefaultFrameworks...),
		Files:         files,
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	content = append(content, '\n')
	entry, err := writeBundleFile(opts.OutDir, "manifest.json", "manifest", content)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Files = append(manifest.Files, entry)
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	return manifest, nil
}

func prepareOutputDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("output directory must be empty: %s", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func copySkills(sourceRoot, outDir string) ([]SkillEntry, []FileEntry, error) {
	skillsRoot := filepath.Join(sourceRoot, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return nil, nil, err
	}
	var skills []SkillEntry
	var files []FileEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		skillRoot := filepath.Join(skillsRoot, name)
		skillFile := filepath.Join(skillRoot, "SKILL.md")
		content, err := os.ReadFile(skillFile)
		if err != nil {
			return nil, nil, fmt.Errorf("read skill %s: %w", name, err)
		}
		skills = append(skills, SkillEntry{
			Name:        name,
			Path:        filepath.ToSlash(filepath.Join("skills", name, "SKILL.md")),
			Description: frontmatterValue(string(content), "description"),
		})
		err = filepath.WalkDir(skillRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(skillsRoot, path)
			if err != nil {
				return err
			}
			entry, err := copyFile(path, outDir, filepath.ToSlash(filepath.Join("skills", rel)), "skill")
			if err != nil {
				return err
			}
			files = append(files, entry)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(skills) == 0 {
		return nil, nil, fmt.Errorf("no skills found under %s", skillsRoot)
	}
	return skills, files, nil
}

func copyFile(source, outDir, bundlePath, kind string) (FileEntry, error) {
	content, err := os.ReadFile(source)
	if err != nil {
		return FileEntry{}, err
	}
	return writeBundleFile(outDir, bundlePath, kind, content)
}

func writeGeneratedDocs(outDir, gatewayURL string) ([]FileEntry, error) {
	docs := map[string]string{
		"INSTALL.md":                      installDoc(gatewayURL),
		"frameworks/codex.md":             frameworkDoc("Codex", gatewayURL),
		"frameworks/claude-code.md":       frameworkDoc("Claude Code", gatewayURL),
		"frameworks/hermes.md":            frameworkDoc("Hermes", gatewayURL),
		"frameworks/openclaw-opencode.md": frameworkDoc("OpenClaw / OpenCode", gatewayURL),
		"frameworks/generic-mcp-agent.md": frameworkDoc("Generic MCP Agent", gatewayURL),
		"frameworks/README.md":            frameworksIndex(),
	}
	paths := make([]string, 0, len(docs))
	for path := range docs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]FileEntry, 0, len(paths))
	for _, path := range paths {
		entry, err := writeBundleFile(outDir, path, "generated-doc", []byte(docs[path]))
		if err != nil {
			return nil, err
		}
		files = append(files, entry)
	}
	return files, nil
}

func writeBundleFile(root, bundlePath, kind string, content []byte) (FileEntry, error) {
	clean := filepath.Clean(filepath.FromSlash(bundlePath))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return FileEntry{}, fmt.Errorf("invalid bundle path %q", bundlePath)
	}
	path := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return FileEntry{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return FileEntry{}, err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return FileEntry{}, err
	}
	if err := file.Close(); err != nil {
		return FileEntry{}, err
	}
	sum := sha256.Sum256(content)
	return FileEntry{
		Path:      filepath.ToSlash(clean),
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		SizeBytes: len(content),
		Kind:      kind,
	}, nil
}

func frontmatterValue(content, key string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	prefix := key + ":"
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return ""
		}
		if strings.HasPrefix(trimmed, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)), `"'`)
		}
	}
	return ""
}

func installDoc(gatewayURL string) string {
	gateway := gatewayOrPlaceholder(gatewayURL)
	return fmt.Sprintf(strings.Join([]string{
		"# Remote Dev Skillkit Install Bundle",
		"",
		"This bundle contains portable Agent Skills and MCP tool contracts for Remote Dev Skillkit.",
		"",
		"## Contents",
		"",
		"- `skills/` - agent-loadable workflows.",
		"- `mcp/tools.json` - stable tool contract metadata.",
		"- `frameworks/` - install notes for common agent frameworks.",
		"- `manifest.json` - checksums and bundle metadata.",
		"",
		"## Verify Before Installing",
		"",
		"```bash",
		"rdev skillkit verify --bundle <bundle-dir>",
		"```",
		"",
		"Do not copy skills into an agent runtime until verification returns `ok=true`.",
		"",
		"## Default Gateway",
		"",
		"%s",
		"",
		"## Generic Install Shape",
		"",
		"1. Verify the bundle with `rdev skillkit verify --bundle <bundle-dir>`.",
		"2. Copy the folders under `skills/` into your agent runtime's skill or instruction directory.",
		"3. Configure the agent's MCP client to call `rdev mcp serve` for local stdio mode, or point it at your hosted rdev MCP HTTP endpoint.",
		"4. Keep `safe-remote-support`, `host-triage`, `remote-vibe-coding`, and `remote-job-review` installed together.",
		"5. Run a read-only host triage job before any repair or coding job.",
		"6. Export an evidence bundle before declaring remote work complete.",
		"",
		"## Safety Defaults",
		"",
		"- Temporary hosts should use attended foreground mode.",
		"- Agents must request typed jobs, not raw machine access.",
		"- Package install, elevation, GUI control, service mutation, push, merge, deploy, publish, paid action, and credential changes require approval.",
		"- Evidence and audit are part of completion, not optional logs.",
		"",
	}, "\n"), gateway)
}

func frameworkDoc(name, gatewayURL string) string {
	gateway := gatewayOrPlaceholder(gatewayURL)
	return fmt.Sprintf(strings.Join([]string{
		"# %s Install Notes",
		"",
		"Use this bundle to teach %s how to request safe remote work through Remote Dev Skillkit.",
		"",
		"## Install",
		"",
		"1. Install or build the `rdev` binary.",
		"2. Verify the bundle with `rdev skillkit verify --bundle <bundle-dir>`.",
		"3. Copy `skills/*` into the framework's skill/instructions location.",
		"4. Configure MCP stdio with:",
		"",
		"   ```bash",
		"   rdev mcp serve",
		"   ```",
		"",
		"5. For hosted gateways, configure the framework's MCP HTTP or API client to use:",
		"",
		"   ```text",
		"   %s",
		"   ```",
		"",
		"6. Ask the agent to use `host-triage` first, then `safe-remote-support` or `remote-vibe-coding`, and finally `remote-job-review`.",
		"",
		"## Required Review Step",
		"",
		"Before the agent claims success, it should call the artifact/evidence tools and review the exported `rdev.evidence-bundle.v1` bundle.",
		"",
	}, "\n"), name, name, gateway)
}

func frameworksIndex() string {
	return "# Framework Install Notes\n\n" +
		"- `codex.md` - Codex-oriented install notes.\n" +
		"- `claude-code.md` - Claude Code-oriented install notes.\n" +
		"- `hermes.md` - Hermes/Lucky-oriented install notes.\n" +
		"- `openclaw-opencode.md` - OpenClaw/OpenCode-oriented install notes.\n" +
		"- `generic-mcp-agent.md` - Generic MCP-compatible agent notes.\n"
}

func gatewayOrPlaceholder(gatewayURL string) string {
	if gatewayURL == "" {
		return "<your-rdev-gateway-url>"
	}
	return gatewayURL
}
