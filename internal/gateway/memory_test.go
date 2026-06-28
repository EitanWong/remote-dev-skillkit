package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/audit"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestMemoryGatewayDemoFlow(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if host.Status != model.HostStatusPending {
		t.Fatalf("host should start pending, got %s", host.Status)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host.Status != model.HostStatusActive {
		t.Fatalf("host should be active, got %s", host.Status)
	}
	job, err := gw.CreateJob(host.ID, "powershell", "diagnose node", map[string]any{"cwd": "%USERPROFILE%"})
	if err != nil {
		t.Fatal(err)
	}
	job, artifact, err := gw.CompleteJob(job.ID, "demo complete")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.JobStatusSucceeded {
		t.Fatalf("job should succeed, got %s", job.Status)
	}
	if job.Envelope == nil {
		t.Fatal("job should include a signed envelope")
	}
	if artifact.Content != "demo complete" {
		t.Fatalf("unexpected artifact content %q", artifact.Content)
	}
	if got := len(gw.AuditEvents()); got != 5 {
		t.Fatalf("expected 5 audit events, got %d", got)
	}
}

func TestMemoryGatewaySignsJobEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "codex", "fix tests", map[string]any{
		"operator_id":          "eitan",
		"workspace_root":       "/repo",
		"write_scope":          []any{"/repo"},
		"branch":               "rdev/job",
		"capabilities":         []any{"fs.read", "fs.write.scoped", "dev.codex"},
		"approvals_required":   []any{"git.push"},
		"max_duration_seconds": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Envelope == nil {
		t.Fatal("job envelope must be present")
	}
	if err := gw.VerifyJobEnvelope(*job.Envelope, host.ID); err != nil {
		t.Fatalf("expected gateway-signed envelope to verify: %v", err)
	}
	if job.Envelope.OperatorID != "eitan" {
		t.Fatalf("expected operator_id from policy, got %q", job.Envelope.OperatorID)
	}
	if job.Envelope.Workspace.Root != "/repo" {
		t.Fatalf("unexpected workspace root %q", job.Envelope.Workspace.Root)
	}
	if got := job.Envelope.Limits.MaxDurationSeconds; got != 300 {
		t.Fatalf("expected max duration 300, got %d", got)
	}
}

func TestMemoryGatewayRejectsJobAfterTicketExpiry(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	current := now
	gw := NewMemoryGatewayWithClock(func() time.Time { return current })

	host := activeHost(t, gw)
	current = now.Add(601 * time.Second)
	_, err := gw.CreateJob(host.ID, "powershell", "diagnose", nil)
	if !errors.Is(err, ErrTicketExpired) {
		t.Fatalf("expected ticket expired error, got %v", err)
	}
}

func TestMemoryGatewayRejectsJobForPendingHost(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateJob(host.ID, "powershell", "diagnose node", nil); err == nil {
		t.Fatal("expected pending host job creation to fail")
	}
}

func TestMemoryGatewayRevokeTicketPreventsRegistration(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })

	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RevokeTicket(ticket.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	}); err == nil {
		t.Fatal("expected revoked ticket registration to fail")
	}
}

func TestMemoryGatewayWritesAuditSink(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	store := audit.NewJSONLStore(path)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now }).WithAuditSink(&store)

	if _, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 {
		t.Fatal("expected audit file to contain an event")
	}
}

func activeHost(t *testing.T, gw *MemoryGateway) model.Host {
	t.Helper()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "repair")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "win-temp-01",
		OS:         "windows",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return host
}
