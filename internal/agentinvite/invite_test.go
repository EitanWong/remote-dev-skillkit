package agentinvite

import (
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

func TestConnectionEntryUsesBootstrapScript(t *testing.T) {
	entry := newConnectionEntry("https://gateway.test/join/TARGET")
	for platform, command := range entry.OneLineCommands {
		if !strings.Contains(command, "rdev-bootstrap") {
			t.Fatalf("%s command does not identify the bootstrap boundary: %q", platform, command)
		}
	}
}

func TestConnectionEntryCommandsUseBootstrapOnly(t *testing.T) {
	invite, err := New(Options{
		GatewayURL:            "https://gateway.example.test",
		ManifestURL:           "https://gateway.example.test/v1/tickets/TARGET/manifest",
		ManifestRootPublicKey: "root:" + strings.Repeat("a", 43),
		Ticket:                testInviteTicket(t),
		Transport:             "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	for platform, command := range invite.ConnectionEntry.OneLineCommands {
		if !strings.Contains(command, "rdev-bootstrap") {
			t.Fatalf("%s command must invoke rdev-bootstrap: %q", platform, command)
		}
		for _, forbidden := range []string{"host serve", "rdev-host", "command -v rdev", "Get-Command rdev", "rdev-bootstrap upgrade"} {
			if strings.Contains(command, forbidden) {
				t.Fatalf("%s command contains legacy path %q: %q", platform, forbidden, command)
			}
		}
	}
}

func testInviteTicket(t *testing.T) model.Ticket {
	t.Helper()
	now := time.Now().UTC()
	ticket, err := model.NewTicket(model.HostModeAttendedTemporary, 600, []string{"shell.user"}, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}
