package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestExportDirectoryWritesReviewableBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	job := evidenceTestJob(now)
	artifact := model.Artifact{
		ID:        "art_1",
		JobID:     job.ID,
		Kind:      "text",
		Name:      "shell-result.json",
		Content:   `{"schema_version":"rdev.shell-result.v1","exit_code":0}`,
		CreatedAt: now.Add(2 * time.Second),
	}
	events := []model.AuditEvent{
		{Sequence: 1, Actor: "operator", Action: "ticket.create", TargetID: job.Envelope.TicketID, Message: "created", At: now},
		{Sequence: 2, Actor: "host", Action: "host.register", TargetID: job.HostID, Message: "registered", At: now},
		{Sequence: 3, Actor: "operator", Action: "job.create", TargetID: job.ID, Message: "created", At: now},
		{Sequence: 4, Actor: "host", Action: "job.complete", TargetID: job.ID, Message: "completed", At: now},
		{Sequence: 5, Actor: "operator", Action: "ticket.create", TargetID: "tkt_other", Message: "other", At: now},
	}
	out := filepath.Join(t.TempDir(), "bundle")

	manifest, err := ExportDirectory(out, Input{
		Job:         job,
		Artifacts:   []model.Artifact{artifact},
		AuditEvents: events,
		GeneratedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if manifest.SchemaVersion != BundleSchemaVersion {
		t.Fatalf("unexpected schema %q", manifest.SchemaVersion)
	}
	if manifest.JobID != job.ID {
		t.Fatalf("expected job %q, got %q", job.ID, manifest.JobID)
	}
	if manifest.AuditEventCount != 4 {
		t.Fatalf("expected 4 audit events in slice, got %d", manifest.AuditEventCount)
	}
	for _, path := range []string{
		"manifest.json",
		"job.json",
		"envelope.json",
		"policy-decision.json",
		"artifacts.json",
		"artifacts/art_1-shell-result.json",
		"audit-slice.jsonl",
		"audit-chain.json",
		"checksums.txt",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected bundle file %s: %v", path, err)
		}
	}
	if content := readFile(t, filepath.Join(out, "audit-slice.jsonl")); strings.Contains(content, "tkt_other") {
		t.Fatalf("audit slice included unrelated event: %s", content)
	}
	if content := readFile(t, filepath.Join(out, "checksums.txt")); !strings.Contains(content, "job.json") || strings.Contains(content, "manifest.json") {
		t.Fatalf("unexpected checksums content: %s", content)
	}
	chain := readAuditChain(t, filepath.Join(out, "audit-chain.json"))
	if err := audit.VerifyChain(chain); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyDirectoryAcceptsExportedBundle(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	job := evidenceTestJob(now)
	out := filepath.Join(t.TempDir(), "bundle")
	_, err := ExportDirectory(out, Input{
		Job:         job,
		AuditEvents: []model.AuditEvent{{Sequence: 1, Actor: "host", Action: "job.complete", TargetID: job.ID, Message: "done", At: now}},
		GeneratedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := VerifyDirectory(out)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("expected verification ok: %#v", report.Checks)
	}
	if report.Manifest.JobID != job.ID {
		t.Fatalf("expected job id %q, got %q", job.ID, report.Manifest.JobID)
	}
}

func TestVerifyDirectoryDetectsTamperedArtifact(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	job := evidenceTestJob(now)
	artifact := model.Artifact{
		ID:        "art_1",
		JobID:     job.ID,
		Kind:      "text",
		Name:      "shell-result.json",
		Content:   `{"schema_version":"rdev.shell-result.v1","exit_code":0}`,
		CreatedAt: now,
	}
	out := filepath.Join(t.TempDir(), "bundle")
	if _, err := ExportDirectory(out, Input{Job: job, Artifacts: []model.Artifact{artifact}, GeneratedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "artifacts", "art_1-shell-result.json"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyDirectory(out)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatalf("expected verification failure: %#v", report.Checks)
	}
	if !hasFailedEvidenceCheck(report.Checks, "manifest_files_verified") {
		t.Fatalf("expected file verification failure: %#v", report.Checks)
	}
}

func TestExportDirectoryRejectsNonEmptyDirectory(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ExportDirectory(out, Input{Job: model.Job{ID: "job_1"}})
	if err == nil {
		t.Fatal("expected non-empty output directory to fail")
	}
	if !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func hasFailedEvidenceCheck(checks []VerificationCheck, name string) bool {
	for _, check := range checks {
		if check.Name == name && !check.Passed {
			return true
		}
	}
	return false
}

func evidenceTestJob(now time.Time) model.Job {
	envelope := &model.JobEnvelope{
		SchemaVersion: "rdev.job.v1",
		JobID:         "job_1",
		HostID:        "hst_1",
		TicketID:      "tkt_1",
		OperatorID:    "operator",
		IssuedAt:      now,
		ExpiresAt:     now.Add(time.Hour),
		Nonce:         "nonce",
		Mode:          model.HostModeAttendedTemporary,
		Adapter:       "shell",
		Intent:        "demo",
		Workspace: model.JobWorkspace{
			Root:       "/repo",
			WriteScope: []string{"/repo"},
		},
		Capabilities:      []string{"shell.user"},
		Limits:            model.JobLimits{MaxDurationSeconds: 60, MaxOutputBytes: 1024, Network: "default-deny"},
		ApprovalsRequired: []string{"git.push"},
		Payload:           map[string]any{"argv": []string{"go", "test", "./..."}},
		SigningAlg:        "ed25519",
		SigningKeyID:      "gateway-dev",
		Signature:         "signature",
	}
	return model.Job{
		ID:        "job_1",
		HostID:    "hst_1",
		Adapter:   "shell",
		Intent:    "demo",
		Policy:    map[string]any{"workspace_root": "/repo"},
		Envelope:  envelope,
		Status:    model.JobStatusSucceeded,
		CreatedAt: now,
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func readAuditChain(t *testing.T, path string) audit.Chain {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var chain audit.Chain
	if err := json.Unmarshal(content, &chain); err != nil {
		t.Fatal(err)
	}
	return chain
}
