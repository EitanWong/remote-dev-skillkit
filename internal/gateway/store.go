package gateway

import (
	"fmt"
	"os"
	"strings"
)

const FileStateStoreProvider = "file"

type StateStore interface {
	LoadInto(*MemoryGateway) (Snapshot, bool, error)
	SaveFrom(*MemoryGateway) (Snapshot, error)
	Describe() string
}

type FileStateStore struct {
	Path string
}

func NewFileStateStore(path string) (FileStateStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return FileStateStore{}, fmt.Errorf("file state store path is required")
	}
	return FileStateStore{Path: path}, nil
}

func (s FileStateStore) LoadInto(gw *MemoryGateway) (Snapshot, bool, error) {
	if gw == nil {
		return Snapshot{}, false, fmt.Errorf("gateway is required")
	}
	if err := requireProtectedStateFile(s.Path); err != nil {
		return Snapshot{}, false, err
	}
	return gw.LoadSnapshotIfExists(s.Path)
}

func (s FileStateStore) SaveFrom(gw *MemoryGateway) (Snapshot, error) {
	if gw == nil {
		return Snapshot{}, fmt.Errorf("gateway is required")
	}
	return gw.SaveSnapshot(s.Path)
}

func (s FileStateStore) Describe() string {
	return FileStateStoreProvider + ":" + s.Path
}

func requireProtectedStateFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect gateway state file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("gateway state file must be a regular 0600 file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("gateway state file must use 0600 permissions")
	}
	return nil
}
