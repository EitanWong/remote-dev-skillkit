package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type stagedSupportSessionArtifact struct {
	path, label, tempPath, backupPath string
	data                              []byte
	hadOriginal, committed            bool
}

func stageSupportSessionArtifacts(artifacts []*stagedSupportSessionArtifact) error {
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.path) == "" {
			return fmt.Errorf("%s path is required", artifact.label)
		}
		if err := os.MkdirAll(filepath.Dir(artifact.path), 0o700); err != nil {
			return fmt.Errorf("prepare %s directory: %w", artifact.label, err)
		}
		temp, err := os.CreateTemp(filepath.Dir(artifact.path), "."+filepath.Base(artifact.path)+".stage-*")
		if err != nil {
			return fmt.Errorf("stage %s: %w", artifact.label, err)
		}
		artifact.tempPath = temp.Name()
		if err := temp.Chmod(0o600); err != nil {
			_ = temp.Close()
			return fmt.Errorf("protect staged %s: %w", artifact.label, err)
		}
		if _, err := temp.Write(artifact.data); err != nil {
			_ = temp.Close()
			return fmt.Errorf("write staged %s: %w", artifact.label, err)
		}
		if err := temp.Sync(); err != nil {
			_ = temp.Close()
			return fmt.Errorf("sync staged %s: %w", artifact.label, err)
		}
		if err := temp.Close(); err != nil {
			return fmt.Errorf("close staged %s: %w", artifact.label, err)
		}
	}
	return nil
}

func commitSupportSessionArtifacts(artifacts []*stagedSupportSessionArtifact) error {
	for _, artifact := range artifacts {
		if artifact.hadOriginal {
			if err := os.Rename(artifact.path, artifact.backupPath); err != nil {
				return fmt.Errorf("backup existing %s: %w", artifact.label, err)
			}
		}
		if err := os.Rename(artifact.tempPath, artifact.path); err != nil {
			return fmt.Errorf("publish %s: %w", artifact.label, err)
		}
		artifact.tempPath, artifact.committed = "", true
		if err := syncSupportSessionArtifactDirectory(artifact.path); err != nil {
			return fmt.Errorf("sync published %s: %w", artifact.label, err)
		}
	}
	return nil
}

func prepareSupportSessionArtifactBackups(artifacts []*stagedSupportSessionArtifact) error {
	for _, artifact := range artifacts {
		info, err := os.Lstat(artifact.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect existing %s: %w", artifact.label, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("publish %s: existing target is not a protected regular file", artifact.label)
		}
		backup, err := os.CreateTemp(filepath.Dir(artifact.path), "."+filepath.Base(artifact.path)+".backup-*")
		if err != nil {
			return fmt.Errorf("prepare %s backup: %w", artifact.label, err)
		}
		artifact.backupPath = backup.Name()
		if err := backup.Chmod(0o600); err != nil {
			_ = backup.Close()
			return fmt.Errorf("protect %s backup: %w", artifact.label, err)
		}
		if err := backup.Close(); err != nil {
			return fmt.Errorf("close %s backup: %w", artifact.label, err)
		}
		if err := os.Remove(artifact.backupPath); err != nil {
			return fmt.Errorf("prepare %s backup path: %w", artifact.label, err)
		}
		artifact.hadOriginal = true
	}
	return nil
}

func restoreSupportSessionArtifacts(artifacts []*stagedSupportSessionArtifact) error {
	var errs []error
	for index := len(artifacts) - 1; index >= 0; index-- {
		a := artifacts[index]
		if a.committed {
			if err := os.Remove(a.path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove unpublished %s: %w", a.label, err))
			}
			a.committed = false
		}
		if a.hadOriginal {
			if err := os.Rename(a.backupPath, a.path); err != nil {
				errs = append(errs, fmt.Errorf("restore previous %s: %w", a.label, err))
			} else {
				a.hadOriginal, a.backupPath = false, ""
			}
		}
		if a.tempPath != "" {
			if err := os.Remove(a.tempPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove staged %s: %w", a.label, err))
			}
			a.tempPath = ""
		}
		if err := syncSupportSessionArtifactDirectory(a.path); err != nil {
			errs = append(errs, fmt.Errorf("sync restored %s: %w", a.label, err))
		}
	}
	return errors.Join(errs...)
}

func cleanupStagedSupportSessionArtifacts(artifacts []*stagedSupportSessionArtifact) error {
	var errs []error
	for _, a := range artifacts {
		if a.tempPath != "" {
			if err := os.Remove(a.tempPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove staged %s: %w", a.label, err))
			}
			a.tempPath = ""
		}
	}
	return errors.Join(errs...)
}

func finalizeSupportSessionArtifacts(artifacts []*stagedSupportSessionArtifact) error {
	var errs []error
	for _, a := range artifacts {
		if a.hadOriginal && a.backupPath != "" {
			if err := os.Remove(a.backupPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove previous %s backup: %w", a.label, err))
			}
			a.hadOriginal, a.backupPath = false, ""
		}
	}
	return errors.Join(errs...)
}

func syncSupportSessionArtifactDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
