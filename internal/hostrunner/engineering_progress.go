package hostrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
)

const (
	EngineeringProgressSchemaVersion   = "rdev.engineering-progress.v1"
	EngineeringCheckpointSchemaVersion = "rdev.engineering-checkpoint.v1"
)

const (
	EngineeringPhaseInspect  = "inspect"
	EngineeringPhasePlan     = "plan"
	EngineeringPhaseEdit     = "edit"
	EngineeringPhaseVerify   = "verify"
	EngineeringPhaseDiagnose = "diagnose"
	EngineeringPhaseRepair   = "repair"
	EngineeringPhaseFinalize = "finalize"
)

const (
	EngineeringFailureCodeTest           = "code_test_failure"
	EngineeringFailureEnvironmentTool    = "environment_tool_failure"
	EngineeringFailurePolicyDenial       = "policy_denial"
	EngineeringFailureTimeout            = "timeout"
	EngineeringFailureCancellation       = "cancellation"
	EngineeringFailureTransportReconnect = "transport_reconnect_failure"
	EngineeringFailureFlakyTest          = "flaky_test"
)

// EngineeringArtifactReference identifies bounded execution evidence without
// embedding logs, diffs, prompts, or local file contents in progress events.
type EngineeringArtifactReference struct {
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

// EngineeringProgress is emitted at every engineering execution phase. It is
// deliberately small enough for the control-plane event stream and carries a
// durable local checkpoint ID for recovery.
type EngineeringProgress struct {
	SchemaVersion string                         `json:"schema_version"`
	TaskID        string                         `json:"task_id"`
	AttemptID     string                         `json:"attempt_id"`
	Phase         string                         `json:"phase"`
	Attempt       int                            `json:"attempt"`
	Summary       string                         `json:"summary"`
	ArtifactRefs  []EngineeringArtifactReference `json:"artifact_refs,omitempty"`
	CheckpointID  string                         `json:"checkpoint_id"`
	Recoverable   bool                           `json:"recoverable"`
	FailureClass  string                         `json:"failure_class,omitempty"`
}

// EngineeringProgressReporter is implemented by the host connector to publish
// progress through the authenticated session event stream. Reporter failures
// never cause the host runner to repeat external code actions; the local
// checkpoint remains available for reconnect/resume.
type EngineeringProgressReporter func(context.Context, EngineeringProgress) error

// EngineeringCheckpoint is the local, redacted recovery record paired with a
// progress event. It contains task identity hashes and evidence references, not
// task prompts, repository contents, credentials, or environment data.
type EngineeringCheckpoint struct {
	SchemaVersion      string                         `json:"schema_version"`
	ID                 string                         `json:"id"`
	TaskID             string                         `json:"task_id"`
	AttemptID          string                         `json:"attempt_id"`
	TaskIdentitySHA256 string                         `json:"task_identity_sha256"`
	Phase              string                         `json:"phase"`
	Attempt            int                            `json:"attempt"`
	ArtifactRefs       []EngineeringArtifactReference `json:"artifact_refs,omitempty"`
	WorkspaceRoot      string                         `json:"workspace_root,omitempty"`
	Isolation          string                         `json:"isolation,omitempty"`
	Branch             string                         `json:"branch,omitempty"`
	BaseSHA            string                         `json:"base_sha,omitempty"`
	Recoverable        bool                           `json:"recoverable"`
	FailureClass       string                         `json:"failure_class,omitempty"`
	CreatedAt          time.Time                      `json:"created_at"`
}

type engineeringProgressEmitter struct {
	envelope         taskEnvelope
	task             contracts.EngineeringTask
	store            engineeringCheckpointStore
	reporter         EngineeringProgressReporter
	progress         []EngineeringProgress
	deliveryFailures int
}

type engineeringResumeState struct {
	Checkpoint   EngineeringCheckpoint
	SourceTaskID string
	StartAttempt int
}

const (
	engineeringResumeCheckpointPayloadKey = "engineering_resume_checkpoint_id"
	engineeringResumeTaskPayloadKey       = "engineering_resume_task_id"
)

type engineeringCheckpointStore interface {
	Save(EngineeringCheckpoint) error
	Load(taskID, checkpointID string) (EngineeringCheckpoint, error)
}

type fileEngineeringCheckpointStore struct {
	dir string
}

const maxEngineeringCheckpointIDLength = 128

func newEngineeringProgressEmitter(envelope taskEnvelope, task contracts.EngineeringTask, opts Options) (*engineeringProgressEmitter, error) {
	storeDir, err := engineeringCheckpointStoreDir(opts)
	if err != nil {
		return nil, err
	}
	return &engineeringProgressEmitter{
		envelope: envelope,
		task:     task,
		store:    fileEngineeringCheckpointStore{dir: storeDir},
		reporter: opts.EngineeringProgressReporter,
	}, nil
}

func engineeringCheckpointStoreDir(opts Options) (string, error) {
	if configured := strings.TrimSpace(opts.EngineeringCheckpointStore); configured != "" {
		return filepath.Abs(configured)
	}
	if lockStore := strings.TrimSpace(opts.WorkspaceLockStore); lockStore != "" {
		return filepath.Join(filepath.Dir(lockStore), "engineering-checkpoints"), nil
	}
	if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "rdev", "engineering-checkpoints"), nil
	}
	return filepath.Join(os.TempDir(), "rdev", "engineering-checkpoints"), nil
}

