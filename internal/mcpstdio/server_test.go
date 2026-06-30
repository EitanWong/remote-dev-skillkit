package mcpstdio

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

func TestServerInitializeAndToolsList(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(lines))
	}
	if lines[0]["error"] != nil {
		t.Fatalf("initialize failed: %v", lines[0]["error"])
	}
	if lines[1]["error"] != nil {
		t.Fatalf("tools/list failed: %v", lines[1]["error"])
	}
}

func TestServerToolCallCreateTicket(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.tickets.create","arguments":{"mode":"attended-temporary","ttl_seconds":600,"reason":"test"}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 response, got %d", len(lines))
	}
	result, ok := lines[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %#v", lines[0])
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structured content: %#v", result)
	}
	if _, ok := structured["joinUrl"].(string); !ok {
		t.Fatalf("expected joinUrl in structured content: %#v", structured)
	}
}

func TestServerToolCallCreateJobReturnsEnvelope(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "local",
		OS:         "darwin",
		Arch:       "arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.jobs.create","arguments":{"host_id":"` + host.ID + `","adapter":"codex","intent":"fix tests","policy":{"workspace_root":"/repo","capabilities":["fs.read","fs.write.scoped","dev.codex"]}}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gw)

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	envelope := structured["envelope"].(map[string]any)
	if envelope["signature"] == "" {
		t.Fatalf("expected signed envelope, got %#v", envelope)
	}
	if envelope["host_id"] != host.ID {
		t.Fatalf("expected envelope host binding %q, got %#v", host.ID, envelope["host_id"])
	}
}

