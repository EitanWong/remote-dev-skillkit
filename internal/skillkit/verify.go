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

const VerificationSchemaVersion = "rdev.skillkit-bundle-verification.v1"

type VerifyOptions struct {
	BundleDir   string
	GeneratedAt time.Time
}

type VerificationReport struct {
	SchemaVersion      string              `json:"schema_version"`
	GeneratedAt        time.Time           `json:"generated_at"`
	BundleDir          string              `json:"bundle_dir"`
	ManifestPath       string              `json:"manifest_path"`
	ManifestSchema     string              `json:"manifest_schema,omitempty"`
	Checks             []VerificationCheck `json:"checks"`
	FilesVerified      int                 `json:"files_verified"`
	SkillsVerified     int                 `json:"skills_verified"`
	FrameworksVerified int                 `json:"frameworks_verified"`
	RecommendedActions []string            `json:"recommended_actions,omitempty"`
}

type VerificationCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func (r VerificationReport) OK() bool {
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

func Verify(opts VerifyOptions) (VerificationReport, error) {
	if strings.TrimSpace(opts.BundleDir) == "" {
		return VerificationReport{}, fmt.Errorf("bundle directory is required")
	}
	bundleDir, err := filepath.Abs(opts.BundleDir)
	if err != nil {
		return VerificationReport{}, err
	}
	now := opts.GeneratedAt
	if now.IsZero() {
		now = time.Now()
	}
	report := VerificationReport{
		SchemaVersion: VerificationSchemaVersion,
		GeneratedAt:   now.UTC(),
		BundleDir:     bundleDir,
		ManifestPath:  filepath.Join(bundleDir, "manifest.json"),
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, VerificationCheck{Name: name, Passed: passed, Detail: detail})
	}

	manifestBytes, err := os.ReadFile(report.ManifestPath)
	add("manifest_exists", err == nil, report.ManifestPath)
	if err != nil {
		report.RecommendedActions = failedSkillkitVerificationActions()
		return report, nil
	}
	var manifest Manifest
	err = json.Unmarshal(manifestBytes, &manifest)
	add("manifest_json_valid", err == nil, errorString(err))
	if err != nil {
		report.RecommendedActions = failedSkillkitVerificationActions()
		return report, nil
	}
	report.ManifestSchema = manifest.SchemaVersion
	add("manifest_schema", manifest.SchemaVersion == ManifestSchemaVersion, manifest.SchemaVersion)
	add("adaptive_configuration_contract", adaptiveContractFailure(manifest.AdaptiveConfiguration) == "", adaptiveContractFailure(manifest.AdaptiveConfiguration))
	add("file_entries_present", len(manifest.Files) > 0, fmt.Sprintf("%d", len(manifest.Files)))
	add("skills_present", len(manifest.Skills) > 0, fmt.Sprintf("%d", len(manifest.Skills)))
	add("frameworks_present", len(manifest.Frameworks) > 0, fmt.Sprintf("%d", len(manifest.Frameworks)))

	seenFiles := map[string]bool{}
	var duplicateFiles []string
	var unsafeFiles []string
	for _, file := range manifest.Files {
		if seenFiles[file.Path] {
			duplicateFiles = append(duplicateFiles, file.Path)
		}
		seenFiles[file.Path] = true
		if !safeBundlePath(file.Path) {
			unsafeFiles = append(unsafeFiles, file.Path)
		}
	}
	sort.Strings(duplicateFiles)
	sort.Strings(unsafeFiles)
	add("file_paths_unique", len(duplicateFiles) == 0, strings.Join(duplicateFiles, ","))
	add("file_paths_safe", len(unsafeFiles) == 0, strings.Join(unsafeFiles, ","))

	verified, fileFailures := verifyListedFiles(bundleDir, manifest.Files)
	report.FilesVerified = verified
	add("listed_files_exist", !strings.Contains(fileFailures, "missing:"), fileFailures)
	add("listed_files_sha256_match", !strings.Contains(fileFailures, "sha256:"), fileFailures)
	add("listed_files_size_match", !strings.Contains(fileFailures, "size:"), fileFailures)

	seenFiles["manifest.json"] = true
	unlisted, err := findUnlistedFiles(bundleDir, seenFiles)
	add("bundle_has_no_unlisted_files", err == nil && len(unlisted) == 0, firstNonEmpty(errorString(err), strings.Join(unlisted, ",")))

	skillFailures := verifySkills(manifest)
	report.SkillsVerified = len(manifest.Skills)
	add("required_skills_present", skillFailures == "", skillFailures)
	add("skill_paths_match_names", skillPathFailures(manifest) == "", skillPathFailures(manifest))
	add("required_skills_keep_adaptive_contract", skillsAdaptiveContractFailures(bundleDir, manifest) == "", skillsAdaptiveContractFailures(bundleDir, manifest))
	add("skill_agents_metadata", skillAgentMetadataFailures(bundleDir, manifest) == "", skillAgentMetadataFailures(bundleDir, manifest))

	frameworkFailures := missingStrings(DefaultFrameworks, manifest.Frameworks)
	report.FrameworksVerified = len(manifest.Frameworks)
	add("required_frameworks_present", frameworkFailures == "", frameworkFailures)

	requiredFileFailures := missingRequiredFiles(seenFiles)
	add("required_install_files_present", requiredFileFailures == "", requiredFileFailures)
	add("install_doc_keeps_adaptive_contract", installDocAdaptiveContractFailures(bundleDir, manifest) == "", installDocAdaptiveContractFailures(bundleDir, manifest))

	if !report.OK() {
		report.RecommendedActions = failedSkillkitVerificationActions()
	}
	return report, nil
}

