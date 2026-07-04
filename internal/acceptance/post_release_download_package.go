package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const PostReleaseDownloadPackageSchemaVersion = "rdev.acceptance-package.post-release-download.v1"
const PostReleaseDownloadVerificationSchemaVersion = "rdev.acceptance-verification.post-release-download-package.v1"

type PostReleaseDownloadPackageOptions struct {
	PlanPath             string
	PlanVerificationPath string
	OutDir               string
	EvidenceDir          string
	SkillkitEvidenceDir  string
	NotesPath            string
	Now                  time.Time
}

type PostReleaseDownloadAcceptancePackage struct {
	SchemaVersion       string                  `json:"schema_version"`
	GeneratedAt         time.Time               `json:"generated_at"`
	OutDir              string                  `json:"out_dir"`
	Plan                string                  `json:"plan"`
	PlanVerification    string                  `json:"plan_verification"`
	Repo                string                  `json:"repo,omitempty"`
	Tag                 string                  `json:"tag,omitempty"`
	PlatformTargets     []string                `json:"platform_targets"`
	SkillkitIncluded    bool                    `json:"skillkit_included"`
	Checks              []Check                 `json:"checks"`
	Files               []AcceptancePackageFile `json:"files"`
	RedactionRuleCounts map[string]int          `json:"redaction_rule_counts,omitempty"`
	RequiredEvidence    []string                `json:"required_evidence"`
	RecommendedActions  []string                `json:"recommended_actions,omitempty"`
}

type PostReleaseDownloadAcceptanceVerification struct {
	SchemaVersion      string                  `json:"schema_version"`
	PackagePath        string                  `json:"package_path"`
	PackageSchema      string                  `json:"package_schema"`
	Repo               string                  `json:"repo,omitempty"`
	Tag                string                  `json:"tag,omitempty"`
	PlatformTargets    []string                `json:"platform_targets"`
	SkillkitIncluded   bool                    `json:"skillkit_included"`
	GeneratedAt        time.Time               `json:"generated_at"`
	Checks             []Check                 `json:"checks"`
	Files              []RelayPackageFileCheck `json:"files"`
	RecommendedActions []string                `json:"recommended_actions,omitempty"`
}

type postReleaseInstallPlan struct {
	SchemaVersion string `json:"schema_version"`
	Repo          string `json:"repo"`
	Tag           string `json:"tag"`
	Platforms     []struct {
		Target string `json:"target"`
	} `json:"platforms"`
	Skillkit map[string]any `json:"skillkit"`
}