func TestServerToolCallExplainShellPolicy(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rdev.policy.explain_shell","arguments":{"mode":"attended-temporary","policy":{"workspace_root":".","capabilities":["shell.user"],"argv":["go","env","GOOS"],"allow_commands":["go"]}}}}` + "\n"
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["allowed"] != true {
		t.Fatalf("expected shell policy allowed, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterResult(t *testing.T) {
	artifact := `{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
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
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_result", map[string]any{
		"adapter":       "shell",
		"schema":        "rdev.shell-result.v1",
		"artifact_json": artifact,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter conformance success, got %#v", structured)
	}
}

func TestServerToolCallVerifyEnrollmentCertificate(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	certificate, root := enrollmentCertificateForMCPTest(t, now)
	content, err := json.Marshal(certificate)
	if err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.enrollment.verify_certificate", map[string]any{
		"certificate_json": string(content),
		"root_public_key":  root,
		"verify_at":        now.Add(time.Minute).Format(time.RFC3339),
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != EnrollmentCertificateVerificationSchemaVersion || structured["ok"] != true {
		t.Fatalf("expected enrollment certificate verification success, got %#v", structured)
	}
	if structured["ticket_code"] != "ABCD-1234" || structured["issuer_key_id"] != "enrollment-root" {
		t.Fatalf("unexpected certificate identity in report: %#v", structured)
	}
}

func TestServerToolCallVerifyEnrollmentCertificateReportsFailure(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	certificate, _ := enrollmentCertificateForMCPTest(t, now)
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(certificate)
	if err != nil {
		t.Fatal(err)
	}
	input := mcpRequestLine(t, "rdev.enrollment.verify_certificate", map[string]any{
		"certificate_json": string(content),
		"root_public_key":  trustref.Encode("enrollment-root", wrongPublicKey),
		"verify_at":        now.Add(time.Minute).Format(time.RFC3339),
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("verification failure should be structured content, got RPC error: %#v", lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["ok"] != false {
		t.Fatalf("expected enrollment certificate failure report, got %#v", structured)
	}
	errors, ok := structured["errors"].([]any)
	if !ok || len(errors) == 0 || !strings.Contains(errors[0].(string), "signature mismatch") {
		t.Fatalf("expected signature mismatch report, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterResultReportsFailure(t *testing.T) {
	artifact := `{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "redacted": false,
  "redaction_rules": ["openai_api_key"]
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_result", map[string]any{
		"adapter":       "shell",
		"schema":        "rdev.shell-result.v1",
		"artifact_json": artifact,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	if lines[0]["error"] != nil {
		t.Fatalf("conformance failure should be structured content, got RPC error: %#v", lines[0]["error"])
	}
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["ok"] != false {
		t.Fatalf("expected adapter conformance failure report, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterLifecycle(t *testing.T) {
	manifest := `{
  "schema_version": "rdev.adapter-lifecycle.v1",
  "adapter": "claude-code",
  "phases": {
    "detect": {"implemented": true, "evidence": ["version"]},
    "plan": {"implemented": true, "evidence": ["commands"], "declares_external_consequences": true, "declares_required_approvals": true},
    "prepare": {"implemented": true, "evidence": ["workspace"], "enforces_workspace_boundary": true, "uses_workspace_lock": true},
    "run": {"implemented": true, "evidence": ["process"], "supports_timeout": true, "supports_cancellation": true},
    "collect": {"implemented": true, "evidence": ["result"], "emits_result_artifact": true, "result_schema": "rdev.claude-code-result.v1"},
    "cleanup": {"implemented": true, "evidence": ["cleanup"], "idempotent": true, "releases_locks": true}
  },
  "safety": {
    "adapter_authorizes_jobs": false,
    "adapter_approves_dangerous_actions": false,
    "adapter_installs_persistence": false,
    "host_validates_before_run": true,
    "redacts_outputs": true
  },
  "cancellation": {"supported": true, "evidence_field": "canceled", "timeout_exclusive": true, "cleanup_on_cancel": true}
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_lifecycle", map[string]any{
		"adapter":       "claude-code",
		"artifact_json": manifest,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["artifact_schema"] != "rdev.adapter-lifecycle.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter lifecycle conformance success, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterCancellation(t *testing.T) {
	artifact := `{
  "schema_version": "rdev.shell-result.v1",
  "adapter": "shell",
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
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_cancellation", map[string]any{
		"adapter":       "shell",
		"schema":        "rdev.shell-result.v1",
		"artifact_json": artifact,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter cancellation conformance success, got %#v", structured)
	}
}

func TestServerToolCallVerifyAdapterRuntime(t *testing.T) {
	fixture := `{
  "schema_version": "rdev.adapter-runtime-fixture.v1",
  "adapter": "fake",
  "job_id": "job_123",
  "workspace_root": "/tmp/repo",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "canceled": false,
  "timed_out": false,
  "cleanup_attempted": true,
  "cleanup_ok": true,
  "result_artifact_schema": "rdev.fake-result.v1",
  "result_artifact": {"schema_version": "rdev.fake-result.v1", "adapter": "fake", "workspace_root": "/tmp/repo"},
  "phases": [
    {"phase": "detect", "ok": true, "evidence": ["version"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "plan", "ok": true, "evidence": ["commands"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "prepare", "ok": true, "evidence": ["workspace"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "run", "ok": true, "evidence": ["process"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "collect", "ok": true, "evidence": ["result"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "cleanup", "ok": true, "evidence": ["cleanup"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}
  ]
}`
	input := mcpRequestLine(t, "rdev.adapter.verify_runtime", map[string]any{
		"adapter":                 "fake",
		"artifact_json":           fixture,
		"require_result_artifact": true,
	})
	var out bytes.Buffer
	server := NewServer(gateway.NewMemoryGateway())

	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := responseLines(t, out.String())
	result := lines[0]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != "rdev.adapter-conformance-report.v1" || structured["artifact_schema"] != "rdev.adapter-runtime-fixture.v1" || structured["ok"] != true {
		t.Fatalf("expected adapter runtime conformance success, got %#v", structured)
	}
}

func enrollmentCertificateForMCPTest(t *testing.T, now time.Time) (model.HostEnrollmentCertificate, string) {
	t.Helper()
	hostPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuerPublicKey, issuerPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registration := model.HostRegistration{
		TicketCode:          "ABCD-1234",
		Name:                "managed-mac",
		OS:                  "darwin",
		Arch:                "arm64",
		Capabilities:        []string{"codex.run", "git.diff"},
		IdentityKeyID:       "host-test",
		IdentityPublicKey:   base64.RawURLEncoding.EncodeToString(hostPublicKey),
		IdentityFingerprint: enrollmentFingerprintForMCPTest(hostPublicKey),
	}
	ticket := model.Ticket{
		Code:         registration.TicketCode,
		Mode:         model.HostModeManaged,
		Capabilities: registration.Capabilities,
	}
	certificate, err := model.SignHostEnrollmentCertificate(registration, ticket, "enrollment-root", issuerPrivateKey, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, trustref.Encode("enrollment-root", issuerPublicKey)
}

func enrollmentFingerprintForMCPTest(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mcpRequestLine(t *testing.T, tool string, arguments map[string]any) string {
	t.Helper()
	content, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(content) + "\n"
}

func responseLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	parts := strings.Split(strings.TrimSpace(output), "\n")
	responses := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(part), &decoded); err != nil {
			t.Fatalf("invalid response line %q: %v", part, err)
		}
		responses = append(responses, decoded)
	}
	return responses
}
