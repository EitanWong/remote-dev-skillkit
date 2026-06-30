package skillkit

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const InstallReportSchemaVersion = "rdev.skillkit-install-report.v1"

type InstallOptions struct {
	BundleDir   string
	Framework   string
	TargetDir   string
	Execute     bool
	Force       bool
	GeneratedAt time.Time
}

type InstallReport struct {
	SchemaVersion        string              `json:"schema_version"`
	GeneratedAt          time.Time           `json:"generated_at"`
	BundleDir            string              `json:"bundle_dir"`
	Framework            string              `json:"framework"`
	DisplayName          string              `json:"display_name"`
	TargetDir            string              `json:"target_dir"`
	Execute              bool                `json:"execute"`
	Executed             bool                `json:"executed"`
	Force                bool                `json:"force"`
	LocalMutation        bool                `json:"local_mutation"`
	ExternalMutation     bool                `json:"external_mutation"`
	BundleVerification   VerificationReport  `json:"bundle_verification"`
	Checks               []VerificationCheck `json:"checks"`
	Actions              []InstallAction     `json:"actions"`
	InstalledSkills      []string            `json:"installed_skills,omitempty"`
	ReferenceFiles       []string            `json:"reference_files,omitempty"`
	RecommendedNextSteps []string            `json:"recommended_next_steps,omitempty"`
	RecommendedActions   []string            `json:"recommended_actions,omitempty"`
}

