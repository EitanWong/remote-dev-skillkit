package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/policy"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func createFinalProbedSupportTicket(
	ctx context.Context,
	gw *gateway.MemoryGateway,
	store gateway.StateStore,
	initial tunnel.AvailabilitySet,
	ttlSeconds int,
	reason string,
	metadata func([]supportsession.GatewayURLCandidate) map[string]string,
	probe func(context.Context, tunnel.Candidate, string, string) error,
	instance string,
) (model.Ticket, tunnel.AvailabilitySet, error) {
	survivors := initial
	survivors.Candidates = append([]tunnel.Candidate(nil), initial.Candidates...)
	survivors.Attempts = append([]tunnel.Attempt(nil), initial.Attempts...)
	for attemptsRemaining := len(initial.Candidates); attemptsRemaining > 0 && len(survivors.Candidates) > 0; attemptsRemaining-- {
		ticket, err := gw.CreateProbingTicketWithMetadata(
			model.HostModeAttendedTemporary,
			ttlSeconds,
			cliPolicyCapabilitiesToStrings(policy.TemporaryDefaults()),
			reason,
			metadata(gatewayURLCandidatesFromTunnelCandidates(survivors.Candidates)),
		)
		if err != nil {
			return model.Ticket{}, tunnel.AvailabilitySet{}, err
		}
		probed := finalProbeAvailability(ctx, survivors, ticket.Code, instance, probe)
		if len(probed.Candidates) == len(survivors.Candidates) {
			return ticket, probed, nil
		}
		if err := rollbackSupportTicket(gw, store, ticket.ID, "one or more final ticket bootstrap probes failed"); err != nil {
			return model.Ticket{}, tunnel.AvailabilitySet{}, fmt.Errorf("persist replacement-ticket rollback: %w", err)
		}
		survivors = probed
	}
	return model.Ticket{}, tunnel.AvailabilitySet{}, errors.New("final ticket bootstrap probes rejected every public gateway candidate")
}

func finalProbeAvailability(
	ctx context.Context,
	set tunnel.AvailabilitySet,
	ticketCode string,
	instance string,
	probe func(context.Context, tunnel.Candidate, string, string) error,
) tunnel.AvailabilitySet {
	result := set
	result.Candidates = make([]tunnel.Candidate, 0, len(set.Candidates))
	result.Attempts = append([]tunnel.Attempt(nil), set.Attempts...)
	for _, candidate := range set.Candidates {
		if err := probe(ctx, candidate, ticketCode, instance); err != nil {
			for index := range result.Attempts {
				if result.Attempts[index].ProviderID == candidate.ProviderID {
					result.Attempts[index].Status = tunnel.AttemptDegraded
					result.Attempts[index].ErrorClass = "final-ticket-bootstrap-probe-failed"
				}
			}
			continue
		}
		result.Candidates = append(result.Candidates, candidate)
		for index := range result.Attempts {
			if result.Attempts[index].ProviderID == candidate.ProviderID {
				result.Attempts[index].Probe.TicketBoundBootstrapOK = true
			}
		}
	}
	return result
}

func rollbackSupportTicket(gw *gateway.MemoryGateway, store gateway.StateStore, ticketID, reason string) error {
	if _, _, err := gw.RollbackTicket(ticketID, reason); err != nil {
		return fmt.Errorf("rollback support ticket: %w", err)
	}
	if _, err := store.SaveFrom(gw); err != nil {
		return fmt.Errorf("persist support ticket rollback: %w", err)
	}
	return nil
}