func verifyListedFiles(root string, files []FileEntry) (int, string) {
	var failures []string
	verified := 0
	for _, file := range files {
		if !safeBundlePath(file.Path) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(file.Path))
		content, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, "missing:"+file.Path)
			continue
		}
		verified++
		sum := sha256.Sum256(content)
		gotSHA := "sha256:" + hex.EncodeToString(sum[:])
		if gotSHA != file.SHA256 {
			failures = append(failures, "sha256:"+file.Path)
		}
		if len(content) != file.SizeBytes {
			failures = append(failures, "size:"+file.Path)
		}
	}
	sort.Strings(failures)
	return verified, strings.Join(failures, ",")
}

func findUnlistedFiles(root string, listed map[string]bool) ([]string, error) {
	var unlisted []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		bundlePath := filepath.ToSlash(rel)
		if !listed[bundlePath] {
			unlisted = append(unlisted, bundlePath)
		}
		return nil
	})
	sort.Strings(unlisted)
	return unlisted, err
}

func verifySkills(manifest Manifest) string {
	var names []string
	for _, skill := range manifest.Skills {
		names = append(names, skill.Name)
	}
	return missingStrings([]string{"safe-remote-support", "host-triage", "remote-vibe-coding", "remote-job-review"}, names)
}

func skillPathFailures(manifest Manifest) string {
	var failures []string
	for _, skill := range manifest.Skills {
		expected := filepath.ToSlash(filepath.Join("skills", skill.Name, "SKILL.md"))
		if skill.Path != expected {
			failures = append(failures, skill.Name)
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func skillsAdaptiveContractFailures(bundleDir string, manifest Manifest) string {
	required := map[string]bool{
		"safe-remote-support": true,
		"host-triage":         true,
		"remote-vibe-coding":  true,
		"remote-job-review":   true,
	}
	var failures []string
	for _, skill := range manifest.Skills {
		if !required[skill.Name] || !safeBundlePath(skill.Path) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(bundleDir, filepath.FromSlash(skill.Path)))
		if err != nil || !skillKeepsAdaptiveContract(string(content)) {
			failures = append(failures, skill.Name)
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func skillAgentMetadataFailures(bundleDir string, manifest Manifest) string {
	required := map[string]bool{
		"safe-remote-support": true,
		"host-triage":         true,
		"remote-vibe-coding":  true,
		"remote-job-review":   true,
	}
	var failures []string
	for _, skill := range manifest.Skills {
		if !required[skill.Name] {
			continue
		}
		path := filepath.ToSlash(filepath.Join("skills", skill.Name, "agents", "openai.yaml"))
		if !safeBundlePath(path) {
			failures = append(failures, skill.Name+":unsafe-path")
			continue
		}
		content, err := os.ReadFile(filepath.Join(bundleDir, filepath.FromSlash(path)))
		if err != nil {
			failures = append(failures, skill.Name+":missing")
			continue
		}
		if !agentMetadataLooksUseful(skill.Name, string(content)) {
			failures = append(failures, skill.Name+":invalid")
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func agentMetadataLooksUseful(skillName, content string) bool {
	required := []string{
		"interface:",
		"display_name:",
		"short_description:",
		"default_prompt:",
		"$" + skillName,
		"policy:",
		"allow_implicit_invocation: true",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			return false
		}
	}
	return true
}

func installDocAdaptiveContractFailures(bundleDir string, manifest Manifest) string {
	var failures []string
	for _, path := range []string{
		"INSTALL.md",
		"frameworks/codex.md",
		"frameworks/claude-code.md",
		"frameworks/hermes.md",
		"frameworks/openclaw-opencode.md",
		"frameworks/generic-mcp-agent.md",
	} {
		if !safeBundlePath(path) {
			failures = append(failures, path)
			continue
		}
		content, err := os.ReadFile(filepath.Join(bundleDir, filepath.FromSlash(path)))
		if err != nil || !textKeepsAdaptiveContract(string(content)) {
			failures = append(failures, path)
		}
	}
	sort.Strings(failures)
	return strings.Join(failures, ",")
}

func missingRequiredFiles(listed map[string]bool) string {
	required := []string{
		"INSTALL.md",
		"mcp/tools.json",
		"frameworks/README.md",
		"frameworks/codex.md",
		"frameworks/claude-code.md",
		"frameworks/hermes.md",
		"frameworks/openclaw-opencode.md",
		"frameworks/generic-mcp-agent.md",
	}
	var missing []string
	for _, path := range required {
		if !listed[path] {
			missing = append(missing, path)
		}
	}
	return strings.Join(missing, ",")
}

func missingStrings(required, actual []string) string {
	seen := map[string]bool{}
	for _, value := range actual {
		seen[value] = true
	}
	var missing []string
	for _, value := range required {
		if !seen[value] {
			missing = append(missing, value)
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, ",")
}

func safeBundlePath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.Contains(path, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(clean) && filepath.VolumeName(clean) == ""
}

func failedSkillkitVerificationActions() []string {
	return []string{
		"Re-export the Skillkit bundle with rdev skillkit export into an empty directory.",
		"Do not install this bundle into an agent runtime until verification passes.",
		"If verification fails after download, discard the bundle and fetch it again from a trusted release source.",
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
