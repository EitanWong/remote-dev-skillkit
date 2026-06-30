package hosttrust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/protectedstore"
)

const FileSchemaVersion = "rdev.host-trust-store.v1"

type FileStore struct {
	Path string
}

type Store interface {
	Load() (model.SignedTrustBundle, bool, error)
	Save(model.SignedTrustBundle) error
	VerifyAndSaveUpdate(model.SignedTrustBundle, model.TrustBundle, time.Time) error
}

type NoopStore struct{}

type ProtectedStore struct {
	Ref string
}

type fileState struct {
	SchemaVersion string                  `json:"schema_version"`
	TrustBundle   model.SignedTrustBundle `json:"trust_bundle"`
}

func OpenStore(ref string) (Store, error) {
	if ref == "" {
		return NoopStore{}, nil
	}
	if protectedstore.IsRef(ref) {
		if _, err := protectedstore.Open(ref); err != nil {
			return nil, err
		}
		return ProtectedStore{Ref: ref}, nil
	}
	return FileStore{Path: ref}, nil
}

func (NoopStore) Load() (model.SignedTrustBundle, bool, error) {
	return model.SignedTrustBundle{}, false, nil
}

func (NoopStore) Save(model.SignedTrustBundle) error {
	return nil
}

func (s NoopStore) VerifyAndSaveUpdate(next model.SignedTrustBundle, root model.TrustBundle, now time.Time) error {
	return verifyAndSaveUpdate(s, next, root, now)
}

func (s FileStore) Load() (model.SignedTrustBundle, bool, error) {
	if s.Path == "" {
		return model.SignedTrustBundle{}, false, nil
	}
	content, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return model.SignedTrustBundle{}, false, nil
	}
	if err != nil {
		return model.SignedTrustBundle{}, false, err
	}
	var state fileState
	if err := json.Unmarshal(content, &state); err != nil {
		return model.SignedTrustBundle{}, false, err
	}
	if state.SchemaVersion != FileSchemaVersion {
		return model.SignedTrustBundle{}, false, fmt.Errorf("unsupported host trust store schema %q", state.SchemaVersion)
	}
	return state.TrustBundle, true, nil
}

func (s FileStore) Save(bundle model.SignedTrustBundle) error {
	if s.Path == "" {
		return nil
	}
	state := fileState{
		SchemaVersion: FileSchemaVersion,
		TrustBundle:   bundle,
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.Path), ".trust-bundle-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.Path); err != nil {
		return err
	}
	cleanup = false
	return os.Chmod(s.Path, 0o600)
}

func (s FileStore) VerifyAndSaveUpdate(next model.SignedTrustBundle, root model.TrustBundle, now time.Time) error {
	return verifyAndSaveUpdate(s, next, root, now)
}

func (s ProtectedStore) Load() (model.SignedTrustBundle, bool, error) {
	store, err := protectedstore.Open(s.Ref)
	if err != nil {
		return model.SignedTrustBundle{}, false, err
	}
	content, ok, err := store.Load()
	if err != nil || !ok {
		return model.SignedTrustBundle{}, ok, err
	}
	var state fileState
	if err := json.Unmarshal(content, &state); err != nil {
		return model.SignedTrustBundle{}, false, err
	}
	if state.SchemaVersion != FileSchemaVersion {
		return model.SignedTrustBundle{}, false, fmt.Errorf("unsupported host trust store schema %q", state.SchemaVersion)
	}
	return state.TrustBundle, true, nil
}

func (s ProtectedStore) Save(bundle model.SignedTrustBundle) error {
	state := fileState{
		SchemaVersion: FileSchemaVersion,
		TrustBundle:   bundle,
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	store, err := protectedstore.Open(s.Ref)
	if err != nil {
		return err
	}
	return store.Save(content)
}

func (s ProtectedStore) VerifyAndSaveUpdate(next model.SignedTrustBundle, root model.TrustBundle, now time.Time) error {
	return verifyAndSaveUpdate(s, next, root, now)
}

func verifyAndSaveUpdate(store Store, next model.SignedTrustBundle, root model.TrustBundle, now time.Time) error {
	current, ok, err := store.Load()
	if err != nil {
		return err
	}
	if ok {
		currentRoot, err := current.ActiveTrustBundle(next.SigningKeyID, now)
		if err != nil {
			return err
		}
		if err := next.VerifyUpdate(current, currentRoot, now); err != nil {
			return err
		}
	} else if err := next.Verify(root, now); err != nil {
		return err
	}
	return store.Save(next)
}
