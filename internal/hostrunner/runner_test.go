package hostrunner

import (
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestRunDevJobAcceptsScopedShellJob(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, job, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactContent == "" {
		t.Fatal("artifact content must be set")
	}
}

func TestRunDevJobRejectsWrongHost(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunDevJob("hst_other", job, now); err == nil {
		t.Fatal("expected wrong host to fail")
	}
}

func TestRunDevJobRejectsMissingWorkspace(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"capabilities": []string{"shell.user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunDevJob(host.ID, job, now); err == nil {
		t.Fatal("expected missing workspace to fail")
	}
}

func activeHost(t *testing.T, gw *gateway.MemoryGateway) model.Host {
	t.Helper()
	ticket, err := gw.CreateTicket(model.HostModeAttendedTemporary, 600, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	host, err := gw.RegisterHost(model.HostRegistration{
		TicketCode: ticket.Code,
		Name:       "host",
		OS:         "darwin",
		Arch:       "arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err = gw.ApproveHost(host.ID, []string{"shell.user"})
	if err != nil {
		t.Fatal(err)
	}
	return host
}
