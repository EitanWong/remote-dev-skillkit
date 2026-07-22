package windowsentry

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/release"
)

const (
	AttemptStateSchemaVersion = "rdev.windows-layered-attempt.v1"
	attemptStateFilename      = "state.json"
	attemptLockFilename       = "attempt.lock"
	attemptStateTempFilename  = "state.json.tmp"
	maxAttemptStateBytes      = 1024
)

type attemptStage string

const (
	attemptStagePreCore     attemptStage = "pre_core"
	attemptStageCoreStarted attemptStage = "core_started"
	attemptStageCoreExited  attemptStage = "core_exited"
)

type attemptLauncher string

const (
	launcherPowerShell       attemptLauncher = "powershell"
	launcherPowerShellBypass attemptLauncher = "powershell-bypass"
	launcherCMD              attemptLauncher = "cmd"
)

type attemptState struct {
	SchemaVersion string
	AttemptID     string
	Stage         attemptStage
	Launcher      attemptLauncher
	UpdatedAt     string
}

func newPreCoreError(class string) error {
	return errors.New("layered pre-core failure:" + class)
}

var (
	errAttemptBusy         = errors.New("attempt lock is active")
	errAttemptClosed       = errors.New("attempt already started")
	errInvalidAttemptState = errors.New("invalid attempt state")
)

type attemptGuard struct {
	directory     string
	directoryInfo os.FileInfo
	statePath     string
	lockPath      string
	lock          *os.File
	launcher      attemptLauncher
	state         attemptState
}

func acquireAttempt(directory string, launcher attemptLauncher, now time.Time) (*attemptGuard, error) {
	if !launcher.valid() {
		return nil, fmt.Errorf("invalid attempt launcher")
	}
	clean, err := validateAttemptPathInput(directory)
	if err != nil {
		return nil, err
	}
	directoryInfo, err := validatePrivateAttemptDirectory(clean)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(clean, attemptLockFilename)
	lock, err := createPrivateAttemptFile(clean, attemptLockFilename)
	if err != nil {
		if os.IsExist(err) {
			if _, validationErr := validatePrivateAttemptFile(lockPath, 0); validationErr != nil {
				return nil, validationErr
			}
			return nil, errAttemptBusy
		}
		return nil, err
	}
	guard := &attemptGuard{
		directory:     clean,
		directoryInfo: directoryInfo,
		statePath:     filepath.Join(clean, attemptStateFilename),
		lockPath:      lockPath,
		lock:          lock,
		launcher:      launcher,
	}
	valid := false
	defer func() {
		if !valid {
			_ = guard.close()
		}
	}()
	if err := lock.Sync(); err != nil {
		return nil, err
	}
	if err := guard.revalidate(); err != nil {
		return nil, err
	}
	state, err := readAttemptState(guard.statePath)
	if os.IsNotExist(err) {
		state = attemptState{
			SchemaVersion: AttemptStateSchemaVersion,
			AttemptID:     filepath.Base(clean),
			Stage:         attemptStagePreCore,
			Launcher:      launcher,
			UpdatedAt:     attemptTimestamp(now),
		}
		if err := writeAttemptState(clean, state); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if state.AttemptID != filepath.Base(clean) {
		return nil, fmt.Errorf("attempt identity does not match its directory")
	}
	if state.Stage != attemptStagePreCore {
		return nil, errAttemptClosed
	}
	guard.state = state
	valid = true
	return guard, nil
}

func (guard *attemptGuard) transition(next attemptStage, now time.Time) error {
	if guard == nil || guard.lock == nil {
		return fmt.Errorf("attempt lock is required")
	}
	if !validAttemptTransition(guard.state.Stage, next) {
		return fmt.Errorf("invalid attempt stage transition")
	}
	if err := guard.revalidate(); err != nil {
		return err
	}
	current, err := readAttemptState(guard.statePath)
	if err != nil {
		return err
	}
	if current.AttemptID != guard.state.AttemptID || current.Stage != guard.state.Stage {
		return fmt.Errorf("attempt state changed while locked")
	}
	nextState := attemptState{
		SchemaVersion: AttemptStateSchemaVersion,
		AttemptID:     current.AttemptID,
		Stage:         next,
		Launcher:      guard.launcher,
		UpdatedAt:     attemptTimestamp(now),
	}
	if err := writeAttemptState(guard.directory, nextState); err != nil {
		return err
	}
	guard.state = nextState
	return nil
}

func (guard *attemptGuard) revalidate() error {
	if guard == nil || guard.lock == nil {
		return fmt.Errorf("attempt lock is required")
	}
	directoryInfo, err := validatePrivateAttemptDirectory(guard.directory)
	if err != nil || !os.SameFile(guard.directoryInfo, directoryInfo) {
		return fmt.Errorf("attempt directory identity changed")
	}
	lockInfo, err := guard.lock.Stat()
	if err != nil {
		return err
	}
	pathInfo, err := validatePrivateAttemptFile(guard.lockPath, 0)
	if err != nil || !os.SameFile(lockInfo, pathInfo) {
		return fmt.Errorf("attempt lock identity changed")
	}
	return nil
}

func (guard *attemptGuard) close() error {
	if guard == nil || guard.lock == nil {
		return nil
	}
	lock := guard.lock
	guard.lock = nil
	info, statErr := lock.Stat()
	pathInfo, pathErr := os.Lstat(guard.lockPath)
	closeErr := lock.Close()
	if statErr == nil && pathErr == nil && os.SameFile(info, pathInfo) {
		return errors.Join(closeErr, os.Remove(guard.lockPath))
	}
	return errors.Join(statErr, pathErr, closeErr)
}

func (launcher attemptLauncher) valid() bool {
	return launcher == launcherPowerShell || launcher == launcherPowerShellBypass || launcher == launcherCMD
}

func (stage attemptStage) valid() bool {
	return stage == attemptStagePreCore || stage == attemptStageCoreStarted || stage == attemptStageCoreExited
}

func validAttemptTransition(from, to attemptStage) bool {
	return from == attemptStagePreCore && to == attemptStageCoreStarted || from == attemptStageCoreStarted && to == attemptStageCoreExited
}

func attemptTimestamp(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC().Format(time.RFC3339Nano)
}

func validateAttemptPathInput(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, "//") {
		return "", fmt.Errorf("attempt must use a local path")
	}
	clean := filepath.Clean(raw)
	if clean != raw || !filepath.IsAbs(clean) {
		return "", fmt.Errorf("attempt must use an absolute canonical path")
	}
	if !validWindowsCacheBasename(filepath.Base(clean)) {
		return "", fmt.Errorf("attempt has an unsafe identifier")
	}
	return clean, nil
}

