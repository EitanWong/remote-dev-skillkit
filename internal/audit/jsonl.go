package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

type JSONLStore struct {
	mu   sync.Mutex
	path string
}

func NewJSONLStore(path string) JSONLStore {
	return JSONLStore{path: path}
}

func (s *JSONLStore) Append(event model.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}
