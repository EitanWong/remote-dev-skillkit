package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/gateway"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/supportsession"
	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestPublishSupportSessionHandoffRollsBackWhenActiveStateSaveFails(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{failSaves: 1}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "save failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	err = publishSupportSessionHandoff(gw, store, ticket.ID, io.Discard, io.Discard, filepath.Join(root, "ready.json"), filepath.Join(root, "handoff.txt"), filepath.Join(root, "journal.json"), validStartedPayloadForPublicationTest())
	if err == nil || !strings.Contains(err.Error(), "persist published support-session ticket") {
		t.Fatalf("expected state save failure, got %v", err)
	}
	snapshot := gw.Snapshot()
	if len(snapshot.Tickets) != 1 || snapshot.Tickets[0].Status != model.TicketStatusRevoked {
		t.Fatalf("failed save left active ticket: %#v", snapshot.Tickets)
	}
	if !recordedSnapshotHasRevokedTicket(store.snapshots) {
		t.Fatalf("rollback was not persisted: %#v", store.snapshots)
	}
}

func TestPublishSupportSessionHandoffRollbackSaveFailureLeavesNoDurableActiveTicket(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{failSaves: 2}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "double save failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	err = publishSupportSessionHandoff(gw, store, ticket.ID, io.Discard, io.Discard, filepath.Join(root, "ready.json"), filepath.Join(root, "handoff.txt"), filepath.Join(root, "journal.json"), validStartedPayloadForPublicationTest())
	if err == nil || !strings.Contains(err.Error(), "persist support ticket rollback") {
		t.Fatalf("expected active and rollback save failures, got %v", err)
	}
	rolledBack, ok := gw.TicketForCode(ticket.Code)
	if !ok || rolledBack.Status != model.TicketStatusRevoked {
		t.Fatalf("memory ticket after failed rollback save = %#v, found=%v", rolledBack, ok)
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("failed saves durably recorded active state: %#v", store.snapshots)
	}
}

func TestPublishSupportSessionHandoffAmbiguousActiveSaveRetainsJournalForRestartRecovery(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &ambiguousRollbackStore{}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "ambiguous save", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	journalPath := filepath.Join(root, "journal.json")
	err = publishSupportSessionHandoff(gw, store, ticket.ID, io.Discard, io.Discard, readyFile, handoffFile, journalPath, validStartedPayloadForPublicationTest())
	if err == nil || !strings.Contains(err.Error(), "rollback save unavailable") {
		t.Fatalf("expected ambiguous active and rollback save failures, got %v", err)
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("uncertain durable state lost recovery journal: %v", err)
	}
	if len(store.durable) != 1 || store.durable[0].Tickets[0].Status != model.TicketStatusActive {
		t.Fatalf("test did not simulate ambiguous durable active state: %#v", store.durable)
	}
	if _, _, err := store.LoadInto(gw); err != nil {
		t.Fatal(err)
	}
	if err := recoverSupportSessionPublication(gw, store, journalPath, []string{readyFile, handoffFile}); err != nil {
		t.Fatal(err)
	}
	last := store.durable[len(store.durable)-1]
	if last.Tickets[0].Status != model.TicketStatusRevoked {
		t.Fatalf("restart recovery did not durably revoke ambiguous active ticket: %#v", last.Tickets)
	}
}

func TestCreateFinalProbedSupportTicketRejectsHostRegistrationDuringSuccessfulProbe(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	var registrationErr error
	ticket, _, err := createFinalProbedSupportTicket(
		context.Background(), gw, store, supportSessionAvailabilityForTests("registering-failure"), 60, "registration race",
		func(candidates []supportsession.GatewayURLCandidate) map[string]string {
			return addGatewayCandidateTicketMetadata(map[string]string{"auto_activate": "attended-temporary"}, candidates)
		},
		func(_ context.Context, _ tunnel.Candidate, ticketCode, _ string) error {
			_, _, registrationErr = gw.RegisterHostWithIdempotencyKey("probe-registration", "request-hash", model.HostRegistration{
				TicketCode: ticketCode,
				Name:       "probe-race-host",
				OS:         "windows",
				Arch:       "amd64",
			})
			return nil
		},
		"gateway-instance",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(registrationErr, gateway.ErrInvalidState) {
		t.Fatalf("registration during probing error = %v, want invalid state", registrationErr)
	}
	if hosts := gw.Hosts(""); len(hosts) != 0 {
		t.Fatalf("successful final probe created hosts: %#v", hosts)
	}
	if _, _, err := gw.RollbackTicket(ticket.ID, "test cleanup"); err != nil {
		t.Fatal(err)
	}
}

func TestPublishSupportSessionHandoffRollsBackOnEveryOutputFailure(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		configure      func(string) (io.Writer, string, string)
		expectedError  string
		readyRemoved   bool
		handoffRemoved bool
	}{
		{
			name: "ready file",
			configure: func(root string) (io.Writer, string, string) {
				readyDir := filepath.Join(root, "ready-dir")
				if err := os.MkdirAll(readyDir, 0o700); err != nil {
					t.Fatal(err)
				}
				return io.Discard, readyDir, filepath.Join(root, "handoff.txt")
			},
			expectedError: "ready file",
		},
		{
			name: "handoff file",
			configure: func(root string) (io.Writer, string, string) {
				handoffDir := filepath.Join(root, "handoff-dir")
				if err := os.MkdirAll(handoffDir, 0o700); err != nil {
					t.Fatal(err)
				}
				return io.Discard, filepath.Join(root, "ready.json"), handoffDir
			},
			expectedError: "handoff file",
			readyRemoved:  true,
		},
		{
			name: "stdout",
			configure: func(root string) (io.Writer, string, string) {
				return failingWriter{err: errors.New("stdout closed")}, filepath.Join(root, "ready.json"), filepath.Join(root, "handoff.txt")
			},
			expectedError:  "stdout payload",
			readyRemoved:   true,
			handoffRemoved: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			gw := gateway.NewMemoryGateway()
			store := &recordingStateStore{}
			ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "publication failure", nil)
			if err != nil {
				t.Fatal(err)
			}
			stdout, readyFile, handoffFile := testCase.configure(t.TempDir())
			err = publishSupportSessionHandoff(gw, store, ticket.ID, stdout, io.Discard, readyFile, handoffFile, filepath.Join(filepath.Dir(readyFile), "journal.json"), validStartedPayloadForPublicationTest())
			if err == nil || !strings.Contains(err.Error(), testCase.expectedError) {
				t.Fatalf("publication error = %v", err)
			}
			rolledBack, ok := gw.TicketForCode(ticket.Code)
			if !ok || rolledBack.Status != model.TicketStatusRevoked {
				t.Fatalf("ticket after publication failure = %#v, found=%v", rolledBack, ok)
			}
			if !recordedSnapshotHasRevokedTicket(store.snapshots) {
				t.Fatalf("rollback was not persisted: %#v", store.snapshots)
			}
			if testCase.readyRemoved {
				if _, statErr := os.Stat(readyFile); !os.IsNotExist(statErr) {
					t.Fatalf("unpublished ready file remains: %v", statErr)
				}
			}
			if testCase.handoffRemoved {
				if _, statErr := os.Stat(handoffFile); !os.IsNotExist(statErr) {
					t.Fatalf("unpublished handoff file remains: %v", statErr)
				}
			}
		})
	}
}

