package model

import (
	"testing"
	"time"
)

func TestNewTicketSetsExpiry(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	ticket, err := NewTicket(HostModeAttendedTemporary, 3600, []string{"shell.user"}, "repair", now)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.ID == "" {
		t.Fatal("ticket id must be set")
	}
	if len(ticket.Code) != 9 {
		t.Fatalf("expected code like ABCD-1234, got %q", ticket.Code)
	}
	if !ticket.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected expiry %s", ticket.ExpiresAt)
	}
}

func TestHostModeValid(t *testing.T) {
	if !HostModeAttendedTemporary.Valid() {
		t.Fatal("attended temporary should be valid")
	}
	if HostMode("hidden").Valid() {
		t.Fatal("hidden mode should be invalid")
	}
}
