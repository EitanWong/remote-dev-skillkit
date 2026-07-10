package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
)

func publishSupportSessionHandoff(
	gw *gateway.MemoryGateway,
	store gateway.StateStore,
	ticketID string,
	stdout io.Writer,
	warnings io.Writer,
	readyFile string,
	handoffTextFile string,
	journalPath string,
	started map[string]any,
) (returnErr error) {
	var artifacts []*stagedSupportSessionArtifact
	committed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			if !committed {
				_ = restoreSupportSessionArtifacts(artifacts)
				_ = rollbackSupportTicket(gw, store, ticketID, "support-session publication panicked")
			}
			panic(recovered)
		}
	}()
	readyData, err := json.MarshalIndent(started, "", "  ")
	if err != nil {
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session ready payload could not be encoded")
		return errors.Join(fmt.Errorf("encode support-session ready payload: %w", err), rollbackErr)
	}
	readyData = append(readyData, '\n')
	handoffText, err := supportSessionHandoffText(started)
	if err != nil {
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session handoff payload was invalid")
		return errors.Join(err, rollbackErr)
	}
	artifacts = []*stagedSupportSessionArtifact{
		{path: readyFile, label: "ready file", data: readyData},
		{path: handoffTextFile, label: "handoff file", data: []byte(handoffText + "\n")},
	}
	if err := stageSupportSessionArtifacts(artifacts); err != nil {
		cleanupErr := cleanupStagedSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session handoff staging failed")
		return errors.Join(err, cleanupErr, rollbackErr)
	}
	if err := prepareSupportSessionArtifactBackups(artifacts); err != nil {
		cleanupErr := cleanupStagedSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session artifact backup preparation failed")
		return errors.Join(err, cleanupErr, rollbackErr)
	}
	if err := writeSupportSessionPublicationJournal(journalPath, publicationJournalFromArtifacts(ticketID, "publishing", artifacts)); err != nil {
		cleanupErr := cleanupStagedSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session publication journal failed")
		return errors.Join(fmt.Errorf("write support-session publication journal: %w", err), cleanupErr, rollbackErr)
	}
	if err := commitSupportSessionArtifacts(artifacts); err != nil {
		restoreErr := restoreSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session handoff publication failed")
		journalErr := cleanupFailedSupportSessionPublicationJournal(journalPath, restoreErr, rollbackErr)
		return errors.Join(err, restoreErr, rollbackErr, journalErr)
	}
	if _, err := gw.PublishTicket(ticketID); err != nil {
		restoreErr := restoreSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session ticket publication failed")
		journalErr := cleanupFailedSupportSessionPublicationJournal(journalPath, restoreErr, rollbackErr)
		return errors.Join(fmt.Errorf("publish support-session ticket: %w", err), restoreErr, rollbackErr, journalErr)
	}
	if _, err := store.SaveFrom(gw); err != nil {
		restoreErr := restoreSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session active ticket persistence failed")
		journalErr := cleanupFailedSupportSessionPublicationJournal(journalPath, restoreErr, rollbackErr)
		return errors.Join(fmt.Errorf("persist published support-session ticket: %w", err), restoreErr, rollbackErr, journalErr)
	}
	if err := writeJSON(stdout, started); err != nil {
		restoreErr := restoreSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session stdout publication failed")
		journalErr := cleanupFailedSupportSessionPublicationJournal(journalPath, restoreErr, rollbackErr)
		return errors.Join(fmt.Errorf("write support-session stdout payload: %w", err), restoreErr, rollbackErr, journalErr)
	}
	if err := writeSupportSessionPublicationJournal(journalPath, publicationJournalFromArtifacts(ticketID, "committed", artifacts)); err != nil {
		restoreErr := restoreSupportSessionArtifacts(artifacts)
		rollbackErr := rollbackSupportTicket(gw, store, ticketID, "support-session publication commit journal failed")
		return errors.Join(fmt.Errorf("commit support-session publication journal: %w", err), restoreErr, rollbackErr)
	}
	committed = true
	if err := finalizeSupportSessionArtifacts(artifacts); err != nil {
		if warnings != nil {
			_, _ = fmt.Fprintf(warnings, "[rdev] warning: support-session handoff committed, but previous artifact backup cleanup needs attention: %v\n", err)
		}
		return nil
	}
	if err := removeSupportSessionPublicationJournal(journalPath); err != nil && warnings != nil {
		_, _ = fmt.Fprintf(warnings, "[rdev] warning: support-session handoff committed, but publication journal cleanup needs attention: %v\n", err)
	}
	return nil
}