type InstallAction struct {
	Type        string `json:"type"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Executed    bool   `json:"executed"`
	Detail      string `json:"detail,omitempty"`
}

func (r InstallReport) OK() bool {
	if len(r.Checks) == 0 {
		return false
	}
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func Install(opts InstallOptions) (InstallReport, error) {
	if strings.TrimSpace(opts.BundleDir) == "" {
		return InstallReport{}, fmt.Errorf("bundle directory is required")
	}
	if strings.TrimSpace(opts.Framework) == "" {
		return InstallReport{}, fmt.Errorf("framework is required")
	}
	if opts.GeneratedAt.IsZero() {
		opts.GeneratedAt = time.Now()
	}
	bundleDir, err := filepath.Abs(opts.BundleDir)
	if err != nil {
		return InstallReport{}, err
	}
	frameworks, err := normalizeFrameworks([]string{opts.Framework})
	if err != nil {
		return InstallReport{}, err
	}
	if len(frameworks) != 1 {
		return InstallReport{}, fmt.Errorf("exactly one framework is required")
	}
	spec := frameworkSpec(frameworks[0])
	report := InstallReport{
		SchemaVersion:    InstallReportSchemaVersion,
		GeneratedAt:      opts.GeneratedAt.UTC(),
		BundleDir:        bundleDir,
		Framework:        spec.Name,
		DisplayName:      spec.DisplayName,
		Execute:          opts.Execute,
		Force:            opts.Force,
		LocalMutation:    false,
		ExternalMutation: false,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, VerificationCheck{Name: name, Passed: passed, Detail: detail})
	}

	targetDir, targetExplicit, err := resolveInstallTarget(spec, opts.TargetDir)
	if err != nil {
		return InstallReport{}, err
	}
	report.TargetDir = targetDir
	add("framework_supported", spec.Name != "", spec.Name)
	add("target_dir_resolved", strings.TrimSpace(targetDir) != "", targetDir)
	add("generic_target_explicit", spec.Name != "generic-mcp-agent" || targetExplicit, spec.TargetEnv)
	add("target_dir_not_root", targetDir != "" && !isFilesystemRoot(targetDir), targetDir)

	bundleVerification, err := Verify(VerifyOptions{BundleDir: bundleDir, GeneratedAt: opts.GeneratedAt})
	report.BundleVerification = bundleVerification
	add("bundle_verifies", err == nil && bundleVerification.OK(), firstNonEmpty(errorString(err), bundleVerificationFailures(bundleVerification)))
	if !bundleVerification.OK() || err != nil {
		report.RecommendedActions = failedInstallActions()
		return report, nil
	}
	manifest, err := readManifest(bundleDir)
	if err != nil {
		return InstallReport{}, err
	}
	skills := skillNames(manifest)
	add("skills_present", len(skills) > 0, fmt.Sprintf("%d", len(skills)))
	sourceFailures := installSourceFailures(bundleDir, spec, skills)
	add("install_sources_present", sourceFailures == "", sourceFailures)
	conflicts := installConflicts(targetDir, skills)
	add("existing_skill_conflicts", len(conflicts) == 0 || opts.Force, strings.Join(conflicts, ","))

	report.Actions = plannedInstallActions(bundleDir, targetDir, spec, skills)
	if !report.OK() {
		report.RecommendedActions = failedInstallActions()
		return report, nil
	}
	if !opts.Execute {
		report.RecommendedNextSteps = []string{
			"Review the dry-run actions.",
			"Re-run with --execute to copy the verified Skillkit into the target skill directory.",
			"Configure the agent MCP client to execute: rdev mcp serve.",
		}
		return report, nil
	}
	if err := executeInstall(bundleDir, targetDir, spec, skills, opts.Force); err != nil {
		return InstallReport{}, err
	}
	report.Executed = true
	report.LocalMutation = true
	for i := range report.Actions {
		report.Actions[i].Executed = true
		report.Actions[i].Detail = "executed"
	}
	report.InstalledSkills = append([]string(nil), skills...)
	report.ReferenceFiles = []string{
		filepath.Join(targetDir, ".remote-dev-skillkit", "mcp", "tools.json"),
		filepath.Join(targetDir, ".remote-dev-skillkit", "frameworks", filepath.Base(spec.DocPath)),
	}
	report.RecommendedNextSteps = []string{
		"Configure the agent MCP client to execute: rdev mcp serve.",
		"Ask the agent to use host-triage before remote-vibe-coding or safe-remote-support.",
	}
	return report, nil
}

func resolveInstallTarget(spec frameworkInstallSpec, target string) (string, bool, error) {
	if strings.TrimSpace(target) != "" {
		expanded, err := expandUserPath(target)
		if err != nil {
			return "", true, err
		}
		abs, err := filepath.Abs(expanded)
		return abs, true, err
	}
	if envTarget := os.Getenv(spec.TargetEnv); strings.TrimSpace(envTarget) != "" {
		expanded, err := expandUserPath(envTarget)
		if err != nil {
			return "", true, err
		}
		abs, err := filepath.Abs(expanded)
		return abs, true, err
	}
	if spec.Name == "generic-mcp-agent" {
		return "", false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	var base string
	switch spec.Name {
	case "codex":
		base = firstNonEmpty(os.Getenv("CODEX_HOME"), filepath.Join(home, ".codex"))
	case "claude-code":
		base = firstNonEmpty(os.Getenv("CLAUDE_CODE_HOME"), filepath.Join(home, ".claude"))
	case "hermes":
		base = firstNonEmpty(os.Getenv("HERMES_HOME"), filepath.Join(home, ".hermes"))
	case "openclaw":
		base = firstNonEmpty(os.Getenv("OPENCLAW_HOME"), filepath.Join(home, ".openclaw"))
	case "opencode":
		if env := os.Getenv("OPENCODE_HOME"); env != "" {
			base = env
		} else if runtime.GOOS == "windows" && os.Getenv("APPDATA") != "" {
			base = filepath.Join(os.Getenv("APPDATA"), "opencode")
		} else {
			base = filepath.Join(home, ".config", "opencode")
		}
	default:
		return "", false, fmt.Errorf("unsupported framework %q", spec.Name)
	}
	abs, err := filepath.Abs(filepath.Join(base, "skills"))
	return abs, false, err
}

func expandUserPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"+string(filepath.Separator))), nil
	}
	return path, nil
}

func isFilesystemRoot(path string) bool {
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	return clean == parent
}

func installSourceFailures(bundleDir string, spec frameworkInstallSpec, skills []string) string {
	var failures []string
	for _, skill := range skills {
		if !safeInstallName(skill) {
			failures = append(failures, "unsafe-skill:"+skill)
			continue
		}
		if info, err := os.Stat(filepath.Join(bundleDir, "skills", skill)); err != nil || !info.IsDir() {
			failures = append(failures, "missing-skill:"+skill)
		}
	}
	for _, path := range []string{"mcp/tools.json", spec.DocPath} {
		if _, err := os.Stat(filepath.Join(bundleDir, filepath.FromSlash(path))); err != nil {
			failures = append(failures, "missing:"+path)
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func installConflicts(targetDir string, skills []string) []string {
	var conflicts []string
	if targetDir == "" {
		return conflicts
	}
	for _, skill := range skills {
		if !safeInstallName(skill) {
			continue
		}
		if _, err := os.Stat(filepath.Join(targetDir, skill)); err == nil {
			conflicts = append(conflicts, skill)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func plannedInstallActions(bundleDir, targetDir string, spec frameworkInstallSpec, skills []string) []InstallAction {
	actions := []InstallAction{
		{
			Type:        "create_target_dir",
			Destination: targetDir,
			Executed:    false,
		},
	}
	for _, skill := range skills {
		actions = append(actions, InstallAction{
			Type:        "copy_skill",
			Source:      filepath.Join(bundleDir, "skills", skill),
			Destination: filepath.Join(targetDir, skill),
			Executed:    false,
		})
	}
	actions = append(actions,
		InstallAction{
			Type:        "copy_mcp_tools",
			Source:      filepath.Join(bundleDir, "mcp", "tools.json"),
			Destination: filepath.Join(targetDir, ".remote-dev-skillkit", "mcp", "tools.json"),
			Executed:    false,
		},
		InstallAction{
			Type:        "copy_framework_doc",
			Source:      filepath.Join(bundleDir, filepath.FromSlash(spec.DocPath)),
			Destination: filepath.Join(targetDir, ".remote-dev-skillkit", "frameworks", filepath.Base(spec.DocPath)),
			Executed:    false,
		},
	)
	return actions
}

func executeInstall(bundleDir, targetDir string, spec frameworkInstallSpec, skills []string, force bool) error {
	if targetDir == "" || isFilesystemRoot(targetDir) {
		return fmt.Errorf("unsafe target directory: %s", targetDir)
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return err
	}
	for _, skill := range skills {
		if !safeInstallName(skill) {
			return fmt.Errorf("unsafe skill name: %s", skill)
		}
		src := filepath.Join(bundleDir, "skills", skill)
		dst := filepath.Join(targetDir, skill)
		if _, err := os.Stat(dst); err == nil {
			if !force {
				return fmt.Errorf("refusing to overwrite %s", dst)
			}
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
		}
		if err := copyDir(src, dst); err != nil {
			return err
		}
	}
	refDir := filepath.Join(targetDir, ".remote-dev-skillkit")
	if err := os.MkdirAll(filepath.Join(refDir, "mcp"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(refDir, "frameworks"), 0o700); err != nil {
		return err
	}
	if err := copyFileContents(filepath.Join(bundleDir, "mcp", "tools.json"), filepath.Join(refDir, "mcp", "tools.json")); err != nil {
		return err
	}
	return copyFileContents(filepath.Join(bundleDir, filepath.FromSlash(spec.DocPath)), filepath.Join(refDir, "frameworks", filepath.Base(spec.DocPath)))
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink: %s", path)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFileContents(path, target)
	})
}

func copyFileContents(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, content, 0o600)
}

func safeInstallName(name string) bool {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, `/\`) {
		return false
	}
	clean := filepath.Clean(name)
	return clean == name && clean != "." && clean != ".." && filepath.VolumeName(clean) == ""
}

func failedInstallActions() []string {
	return []string{
		"Verify the Skillkit bundle with rdev skillkit verify before installing.",
		"Use --target for generic MCP agents or custom framework paths.",
		"Use --force only after reviewing existing skill directory conflicts.",
	}
}
