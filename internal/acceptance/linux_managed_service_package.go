package acceptance

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const LinuxManagedServicePackageSchemaVersion = "rdev.acceptance-package.linux-managed-service.v1"

type AcceptancePackageFile = WindowsTemporaryPackageFile

type LinuxManagedServicePackageOptions struct {
	PlanPath                string
	OutDir                  string
	StartTranscriptPath     string
	StatusTranscriptPath    string
	LogsPath                string
	ReleaseGatePath         string
	AuditPath               string
	ReconnectPath           string
	SessionEvidenceDir      string
	StopTranscriptPath      string
	UninstallTranscriptPath string
	NotesPath               string
	Now                     time.Time
}

type LinuxManagedServicePackage struct {
	SchemaVersion       string                              `json:"schema_version"`
	GeneratedAt         time.Time                           `json:"generated_at"`
	OutDir              string                              `json:"out_dir"`
	PlanPath            string                              `json:"plan_path"`
	PlanSchema          string                              `json:"plan_schema"`
	PlanVerification    LinuxManagedServicePlanVerification `json:"plan_verification"`
	Checks              []Check                             `json:"checks"`
	Files               []AcceptancePackageFile             `json:"files"`
	RedactionRuleCounts map[string]int                      `json:"redaction_rule_counts,omitempty"`
	RequiredEvidence    []string                            `json:"required_evidence"`
	RecommendedActions  []string                            `json:"recommended_actions,omitempty"`
}

