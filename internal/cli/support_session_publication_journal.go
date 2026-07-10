package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const supportSessionPublicationJournalSchema = "rdev.support-session-publication-journal.v1"

type supportSessionPublicationJournal struct {
	SchemaVersion string                                     `json:"schema_version"`
	TicketID      string                                     `json:"ticket_id"`
	Phase         string                                     `json:"phase"`
	Artifacts     []supportSessionPublicationJournalArtifact `json:"artifacts"`
	StatusPath    string                                     `json:"status_path,omitempty"`
	Reason        string                                     `json:"reason,omitempty"`
	Availability  tunnel.AvailabilitySet                     `json:"availability,omitempty"`
}

type supportSessionPublicationJournalArtifact struct {
	Path        string `json:"path"`
	TempPath    string `json:"temp_path,omitempty"`
	BackupPath  string `json:"backup_path,omitempty"`
	HadOriginal bool   `json:"had_original"`
}

func publicationJournalFromArtifacts(ticketID, phase string, artifacts []*stagedSupportSessionArtifact) supportSessionPublicationJournal {
	journal := supportSessionPublicationJournal{SchemaVersion: supportSessionPublicationJournalSchema, TicketID: ticketID, Phase: phase, Artifacts: make([]supportSessionPublicationJournalArtifact, 0, len(artifacts))}
	for _, artifact := range artifacts {
		journal.Artifacts = append(journal.Artifacts, supportSessionPublicationJournalArtifact{Path: artifact.path, TempPath: artifact.tempPath, BackupPath: artifact.backupPath, HadOriginal: artifact.hadOriginal})
	}
	return journal
}

func writeSupportSessionPublicationJournal(path string, journal supportSessionPublicationJournal) error {
	return writeJSONFile0600(path, journal)
}

func removeSupportSessionPublicationJournal(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncSupportSessionArtifactDirectory(path)
}

func cleanupFailedSupportSessionPublicationJournal(path string, restoreErr, rollbackErr error) error {
	if restoreErr != nil || rollbackErr != nil {
		return nil
	}
	return removeSupportSessionPublicationJournal(path)
}

func recoverSupportSessionPublication(gw *gateway.MemoryGateway, store gateway.StateStore, journalPath string, expectedPaths []string, expectedStatusPath ...string) error {
	var journal supportSessionPublicationJournal
	if err := tunnel.ReadProtectedJSONFile(journalPath, &journal); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if journal.SchemaVersion != supportSessionPublicationJournalSchema || strings.TrimSpace(journal.TicketID) == "" {
		return fmt.Errorf("invalid support-session publication journal")
	}
	if err := validateSupportSessionPublicationJournalArtifacts(journal.Phase, journal.Artifacts, expectedPaths, journalPath); err != nil {
		return err
	}
	artifacts := make([]*stagedSupportSessionArtifact, 0, len(journal.Artifacts))
	for _, record := range journal.Artifacts {
		if !filepath.IsAbs(record.Path) || (record.TempPath != "" && !filepath.IsAbs(record.TempPath)) || (record.BackupPath != "" && !filepath.IsAbs(record.BackupPath)) {
			return fmt.Errorf("publication journal artifact paths must be absolute")
		}
		artifacts = append(artifacts, &stagedSupportSessionArtifact{path: record.Path, tempPath: record.TempPath, backupPath: record.BackupPath, hadOriginal: record.HadOriginal})
	}
	if journal.Phase == "invalidating" {
		if len(expectedStatusPath) != 1 || publicationPathKey(journal.StatusPath) != publicationPathKey(expectedStatusPath[0]) {
			return fmt.Errorf("invalidation journal status path does not match expected output")
		}
		_, err := completeSupportSessionInvalidation(gw, store, journal.TicketID, expectedPaths[0], expectedPaths[1], journal.StatusPath, journal.Availability, journal.Reason)
		if err != nil {
			return err
		}
		return errors.Join(finalizeSupportSessionArtifacts(artifacts), cleanupStagedSupportSessionArtifacts(artifacts), removeSupportSessionPublicationJournal(journalPath))
	}
	if journal.Phase == "monitoring" {
		if len(expectedStatusPath) != 1 || publicationPathKey(journal.StatusPath) != publicationPathKey(expectedStatusPath[0]) {
			return fmt.Errorf("monitoring journal status path does not match expected output")
		}
		if gw.TicketHasConnectedHost(journal.TicketID) {
			return errors.Join(finalizeSupportSessionArtifacts(artifacts), cleanupStagedSupportSessionArtifacts(artifacts), removeSupportSessionPublicationJournal(journalPath))
		}
		lost := journal.Availability
		lost.Candidates = nil
		for index := range lost.Attempts {
			lost.Attempts[index].Status = tunnel.AttemptExited
			lost.Attempts[index].ErrorClass = "runtime-not-recovered"
		}
		_, err := completeSupportSessionInvalidation(gw, store, journal.TicketID, expectedPaths[0], expectedPaths[1], journal.StatusPath, lost, "runtime_not_recovered")
		if err != nil {
			return err
		}
		return errors.Join(finalizeSupportSessionArtifacts(artifacts), cleanupStagedSupportSessionArtifacts(artifacts), removeSupportSessionPublicationJournal(journalPath))
	}
	if journal.Phase == "committed" {
		if ticket, ok := gw.Ticket(journal.TicketID); ok && ticket.Status == model.TicketStatusActive {
			if err := errors.Join(finalizeSupportSessionArtifacts(artifacts), cleanupStagedSupportSessionArtifacts(artifacts)); err != nil {
				return err
			}
			return removeSupportSessionPublicationJournal(journalPath)
		}
		for _, artifact := range artifacts {
			artifact.committed = true
		}
		if err := restoreSupportSessionArtifacts(artifacts); err != nil {
			return err
		}
		if _, _, err := gw.RollbackTicket(journal.TicketID, "reconciled committed journal with non-active ticket"); err != nil && !errors.Is(err, gateway.ErrNotFound) {
			return err
		}
		if _, err := store.SaveFrom(gw); err != nil {
			return err
		}
		return removeSupportSessionPublicationJournal(journalPath)
	}
	if journal.Phase != "publishing" {
		return fmt.Errorf("unsupported publication journal phase %q", journal.Phase)
	}
	if err := recoverUncommittedSupportSessionArtifacts(artifacts); err != nil {
		return err
	}
	if _, _, err := gw.RollbackTicket(journal.TicketID, "recovered interrupted support-session publication"); err != nil && !errors.Is(err, gateway.ErrNotFound) {
		return err
	}
	if _, err := store.SaveFrom(gw); err != nil {
		return fmt.Errorf("persist recovered support-session rollback: %w", err)
	}
	return removeSupportSessionPublicationJournal(journalPath)
}

