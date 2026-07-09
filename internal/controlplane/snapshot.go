package controlplane

import (
	"fmt"
	"sort"
	"time"
)

const SnapshotSchemaVersion = "rdev.control-plane-snapshot.v1"

type Snapshot struct {
	SchemaVersion     string                 `json:"schema_version"`
	Sessions          []Session              `json:"sessions"`
	Events            map[string][]Event     `json:"events"`
	Idempotency       []IdempotencyRecord    `json:"idempotency,omitempty"`
	TaskIdempotency   map[string]TaskRecord  `json:"task_idempotency,omitempty"`
	CancelIdempotency map[string]TaskRecord  `json:"cancel_idempotency,omitempty"`
	ResultIdempotency map[string]TaskRecord  `json:"result_idempotency,omitempty"`
	Leases            map[string]LeaseRecord `json:"leases,omitempty"`
	TerminalAt        map[string]time.Time   `json:"terminal_at,omitempty"`
}

type IdempotencyRecord struct {
	SessionID      string `json:"session_id"`
	FromEndpointID string `json:"from_endpoint_id"`
	Key            string `json:"key"`
	Fingerprint    string `json:"fingerprint"`
	Event          Event  `json:"event"`
}

type TaskRecord struct {
	TaskID  string `json:"task_id"`
	EventID string `json:"event_id"`
}

type LeaseRecord struct {
	Current         Lease                `json:"current"`
	PreviousSecrets map[string]time.Time `json:"previous_secrets,omitempty"`
}

func (s *MemoryStore) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions := make([]Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session.clone())
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})

	events := make(map[string][]Event, len(s.events))
	for sessionID, values := range s.events {
		events[sessionID] = cloneEvents(values)
	}

	idempotency := make([]IdempotencyRecord, 0, len(s.idempotency))
	for key, record := range s.idempotency {
		idempotency = append(idempotency, IdempotencyRecord{
			SessionID:      key.SessionID,
			FromEndpointID: key.FromEndpointID,
			Key:            key.Key,
			Fingerprint:    record.Fingerprint,
			Event:          cloneEvent(record.Event),
		})
	}
	sort.Slice(idempotency, func(i, j int) bool {
		if idempotency[i].SessionID != idempotency[j].SessionID {
			return idempotency[i].SessionID < idempotency[j].SessionID
		}
		if idempotency[i].FromEndpointID != idempotency[j].FromEndpointID {
			return idempotency[i].FromEndpointID < idempotency[j].FromEndpointID
		}
		return idempotency[i].Key < idempotency[j].Key
	})

	return Snapshot{
		SchemaVersion:     SnapshotSchemaVersion,
		Sessions:          sessions,
		Events:            events,
		Idempotency:       idempotency,
		TaskIdempotency:   cloneTaskRecordMap(s.taskIdempotency),
		CancelIdempotency: cloneTaskRecordMap(s.cancelIdempotency),
		ResultIdempotency: cloneTaskRecordMap(s.resultIdempotency),
		Leases:            cloneLeaseRecordMap(s.leases),
		TerminalAt:        cloneTimeMap(s.terminalAt),
	}
}

func (s *MemoryStore) RestoreSnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion == "" {
		return nil
	}
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return fmt.Errorf("unsupported control plane snapshot schema %q", snapshot.SchemaVersion)
	}

	sessions := make(map[string]Session, len(snapshot.Sessions))
	joinCodes := make(map[string]string, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if session.ID == "" || session.JoinCode == "" {
			return fmt.Errorf("control plane snapshot contains session with missing id or join code")
		}
		if _, exists := sessions[session.ID]; exists {
			return fmt.Errorf("control plane snapshot contains duplicate session id %q", session.ID)
		}
		if _, exists := joinCodes[session.JoinCode]; exists {
			return fmt.Errorf("control plane snapshot contains duplicate join code %q", session.JoinCode)
		}
		sessions[session.ID] = session.clone()
		joinCodes[session.JoinCode] = session.ID
	}

	events := make(map[string][]Event, len(snapshot.Events))
	for sessionID, values := range snapshot.Events {
		if _, exists := sessions[sessionID]; !exists {
			return fmt.Errorf("control plane snapshot events reference missing session %q", sessionID)
		}
		events[sessionID] = cloneEvents(values)
	}
	for sessionID := range sessions {
		if _, exists := events[sessionID]; !exists {
			events[sessionID] = []Event{}
		}
	}

	idempotency := make(map[idempotencyKey]idempotencyRecord, len(snapshot.Idempotency))
	for _, record := range snapshot.Idempotency {
		if record.SessionID == "" || record.Key == "" {
			return fmt.Errorf("control plane snapshot contains idempotency record with missing scope")
		}
		if _, exists := sessions[record.SessionID]; !exists {
			return fmt.Errorf("control plane snapshot idempotency references missing session %q", record.SessionID)
		}
		key := idempotencyKey{SessionID: record.SessionID, FromEndpointID: record.FromEndpointID, Key: record.Key}
		if _, exists := idempotency[key]; exists {
			return fmt.Errorf("control plane snapshot contains duplicate idempotency key %q", record.Key)
		}
		idempotency[key] = idempotencyRecord{Fingerprint: record.Fingerprint, Event: cloneEvent(record.Event)}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = sessions
	s.joinCodes = joinCodes
	s.events = events
	s.idempotency = idempotency
	s.taskIdempotency = taskRecordsFromSnapshot(snapshot.TaskIdempotency)
	s.cancelIdempotency = taskRecordsFromSnapshot(snapshot.CancelIdempotency)
	s.resultIdempotency = taskRecordsFromSnapshot(snapshot.ResultIdempotency)
	s.leases = leaseRecordsFromSnapshot(snapshot.Leases)
	s.terminalAt = cloneTimeMap(snapshot.TerminalAt)
	return nil
}

func cloneEvents(events []Event) []Event {
	out := append([]Event(nil), events...)
	for i := range out {
		out[i] = cloneEvent(out[i])
	}
	return out
}

func cloneTaskRecordMap(records map[string]taskRecord) map[string]TaskRecord {
	if len(records) == 0 {
		return nil
	}
	out := make(map[string]TaskRecord, len(records))
	for key, record := range records {
		out[key] = TaskRecord(record)
	}
	return out
}

func taskRecordsFromSnapshot(records map[string]TaskRecord) map[string]taskRecord {
	out := make(map[string]taskRecord, len(records))
	for key, record := range records {
		out[key] = taskRecord(record)
	}
	return out
}

func cloneLeaseRecordMap(records map[string]leaseRecord) map[string]LeaseRecord {
	if len(records) == 0 {
		return nil
	}
	out := make(map[string]LeaseRecord, len(records))
	for key, record := range records {
		out[key] = LeaseRecord{
			Current:         record.Current,
			PreviousSecrets: cloneTimeMap(record.PreviousSecrets),
		}
	}
	return out
}

func leaseRecordsFromSnapshot(records map[string]LeaseRecord) map[string]leaseRecord {
	out := make(map[string]leaseRecord, len(records))
	for key, record := range records {
		out[key] = leaseRecord{
			Current:         record.Current,
			PreviousSecrets: cloneTimeMap(record.PreviousSecrets),
		}
	}
	return out
}

func cloneTimeMap(values map[string]time.Time) map[string]time.Time {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]time.Time, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