func (p LinuxManagedServicePackage) OK() bool {
	if len(p.Checks) == 0 {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func PackageLinuxManagedServiceEvidence(opts LinuxManagedServicePackageOptions) (LinuxManagedServicePackage, error) {
	if strings.TrimSpace(opts.PlanPath) == "" {
		return LinuxManagedServicePackage{}, fmt.Errorf("plan path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return LinuxManagedServicePackage{}, fmt.Errorf("output directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return LinuxManagedServicePackage{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return LinuxManagedServicePackage{}, err
	}
	planPath, err := filepath.Abs(opts.PlanPath)
	if err != nil {
		return LinuxManagedServicePackage{}, err
	}
	plan, err := readLinuxManagedServicePlan(planPath)
	if err != nil {
		return LinuxManagedServicePackage{}, err
	}
	verification, err := VerifyLinuxManagedServicePlan(planPath)
	if err != nil {
		return LinuxManagedServicePackage{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	pkg := LinuxManagedServicePackage{
		SchemaVersion:    LinuxManagedServicePackageSchemaVersion,
		GeneratedAt:      now.UTC(),
		OutDir:           outDir,
		PlanPath:         planPath,
		PlanSchema:       plan.SchemaVersion,
		PlanVerification: verification,
		RequiredEvidence: plan.RequiredEvidence,
	}
	add := func(name string, passed bool, detail string) {
		pkg.Checks = append(pkg.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	redactor := shelladapter.NewArtifactRedactor()
	add("plan_verification_ok", verification.OK(), failedCheckNames(verification.Checks))

	var files []AcceptancePackageFile
	if entry, err := copyPackageFile(outDir, "plan/linux-managed-service-plan.json", "plan", planPath, nil); err != nil {
		return LinuxManagedServicePackage{}, err
	} else {
		files = append(files, entry)
	}
	unitPath := resolvePlanPath(filepath.Dir(planPath), plan.UnitPath, plan.Unit.UnitName)
	if entry, err := copyPackageFile(outDir, "plan/"+plan.Unit.UnitName, "systemd-unit", unitPath, nil); err != nil {
		add("unit_copied", false, err.Error())
	} else {
		files = append(files, entry)
		add("unit_copied", true, entry.Path)
	}
	if content, err := json.MarshalIndent(verification, "", "  "); err != nil {
		return LinuxManagedServicePackage{}, err
	} else if entry, err := writePackageContent(outDir, "plan/plan-verification.json", "plan-verification", append(content, '\n'), ""); err != nil {
		return LinuxManagedServicePackage{}, err
	} else {
		files = append(files, entry)
	}

	files = append(files, copyOptionalEvidence(outDir, "evidence/start-transcript.txt", "start-transcript", opts.StartTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/status-transcript.txt", "status-transcript", opts.StatusTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/logs.txt", "logs", opts.LogsPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/release-gate.json", "release-gate", opts.ReleaseGatePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/audit.txt", "audit", opts.AuditPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/reconnect.txt", "reconnect", opts.ReconnectPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/stop-transcript.txt", "stop-transcript", opts.StopTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/uninstall-transcript.txt", "uninstall-transcript", opts.UninstallTranscriptPath, redactor, add)...)
	files = append(files, copyNotesEvidence(outDir, opts.NotesPath, redactor, add)...)

	sessionFiles, sessionErr := copyEvidenceTree(outDir, "evidence/session-evidence", "session-evidence", opts.SessionEvidenceDir, redactor)
	if sessionErr != nil {
		add("session_evidence_dir_copied", false, sessionErr.Error())
	} else {
		files = append(files, sessionFiles...)
		add("session_evidence_dir_copied", len(sessionFiles) > 0, fmt.Sprintf("%d", len(sessionFiles)))
	}

	add("start_transcript_present", fileEntryKindPresent(files, "start-transcript"), opts.StartTranscriptPath)
	add("status_transcript_present", fileEntryKindPresent(files, "status-transcript"), opts.StatusTranscriptPath)
	add("logs_present", fileEntryKindPresent(files, "logs"), opts.LogsPath)
	add("release_gate_present", fileEntryKindPresent(files, "release-gate"), opts.ReleaseGatePath)
	add("release_gate_ok", releaseVerificationOK(outDir, "evidence/release-gate.json"), opts.ReleaseGatePath)
	add("audit_present", fileEntryKindPresent(files, "audit"), opts.AuditPath)
	add("reconnect_evidence_present", fileEntryKindPresent(files, "reconnect"), opts.ReconnectPath)
	add("stop_transcript_present", fileEntryKindPresent(files, "stop-transcript"), opts.StopTranscriptPath)
	add("uninstall_transcript_present", fileEntryKindPresent(files, "uninstall-transcript"), opts.UninstallTranscriptPath)
	add("session_evidence_present", fileEntryKindPresent(files, "session-evidence"), opts.SessionEvidenceDir)
	add("session_evidence_manifest_present", packagePathExists(files, "evidence/session-evidence/manifest.json"), opts.SessionEvidenceDir)
	add("host_denial_probe_evidence_present", packageTreeContains(outDir, "evidence/session-evidence", "host-denial"), opts.SessionEvidenceDir)

	if redactor.Redacted() {
		pkg.RedactionRuleCounts = redactor.Counts()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	checksums, checksumEntry, err := writePackageChecksums(outDir, files)
	if err != nil {
		return LinuxManagedServicePackage{}, err
	}
	files = append(files, checksumEntry)
	pkg.Files = files
	add("checksums_written", len(checksums) > 0, "checksums.txt")
	add("package_files_written", len(pkg.Files) >= 12, fmt.Sprintf("%d", len(pkg.Files)))
	if !pkg.OK() {
		pkg.RecommendedActions = []string{
			"Collect missing Linux managed-service evidence from the real host run.",
			"Re-run package-linux-managed-service after redacting transcripts, logs, release-gate output, audit, and session evidence.",
			"Do not publish this Linux managed-service acceptance package as release evidence until every check passes.",
		}
	}
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return LinuxManagedServicePackage{}, err
	}
	content = append(content, '\n')
	if _, err := writePackageContent(outDir, "package.json", "package-manifest", content, ""); err != nil {
		return LinuxManagedServicePackage{}, err
	}
	return pkg, nil
}

func readLinuxManagedServicePlan(path string) (LinuxManagedServicePlan, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return LinuxManagedServicePlan{}, err
	}
	var plan LinuxManagedServicePlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return LinuxManagedServicePlan{}, err
	}
	return plan, nil
}

func copyEvidenceTree(root, bundleDir, kind, sourceDir string, redactor *shelladapter.ArtifactRedactor) ([]AcceptancePackageFile, error) {
	if strings.TrimSpace(sourceDir) == "" {
		return nil, fmt.Errorf("missing")
	}
	var files []AcceptancePackageFile
	if err := filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink evidence file %s", path)
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		bundlePath := sanitizedEvidenceTreePath(bundleDir, rel)
		if bundlePath == "" {
			return nil
		}
		copied, err := copyPackageFile(root, bundlePath, kind, path, redactor)
		if err != nil {
			return err
		}
		files = append(files, copied)
		return nil
	}); err != nil {
		return files, err
	}
	return files, nil
}

func sanitizedEvidenceTreePath(bundleDir, rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cleanParts := make([]string, 0, len(parts))
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return ""
		}
		if index == len(parts)-1 {
			name := sanitizeEvidenceName(part)
			if name == "" {
				return ""
			}
			cleanParts = append(cleanParts, name)
			continue
		}
		name := sanitizeEvidenceStem(part)
		if name == "" {
			return ""
		}
		cleanParts = append(cleanParts, name)
	}
	return filepath.ToSlash(filepath.Join(bundleDir, filepath.Join(cleanParts...)))
}

func packagePathExists(files []AcceptancePackageFile, path string) bool {
	for _, file := range files {
		if file.Path == path && file.SizeBytes > 0 {
			return true
		}
	}
	return false
}

func packageTreeContains(root, bundleDir, needle string) bool {
	dir := filepath.Join(root, filepath.FromSlash(bundleDir))
	needle = strings.ToLower(needle)
	found := false
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || found {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(strings.ToLower(string(content)), needle) {
			found = true
		}
		return nil
	})
	return found
}
