package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const PostReleaseDownloadScaffoldSchemaVersion = "rdev.post-release-download-evidence-scaffold.v1"
const PostReleaseDownloadStatusSchemaVersion = "rdev.post-release-download-evidence-status.v1"

type PostReleaseDownloadScaffoldOptions struct {
	PlanPath             string
	PlanVerificationPath string
	OutDir               string
	CreatePlaceholders   bool
	Force                bool
	Now                  time.Time
}

type PostReleaseDownloadStatusOptions struct {
	ScaffoldPath string
	Now          time.Time
}

type PostReleaseDownloadEvidenceScaffold struct {
	SchemaVersion        string                    `json:"schema_version"`
	GeneratedAt          time.Time                 `json:"generated_at"`
	OK                   bool                      `json:"ok"`
	ReadyForPackaging    bool                      `json:"ready_for_packaging"`
	PlanPath             string                    `json:"plan_path"`
	PlanVerificationPath string                    `json:"plan_verification_path"`
	Repo                 string                    `json:"repo,omitempty"`
	Tag                  string                    `json:"tag,omitempty"`
	OutDir               string                    `json:"out_dir"`
	PlatformEvidenceDir  string                    `json:"platform_evidence_dir"`
	SkillkitEvidenceDir  string                    `json:"skillkit_evidence_dir,omitempty"`
	SkillkitIncluded     bool                      `json:"skillkit_included"`
	CreatePlaceholders   bool                      `json:"create_placeholders"`
	EvidenceFiles        []PostReleaseEvidenceFile `json:"evidence_files"`
	Commands             PostReleaseCommands       `json:"commands"`
	ChecklistPath        string                    `json:"checklist_path"`
	ReportPath           string                    `json:"report_path"`
	PlanCopyPath         string                    `json:"plan_copy_path"`
	PlanVerificationCopy string                    `json:"plan_verification_copy_path"`
	Checks               []Check                   `json:"checks"`
	RecommendedActions   []string                  `json:"recommended_actions,omitempty"`
}

type PostReleaseDownloadEvidenceStatus struct {
	SchemaVersion      string                    `json:"schema_version"`
	GeneratedAt        time.Time                 `json:"generated_at"`
	OK                 bool                      `json:"ok"`
	ReadyForPackaging  bool                      `json:"ready_for_packaging"`
	ScaffoldPath       string                    `json:"scaffold_path"`
	ReportPath         string                    `json:"report_path"`
	Repo               string                    `json:"repo,omitempty"`
	Tag                string                    `json:"tag,omitempty"`
	RequiredReady      int                       `json:"required_ready"`
	RequiredTotal      int                       `json:"required_total"`
	PlaceholderCount   int                       `json:"placeholder_count"`
	MissingCount       int                       `json:"missing_count"`
	EmptyCount         int                       `json:"empty_count"`
	EvidenceFiles      []PostReleaseEvidenceFile `json:"evidence_files"`
	Commands           PostReleaseCommands       `json:"commands"`
	Checks             []Check                   `json:"checks"`
	RecommendedActions []string                  `json:"recommended_actions,omitempty"`
}

type PostReleaseEvidenceFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Exists      bool   `json:"exists"`
	Placeholder bool   `json:"placeholder"`
	Empty       bool   `json:"empty"`
	SizeBytes   int64  `json:"size_bytes"`
	Ready       bool   `json:"ready"`
}

type PostReleaseCommands struct {
	Package []string `json:"package"`
	Verify  []string `json:"verify"`
}

