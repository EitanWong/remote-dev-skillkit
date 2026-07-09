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

const ManagedMacServicePackageSchemaVersion = "rdev.acceptance-package.managed-mac-service.v1"

type ManagedMacServicePackageOptions struct {
	PlanPath                string
	OutDir                  string
	ReviewTranscriptPath    string
	StartTranscriptPath     string
	InspectTranscriptPath   string
	LogsPath                string
	ReleaseGatePath         string
	AuditPath               string
	ReconnectPath           string
	ManagedReportPath       string
	StopTranscriptPath      string
	UninstallTranscriptPath string
	NotesPath               string
	Now                     time.Time
}

type ManagedMacServicePackage struct {
	SchemaVersion       string                            `json:"schema_version"`
	GeneratedAt         time.Time                         `json:"generated_at"`
	OutDir              string                            `json:"out_dir"`
	PlanPath            string                            `json:"plan_path"`
	PlanSchema          string                            `json:"plan_schema"`
	PlanVerification    ManagedMacServicePlanVerification `json:"plan_verification"`
	ManagedVerification ManagedMacVerification            `json:"managed_verification"`
	Checks              []Check                           `json:"checks"`
	Files               []AcceptancePackageFile           `json:"files"`
	RedactionRuleCounts map[string]int                    `json:"redaction_rule_counts,omitempty"`
	RequiredEvidence    []string                          `json:"required_evidence"`
	RecommendedActions  []string                          `json:"recommended_actions,omitempty"`
}