func (p PostReleaseDownloadAcceptancePackage) OK() bool {
	if len(p.Checks) == 0 || len(p.PlatformTargets) == 0 {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (v PostReleaseDownloadAcceptanceVerification) OK() bool {
	if len(v.Checks) == 0 || len(v.Files) == 0 || len(v.PlatformTargets) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	for _, file := range v.Files {
		for _, check := range file.Checks {
			if !check.Passed {
				return false
			}
		}
	}
	return true
}

func PackagePostReleaseDownloadEvidence(opts PostReleaseDownloadPackageOptions) (PostReleaseDownloadAcceptancePackage, error) {
	if strings.TrimSpace(opts.PlanPath) == "" {
		return PostReleaseDownloadAcceptancePackage{}, fmt.Errorf("post-release install plan is required")
	}
	if strings.TrimSpace(opts.PlanVerificationPath) == "" {
		return PostReleaseDownloadAcceptancePackage{}, fmt.Errorf("post-release install plan verification is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return PostReleaseDownloadAcceptancePackage{}, fmt.Errorf("output directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return PostReleaseDownloadAcceptancePackage{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return PostReleaseDownloadAcceptancePackage{}, err
	}
	plan, planContent, err := readPostReleaseInstallPlan(opts.PlanPath)
	if err != nil {
		return PostReleaseDownloadAcceptancePackage{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	pkg := PostReleaseDownloadAcceptancePackage{
		SchemaVersion:    PostReleaseDownloadPackageSchemaVersion,
		GeneratedAt:      now.UTC(),
		OutDir:           outDir,
		Plan:             opts.PlanPath,
		PlanVerification: opts.PlanVerificationPath,
		Repo:             plan.Repo,
		Tag:              plan.Tag,
		PlatformTargets:  postReleasePlanTargets(plan),
		SkillkitIncluded: postReleasePlanHasSkillkit(plan),
		RequiredEvidence: []string{
			"rdev.post-release-install-plan.v1 generated from the reviewed GitHub Release dry-run plan",
			"rdev.post-release-install-verification.v1 with ok=true",
			"one transcript per planned platform verification script that ran against published release downloads",
			"one rdev release verify-candidate output per planned platform with ok=true",
			"one rdev-verify --bundle output per planned platform with ok=true",
			"Skillkit download/verify transcript and rdev skillkit verify output with ok=true when Skillkit is included",
			"checksums.txt covering every archived evidence file",
		},
	}
	add := func(name string, passed bool, detail string) {
		pkg.Checks = append(pkg.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	redactor := shelladapter.NewArtifactRedactor()
	files := []AcceptancePackageFile{}
	if entry, err := writePackageContent(outDir, "post-release/post-release-install-plan.json", "post-release-install-plan", planContent, opts.PlanPath); err != nil {
		add("post_release_plan_copied", false, err.Error())
	} else {
		files = append(files, entry)
		add("post_release_plan_copied", true, entry.Path)
	}
	files = append(files, copyOptionalEvidence(outDir, "post-release/post-release-install-verification.json", "post-release-install-verification", opts.PlanVerificationPath, redactor, add)...)
	platformFiles := copyPostReleasePlatformEvidence(outDir, opts.EvidenceDir, pkg.PlatformTargets, redactor, add)
	files = append(files, platformFiles...)
	if pkg.SkillkitIncluded {
		files = append(files, copyPostReleaseSkillkitEvidence(outDir, opts.SkillkitEvidenceDir, redactor, add)...)
	}
	files = append(files, copyNotesEvidence(outDir, opts.NotesPath, redactor, add)...)

	add("post_release_plan_schema", plan.SchemaVersion == "rdev.post-release-install-plan.v1", plan.SchemaVersion)
	add("post_release_plan_repo_present", strings.TrimSpace(plan.Repo) != "", plan.Repo)
	add("post_release_plan_tag_present", strings.TrimSpace(plan.Tag) != "", plan.Tag)
	add("post_release_plan_platforms_present", len(pkg.PlatformTargets) > 0, fmt.Sprintf("%d", len(pkg.PlatformTargets)))
	add("post_release_plan_verification_ok", postReleaseEvidenceOK(outDir, "post-release/post-release-install-verification.json"), opts.PlanVerificationPath)
	for _, target := range pkg.PlatformTargets {
		slug := postReleaseTargetSlug(target)
		add("platform_"+slug+"_transcript_present", packageKindPresent(files, postReleasePlatformKind(target, "transcript")), target)
		add("platform_"+slug+"_candidate_verify_present", packageKindPresent(files, postReleasePlatformKind(target, "candidate-verify")), target)
		add("platform_"+slug+"_candidate_verify_ok", postReleaseEvidenceOK(outDir, postReleasePlatformEvidencePath(target, "candidate-verify.json")), target)
		add("platform_"+slug+"_bundle_verify_present", packageKindPresent(files, postReleasePlatformKind(target, "bundle-verify")), target)
		add("platform_"+slug+"_bundle_verify_ok", postReleaseEvidenceOK(outDir, postReleasePlatformEvidencePath(target, "bundle-verify.json")), target)
	}
	if pkg.SkillkitIncluded {
		add("skillkit_transcript_present", packageKindPresent(files, "skillkit-transcript"), "")
		add("skillkit_verify_present", packageKindPresent(files, "skillkit-verify"), "")
		add("skillkit_verify_ok", postReleaseEvidenceOK(outDir, "skillkit/skillkit-verify.json"), "")
	}
	add("post_release_files_have_no_private_surface", postReleaseFilesHaveNoPrivateSurface(outDir, files), "")

	if redactor.Redacted() {
		pkg.RedactionRuleCounts = redactor.Counts()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	checksums, checksumEntry, err := writePackageChecksums(outDir, files)
	if err != nil {
		return PostReleaseDownloadAcceptancePackage{}, err
	}
	files = append(files, checksumEntry)
	pkg.Files = files
	add("checksums_written", len(checksums) > 0, "checksums.txt")
	add("package_files_written", len(pkg.Files) >= 4+2*len(pkg.PlatformTargets), fmt.Sprintf("%d", len(pkg.Files)))
	if !pkg.OK() {
		pkg.RecommendedActions = []string{
			"Run the generated post-release verification scripts after the GitHub Release assets exist.",
			"Archive platform transcripts plus rdev release verify-candidate and rdev-verify --bundle outputs for every planned platform.",
			"Do not claim public release download acceptance until this post-release download evidence package verifies.",
		}
	}
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return PostReleaseDownloadAcceptancePackage{}, err
	}
	content = append(content, '\n')
	if _, err := writePackageContent(outDir, "package.json", "package-manifest", content, ""); err != nil {
		return PostReleaseDownloadAcceptancePackage{}, err
	}
	return pkg, nil
}

func VerifyPostReleaseDownloadAcceptancePackage(packagePath string) (PostReleaseDownloadAcceptanceVerification, error) {
	manifestPath, dir, err := resolveAcceptancePackageManifest(packagePath)
	if err != nil {
		return PostReleaseDownloadAcceptanceVerification{}, err
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return PostReleaseDownloadAcceptanceVerification{}, err
	}
	var pkg PostReleaseDownloadAcceptancePackage
	if err := json.Unmarshal(content, &pkg); err != nil {
		return PostReleaseDownloadAcceptanceVerification{}, err
	}
	verification := PostReleaseDownloadAcceptanceVerification{
		SchemaVersion:    PostReleaseDownloadVerificationSchemaVersion,
		PackagePath:      manifestPath,
		PackageSchema:    pkg.SchemaVersion,
		Repo:             pkg.Repo,
		Tag:              pkg.Tag,
		PlatformTargets:  append([]string(nil), pkg.PlatformTargets...),
		SkillkitIncluded: pkg.SkillkitIncluded,
		GeneratedAt:      time.Now().UTC(),
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	add("package_schema", pkg.SchemaVersion == PostReleaseDownloadPackageSchemaVersion, pkg.SchemaVersion)
	add("package_checks_passed", allChecksPassed(pkg.Checks), failedCheckNames(pkg.Checks))
	add("repo_present", strings.TrimSpace(pkg.Repo) != "", pkg.Repo)
	add("tag_present", strings.TrimSpace(pkg.Tag) != "", pkg.Tag)
	add("platform_targets_present", len(pkg.PlatformTargets) > 0, fmt.Sprintf("%d", len(pkg.PlatformTargets)))
	add("required_evidence_declared", len(pkg.RequiredEvidence) >= 6, fmt.Sprintf("%d", len(pkg.RequiredEvidence)))
	verification.Files = verifyAcceptancePackageFiles(dir, pkg.Files)
	add("checksums_file_present", packagePathExists(pkg.Files, "checksums.txt"), "")
	add("post_release_plan_present", packageKindPresent(pkg.Files, "post-release-install-plan"), "")
	add("post_release_plan_verification_present", packageKindPresent(pkg.Files, "post-release-install-verification"), "")
	add("post_release_plan_verification_ok", postReleaseEvidenceOK(dir, "post-release/post-release-install-verification.json"), "")
	for _, target := range pkg.PlatformTargets {
		slug := postReleaseTargetSlug(target)
		add("platform_"+slug+"_transcript_present", packageKindPresent(pkg.Files, postReleasePlatformKind(target, "transcript")), target)
		add("platform_"+slug+"_candidate_verify_present", packageKindPresent(pkg.Files, postReleasePlatformKind(target, "candidate-verify")), target)
		add("platform_"+slug+"_candidate_verify_ok", postReleaseEvidenceOK(dir, postReleasePlatformEvidencePath(target, "candidate-verify.json")), target)
		add("platform_"+slug+"_bundle_verify_present", packageKindPresent(pkg.Files, postReleasePlatformKind(target, "bundle-verify")), target)
		add("platform_"+slug+"_bundle_verify_ok", postReleaseEvidenceOK(dir, postReleasePlatformEvidencePath(target, "bundle-verify.json")), target)
	}
	if pkg.SkillkitIncluded {
		add("skillkit_transcript_present", packageKindPresent(pkg.Files, "skillkit-transcript"), "")
		add("skillkit_verify_present", packageKindPresent(pkg.Files, "skillkit-verify"), "")
		add("skillkit_verify_ok", postReleaseEvidenceOK(dir, "skillkit/skillkit-verify.json"), "")
	}
	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Regenerate the post-release download evidence package from complete real download transcripts.",
			"Confirm every planned platform has transcript, verify-candidate ok=true, and bundle verify ok=true evidence.",
			"Do not attach this package to release evidence until verification passes.",
		}
	}
	return verification, nil
}

func readPostReleaseInstallPlan(path string) (postReleaseInstallPlan, []byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return postReleaseInstallPlan{}, nil, err
	}
	var plan postReleaseInstallPlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return postReleaseInstallPlan{}, nil, err
	}
	return plan, content, nil
}

func postReleasePlanTargets(plan postReleaseInstallPlan) []string {
	var targets []string
	for _, platform := range plan.Platforms {
		if target := strings.TrimSpace(platform.Target); target != "" {
			targets = append(targets, target)
		}
	}
	sort.Strings(targets)
	return targets
}

func postReleasePlanHasSkillkit(plan postReleaseInstallPlan) bool {
	return len(plan.Skillkit) > 0
}

func copyPostReleasePlatformEvidence(root, sourceDir string, targets []string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) []AcceptancePackageFile {
	var files []AcceptancePackageFile
	if strings.TrimSpace(sourceDir) == "" {
		add("platform_evidence_dir_copied", false, "missing")
		return files
	}
	copied := 0
	for _, target := range targets {
		slug := postReleaseTargetSlug(target)
		for _, item := range []struct {
			source string
			dest   string
			kind   string
		}{
			{source: slug + "-transcript.txt", dest: postReleasePlatformEvidencePath(target, "transcript.txt"), kind: postReleasePlatformKind(target, "transcript")},
			{source: slug + "-candidate-verify.json", dest: postReleasePlatformEvidencePath(target, "candidate-verify.json"), kind: postReleasePlatformKind(target, "candidate-verify")},
			{source: slug + "-bundle-verify.json", dest: postReleasePlatformEvidencePath(target, "bundle-verify.json"), kind: postReleasePlatformKind(target, "bundle-verify")},
		} {
			entry, err := copyPackageFile(root, item.dest, item.kind, filepath.Join(sourceDir, item.source), redactor)
			if err != nil {
				add(item.kind+"_copied", false, err.Error())
				continue
			}
			files = append(files, entry)
			add(item.kind+"_copied", true, entry.Path)
			copied++
		}
	}
	add("platform_evidence_dir_copied", copied >= len(targets)*3, fmt.Sprintf("%d", copied))
	return files
}

func copyPostReleaseSkillkitEvidence(root, sourceDir string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) []AcceptancePackageFile {
	var files []AcceptancePackageFile
	if strings.TrimSpace(sourceDir) == "" {
		add("skillkit_evidence_dir_copied", false, "missing")
		return files
	}
	for _, item := range []struct {
		source string
		dest   string
		kind   string
	}{
		{source: "skillkit-transcript.txt", dest: "skillkit/skillkit-transcript.txt", kind: "skillkit-transcript"},
		{source: "skillkit-verify.json", dest: "skillkit/skillkit-verify.json", kind: "skillkit-verify"},
	} {
		entry, err := copyPackageFile(root, item.dest, item.kind, filepath.Join(sourceDir, item.source), redactor)
		if err != nil {
			add(item.kind+"_copied", false, err.Error())
			continue
		}
		files = append(files, entry)
		add(item.kind+"_copied", true, entry.Path)
	}
	add("skillkit_evidence_dir_copied", len(files) == 2, fmt.Sprintf("%d", len(files)))
	return files
}

func postReleaseEvidenceOK(root, path string) bool {
	return releaseVerificationOK(root, path)
}

func postReleaseTargetSlug(target string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", "_", "-")
	slug := strings.ToLower(replacer.Replace(strings.TrimSpace(target)))
	var builder strings.Builder
	lastDash := false
	for _, r := range slug {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-'
		if !allowed {
			r = '-'
		}
		if r == '-' {
			if lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		builder.WriteRune(r)
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "target"
	}
	return result
}

func postReleasePlatformKind(target, suffix string) string {
	return "platform-" + postReleaseTargetSlug(target) + "-" + suffix
}

func postReleasePlatformEvidencePath(target, name string) string {
	return filepath.ToSlash(filepath.Join("platforms", postReleaseTargetSlug(target), name))
}

func postReleaseFilesHaveNoPrivateSurface(root string, files []AcceptancePackageFile) bool {
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil || !relayAcceptanceNoPrivateSurface(content) {
			return false
		}
	}
	return true
}
