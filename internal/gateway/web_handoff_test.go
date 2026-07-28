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
