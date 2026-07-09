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

func TestVerifyCancellationArtifactJSONAcceptsTopLevelCanceledArtifact(t *testing.T) {
	report := VerifyCancellationArtifactJSON([]byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "argv": ["sleep", "30"],
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": false,
  "canceled": true,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), CancellationContract{
		Adapter:                 "shell",
		SchemaVersion:           "rdev.shell-result.v1",
		RequiredStringFields:    []string{"workspace_root"},
		RequireTiming:           true,
		RequireRedaction:        true,
		RejectUnredactedSecrets: true,
	})
	if !report.OK {
		t.Fatalf("expected cancellation conformance success, got %#v", report)
	}
}

func TestVerifyCancellationArtifactJSONAcceptsNestedCanceledArtifact(t *testing.T) {
	report := VerifyCancellationArtifactJSON([]byte(`{
  "schema_version": "rdev.codex-result.v1",
  "adapter": "codex",
  "workspace_root": "/tmp/repo",
  "prompt": "cancel",
  "codex_command": {"argv": ["codex"], "dir": "/tmp/repo", "exit_code": -1, "timed_out": false, "canceled": true, "output_truncated": false},
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), CancellationContract{
		Adapter:              "codex",
		SchemaVersion:        "rdev.codex-result.v1",
		CommandFields:        []string{"codex_command"},
		RequiredStringFields: []string{"workspace_root"},
		RequireTiming:        true,
		RequireRedaction:     true,
	})
	if !report.OK {
		t.Fatalf("expected nested cancellation conformance success, got %#v", report)
	}
}

func TestVerifyCancellationArtifactJSONRejectsTimeoutAsCancellation(t *testing.T) {
	report := VerifyCancellationArtifactJSON([]byte(`{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "exit_code": -1,
  "timed_out": true,
  "canceled": false,
  "output_truncated": false,
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`), CancellationContract{
		Adapter:              "shell",
		SchemaVersion:        "rdev.shell-result.v1",
		RequiredStringFields: []string{"workspace_root"},
		RequireTiming:        true,
		RequireRedaction:     true,
	})
	if report.OK {
		t.Fatalf("expected cancellation conformance failure, got %#v", report)
	}
}

func TestVerifyLifecycleManifestJSONAcceptsCompleteManifest(t *testing.T) {
	report := VerifyLifecycleManifestJSON([]byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version", "path"]},
    "plan": {"implemented": true, "evidence": ["planned_commands"], "declares_external_consequences": true, "declares_required_authorizations": true},
    "prepare": {"implemented": true, "evidence": ["workspace_root"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["argv", "exit_code"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result_artifact"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["locks_released"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_tasks": false,
    "adapter_authorizes_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {
    "supported": true,
    "evidence_field": "canceled",
    "timeout_exclusive": true,
    "cleanup_on_cancel": true
  }
}`), LifecycleContract{
		Adapter:                 "claude-code",
		RequireSafety:           true,
		RequireCancellation:     true,
		RequireResultSchema:     true,
		RejectUnredactedSecrets: true,
	})
	if !report.OK {
		t.Fatalf("expected lifecycle conformance success, got %#v", report)
	}
}

func TestVerifyLifecycleManifestJSONRejectsMissingCancellation(t *testing.T) {
	report := VerifyLifecycleManifestJSON([]byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["plan"], "declares_external_consequences": true, "declares_required_authorizations": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["command"], "supports_timeout": true, "supports_cancellation": false},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_tasks": false,
    "adapter_authorizes_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": false, "evidence_field": "", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`), LifecycleContract{
		Adapter:             "claude-code",
		RequireSafety:       true,
		RequireCancellation: true,
		RequireResultSchema: true,
	})
	if report.OK {
		t.Fatalf("expected lifecycle conformance failure, got %#v", report)
	}
}

func TestVerifyLifecycleManifestJSONRejectsHiddenPersistence(t *testing.T) {
	report := VerifyLifecycleManifestJSON([]byte(`{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "gui",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["plan"], "declares_external_consequences": true, "declares_required_authorizations": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["session"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.gui-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_tasks": false,
    "adapter_authorizes_dangerous_actions": false,
    "adapter_installs_persistence": true,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": true, "evidence_field": "canceled", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`), LifecycleContract{
		Adapter:             "gui",
		RequireSafety:       true,
		RequireCancellation: true,
		RequireResultSchema: true,
	})
	if report.OK {
		t.Fatalf("expected lifecycle conformance failure, got %#v", report)
	}
}
