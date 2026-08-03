package gateway

import (
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestGatewayRenameHostWritesAudit(t *testing.T) {
	gw := NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "host audit fixture", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role:                controlplane.EndpointRoleTarget,
		Name:                "dev-win",
		Platform:            "windows/amd64",
		IdentityFingerprint: "fp-win",
		Transport:           controlplane.TransportLongPoll,
	})
	if err != nil {
		t.Fatal(err)
	}

	hosts := gw.Hosts()
	if len(hosts) != 1 {
		t.Fatalf("gateway hosts = %d, want 1", len(hosts))
	}
	host, err := gw.RenameHost(hosts[0].HostID, "Unity Dev Machine")
	if err != nil {
		t.Fatal(err)
	}
	if host.DisplayName != "Unity Dev Machine" {
		t.Fatalf("renamed host display = %q", host.DisplayName)
	}

	events := gw.AuditEvents()
	found := false
	for _, event := range events {
		if event.Action == "host.rename" && event.TargetID == hosts[0].HostID {
			found = true
			break
		}
	}
	if !found {
		var actions []string
		for _, event := range events {
			actions = append(actions, event.Action+":"+event.TargetID)
		}
		t.Fatalf("host.rename audit missing; got %s", strings.Join(actions, ", "))
	}
}

func TestGatewayRenameHostUnknownFails(t *testing.T) {
	gw := NewMemoryGateway()
	if _, err := gw.RenameHost("hst_missing", "name"); err == nil {
		t.Fatal("rename of unknown host should fail")
	}
}