func ScaffoldPostReleaseDownloadEvidence(opts PostReleaseDownloadScaffoldOptions) (PostReleaseDownloadEvidenceScaffold, error) {
	if strings.TrimSpace(opts.PlanPath) == "" {
		return PostReleaseDownloadEvidenceScaffold{}, fmt.Errorf("post-release install plan is required")
	}
	if strings.TrimSpace(opts.PlanVerificationPath) == "" {
		return PostReleaseDownloadEvidenceScaffold{}, fmt.Errorf("post-release install plan verification is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return PostReleaseDownloadEvidenceScaffold{}, fmt.Errorf("output directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	if err := preparePostReleaseScaffoldOut(outDir, opts.Force); err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	plan, planContent, err := readPostReleaseInstallPlan(opts.PlanPath)
	if err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	planVerificationContent, err := os.ReadFile(opts.PlanVerificationPath)
	if err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	platformDir := filepath.Join(outDir, "platform-download-evidence")
	skillkitDir := filepath.Join(outDir, "skillkit-download-evidence")
	scaffold := PostReleaseDownloadEvidenceScaffold{
		SchemaVersion:        PostReleaseDownloadScaffoldSchemaVersion,
		GeneratedAt:          now.UTC(),
		OK:                   true,
		ReadyForPackaging:    false,
		PlanPath:             opts.PlanPath,
		PlanVerificationPath: opts.PlanVerificationPath,
		Repo:                 plan.Repo,
		Tag:                  plan.Tag,
		OutDir:               outDir,
		PlatformEvidenceDir:  platformDir,
		SkillkitIncluded:     postReleasePlanHasSkillkit(plan),
		CreatePlaceholders:   opts.CreatePlaceholders,
		ChecklistPath:        filepath.Join(outDir, "AGENT_CHECKLIST.md"),
		ReportPath:           filepath.Join(outDir, "scaffold-report.json"),
		PlanCopyPath:         filepath.Join(outDir, "post-release-install-plan.json"),
		PlanVerificationCopy: filepath.Join(outDir, "post-release-install-verification.json"),
	}
	if scaffold.SkillkitIncluded {
		scaffold.SkillkitEvidenceDir = skillkitDir
	}
	add := func(name string, passed bool, detail string) {
		scaffold.Checks = append(scaffold.Checks, Check{Name: name, Passed: passed, Detail: detail})
		if !passed {
			scaffold.OK = false
		}
	}
	add("post_release_plan_schema", plan.SchemaVersion == "rdev.post-release-install-plan.v1", plan.SchemaVersion)
	add("post_release_plan_verification_ok", postReleaseVerificationContentOK(planVerificationContent), opts.PlanVerificationPath)
	add("repo_present", strings.TrimSpace(plan.Repo) != "", plan.Repo)
	add("tag_present", strings.TrimSpace(plan.Tag) != "", plan.Tag)
	targets := postReleasePlanTargets(plan)
	add("platform_targets_present", len(targets) > 0, fmt.Sprintf("%d", len(targets)))
	for _, target := range targets {
		slug := postReleaseTargetSlug(target)
		scaffold.EvidenceFiles = append(scaffold.EvidenceFiles,
			postReleaseScaffoldFile("platform-"+slug+"-transcript", filepath.Join(platformDir, slug+"-transcript.txt"), "platform transcript for "+target),
			postReleaseScaffoldFile("platform-"+slug+"-candidate-verify", filepath.Join(platformDir, slug+"-candidate-verify.json"), "rdev release verify-candidate output with ok=true for "+target),
			postReleaseScaffoldFile("platform-"+slug+"-bundle-verify", filepath.Join(platformDir, slug+"-bundle-verify.json"), "rdev-verify --bundle output with ok=true for "+target),
		)
	}
	if scaffold.SkillkitIncluded {
		scaffold.EvidenceFiles = append(scaffold.EvidenceFiles,
			postReleaseScaffoldFile("skillkit-transcript", filepath.Join(skillkitDir, "skillkit-transcript.txt"), "Skillkit public archive download transcript"),
			postReleaseScaffoldFile("skillkit-verify", filepath.Join(skillkitDir, "skillkit-verify.json"), "rdev skillkit verify output with ok=true"),
		)
	}
	if err := os.MkdirAll(platformDir, 0o755); err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	if scaffold.SkillkitIncluded {
		if err := os.MkdirAll(skillkitDir, 0o755); err != nil {
			return PostReleaseDownloadEvidenceScaffold{}, err
		}
	}
	if err := os.WriteFile(scaffold.PlanCopyPath, append(planContent, '\n'), 0o644); err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	if err := os.WriteFile(scaffold.PlanVerificationCopy, append(planVerificationContent, '\n'), 0o644); err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	for i := range scaffold.EvidenceFiles {
		file := &scaffold.EvidenceFiles[i]
		if opts.CreatePlaceholders {
			if err := writePostReleasePlaceholder(file.Path, file.Name); err != nil {
				return PostReleaseDownloadEvidenceScaffold{}, err
			}
		}
		*file = postReleaseStatusForFile(*file)
	}
	scaffold.Commands = PostReleaseCommands{
		Package: []string{
			"rdev", "acceptance", "package-post-release-download",
			"--plan", scaffold.PlanCopyPath,
			"--plan-verification", scaffold.PlanVerificationCopy,
			"--out", filepath.Join(outDir, "package"),
			"--evidence-dir", platformDir,
		},
		Verify: []string{"rdev", "acceptance", "verify-post-release-download-package", "--package", filepath.Join(outDir, "package", "package.json")},
	}
	if scaffold.SkillkitIncluded {
		scaffold.Commands.Package = append(scaffold.Commands.Package, "--skillkit-evidence-dir", skillkitDir)
	}
	scaffold.RecommendedActions = []string{
		"Run the generated post-release verification scripts after GitHub Release assets exist.",
		"Replace every scaffold placeholder with real redacted public download evidence before packaging.",
		"Run rdev acceptance post-release-evidence-status and require ready_for_packaging=true before package-post-release-download.",
	}
	sort.Slice(scaffold.EvidenceFiles, func(i, j int) bool { return scaffold.EvidenceFiles[i].Path < scaffold.EvidenceFiles[j].Path })
	if err := os.WriteFile(scaffold.ChecklistPath, []byte(renderPostReleaseChecklist(scaffold)), 0o644); err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	report, err := json.MarshalIndent(scaffold, "", "  ")
	if err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	if err := os.WriteFile(scaffold.ReportPath, append(report, '\n'), 0o644); err != nil {
		return PostReleaseDownloadEvidenceScaffold{}, err
	}
	return scaffold, nil
}

func StatusPostReleaseDownloadEvidence(opts PostReleaseDownloadStatusOptions) (PostReleaseDownloadEvidenceStatus, error) {
	if strings.TrimSpace(opts.ScaffoldPath) == "" {
		return PostReleaseDownloadEvidenceStatus{}, fmt.Errorf("scaffold is required")
	}
	scaffoldPath, err := filepath.Abs(opts.ScaffoldPath)
	if err != nil {
		return PostReleaseDownloadEvidenceStatus{}, err
	}
	reportPath := scaffoldPath
	if info, err := os.Stat(scaffoldPath); err == nil && info.IsDir() {
		reportPath = filepath.Join(scaffoldPath, "scaffold-report.json")
	}
	content, err := os.ReadFile(reportPath)
	if err != nil {
		return PostReleaseDownloadEvidenceStatus{}, err
	}
	var scaffold PostReleaseDownloadEvidenceScaffold
	if err := json.Unmarshal(content, &scaffold); err != nil {
		return PostReleaseDownloadEvidenceStatus{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	status := PostReleaseDownloadEvidenceStatus{
		SchemaVersion: PostReleaseDownloadStatusSchemaVersion,
		GeneratedAt:   now.UTC(),
		OK:            true,
		ScaffoldPath:  scaffoldPath,
		ReportPath:    reportPath,
		Repo:          scaffold.Repo,
		Tag:           scaffold.Tag,
		Commands:      scaffold.Commands,
		RecommendedActions: []string{
			"Collect or replace every required post-release download evidence file before packaging.",
			"Run the package command only when ready_for_packaging is true.",
			"Run the matching verify command and require ok=true before making any public download acceptance claim.",
		},
	}
	add := func(name string, passed bool, detail string) {
		status.Checks = append(status.Checks, Check{Name: name, Passed: passed, Detail: detail})
		if !passed {
			status.OK = false
		}
	}
	add("scaffold_schema", scaffold.SchemaVersion == PostReleaseDownloadScaffoldSchemaVersion, scaffold.SchemaVersion)
	add("scaffold_ok", scaffold.OK, fmt.Sprintf("%t", scaffold.OK))
	add("package_command_declared", len(scaffold.Commands.Package) > 0, strings.Join(scaffold.Commands.Package, " "))
	add("verify_command_declared", len(scaffold.Commands.Verify) > 0, strings.Join(scaffold.Commands.Verify, " "))
	for _, file := range scaffold.EvidenceFiles {
		entry := postReleaseStatusForFile(file)
		if entry.Required {
			status.RequiredTotal++
			if entry.Ready {
				status.RequiredReady++
			}
			if !entry.Exists {
				status.MissingCount++
			}
			if entry.Empty {
				status.EmptyCount++
			}
			if entry.Placeholder {
				status.PlaceholderCount++
			}
			add("required_evidence_ready:"+entry.Name, entry.Ready, entry.Path)
		}
		status.EvidenceFiles = append(status.EvidenceFiles, entry)
	}
	status.ReadyForPackaging = status.OK &&
		status.RequiredTotal > 0 &&
		status.RequiredReady == status.RequiredTotal &&
		status.MissingCount == 0 &&
		status.EmptyCount == 0 &&
		status.PlaceholderCount == 0
	add("ready_for_packaging", status.ReadyForPackaging, fmt.Sprintf("%d/%d required ready", status.RequiredReady, status.RequiredTotal))
	if status.ReadyForPackaging {
		status.RecommendedActions = []string{
			"Run the package command from this report.",
			"Run the matching verify command and require ok=true before making any public download acceptance claim.",
		}
	}
	sort.Slice(status.EvidenceFiles, func(i, j int) bool { return status.EvidenceFiles[i].Path < status.EvidenceFiles[j].Path })
	return status, nil
}

func preparePostReleaseScaffoldOut(dir string, force bool) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("out exists and is not a directory: %s", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			if !force {
				return fmt.Errorf("out must be empty or use --force: %s", dir)
			}
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

func postReleaseScaffoldFile(name, path, description string) PostReleaseEvidenceFile {
	return PostReleaseEvidenceFile{
		Name:        name,
		Path:        path,
		Kind:        name,
		Required:    true,
		Description: description,
	}
}

func postReleaseStatusForFile(file PostReleaseEvidenceFile) PostReleaseEvidenceFile {
	info, err := os.Stat(file.Path)
	file.Exists = false
	file.Empty = false
	file.Placeholder = false
	file.SizeBytes = 0
	file.Ready = false
	if err != nil || info.IsDir() {
		return file
	}
	file.Exists = true
	file.SizeBytes = info.Size()
	file.Empty = info.Size() == 0
	if content, err := os.ReadFile(file.Path); err == nil {
		file.Placeholder = evidenceContentIsPlaceholder(content)
	}
	file.Ready = file.Exists && !file.Empty && !file.Placeholder
	return file
}

func postReleaseVerificationContentOK(content []byte) bool {
	var value struct {
		OK bool `json:"ok"`
	}
	return json.Unmarshal(content, &value) == nil && value.OK
}

func writePostReleasePlaceholder(path, name string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "PLACEHOLDER ONLY - replace with real redacted evidence before packaging.\nEvidence: " + name + "\n"
	if strings.HasSuffix(path, ".json") {
		content = "{\n  \"placeholder\": true,\n  \"replace_before_packaging\": true,\n  \"evidence_name\": " + quoteJSON(name) + "\n}\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func renderPostReleaseChecklist(scaffold PostReleaseDownloadEvidenceScaffold) string {
	var b strings.Builder
	b.WriteString("# Post-Release Download Evidence Checklist\n\n")
	b.WriteString("- Schema: `" + scaffold.SchemaVersion + "`\n")
	b.WriteString("- Repo: `" + scaffold.Repo + "`\n")
	b.WriteString("- Tag: `" + scaffold.Tag + "`\n")
	b.WriteString("- Ready for packaging: `false` until every required file contains real redacted evidence.\n")
	b.WriteString("- Package command: `" + strings.Join(scaffold.Commands.Package, " ") + "`\n")
	b.WriteString("- Verify command: `" + strings.Join(scaffold.Commands.Verify, " ") + "`\n\n")
	b.WriteString("## Evidence Files\n\n")
	for _, file := range scaffold.EvidenceFiles {
		b.WriteString("- [ ] `" + file.Path + "`: " + file.Description + "\n")
	}
	b.WriteString("\n## Final Gate\n\n")
	b.WriteString("- [ ] Run `rdev acceptance post-release-evidence-status --scaffold " + scaffold.OutDir + "` and require `ready_for_packaging=true`.\n")
	b.WriteString("- [ ] Run the package command.\n")
	b.WriteString("- [ ] Run the verify command and require `ok=true` before making any public download acceptance claim.\n")
	return b.String()
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
