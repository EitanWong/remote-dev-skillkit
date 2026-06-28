package hostrunner

import (
	"strings"
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
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunDevJob(host.ID, gw.TrustBundle(), job, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactContent == "" {
		t.Fatal("artifact content must be set")
	}
	if !strings.Contains(result.ArtifactContent, `"exit_code": 0`) {
		t.Fatalf("expected successful command evidence, got %s", result.ArtifactContent)
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
	if _, err := RunDevJob("hst_other", gw.TrustBundle(), job, now); err == nil {
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
	if _, err := RunDevJob(host.ID, gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected missing workspace to fail")
	}
}

func TestRunDevJobRejectsCommandNotAllowlisted(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	gw := gateway.NewMemoryGatewayWithClock(func() time.Time { return now })
	host := activeHost(t, gw)
	job, err := gw.CreateJob(host.ID, "shell", "demo", map[string]any{
		"workspace_root": ".",
		"capabilities":   []string{"shell.user"},
		"argv":           []string{"go", "env", "GOOS"},
		"allow_commands": []string{"git"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunDevJob(host.ID, gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected non-allowlisted command to fail")
	}
}

func TestRunDevJobRejectsTamperedEnvelope(t *testing.T) {
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
	job.Envelope.Intent = "tampered"
	if _, err := RunDevJob(host.ID, gw.TrustBundle(), job, now); err == nil {
		t.Fatal("expected tampered envelope to fail")
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
