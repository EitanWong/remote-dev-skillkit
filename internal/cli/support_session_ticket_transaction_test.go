package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestCreateFinalProbedSupportTicketReplacesTicketWithSurvivingCandidate(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	initial := supportSessionAvailabilityForTests("a-failed", "b-healthy")
	probedCodes := map[string][]string{}
	metadata := func(candidates []supportsession.GatewayURLCandidate) map[string]string {
		return addGatewayCandidateTicketMetadata(map[string]string{"connection_entry": "standard-visible"}, candidates)
	}

	ticket, final, err := createFinalProbedSupportTicket(
		context.Background(), gw, store, initial, 60, "survivor retry", metadata,
		func(_ context.Context, candidate tunnel.Candidate, ticketCode, _ string) error {
			probedCodes[candidate.ProviderID] = append(probedCodes[candidate.ProviderID], ticketCode)
			if candidate.ProviderID == "a-failed" {
				return errors.New("candidate failed")
			}
			return nil
		},
		"gateway-instance",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Candidates) != 1 || final.Candidates[0].ProviderID != "b-healthy" {
		t.Fatalf("final candidates = %#v", final.Candidates)
	}
	if len(probedCodes["b-healthy"]) != 2 || probedCodes["b-healthy"][0] == probedCodes["b-healthy"][1] {
		t.Fatalf("survivor must pass both replaced and final tickets, codes=%v", probedCodes["b-healthy"])
	}
	metadataJSON := ticket.Metadata[gateway.TicketMetadataGatewayCandidates]
	if strings.Contains(metadataJSON, "a-failed") || !strings.Contains(metadataJSON, "b-healthy") {
		t.Fatalf("final metadata contains failed candidate: %s", metadataJSON)
	}
	snapshot := gw.Snapshot()
	if len(snapshot.Tickets) != 2 {
		t.Fatalf("tickets = %#v, want replaced and final", snapshot.Tickets)
	}
	revoked := 0
	for _, candidateTicket := range snapshot.Tickets {
		if candidateTicket.ID == ticket.ID && candidateTicket.Status != model.TicketStatusProbing {
			t.Fatalf("final ticket status = %q", candidateTicket.Status)
		}
		if candidateTicket.ID != ticket.ID && candidateTicket.Status == model.TicketStatusRevoked {
			revoked++
		}
	}
	if revoked != 1 || !recordedSnapshotHasRevokedTicket(store.snapshots) {
		t.Fatalf("replaced ticket was not revoked and persisted: %#v", snapshot.Tickets)
	}
}

func TestCreateFinalProbedSupportTicketFailsAfterEverySurvivorIsRejected(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	initial := supportSessionAvailabilityForTests("a-first", "b-second")
	bCalls := 0
	_, _, err := createFinalProbedSupportTicket(
		context.Background(), gw, store, initial, 60, "reject all",
		func(candidates []supportsession.GatewayURLCandidate) map[string]string {
			return addGatewayCandidateTicketMetadata(nil, candidates)
		},
		func(_ context.Context, candidate tunnel.Candidate, _, _ string) error {
			if candidate.ProviderID == "a-first" {
				return errors.New("first rejected")
			}
			bCalls++
			if bCalls == 1 {
				return nil
			}
			return errors.New("survivor rejected by replacement ticket")
		},
		"gateway-instance",
	)
	if err == nil || !strings.Contains(err.Error(), "rejected every") {
		t.Fatalf("expected all-survivors failure, got %v", err)
	}
	snapshot := gw.Snapshot()
	if len(snapshot.Tickets) != 2 {
		t.Fatalf("tickets = %#v, want two attempted tickets", snapshot.Tickets)
	}
	for _, ticket := range snapshot.Tickets {
		if ticket.Status != model.TicketStatusRevoked {
			t.Fatalf("ticket %q status = %q, want revoked", ticket.ID, ticket.Status)
		}
	}
}

func TestIntersectAvailabilityPreservesTicketBoundProbeEvidence(t *testing.T) {
	current := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates:    []tunnel.Candidate{{ProviderID: "provider", URL: "https://provider.example.test"}},
		Attempts: []tunnel.Attempt{{
			ProviderID: "provider", Status: tunnel.AttemptHealthy,
			Probe: tunnel.ProbeEvidence{StaticBootstrapOK: true, TicketBoundBootstrapOK: true},
		}},
	}
	live := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates:    append([]tunnel.Candidate(nil), current.Candidates...),
		Attempts: []tunnel.Attempt{{
			ProviderID: "provider", Status: tunnel.AttemptHealthy,
			Probe: tunnel.ProbeEvidence{DNSOK: true, TCPConnectOK: true, TLSOK: true, HealthOK: true},
		}},
	}

	merged := intersectAvailabilityWithRuntime(current, live)
	if len(merged.Attempts) != 1 || !merged.Attempts[0].Probe.HealthOK ||
		!merged.Attempts[0].Probe.StaticBootstrapOK || !merged.Attempts[0].Probe.TicketBoundBootstrapOK {
		t.Fatalf("runtime intersection lost probe evidence: %#v", merged.Attempts)
	}
}