func validateSupportSessionPublicationJournalArtifacts(phase string, records []supportSessionPublicationJournalArtifact, expectedPaths []string, reservedPaths ...string) error {
	if len(records) != len(expectedPaths) {
		return fmt.Errorf("publication journal artifact set does not match expected outputs")
	}
	expected := make(map[string]bool, len(expectedPaths))
	for _, path := range expectedPaths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		expected[publicationPathKey(abs)] = false
	}
	seenAuxiliary := map[string]bool{}
	for _, reserved := range reservedPaths {
		seenAuxiliary[publicationPathKey(reserved)] = true
	}
	for _, record := range records {
		if phase == "publishing" && strings.TrimSpace(record.TempPath) == "" {
			return fmt.Errorf("publishing journal artifact is missing its staged path")
		}
		if record.HadOriginal && strings.TrimSpace(record.BackupPath) == "" {
			return fmt.Errorf("publication journal artifact is missing its backup path")
		}
		path := filepath.Clean(record.Path)
		pathKey := publicationPathKey(path)
		used, ok := expected[pathKey]
		if !ok || used {
			return fmt.Errorf("publication journal contains an unexpected or duplicate artifact path")
		}
		expected[pathKey] = true
		for _, auxiliary := range []struct{ path, prefix string }{{record.TempPath, "." + filepath.Base(path) + ".stage-"}, {record.BackupPath, "." + filepath.Base(path) + ".backup-"}} {
			if auxiliary.path == "" {
				continue
			}
			clean := filepath.Clean(auxiliary.path)
			cleanKey := publicationPathKey(clean)
			if publicationPathKey(filepath.Dir(clean)) != publicationPathKey(filepath.Dir(path)) || !strings.HasPrefix(filepath.Base(clean), auxiliary.prefix) || cleanKey == pathKey || seenAuxiliary[cleanKey] {
				return fmt.Errorf("publication journal contains an unsafe artifact staging path")
			}
			seenAuxiliary[cleanKey] = true
		}
	}
	return nil
}

func publicationPathKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func recoverUncommittedSupportSessionArtifacts(artifacts []*stagedSupportSessionArtifact) error {
	var recoveryErrors []error
	for index := len(artifacts) - 1; index >= 0; index-- {
		artifact := artifacts[index]
		backupExists := false
		if artifact.backupPath != "" {
			if _, err := os.Lstat(artifact.backupPath); err == nil {
				backupExists = true
			} else if !os.IsNotExist(err) {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
		if backupExists {
			if err := os.Remove(artifact.path); err != nil && !os.IsNotExist(err) {
				recoveryErrors = append(recoveryErrors, err)
			} else if err := os.Rename(artifact.backupPath, artifact.path); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		} else if !artifact.hadOriginal && artifact.tempPath != "" {
			if _, err := os.Lstat(artifact.tempPath); os.IsNotExist(err) {
				if removeErr := os.Remove(artifact.path); removeErr != nil && !os.IsNotExist(removeErr) {
					recoveryErrors = append(recoveryErrors, removeErr)
				}
			}
		}
		if artifact.tempPath != "" {
			if err := os.Remove(artifact.tempPath); err != nil && !os.IsNotExist(err) {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}
	return errors.Join(recoveryErrors...)
}
