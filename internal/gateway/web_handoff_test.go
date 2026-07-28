package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/controlplane"
)

func TestWebHandoffClaimsOnlyOnceAndIssuesExpiringArtifactTicket(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	gw := NewMemoryGatewayWithClock(func() time.Time { return now })
	session, err := gw.CreateSession(controlplane.SessionSpec{
		Reason:     "web handoff fixture",
		JoinPolicy: "single-target",
	})
	if err != nil {
		t.Fatal(err)
	}

	handoff, proof, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID,
		Platform:  WebHandoffPlatformWindowsAMD64,
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.ID == "" || proof == "" || handoff.SessionID != session.ID || handoff.Platform != WebHandoffPlatformWindowsAMD64 {
		t.Fatalf("unexpected created handoff: %#v", handoff)
	}
	if strings.Contains(handoff.ID, proof) {
		t.Fatal("handoff identifier must not contain the claim proof")
	}
	if _, _, err := gw.ClaimWebHandoff(handoff.ID, "wrong-proof", 10*time.Minute); err == nil {
		t.Fatal("claim with an invalid proof should fail")
	}

	claimed, ticket, err := gw.ClaimWebHandoff(handoff.ID, proof, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ClaimedAt.IsZero() || ticket == "" {
		t.Fatalf("claim should record claimed time and issue artifact ticket: %#v", claimed)
	}
	if _, _, err := gw.ClaimWebHandoff(handoff.ID, proof, 10*time.Minute); err == nil {
		t.Fatal("handoff claim should be single-use")
	}
	if _, err := gw.ValidateWebHandoffArtifactTicket(handoff.ID, ticket); err != nil {
		t.Fatalf("fresh artifact ticket should validate: %v", err)
	}

	now = now.Add(11 * time.Minute)
	if _, err := gw.ValidateWebHandoffArtifactTicket(handoff.ID, ticket); err == nil {
		t.Fatal("expired artifact ticket should fail")
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
	if _, _, err := gw.CreateWebHandoff(WebHandoffSpec{
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
		if _, _, err := gw.CreateWebHandoff(spec); err == nil {
			t.Fatalf("invalid web handoff specification accepted: %#v", spec)
		}
	}
	if _, _, err := gw.CreateWebHandoff(WebHandoffSpec{
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
	if _, _, err := gw.CreateWebHandoff(WebHandoffSpec{
		SessionID: session.ID, Platform: WebHandoffPlatformWindowsAMD64, ExpiresAt: now.Add(time.Hour),
	}); err != ErrWebHandoffSessionInvalid {
		t.Fatalf("closed session error = %v, want ErrWebHandoffSessionInvalid", err)
	}
}