func prepareEngineeringResume(envelope taskEnvelope, opts Options) (Options, error) {
	checkpointID := firstNonEmptyString(opts.EngineeringResumeCheckpointID, stringValue(envelope.Payload, engineeringResumeCheckpointPayloadKey, ""))
	if checkpointID == "" {
		return opts, nil
	}
	task, ok := engineeringTaskFromEnvelope(envelope)
	if !ok || !isEngineeringRepairAdapter(envelope.Adapter) {
		return Options{}, fmt.Errorf("engineering resume requires a typed coding task")
	}
	sourceTaskID := firstNonEmptyString(opts.EngineeringResumeSourceTaskID, stringValue(envelope.Payload, engineeringResumeTaskPayloadKey, ""), envelope.TaskID)
	storeDir, err := engineeringCheckpointStoreDir(opts)
	if err != nil {
		return Options{}, fmt.Errorf("engineering resume checkpoint store is unavailable")
	}
	checkpoint, err := (fileEngineeringCheckpointStore{dir: storeDir}).Load(sourceTaskID, checkpointID)
	if err != nil {
		return Options{}, fmt.Errorf("engineering resume checkpoint could not be loaded")
	}
	if !checkpoint.Recoverable || checkpoint.TaskIdentitySHA256 != engineeringTaskIdentitySHA256(task) {
		return Options{}, fmt.Errorf("engineering resume checkpoint is not authorized for this task")
	}
	if expected := strings.TrimSpace(task.Workspace.BaseSHA); expected != "" && checkpoint.BaseSHA != "" && checkpoint.BaseSHA != expected {
		return Options{}, fmt.Errorf("engineering resume checkpoint does not match the task base SHA")
	}
	if expected := strings.TrimSpace(task.Workspace.Branch); expected != "" && checkpoint.Branch != "" && checkpoint.Branch != expected {
		return Options{}, fmt.Errorf("engineering resume checkpoint does not match the task branch")
	}
	startAttempt, err := engineeringResumeStartAttempt(checkpoint)
	if err != nil {
		return Options{}, err
	}
	opts.engineeringResume = &engineeringResumeState{
		Checkpoint:   checkpoint,
		SourceTaskID: sourceTaskID,
		StartAttempt: startAttempt,
	}
	return opts, nil
}

