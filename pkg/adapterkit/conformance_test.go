package adapterkit

import (
	"strings"
	"testing"
)

func TestVerifyResultArtifactJSONAcceptsTopLevelCommandArtifact(t *testing.T) {
	report := VerifyResultArtifactJSON([]byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "argv": ["go", "env", "GOOS"],
  "workspace_root": "/tmp/repo",
  "exit_code": 0,
  "timed_out": false,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), ResultArtifactContract{
		Adapter:                 "shell",
		SchemaVersion:           "rdev.shell-result.v1",
		RequiredStringFields:    []string{"workspace_root"},
		RequireTiming:           true,
		RequireRedaction:        true,
		RejectUnredactedSecrets: true,
	})
	if !report.OK {
		t.Fatalf("expected conformance success, got %#v", report)
	}
}

func TestVerifyResultArtifactJSONAcceptsNestedCommandArtifact(t *testing.T) {
	report := VerifyResultArtifactJSON([]byte(`{
  "schema_version": "rdev.codex-result.v1",
  "adapter": "codex",
  "workspace_root": "/tmp/repo",
  "prompt": "fix tests",
  "codex_command": {"argv": ["codex"], "dir": "/tmp/repo", "exit_code": 0, "timed_out": false, "canceled": false, "output_truncated": false},
  "git_status": {"argv": ["git", "status"], "dir": "/tmp/repo", "exit_code": 0, "timed_out": false, "canceled": false, "output_truncated": false},
  "git_diff_stat": {"argv": ["git", "diff", "--stat"], "dir": "/tmp/repo", "exit_code": 0, "timed_out": false, "canceled": false, "output_truncated": false},
  "git_diff": {"argv": ["git", "diff", "--"], "dir": "/tmp/repo", "exit_code": 0, "timed_out": false, "canceled": false, "output_truncated": false},
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), ResultArtifactContract{
		Adapter:              "codex",
		SchemaVersion:        "rdev.codex-result.v1",
		CommandFields:        []string{"codex_command", "git_status", "git_diff_stat", "git_diff"},
		RequiredStringFields: []string{"workspace_root"},
		RequireTiming:        true,
		RequireRedaction:     true,
	})
	if !report.OK {
		t.Fatalf("expected conformance success, got %#v", report)
	}
}

func TestVerifyResultArtifactJSONRejectsMissingCommandEvidence(t *testing.T) {
	report := VerifyResultArtifactJSON([]byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), ResultArtifactContract{
		Adapter:              "shell",
		SchemaVersion:        "rdev.shell-result.v1",
		RequiredStringFields: []string{"workspace_root"},
		RequireTiming:        true,
		RequireRedaction:     true,
	})
	if report.OK {
		t.Fatalf("expected conformance failure, got %#v", report)
	}
}

func TestVerifyResultArtifactJSONRejectsSecretPatterns(t *testing.T) {
	secret := "sk-" + "testsecret1234567890"
	content := strings.ReplaceAll(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": 0,
  "stdout": "__SECRET__",
  "timed_out": false,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`, "__SECRET__", secret)
	report := VerifyResultArtifactJSON([]byte(content), ResultArtifactContract{
		Adapter:                 "shell",
		SchemaVersion:           "rdev.shell-result.v1",
		RequiredStringFields:    []string{"workspace_root"},
		RequireTiming:           true,
		RequireRedaction:        true,
		RejectUnredactedSecrets: true,
	})
	if report.OK {
		t.Fatalf("expected conformance failure, got %#v", report)
	}
}