func readAttemptState(path string) (attemptState, error) {
	file, err := openPrivateAttemptFile(path, maxAttemptStateBytes)
	if err != nil {
		return attemptState{}, err
	}
	content, readErr := io.ReadAll(io.LimitReader(file, maxAttemptStateBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return attemptState{}, errors.Join(readErr, closeErr)
	}
	if len(content) > maxAttemptStateBytes {
		return attemptState{}, fmt.Errorf("attempt state exceeds its byte bound")
	}
	return decodeAttemptState(content)
}

func writeAttemptState(directory string, state attemptState) (resultErr error) {
	if err := state.validate(); err != nil {
		return err
	}
	content := encodeAttemptState(state)
	temporaryPath := filepath.Join(directory, attemptStateTempFilename)
	file, err := createPrivateAttemptFile(directory, attemptStateTempFilename)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		_ = os.Remove(temporaryPath)
	}()
	written, err := file.Write(content)
	if err != nil {
		return err
	}
	if written != len(content) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	if _, err := validatePrivateAttemptFile(temporaryPath, int64(len(content))); err != nil {
		return err
	}
	statePath := filepath.Join(directory, attemptStateFilename)
	if err := replacePrivateAttemptFile(temporaryPath, statePath); err != nil {
		return err
	}
	_, err = validatePrivateAttemptFile(statePath, int64(len(content)))
	return err
}

func encodeAttemptState(state attemptState) []byte {
	content := make([]byte, 0, 192)
	content = append(content, `{"schema_version":`...)
	content = appendReportString(content, state.SchemaVersion)
	content = append(content, `,"attempt_id":`...)
	content = appendReportString(content, state.AttemptID)
	content = append(content, `,"stage":`...)
	content = appendReportString(content, string(state.Stage))
	content = append(content, `,"launcher":`...)
	content = appendReportString(content, string(state.Launcher))
	content = append(content, `,"updated_at":`...)
	content = appendReportString(content, state.UpdatedAt)
	return append(content, '}', '\n')
}

func decodeAttemptState(content []byte) (attemptState, error) {
	start, end := 0, len(content)
	for start < end && isJSONSpace(content[start]) {
		start++
	}
	for start < end && isJSONSpace(content[end-1]) {
		end--
	}
	text := string(content[start:end])
	const prefix = `{"schema_version":"`
	if !strings.HasPrefix(text, prefix) {
		return attemptState{}, errInvalidAttemptState
	}
	schema, rest, ok := strings.Cut(text[len(prefix):], `","attempt_id":"`)
	if !ok {
		return attemptState{}, errInvalidAttemptState
	}
	attemptID, rest, ok := strings.Cut(rest, `","stage":"`)
	if !ok {
		return attemptState{}, errInvalidAttemptState
	}
	stage, rest, ok := strings.Cut(rest, `","launcher":"`)
	if !ok {
		return attemptState{}, errInvalidAttemptState
	}
	launcher, rest, ok := strings.Cut(rest, `","updated_at":"`)
	if !ok {
		return attemptState{}, errInvalidAttemptState
	}
	updatedAt, trailing, ok := strings.Cut(rest, `"}`)
	if !ok || trailing != "" {
		return attemptState{}, errInvalidAttemptState
	}
	state := attemptState{
		SchemaVersion: schema,
		AttemptID:     attemptID,
		Stage:         attemptStage(stage),
		Launcher:      attemptLauncher(launcher),
		UpdatedAt:     updatedAt,
	}
	return state, state.validate()
}

func isJSONSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func (state attemptState) validate() error {
	if state.SchemaVersion != AttemptStateSchemaVersion || !state.Stage.valid() || !state.Launcher.valid() ||
		!validWindowsCacheBasename(state.AttemptID) {
		return errInvalidAttemptState
	}
	if !release.IsCanonicalUTCTimestamp(state.UpdatedAt) {
		return errInvalidAttemptState
	}
	return nil
}