func engineeringResumeStartAttempt(checkpoint EngineeringCheckpoint) (int, error) {
	switch checkpoint.Phase {
	case EngineeringPhaseInspect, EngineeringPhasePlan:
		return 1, nil
	case EngineeringPhaseRepair:
		if checkpoint.Attempt < 1 {
			return 0, fmt.Errorf("engineering resume checkpoint has an invalid repair attempt")
		}
		return checkpoint.Attempt, nil
	case EngineeringPhaseEdit, EngineeringPhaseVerify, EngineeringPhaseDiagnose:
		if checkpoint.Attempt < 1 {
			return 0, fmt.Errorf("engineering resume checkpoint has an invalid attempt")
		}
		return checkpoint.Attempt + 1, nil
	default:
		return 0, fmt.Errorf("engineering resume checkpoint phase is not resumable")
	}
}

func (e *engineeringProgressEmitter) emit(ctx context.Context, phase string, attempt int, summary string, refs []EngineeringArtifactReference, recoverable bool, failureClass string) (EngineeringProgress, error) {
	refs = boundedEngineeringArtifactRefs(refs)
	progress := EngineeringProgress{
		SchemaVersion: EngineeringProgressSchemaVersion,
		TaskID:        e.envelope.TaskID,
		AttemptID:     engineeringEnvelopeAttemptID(e.envelope),
		Phase:         phase,
		Attempt:       attempt,
		Summary:       summary,
		ArtifactRefs:  refs,
		Recoverable:   recoverable,
		FailureClass:  failureClass,
	}
	progress.CheckpointID = engineeringCheckpointID(progress)
	checkpoint := EngineeringCheckpoint{
		SchemaVersion:      EngineeringCheckpointSchemaVersion,
		ID:                 progress.CheckpointID,
		TaskID:             progress.TaskID,
		AttemptID:          progress.AttemptID,
		TaskIdentitySHA256: engineeringTaskIdentitySHA256(e.task),
		Phase:              progress.Phase,
		Attempt:            progress.Attempt,
		ArtifactRefs:       append([]EngineeringArtifactReference(nil), progress.ArtifactRefs...),
		WorkspaceRoot:      e.envelope.Workspace.Root,
		Isolation:          e.envelope.Workspace.Isolation,
		Branch:             e.envelope.Workspace.Branch,
		BaseSHA:            e.envelope.Workspace.BaseSHA,
		Recoverable:        progress.Recoverable,
		FailureClass:       progress.FailureClass,
		CreatedAt:          time.Now().UTC(),
	}
	if err := e.store.Save(checkpoint); err != nil {
		return EngineeringProgress{}, fmt.Errorf("save engineering checkpoint: %w", err)
	}
	if e.reporter != nil {
		if err := e.reporter(ctx, progress); err != nil {
			e.deliveryFailures++
		}
	}
	e.progress = append(e.progress, progress)
	return progress, nil
}

func (e *engineeringProgressEmitter) Values() []EngineeringProgress {
	return append([]EngineeringProgress(nil), e.progress...)
}

func (e *engineeringProgressEmitter) DeliveryFailures() int {
	return e.deliveryFailures
}