func (p ManagedMacServicePackage) OK() bool {
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

func PackageManagedMacServiceEvidence(opts ManagedMacServicePackageOptions) (ManagedMacServicePackage, error) {
	if strings.TrimSpace(opts.PlanPath) == "" {
		return ManagedMacServicePackage{}, fmt.Errorf("plan path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return ManagedMacServicePackage{}, fmt.Errorf("output directory is required")
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ManagedMacServicePackage{}, err
	}
	if err := prepareAcceptanceOut(outDir); err != nil {
		return ManagedMacServicePackage{}, err
	}
	planPath, err := filepath.Abs(opts.PlanPath)
	if err != nil {
		return ManagedMacServicePackage{}, err
	}
	plan, err := readManagedMacServicePlan(planPath)
	if err != nil {
		return ManagedMacServicePackage{}, err
	}
	planVerification, err := VerifyManagedMacServicePlan(planPath)
	if err != nil {
		return ManagedMacServicePackage{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	pkg := ManagedMacServicePackage{
		SchemaVersion:    ManagedMacServicePackageSchemaVersion,
		GeneratedAt:      now.UTC(),
		OutDir:           outDir,
		PlanPath:         planPath,
		PlanSchema:       plan.SchemaVersion,
		PlanVerification: planVerification,
		RequiredEvidence: plan.RequiredEvidence,
	}
	add := func(name string, passed bool, detail string) {
		pkg.Checks = append(pkg.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	redactor := shelladapter.NewArtifactRedactor()
	add("plan_verification_ok", planVerification.OK(), failedCheckNames(planVerification.Checks))

	var files []AcceptancePackageFile
	if entry, err := copyPackageFile(outDir, "plan/service-plan.json", "plan", planPath, nil); err != nil {
		return ManagedMacServicePackage{}, err
	} else {
		files = append(files, entry)
	}
	plistPath := resolvePlanPath(filepath.Dir(planPath), plan.PlistPath, plan.LaunchAgent.Label+".plist")
	if entry, err := copyPackageFile(outDir, "plan/"+plan.LaunchAgent.Label+".plist", "launch-agent-plist", plistPath, nil); err != nil {
		add("plist_copied", false, err.Error())
	} else {
		files = append(files, entry)
		add("plist_copied", true, entry.Path)
	}
	if content, err := json.MarshalIndent(planVerification, "", "  "); err != nil {
		return ManagedMacServicePackage{}, err
	} else if entry, err := writePackageContent(outDir, "plan/plan-verification.json", "plan-verification", append(content, '\n'), ""); err != nil {
		return ManagedMacServicePackage{}, err
	} else {
		files = append(files, entry)
	}

	files = append(files, copyOptionalEvidence(outDir, "evidence/review-transcript.txt", "review-transcript", opts.ReviewTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/start-transcript.txt", "start-transcript", opts.StartTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/inspect-transcript.txt", "inspect-transcript", opts.InspectTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/logs.txt", "logs", opts.LogsPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/release-gate.json", "release-gate", opts.ReleaseGatePath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/audit.txt", "audit", opts.AuditPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/reconnect.txt", "reconnect", opts.ReconnectPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/stop-transcript.txt", "stop-transcript", opts.StopTranscriptPath, redactor, add)...)
	files = append(files, copyOptionalEvidence(outDir, "evidence/uninstall-transcript.txt", "uninstall-transcript", opts.UninstallTranscriptPath, redactor, add)...)
	files = append(files, copyNotesEvidence(outDir, opts.NotesPath, redactor, add)...)

	reportPath := strings.TrimSpace(opts.ManagedReportPath)
	if reportPath == "" {
		add("managed_report_copied", false, "missing")
	} else {
		managedVerification, verifyErr := VerifyManagedMacReport(reportPath)
		if verifyErr != nil {
			add("managed_report_verified", false, verifyErr.Error())
		} else {
			pkg.ManagedVerification = managedVerification
			add("managed_report_verified", managedVerification.OK(), failedCheckNames(managedVerification.Checks))
			if entry, err := copyPackageFile(outDir, "evidence/managed-mac/report.json", "managed-mac-report", reportPath, redactor); err != nil {
				add("managed_report_copied", false, err.Error())
			} else {
				files = append(files, entry)
				add("managed_report_copied", true, entry.Path)
			}
			if content, err := json.MarshalIndent(managedVerification, "", "  "); err != nil {
				return ManagedMacServicePackage{}, err
			} else if entry, err := writePackageContent(outDir, "evidence/managed-mac/verification.json", "managed-mac-verification", append(content, '\n'), ""); err != nil {
				return ManagedMacServicePackage{}, err
			} else {
				files = append(files, entry)
			}
			files = append(files, copyManagedMacEvidenceTrees(outDir, reportPath, redactor, add)...)
		}
	}

	add("review_transcript_present", fileEntryKindPresent(files, "review-transcript"), opts.ReviewTranscriptPath)
	add("start_transcript_present", fileEntryKindPresent(files, "start-transcript"), opts.StartTranscriptPath)
	add("inspect_transcript_present", fileEntryKindPresent(files, "inspect-transcript"), opts.InspectTranscriptPath)
	add("logs_present", fileEntryKindPresent(files, "logs"), opts.LogsPath)
	add("release_gate_present", fileEntryKindPresent(files, "release-gate"), opts.ReleaseGatePath)
	add("release_gate_ok", releaseVerificationOK(outDir, "evidence/release-gate.json"), opts.ReleaseGatePath)
	add("audit_present", fileEntryKindPresent(files, "audit"), opts.AuditPath)
	add("reconnect_evidence_present", fileEntryKindPresent(files, "reconnect"), opts.ReconnectPath)
	add("stop_transcript_present", fileEntryKindPresent(files, "stop-transcript"), opts.StopTranscriptPath)
	add("uninstall_transcript_present", fileEntryKindPresent(files, "uninstall-transcript"), opts.UninstallTranscriptPath)
	add("managed_evidence_manifest_present", packagePathExists(files, "evidence/managed-mac/evidence/manifest.json"), opts.ManagedReportPath)
	add("side_effect_probe_evidence_manifest_present", packagePathExists(files, "evidence/managed-mac/side-effect-probe-evidence/manifest.json"), opts.ManagedReportPath)
	add("side_effect_probe_denial_evidence_present", packageTreeContains(outDir, "evidence/managed-mac/side-effect-probe-evidence", "rdev.host-denial.v1"), opts.ManagedReportPath)

	if redactor.Redacted() {
		pkg.RedactionRuleCounts = redactor.Counts()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	checksums, checksumEntry, err := writePackageChecksums(outDir, files)
	if err != nil {
		return ManagedMacServicePackage{}, err
	}
	files = append(files, checksumEntry)
	pkg.Files = files
	add("checksums_written", len(checksums) > 0, "checksums.txt")
	add("package_files_written", len(pkg.Files) >= 16, fmt.Sprintf("%d", len(pkg.Files)))
	if !pkg.OK() {
		pkg.RecommendedActions = []string{
			"Collect missing managed Mac LaunchAgent service evidence from the real service-backed run.",
			"Re-run package-managed-mac-service after redacting transcripts, logs, release-gate output, audit, and managed Mac evidence bundles.",
			"Do not publish this managed Mac service acceptance package as release evidence until every check passes.",
		}
	}
	content, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return ManagedMacServicePackage{}, err
	}
	content = append(content, '\n')
	if _, err := writePackageContent(outDir, "package.json", "package-manifest", content, ""); err != nil {
		return ManagedMacServicePackage{}, err
	}
	return pkg, nil
}

func copyManagedMacEvidenceTrees(root, reportPath string, redactor *shelladapter.ArtifactRedactor, add func(string, bool, string)) []AcceptancePackageFile {
	baseDir := filepath.Dir(reportPath)
	report, reportErr := readManagedMacReport(reportPath)
	if reportErr != nil {
		add("managed_report_read_for_evidence", false, reportErr.Error())
		return nil
	}
	add("managed_report_read_for_evidence", true, report.SchemaVersion)
	evidenceDir := resolveReportPath(baseDir, report.EvidenceDir, "evidence")
	if strings.TrimSpace(evidenceDir) == "" || !pathExists(evidenceDir) {
		evidenceDir = filepath.Join(baseDir, "evidence")
	}
	probeDir := resolveReportPath(baseDir, report.ProbeEvidenceDir, "side-effect-probe-evidence")
	if strings.TrimSpace(probeDir) == "" || !pathExists(probeDir) {
		probeDir = filepath.Join(baseDir, "side-effect-probe-evidence")
	}
	var files []AcceptancePackageFile
	evidenceFiles, evidenceErr := copyEvidenceTree(root, "evidence/managed-mac/evidence", "managed-mac-evidence", evidenceDir, redactor)
	if evidenceErr != nil {
		add("managed_evidence_dir_copied", false, evidenceErr.Error())
	} else {
		files = append(files, evidenceFiles...)
		add("managed_evidence_dir_copied", len(evidenceFiles) > 0, fmt.Sprintf("%d", len(evidenceFiles)))
	}
	probeFiles, probeErr := copyEvidenceTree(root, "evidence/managed-mac/side-effect-probe-evidence", "managed-mac-side-effect-probe-evidence", probeDir, redactor)
	if probeErr != nil {
		add("managed_side_effect_probe_evidence_dir_copied", false, probeErr.Error())
	} else {
		files = append(files, probeFiles...)
		add("managed_side_effect_probe_evidence_dir_copied", len(probeFiles) > 0, fmt.Sprintf("%d", len(probeFiles)))
	}
	return files
}

func readManagedMacReport(path string) (ManagedMacReport, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ManagedMacReport{}, err
	}
	var report ManagedMacReport
	if err := json.Unmarshal(content, &report); err != nil {
		return ManagedMacReport{}, err
	}
	return report, nil
}
