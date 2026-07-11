package cli

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestSupportSessionStartDiagnosticUsesStrictAllowlist(t *testing.T) {
	diagnostic := newSupportSessionStartDiagnostic("secret-phase", "secret-reason", "secret-action", tunnel.AvailabilitySet{
		Candidates: []tunnel.Candidate{{ProviderID: "safe-provider", URL: "https://secret.example.test/?token=query-secret"}},
		Attempts: []tunnel.Attempt{
			{
				ProviderID: "safe-provider", CandidateID: "0123456789abcdef", Status: tunnel.AttemptHealthy,
				ErrorClass: "secret-error-class", Probe: tunnel.ProbeEvidence{InstanceMarker: "instance-marker-secret"},
			},
			{ProviderID: "unsafe provider", CandidateID: "NOT-A-CANDIDATE", Status: tunnel.AttemptStatus("secret-status")},
		},
	})
	content, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, forbidden := range []string{
		"secret.example.test", "query-secret", "instance-marker-secret", "secret-error-class", "secret-status",
		"secret-phase", "secret-reason", "secret-action", "unsafe provider", "NOT-A-CANDIDATE",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostic leaked %q: %s", forbidden, text)
		}
	}
	if diagnostic.SchemaVersion != "rdev.support-session-start-diagnostic.v2" || diagnostic.ReadyToSend ||
		diagnostic.Phase != "support-session-start" || diagnostic.Reason != "support_session_unavailable" ||
		diagnostic.NextActionClass != "inspect-protected-status" || len(diagnostic.Attempts) != 2 ||
		diagnostic.Attempts[0].CandidateID != "0123456789abcdef" || diagnostic.Attempts[0].ErrorClass != "failed" ||
		diagnostic.Attempts[1].ProviderID != "unknown" || diagnostic.Attempts[1].CandidateID != "" ||
		diagnostic.Attempts[1].Status != "unknown" {
		t.Fatalf("unexpected diagnostic projection: %#v", diagnostic)
	}
}

func TestSupportSessionDiagnosticPublicationErrorsArePathSafe(t *testing.T) {
	diagnostic := newSupportSessionStartDiagnostic("readiness-policy", "direct_handoff_readiness_not_satisfied", "configure-redundant-public-gateway", tunnel.AvailabilitySet{})
	privatePath := filepath.Join(t.TempDir(), "operator-secret", "status.json")
	err := writeSupportSessionDiagnostic(privatePath, failingWriter{err: errors.New("private writer failure")}, diagnostic)
	if err == nil {
		t.Fatal("expected diagnostic publication failure")
	}
	for _, forbidden := range []string{privatePath, "operator-secret", "private writer failure"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("diagnostic publication error leaked %q: %v", forbidden, err)
		}
	}
}

func TestSupportSessionStderrEventMapsUnknownInputToFixedClasses(t *testing.T) {
	event := newSupportSessionStderrEvent("secret-event", map[string]any{
		"connected": false, "ticket": "ABCD-1234", "gateway_url": "https://secret.example.test",
	})
	if event.SchemaVersion != "rdev.support-session-foreground-log-event.v1" || event.Event != "status" ||
		event.StatusClass != "status" || event.Connected || event.ActionClass != "inspect-protected-status" {
		t.Fatalf("unexpected stderr event projection: %#v", event)
	}
}

func TestSupportSessionStderrEventMapsLifecycleClasses(t *testing.T) {
	for _, test := range []struct {
		event       string
		status      map[string]any
		statusClass string
		actionClass string
		connected   bool
	}{
		{event: "waiting", status: map[string]any{}, statusClass: "waiting", actionClass: "wait-for-target"},
		{event: "pending-activation", status: map[string]any{}, statusClass: "pending-activation", actionClass: "review-activation"},
		{event: "connected", status: map[string]any{}, statusClass: "connected", actionClass: "report-connection-established", connected: true},
	} {
		t.Run(test.event, func(t *testing.T) {
			got := newSupportSessionStderrEvent(test.event, test.status)
			if got.StatusClass != test.statusClass || got.ActionClass != test.actionClass || got.Connected != test.connected {
				t.Fatalf("event projection = %#v", got)
			}
		})
	}
}

func TestSupportSessionStderrEventPromotesConnectedStatus(t *testing.T) {
	event := newSupportSessionStderrEvent("waiting", map[string]any{"connected": true})
	if event.Event != "connected" || event.StatusClass != "connected" || !event.Connected ||
		event.ActionClass != "report-connection-established" {
		t.Fatalf("connected status retained contradictory waiting projection: %#v", event)
	}
}