func TestPublishSupportSessionHandoffRestoresPreviousFilesAfterStdoutFailure(t *testing.T) {
	root := t.TempDir()
	readyFile := filepath.Join(root, "ready.json")
	handoffFile := filepath.Join(root, "handoff.txt")
	if err := os.WriteFile(readyFile, []byte("previous-ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handoffFile, []byte("previous-handoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "stdout failure", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = publishSupportSessionHandoff(gw, store, ticket.ID, failingWriter{err: errors.New("stdout closed")}, io.Discard, readyFile, handoffFile, filepath.Join(root, "journal.json"), map[string]any{
		"schema_version":          "rdev.support-session-started.v1",
		"target_handoff_envelope": map[string]any{"full_text": "replacement handoff"},
	})
	if err == nil || !strings.Contains(err.Error(), "stdout payload") {
		t.Fatalf("publication error = %v", err)
	}
	readyContent, readyErr := os.ReadFile(readyFile)
	handoffContent, handoffErr := os.ReadFile(handoffFile)
	if readyErr != nil || string(readyContent) != "previous-ready\n" {
		t.Fatalf("previous ready file was not restored: content=%q err=%v", readyContent, readyErr)
	}
	if handoffErr != nil || string(handoffContent) != "previous-handoff\n" {
		t.Fatalf("previous handoff file was not restored: content=%q err=%v", handoffContent, handoffErr)
	}
}

func TestPublishSupportSessionHandoffRollsBackInvalidPayloadBeforeStaging(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		started map[string]any
	}{
		{name: "empty handoff", started: map[string]any{"schema_version": "rdev.support-session-started.v1"}},
		{name: "invalid json", started: map[string]any{"invalid": func() {}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			gw := gateway.NewMemoryGateway()
			store := &recordingStateStore{}
			ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "invalid publication", nil)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			err = publishSupportSessionHandoff(
				gw, store, ticket.ID, io.Discard, io.Discard,
				filepath.Join(root, "ready.json"), filepath.Join(root, "handoff.txt"), filepath.Join(root, "journal.json"), testCase.started,
			)
			if err == nil {
				t.Fatal("expected invalid publication error")
			}
			rolledBack, ok := gw.TicketForCode(ticket.Code)
			if !ok || rolledBack.Status != model.TicketStatusRevoked {
				t.Fatalf("invalid payload left ticket active: %#v, found=%v", rolledBack, ok)
			}
			if !recordedSnapshotHasRevokedTicket(store.snapshots) {
				t.Fatalf("invalid payload rollback was not persisted: %#v", store.snapshots)
			}
		})
	}
}

func TestPublishSupportSessionHandoffPanicRollsBackAndLeavesRecoveryJournal(t *testing.T) {
	gw := gateway.NewMemoryGateway()
	store := &recordingStateStore{}
	ticket, err := gw.CreateProbingTicketWithMetadata(model.HostModeAttendedTemporary, 60, nil, "panic publication", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	journalPath := filepath.Join(root, "journal.json")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected stdout panic")
			}
		}()
		_ = publishSupportSessionHandoff(gw, store, ticket.ID, panicWriter{}, io.Discard, filepath.Join(root, "ready.json"), filepath.Join(root, "handoff.txt"), journalPath, validStartedPayloadForPublicationTest())
	}()
	rolledBack, ok := gw.TicketForCode(ticket.Code)
	if !ok || rolledBack.Status != model.TicketStatusRevoked {
		t.Fatalf("panic left ticket active: %#v, found=%v", rolledBack, ok)
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("panic did not retain recovery journal: %v", err)
	}
}
