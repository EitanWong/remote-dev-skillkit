package hostnonce

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const FileSchemaVersion = "rdev.host-nonce-store.v1"

type Entry struct {
	JobID     string    `json:"job_id"`
	HostID    string    `json:"host_id"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
	SeenAt    time.Time `json:"seen_at"`
}

type Store interface {
	Remember(entry Entry, now time.Time) error
}

type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]Entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: map[string]Entry{}}
}

func (s *MemoryStore) Remember(entry Entry, now time.Time) error {
	if s == nil {
		return nil
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string]Entry{}
	}
	pruneExpired(s.entries, now)
	key := entryKey(entry)
	if _, ok := s.entries[key]; ok {
		return fmt.Errorf("job envelope nonce replay detected")
	}
	entry.SeenAt = now.UTC()
	s.entries[key] = entry
	return nil
}

type FileStore struct {
	Path string
}

func (s FileStore) Remember(entry Entry, now time.Time) error {
	if s.Path == "" {
		return nil
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	entries, err := s.load()
	if err != nil {
		return err
	}
	pruneExpired(entries, now)
	key := entryKey(entry)
	if _, ok := entries[key]; ok {
		return fmt.Errorf("job envelope nonce replay detected")
	}
	entry.SeenAt = now.UTC()
	entries[key] = entry
	return s.save(entries)
}

type fileStore struct {
	SchemaVersion string  `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

func (s FileStore) load() (map[string]Entry, error) {
	content, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var encoded fileStore
	if err := json.Unmarshal(content, &encoded); err != nil {
		return nil, err
	}
	if encoded.SchemaVersion != FileSchemaVersion {
		return nil, fmt.Errorf("unsupported host nonce store schema %q", encoded.SchemaVersion)
	}
	entries := make(map[string]Entry, len(encoded.Entries))
	for _, entry := range encoded.Entries {
		if err := validateEntry(entry); err != nil {
			return nil, err
		}
		entries[entryKey(entry)] = entry
	}
	return entries, nil
}

func (s FileStore) save(entries map[string]Entry) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	list := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, entry)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].ExpiresAt.Equal(list[j].ExpiresAt) {
			return list[i].Nonce < list[j].Nonce
		}
		return list[i].ExpiresAt.Before(list[j].ExpiresAt)
	})
	content, err := json.MarshalIndent(fileStore{
		SchemaVersion: FileSchemaVersion,
		Entries:       list,
	}, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.WriteFile(s.Path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(s.Path, 0o600)
}

func validateEntry(entry Entry) error {
	if entry.JobID == "" || entry.HostID == "" || entry.Nonce == "" {
		return fmt.Errorf("host nonce entry requires job id, host id, and nonce")
	}
	if entry.ExpiresAt.IsZero() {
		return fmt.Errorf("host nonce entry expires_at is required")
	}
	return nil
}

func pruneExpired(entries map[string]Entry, now time.Time) {
	for key, entry := range entries {
		if !now.UTC().Before(entry.ExpiresAt.UTC()) {
			delete(entries, key)
		}
	}
}

func entryKey(entry Entry) string {
	return entry.HostID + ":" + entry.JobID + ":" + entry.Nonce
}
