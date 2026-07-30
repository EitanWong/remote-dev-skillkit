package gateway

import (
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestWebHandoffClaimsOnlyOnceAndIssuesExpiringArtifactTicket(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "direct-link handoff fixture", JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID,
		Platform:  WebHandoffPlatformWindowsAMD64,
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.ID == "" || handoff.SessionID != session.ID || handoff.Platform != WebHandoffPlatformWindowsAMD64 {
		t.Fatalf("unexpected created handoff: %#v", handoff)
	}
	claimed, ticket, err := gw.ClaimWebHandoff(handoff.ID, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ClaimedAt.IsZero() || ticket == "" {
		t.Fatalf("claim should record claimed time and issue artifact ticket: %#v", claimed)
	}
	if _, _, err := gw.ClaimWebHandoff(handoff.ID, 10*time.Minute); err != ErrWebHandoffClaimed {
		t.Fatalf("second claim error = %v, want %v", err, ErrWebHandoffClaimed)
	}
	if _, err := gw.ValidateWebHandoffArtifactTicket(handoff.ID, ticket); err != nil {
		t.Fatalf("fresh artifact ticket should validate: %v", err)
	}
	now = now.Add(11 * time.Minute)
	if _, err := gw.ValidateWebHandoffArtifactTicket(handoff.ID, ticket); err == nil {
		t.Fatal("expired artifact ticket should fail")
	}
	for _, event := range gw.AuditEvents() {
		if (event.Action == "web_handoff.create" || event.Action == "web_handoff.claim") && event.TargetID == handoff.ID {
			t.Fatalf("audit event %q exposed the direct-link capability", event.Action)
		}
	}
}

func TestWebHandoffRejectsSessionThatAlreadyHasTarget(t *testing.T) {
	gw := NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{JoinPolicy: "single-target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role: controlplane.EndpointRoleTarget,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID,
		Platform:  WebHandoffPlatformWindowsAMD64,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("single-target session with an endpoint should not issue another web handoff")
	}
}

func TestWebHandoffRejectsInvalidExpiredMissingAndClosedSessions(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	if got := gw.Now(); !got.Equal(now) {
		t.Fatalf("gateway clock = %s, want %s", got, now)
	}

	for _, spec := range []WebHandoffSpec{
		{SessionID: "", Platform: WebHandoffPlatformWindowsAMD64, ExpiresAt: now.Add(time.Hour)},
		{SessionID: "ses_missing", Platform: "linux-amd64", ExpiresAt: now.Add(time.Hour)},
		{SessionID: "ses_missing", Platform: WebHandoffPlatformWindowsAMD64, ExpiresAt: now},
	} {
		if _, err := gw.CreateWebHandoff(spec); err == nil {
			t.Fatalf("invalid web handoff specification accepted: %#v", spec)
		}
	}
	if _, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: "ses_missing", Platform: WebHandoffPlatformWindowsAMD64, ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("missing session was accepted for a web handoff")
	}

	session, err := gw.CreateSession(controlplane.SessionSpec{Reason: "closed web handoff fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gw.CloseSession(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID, Platform: WebHandoffPlatformWindowsAMD64, ExpiresAt: now.Add(time.Hour),
	}); err != ErrWebHandoffSessionInvalid {
		t.Fatalf("closed session error = %v, want ErrWebHandoffSessionInvalid", err)
	}
}

func TestWebHandoffCapsExpiryAtSessionExpiry(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:    "expiring web handoff fixture",
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	handoff, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID,
		Platform:  WebHandoffPlatformWindowsAMD64,
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handoff.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("handoff expiry = %s, want session expiry %s", handoff.ExpiresAt, session.ExpiresAt)
	}
}

func TestWebHandoffClaimRevalidatesSessionWithoutConsumingHandoff(t *testing.T) {
	gw := NewMemoryGateway()
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:     "single-target claim revalidation fixture",
		JoinPolicy: "single-target",
	})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID,
		Platform:  WebHandoffPlatformWindowsAMD64,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := gw.JoinSessionByCode(session.JoinCode, controlplane.EndpointSpec{
		Role: controlplane.EndpointRoleTarget,
	}); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, _, err := gw.ClaimWebHandoff(handoff.ID, 10*time.Minute); err != ErrWebHandoffSessionInvalid {
			t.Fatalf("claim attempt %d error = %v, want %v", attempt, err, ErrWebHandoffSessionInvalid)
		}
	}
}