func (s fileEngineeringCheckpointStore) Save(checkpoint EngineeringCheckpoint) error {
	if err := validateEngineeringCheckpointLocator(checkpoint.TaskID, checkpoint.ID); err != nil {
		return err
	}
	path := s.path(checkpoint.TaskID, checkpoint.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil {
		var decoded EngineeringCheckpoint
		if json.Unmarshal(existing, &decoded) == nil && decoded.ID == checkpoint.ID && decoded.TaskID == checkpoint.TaskID {
			return nil
		}
		return fmt.Errorf("engineering checkpoint id conflicts with existing checkpoint")
	} else if !os.IsNotExist(err) {
		return err
	}
	encoded, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return os.Chmod(path, 0o600)
}

func (s fileEngineeringCheckpointStore) Load(taskID, checkpointID string) (EngineeringCheckpoint, error) {
	if err := validateEngineeringCheckpointLocator(taskID, checkpointID); err != nil {
		return EngineeringCheckpoint{}, err
	}
	path := s.path(taskID, checkpointID)
	content, err := os.ReadFile(path)
	if err != nil {
		return EngineeringCheckpoint{}, err
	}
	var checkpoint EngineeringCheckpoint
	if err := json.Unmarshal(content, &checkpoint); err != nil {
		return EngineeringCheckpoint{}, fmt.Errorf("decode engineering checkpoint: %w", err)
	}
	if checkpoint.SchemaVersion != EngineeringCheckpointSchemaVersion || checkpoint.ID != checkpointID || checkpoint.TaskID != taskID {
		return EngineeringCheckpoint{}, fmt.Errorf("engineering checkpoint identity does not match request")
	}
	return checkpoint, nil
}

func (s fileEngineeringCheckpointStore) path(taskID, checkpointID string) string {
	taskHash := sha256.Sum256([]byte(taskID))
	return filepath.Join(s.dir, hex.EncodeToString(taskHash[:]), checkpointID+".json")
}

// validateEngineeringCheckpointLocator keeps externally supplied checkpoint
// IDs from becoming path components. Task IDs are hashed by path(), while the
// checkpoint ID is intentionally restricted to the identifier alphabet emitted
// by engineeringCheckpointID plus compatible opaque suffixes.
func validateEngineeringCheckpointLocator(taskID, checkpointID string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("engineering checkpoint task id is required")
	}
	if len(checkpointID) == 0 || len(checkpointID) > maxEngineeringCheckpointIDLength {
		return fmt.Errorf("engineering checkpoint id is invalid")
	}
	for _, value := range checkpointID {
		if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '_' || value == '-' {
			continue
		}
		return fmt.Errorf("engineering checkpoint id is invalid")
	}
	return nil
}

func engineeringEnvelopeAttemptID(envelope taskEnvelope) string {
	if strings.TrimSpace(envelope.AttemptID) != "" {
		return envelope.AttemptID
	}
	return "attempt:" + envelope.TaskID
}

func engineeringTaskIdentitySHA256(task contracts.EngineeringTask) string {
	sum := sha256.Sum256([]byte(task.SchemaVersion + "\x00" + task.IdempotencyKey + "\x00" + task.Workspace.Root + "\x00" + task.Workspace.BaseSHA))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func engineeringCheckpointID(progress EngineeringProgress) string {
	hash := sha256.New()
	for _, value := range []string{
		progress.TaskID,
		progress.AttemptID,
		progress.Phase,
		strconv.Itoa(progress.Attempt),
		progress.Summary,
		strconv.FormatBool(progress.Recoverable),
		progress.FailureClass,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{'\x00'})
	}
	for _, ref := range progress.ArtifactRefs {
		_, _ = hash.Write([]byte(ref.Kind))
		_, _ = hash.Write([]byte{'\x00'})
		_, _ = hash.Write([]byte(ref.SHA256))
		_, _ = hash.Write([]byte{'\x00'})
	}
	return "ecp_" + hex.EncodeToString(hash.Sum(nil)[:12])
}

func boundedEngineeringArtifactRefs(refs []EngineeringArtifactReference) []EngineeringArtifactReference {
	const maxRefs = 8
	out := make([]EngineeringArtifactReference, 0, min(len(refs), maxRefs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.SHA256 = strings.TrimSpace(ref.SHA256)
		if ref.Kind == "" || ref.SHA256 == "" {
			continue
		}
		key := ref.Kind + "\x00" + ref.SHA256
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
		if len(out) == maxRefs {
			break
		}
	}
	return out
}

func engineeringArtifactRef(kind, value string) []EngineeringArtifactReference {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "sha256:") {
		sum := sha256.Sum256([]byte(value))
		value = "sha256:" + hex.EncodeToString(sum[:])
	}
	return []EngineeringArtifactReference{{Kind: kind, SHA256: value}}
}
