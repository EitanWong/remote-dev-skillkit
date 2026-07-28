package gateway

import (
	"fmt"
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
