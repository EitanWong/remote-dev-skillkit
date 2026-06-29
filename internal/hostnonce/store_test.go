package hostnonce

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStoreRejectsReplay(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	entry := Entry{
		JobID:     "job_1",
		HostID:    "hst_1",
		Nonce:     "nonce",
		ExpiresAt: now.Add(time.Minute),
	}
	if err := store.Remember(entry, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(entry, now); err == nil {
		t.Fatal("expected replay rejection")
	}
}

func TestMemoryStorePrunesExpiredEntries(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	entry := Entry{
		JobID:     "job_1",
		HostID:    "hst_1",
		Nonce:     "nonce",
		ExpiresAt: now.Add(time.Minute),
	}
	if err := store.Remember(entry, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(entry, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("expected expired nonce to be pruned, got %v", err)
	}
}

func TestFileStorePersistsAndRejectsReplay(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "nonce", "store.json")
	store := FileStore{Path: path}
	entry := Entry{
		JobID:     "job_1",
		HostID:    "hst_1",
		Nonce:     "nonce",
		ExpiresAt: now.Add(time.Minute),
	}
	if err := store.Remember(entry, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(entry, now); err == nil {
		t.Fatal("expected replay rejection")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %#o", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected 0700 directory permissions, got %#o", got)
	}
}
