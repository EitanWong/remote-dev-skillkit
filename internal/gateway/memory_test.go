package gateway

import (
	"testing"
	"time"

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
	if artifact.Content != "demo complete" {
		t.Fatalf("unexpected artifact content %q", artifact.Content)
	}
	if got := len(gw.AuditEvents()); got != 5 {
		t.Fatalf("expected 5 audit events, got %d", got)
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
