package acceptance

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/codexadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ManagedMacVerificationSchemaVersion = "rdev.acceptance-verification.managed-mac.v1"

type ManagedMacVerification struct {
	SchemaVersion      string                      `json:"schema_version"`
	ReportPath         string                      `json:"report_path"`
	ReportSchema       string                      `json:"report_schema"`
	GeneratedAt        time.Time                   `json:"generated_at"`
	Evidence           SessionEvidenceVerification `json:"evidence"`
	ProbeEvidence      SessionEvidenceVerification `json:"side_effect_probe_evidence"`
	Checks             []Check                     `json:"checks"`
	RecommendedActions []string                    `json:"recommended_actions,omitempty"`
}

type SessionEvidenceVerification struct {
	SchemaVersion string                  `json:"schema_version"`
	Directory     string                  `json:"directory"`
	Manifest      SessionEvidenceManifest `json:"manifest"`
	Checks        []Check                 `json:"checks"`
}

func (v SessionEvidenceVerification) OK() bool {
	if v.Manifest.SchemaVersion != SessionEvidenceSchemaVersion || len(v.Checks) == 0 {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func (v ManagedMacVerification) OK() bool {
	if len(v.Checks) == 0 || !v.Evidence.OK() || !v.ProbeEvidence.OK() {
		return false
	}
	for _, check := range v.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func VerifyManagedMacReport(reportPath string) (ManagedMacVerification, error) {
	if strings.TrimSpace(reportPath) == "" {
		return ManagedMacVerification{}, fmt.Errorf("report path is required")
	}
	abs, err := filepath.Abs(reportPath)
	if err != nil {
		return ManagedMacVerification{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return ManagedMacVerification{}, err
	}
	var report ManagedMacReport
	if err := json.Unmarshal(content, &report); err != nil {
		return ManagedMacVerification{}, err
	}
	baseDir := filepath.Dir(abs)
	evidenceDir := resolveReportPath(baseDir, report.EvidenceDir, "evidence")
	probeEvidenceDir := resolveReportPath(baseDir, report.ProbeEvidenceDir, "side-effect-probe-evidence")

	evidenceReport, evidenceErr := VerifySessionEvidenceDirectory(evidenceDir)
	probeEvidenceReport, probeEvidenceErr := VerifySessionEvidenceDirectory(probeEvidenceDir)
	verification := ManagedMacVerification{
		SchemaVersion: ManagedMacVerificationSchemaVersion,
		ReportPath:    abs,
		ReportSchema:  report.SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Evidence:      evidenceReport,
		ProbeEvidence: probeEvidenceReport,
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	add("report_schema", report.SchemaVersion == ManagedMacReportSchemaVersion, report.SchemaVersion)
	add("report_checks_passed", allChecksPassed(report.Checks), failedCheckNames(report.Checks))
	add("protocol_session", report.Protocol == controlplane.SessionSchemaVersion && report.SessionID != "", report.Protocol)
	add("target_endpoint_online", report.TargetEndpoint.State == controlplane.EndpointStateOnline, string(report.TargetEndpoint.State))
	add("coding_task_succeeded", report.CodingTask.Status == controlplane.TaskStatusSucceeded, string(report.CodingTask.Status))
	add("side_effect_probe_failed_before_external_consequence", report.SideEffectProbeTask.Status == controlplane.TaskStatusFailed, string(report.SideEffectProbeTask.Status))
	add("codex_result_artifact", strings.Contains(joinedArtifacts(report.CodingArtifacts), codexadapter.ResultSchemaVersion), "")
	add("diff_evidence_present", strings.Contains(joinedArtifacts(report.CodingArtifacts), "git_diff") && strings.Contains(joinedArtifacts(report.CodingArtifacts), "git_diff_stat"), "")
	add("verification_evidence_present", strings.Contains(joinedArtifacts(report.CodingArtifacts), "verification_results"), "")
	add("test_report_present_when_fixture", !report.FixtureRepo || strings.Contains(joinedArtifacts(report.CodingArtifacts), codexadapter.TestReportSchemaVersion), "")
	add("side_effect_probe_denial", strings.Contains(joinedArtifacts(report.ProbeArtifacts), hostrunner.DenialSchemaVersion), "")
	add("session_evidence_verifier_ok", evidenceErr == nil && evidenceReport.OK(), sessionEvidenceErrorDetail(evidenceErr, evidenceReport))
	add("probe_session_evidence_verifier_ok", probeEvidenceErr == nil && probeEvidenceReport.OK(), sessionEvidenceErrorDetail(probeEvidenceErr, probeEvidenceReport))
	add("embedded_evidence_manifest_matches", evidenceErr == nil && sessionManifestsEquivalent(report.EvidenceManifest, evidenceReport.Manifest), "")
	add("embedded_probe_manifest_matches", probeEvidenceErr == nil && sessionManifestsEquivalent(report.ProbeManifest, probeEvidenceReport.Manifest), "")
	add("evidence_task_matches", evidenceErr == nil && evidenceReport.Manifest.TaskID == report.CodingTask.ID, evidenceReport.Manifest.TaskID)
	add("probe_evidence_task_matches", probeEvidenceErr == nil && probeEvidenceReport.Manifest.TaskID == report.SideEffectProbeTask.ID, probeEvidenceReport.Manifest.TaskID)
	add("workspace_lock_released", workspaceLockReleased(report), report.Worktree.WorktreePath)

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Re-run the acceptance command in a fresh output directory.",
			"Inspect evidence/manifest.json, side-effect-probe-evidence/manifest.json, and checksums.txt for the first failed check.",
			"Do not publish this acceptance transcript as release evidence until verification passes.",
		}
	}
	return verification, nil
}

func VerifySessionEvidenceDirectory(dir string) (SessionEvidenceVerification, error) {
	report := SessionEvidenceVerification{
		SchemaVersion: "rdev.session-evidence-verification.v1",
		Directory:     dir,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	content, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		add("manifest_read", false, err.Error())
		return report, err
	}
	add("manifest_read", true, "manifest.json")
	if err := json.Unmarshal(content, &report.Manifest); err != nil {
		add("manifest_json", false, err.Error())
		return report, err
	}
	add("manifest_json", true, report.Manifest.SchemaVersion)
	add("manifest_schema", report.Manifest.SchemaVersion == SessionEvidenceSchemaVersion, report.Manifest.SchemaVersion)
	add("manifest_task", report.Manifest.TaskID != "", report.Manifest.TaskID)
	add("manifest_artifacts", len(report.Manifest.Artifacts) > 0, fmt.Sprintf("%d", len(report.Manifest.Artifacts)))
	for _, file := range report.Manifest.Files {
		path := filepath.Join(dir, file.Path)
		fileContent, err := os.ReadFile(path)
		if err != nil {
			add("file_present:"+file.Path, false, err.Error())
			continue
		}
		add("file_present:"+file.Path, true, file.Path)
		sum := sha256Hex(fileContent)
		add("file_sha256:"+file.Path, sum == file.SHA256, sum)
		add("file_size:"+file.Path, int64(len(fileContent)) == file.SizeBytes, fmt.Sprintf("%d", len(fileContent)))
	}
	return report, nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}

func allChecksPassed(checks []Check) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func failedCheckNames(checks []Check) string {
	var failed []string
	for _, check := range checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}

func sessionEvidenceErrorDetail(err error, report SessionEvidenceVerification) string {
	if err != nil {
		return err.Error()
	}
	var failed []string
	for _, check := range report.Checks {
		if !check.Passed {
			failed = append(failed, check.Name)
		}
	}
	return strings.Join(failed, ",")
}

func sessionManifestsEquivalent(left, right SessionEvidenceManifest) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.SessionID == right.SessionID &&
		left.TaskID == right.TaskID &&
		left.TaskStatus == right.TaskStatus &&
		reflect.DeepEqual(left.Artifacts, right.Artifacts) &&
		reflect.DeepEqual(left.Files, right.Files)
}

func workspaceLockReleased(report ManagedMacReport) bool {
	if strings.TrimSpace(report.WorkspaceLockStore) == "" || strings.TrimSpace(report.Worktree.WorktreePath) == "" {
		return false
	}
	status, err := workspace.NewFileLockStore(report.WorkspaceLockStore).Status(report.Worktree.WorktreePath, time.Now())
	return err == nil && !status.Exists
}

func resolveReportPath(baseDir, path, fallback string) string {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if strings.TrimSpace(path) != "" && !filepath.IsAbs(path) {
		candidate := filepath.Join(baseDir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(baseDir, fallback)
}
