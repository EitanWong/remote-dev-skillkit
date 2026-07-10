package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

type foregroundSupportSessionOptions struct {
	Out                 io.Writer
	StatusFile          string
	ReadyFile           string
	HandoffTextFile     string
	ConnectedReportFile string
	JournalPath         string
	Gateway             *gateway.MemoryGateway
	Store               gateway.StateStore
	TicketID            string
	TicketCode          string
	Locale              string
	GatewayURL          string
	Runtime             *tunnel.Runtime
	Published           tunnel.AvailabilitySet
	BeforeInvalidation  func()
	OnInvalidated       func(error)
}

func watchForegroundSupportSessionAvailability(ctx context.Context, opts foregroundSupportSessionOptions) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	seenPending := false
	writeSupportSessionEvent(opts.Out, opts.StatusFile, "waiting", foregroundSupportStatus(opts))
	var runtimeChanges <-chan struct{}
	if opts.Runtime != nil {
		runtimeChanges = opts.Runtime.Changes()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-runtimeChanges:
			status := foregroundSupportStatus(opts)
			if status["connected"] == true {
				writeConnectedSupportSession(opts, status)
				return
			}
			live := intersectAvailabilityWithRuntime(opts.Published, opts.Runtime.Snapshot())
			if sameAvailabilityCandidates(opts.Published, live) {
				continue
			}
			if opts.BeforeInvalidation != nil {
				opts.BeforeInvalidation()
			}
			connected, err := invalidatePublishedSupportSession(opts, live)
			if connected {
				writeConnectedSupportSession(opts, foregroundSupportStatus(opts))
				return
			}
			if err != nil {
				logTunnelAvailabilityLoss(opts.Out, live, "invalidation-failed")
			} else {
				logTunnelAvailabilityLoss(opts.Out, live, "tunnel-availability-lost")
			}
			if opts.OnInvalidated != nil {
				opts.OnInvalidated(errors.Join(errors.New("tunnel availability lost before target connection"), err))
			}
			return
		case <-ticker.C:
			status := foregroundSupportStatus(opts)
			if status["connected"] == true {
				writeConnectedSupportSession(opts, status)
				return
			}
			if status["status"] == "pending-activation" && !seenPending {
				seenPending = true
				writeSupportSessionEvent(opts.Out, opts.StatusFile, "pending-activation", status)
			}
		}
	}
}

func foregroundSupportStatus(opts foregroundSupportSessionOptions) map[string]any {
	return supportsession.BuildStatus(supportsession.StatusOptions{
		TicketCode: opts.TicketCode, Hosts: opts.Gateway.HostsForTicketCode(opts.TicketCode, ""),
		Locale: opts.Locale, GatewayURL: opts.GatewayURL, Preconnects: opts.Gateway.SupportSessionPreconnects(opts.TicketCode),
	})
}

func writeConnectedSupportSession(opts foregroundSupportSessionOptions, status map[string]any) {
	_ = writeSupportSessionConnectedReportFile0600(opts.ConnectedReportFile, status)
	writeSupportSessionEvent(opts.Out, opts.StatusFile, "connected", status)
	if strings.TrimSpace(opts.JournalPath) != "" {
		_ = removeSupportSessionPublicationJournal(opts.JournalPath)
	}
}

func invalidatePublishedSupportSession(opts foregroundSupportSessionOptions, live tunnel.AvailabilitySet) (bool, error) {
	journal := supportSessionPublicationJournal{
		SchemaVersion: supportSessionPublicationJournalSchema, TicketID: opts.TicketID, Phase: "invalidating",
		StatusPath: opts.StatusFile, Availability: live,
		Artifacts: []supportSessionPublicationJournalArtifact{{Path: opts.ReadyFile}, {Path: opts.HandoffTextFile}},
	}
	journalErr := writeSupportSessionPublicationJournal(opts.JournalPath, journal)
	connected, err := completeSupportSessionInvalidation(opts.Gateway, opts.Store, opts.TicketID, opts.ReadyFile, opts.HandoffTextFile, opts.StatusFile, live)
	if err != nil {
		return false, errors.Join(journalErr, err)
	}
	cleanupErr := removeSupportSessionPublicationJournal(opts.JournalPath)
	return connected, errors.Join(journalErr, cleanupErr)
}

func completeSupportSessionInvalidation(gw *gateway.MemoryGateway, store gateway.StateStore, ticketID, readyFile, handoffFile, statusFile string, live tunnel.AvailabilitySet) (bool, error) {
	_, _, rolledBack, rollbackErr := gw.RollbackTicketIfNoConnectedHost(ticketID, "tunnel availability changed before target connection")
	if rollbackErr != nil && !errors.Is(rollbackErr, gateway.ErrInvalidState) && !errors.Is(rollbackErr, gateway.ErrNotFound) {
		return false, rollbackErr
	}
	if rollbackErr == nil && !rolledBack {
		return true, nil
	}
	if _, err := store.SaveFrom(gw); err != nil {
		return false, fmt.Errorf("persist tunnel availability rollback: %w", err)
	}
	diagnostic := map[string]any{
		"schema_version": "rdev.support-session-start-diagnostic.v1", "ready_to_send": false,
		"reason": "tunnel_availability_lost", "availability": live,
	}
	if err := writeJSONFile0600(readyFile, diagnostic); err != nil {
		return false, err
	}
	if err := writeTextFile0600(handoffFile, "This support-session handoff is no longer valid. Generate a new handoff before contacting the target."); err != nil {
		return false, err
	}
	return false, writeJSONFile0600(statusFile, diagnostic)
}

func logTunnelAvailabilityLoss(out io.Writer, live tunnel.AvailabilitySet, errorClass string) {
	providerIDs := make([]string, 0, len(live.Attempts))
	candidateIDs := make([]string, 0, len(live.Attempts))
	for _, attempt := range live.Attempts {
		if attempt.ProviderID != "" {
			providerIDs = append(providerIDs, attempt.ProviderID)
		}
		if attempt.CandidateID != "" {
			candidateIDs = append(candidateIDs, attempt.CandidateID)
		}
	}
	sort.Strings(providerIDs)
	sort.Strings(candidateIDs)
	_, _ = fmt.Fprintf(out, "[rdev] tunnel availability changed providers=%s candidates=%s error_class=%s\n", strings.Join(providerIDs, ","), strings.Join(candidateIDs, ","), errorClass)
}
