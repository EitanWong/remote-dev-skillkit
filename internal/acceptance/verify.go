package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/codexadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/evidence"
	"github.com/EitanWong/remote-dev-skillkit/internal/hostrunner"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

const ManagedMacVerificationSchemaVersion = "rdev.acceptance-verification.managed-mac.v1"

type ManagedMacVerification struct {
	SchemaVersion      string                      `json:"schema_version"`
	ReportPath         string                      `json:"report_path"`
	ReportSchema       string                      `json:"report_schema"`
	GeneratedAt        time.Time                   `json:"generated_at"`
	Evidence           evidence.VerificationReport `json:"evidence"`
	ApprovalEvidence   evidence.VerificationReport `json:"approval_evidence"`
	Checks             []Check                     `json:"checks"`
	RecommendedActions []string                    `json:"recommended_actions,omitempty"`
}

func (v ManagedMacVerification) OK() bool {
	if len(v.Checks) == 0 || !v.Evidence.OK() || !v.ApprovalEvidence.OK() {
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
	approvalEvidenceDir := resolveReportPath(baseDir, report.ApprovalEvidenceDir, "approval-evidence")

	evidenceReport, evidenceErr := evidence.VerifyDirectory(evidenceDir)
	approvalEvidenceReport, approvalEvidenceErr := evidence.VerifyDirectory(approvalEvidenceDir)
	verification := ManagedMacVerification{
		SchemaVersion:    ManagedMacVerificationSchemaVersion,
		ReportPath:       abs,
		ReportSchema:     report.SchemaVersion,
		GeneratedAt:      time.Now().UTC(),
		Evidence:         evidenceReport,
		ApprovalEvidence: approvalEvidenceReport,
	}
	add := func(name string, passed bool, detail string) {
		verification.Checks = append(verification.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	add("report_schema", report.SchemaVersion == ManagedMacReportSchemaVersion, report.SchemaVersion)
	add("report_checks_passed", allChecksPassed(report.Checks), failedCheckNames(report.Checks))
	add("mode_managed", report.Mode == string(model.HostModeManaged), report.Mode)
	add("host_active", report.Host.Status == model.HostStatusActive, string(report.Host.Status))
	add("coding_job_succeeded", report.CodingJob.Status == model.JobStatusSucceeded, string(report.CodingJob.Status))
	add("approval_job_failed_before_side_effect", report.ApprovalJob.Status == model.JobStatusFailed, string(report.ApprovalJob.Status))
	add("codex_result_artifact", strings.Contains(joinedArtifacts(report.CodingArtifacts), codexadapter.ResultSchemaVersion), "")
	add("diff_evidence_present", strings.Contains(joinedArtifacts(report.CodingArtifacts), "git_diff") && strings.Contains(joinedArtifacts(report.CodingArtifacts), "git_diff_stat"), "")
	add("verification_evidence_present", strings.Contains(joinedArtifacts(report.CodingArtifacts), "verification_results"), "")
	add("test_report_present_when_fixture", !report.FixtureRepo || strings.Contains(joinedArtifacts(report.CodingArtifacts), codexadapter.TestReportSchemaVersion), "")
	add("approval_required_probe", strings.Contains(joinedArtifacts(report.ApprovalArtifacts), hostrunner.ApprovalRequiredSchemaVersion) && strings.Contains(joinedArtifacts(report.ApprovalArtifacts), "git.push"), "")
	add("evidence_verifier_ok", evidenceErr == nil && evidenceReport.OK(), errorDetail(evidenceErr, evidenceReport))
	add("approval_evidence_verifier_ok", approvalEvidenceErr == nil && approvalEvidenceReport.OK(), errorDetail(approvalEvidenceErr, approvalEvidenceReport))
	add("embedded_evidence_manifest_matches", evidenceErr == nil && manifestsEquivalent(report.EvidenceManifest, evidenceReport.Manifest), "")
	add("embedded_approval_manifest_matches", approvalEvidenceErr == nil && manifestsEquivalent(report.ApprovalManifest, approvalEvidenceReport.Manifest), "")
	add("evidence_job_matches", evidenceErr == nil && evidenceReport.Manifest.JobID == report.CodingJob.ID, evidenceReport.Manifest.JobID)
	add("approval_evidence_job_matches", approvalEvidenceErr == nil && approvalEvidenceReport.Manifest.JobID == report.ApprovalJob.ID, approvalEvidenceReport.Manifest.JobID)
	add("workspace_lock_released", workspaceLockReleased(report), report.Worktree.WorktreePath)

	if !verification.OK() {
		verification.RecommendedActions = []string{
			"Re-run the acceptance command in a fresh output directory.",
			"Inspect evidence/manifest.json, approval-evidence/manifest.json, checksums.txt, and audit-chain.json for the first failed check.",
			"Do not publish this acceptance transcript as release evidence until verification passes.",
		}
	}
	return verification, nil
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

func errorDetail(err error, report evidence.VerificationReport) string {
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

func manifestsEquivalent(left, right evidence.Manifest) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.JobID == right.JobID &&
		left.JobStatus == right.JobStatus &&
		left.EnvelopeHash == right.EnvelopeHash &&
		left.AuditEventCount == right.AuditEventCount &&
		left.AuditRootHash == right.AuditRootHash &&
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
