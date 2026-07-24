package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
)

func TestFileEngineeringCheckpointStoreRejectsTraversalCheckpointIDs(t *testing.T) {
	dir := t.TempDir()
	store := fileEngineeringCheckpointStore{dir: dir}
	taskID := "task-checkpoint-source"
	checkpointID := "../escaped"
	escaped := EngineeringCheckpoint{
		SchemaVersion: EngineeringCheckpointSchemaVersion,
		ID:            checkpointID,
		TaskID:        taskID,
	}
	encoded, err := json.Marshal(escaped)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "escaped.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(taskID, checkpointID); err == nil {
		t.Fatal("checkpoint load must reject a traversal checkpoint id before reading outside its task directory")
	}
	if err := store.Save(escaped); err == nil {
		t.Fatal("checkpoint save must reject a traversal checkpoint id before writing outside its task directory")
	}
}

func TestFileEngineeringCheckpointStoreRoundTripsRedactedIdentityAndRejectsConflicts(t *testing.T) {
	store := fileEngineeringCheckpointStore{dir: t.TempDir()}
	checkpoint := engineeringCheckpointFixture("task-checkpoint-roundtrip", "ecp_roundtrip")
	checkpoint.ArtifactRefs = []EngineeringArtifactReference{{Kind: "adapter_result", SHA256: "sha256:fixture"}}
	if err := store.Save(checkpoint); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path(checkpoint.TaskID, checkpoint.ID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint file mode = %o, want 0600", info.Mode().Perm())
	}
	loaded, err := store.Load(checkpoint.TaskID, checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != checkpoint.ID || loaded.TaskID != checkpoint.TaskID || loaded.TaskIdentitySHA256 != checkpoint.TaskIdentitySHA256 || len(loaded.ArtifactRefs) != 1 {
		t.Fatalf("checkpoint round trip lost bounded identity evidence: %#v", loaded)
	}
	if err := store.Save(checkpoint); err != nil {
		t.Fatalf("same checkpoint identity must be idempotent: %v", err)
	}
	if err := store.Save(EngineeringCheckpoint{}); err == nil {
		t.Fatal("checkpoint save must require task and checkpoint IDs")
	}

	conflict := engineeringCheckpointFixture("task-checkpoint-conflict", "ecp_conflict")
	conflictPath := store.path(conflict.TaskID, conflict.ID)
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(conflict); err == nil {
		t.Fatal("malformed pre-existing checkpoint must not be overwritten")
	}

	mismatch := engineeringCheckpointFixture("task-checkpoint-mismatch", "ecp_other")
	mismatchPath := store.path(mismatch.TaskID, "ecp_requested")
	if err := os.MkdirAll(filepath.Dir(mismatchPath), 0o700); err != nil {
		t.Fatal(err)
	}
	mismatchJSON, err := json.Marshal(mismatch)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mismatchPath, mismatchJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(mismatch.TaskID, "ecp_requested"); err == nil {
		t.Fatal("checkpoint load must reject a file whose identity does not match the request")
	}
}

func TestEngineeringResumePhaseAndFailureMappings(t *testing.T) {
	for _, test := range []struct {
		name    string
		phase   string
		attempt int
		want    int
		valid   bool
	}{
		{name: "inspect starts first attempt", phase: EngineeringPhaseInspect, want: 1, valid: true},
		{name: "plan starts first attempt", phase: EngineeringPhasePlan, want: 1, valid: true},
		{name: "repair retains announced attempt", phase: EngineeringPhaseRepair, attempt: 2, want: 2, valid: true},
		{name: "edit advances attempt", phase: EngineeringPhaseEdit, attempt: 1, want: 2, valid: true},
		{name: "verify advances attempt", phase: EngineeringPhaseVerify, attempt: 2, want: 3, valid: true},
		{name: "diagnose advances attempt", phase: EngineeringPhaseDiagnose, attempt: 3, want: 4, valid: true},
		{name: "repair requires positive attempt", phase: EngineeringPhaseRepair, valid: false},
		{name: "edit requires positive attempt", phase: EngineeringPhaseEdit, valid: false},
		{name: "finalize is terminal", phase: EngineeringPhaseFinalize, attempt: 1, valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := engineeringResumeStartAttempt(EngineeringCheckpoint{Phase: test.phase, Attempt: test.attempt})
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("engineeringResumeStartAttempt() = %d, %v; want %d, nil", got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("engineeringResumeStartAttempt(%q, %d) unexpectedly succeeded with %d", test.phase, test.attempt, got)
			}
		})
	}

	for _, test := range []struct {
		failureClass string
		wantClass    string
	}{
		{EngineeringFailureCodeTest, "verification_failed"},
		{EngineeringFailureEnvironmentTool, "infrastructure_failed"},
		{EngineeringFailureTimeout, "timed_out"},
		{EngineeringFailureTransportReconnect, "transport_reconnect_failure"},
		{EngineeringFailurePolicyDenial, "resume"},
	} {
		diagnosis := engineeringResumeDiagnosis(EngineeringCheckpoint{FailureClass: test.failureClass})
		if diagnosis.Class != test.wantClass || diagnosis.Summary == "" {
			t.Fatalf("resume diagnosis for %q = %#v, want class %q with a summary", test.failureClass, diagnosis, test.wantClass)
		}
	}

	if got := engineeringFailureClassForExecution(engineeringExecutionEvidence{}, context.Canceled); got != EngineeringFailureCancellation {
		t.Fatalf("canceled execution failure class = %q", got)
	}
	if got := engineeringFailureClassForExecution(engineeringExecutionEvidence{}, context.DeadlineExceeded); got != EngineeringFailureTimeout {
		t.Fatalf("timed-out execution failure class = %q", got)
	}
	if got := engineeringFailureClassForExecution(engineeringExecutionEvidence{}, errors.New("adapter unavailable")); got != EngineeringFailureEnvironmentTool {
		t.Fatalf("unknown execution failure class = %q", got)
	}
}

func TestPrepareEngineeringResumeBindsOnlyMatchingRecoverableTask(t *testing.T) {
	task := contracts.EngineeringTask{
		SchemaVersion:  contracts.EngineeringTaskSchemaVersion,
		IdempotencyKey: "resume-contract-key",
		Workspace: contracts.EngineeringWorkspace{
			Root:    "/workspace/resume",
			BaseSHA: strings.Repeat("a", 40),
			Branch:  "rdev/resume-contract",
		},
	}
	envelope := taskEnvelope{
		TaskID:  "task-resume-current",
		Adapter: "codex",
		Payload: map[string]any{"engineering_task": task},
	}
	storeDir := t.TempDir()
	checkpoint := engineeringCheckpointFixture("task-resume-source", "ecp_resume")
	checkpoint.TaskIdentitySHA256 = engineeringTaskIdentitySHA256(task)
	checkpoint.Phase = EngineeringPhaseRepair
	checkpoint.Attempt = 2
	checkpoint.Recoverable = true
	checkpoint.BaseSHA = task.Workspace.BaseSHA
	checkpoint.Branch = task.Workspace.Branch
	store := fileEngineeringCheckpointStore{dir: storeDir}
	if err := store.Save(checkpoint); err != nil {
		t.Fatal(err)
	}

	opts, err := prepareEngineeringResume(envelope, Options{
		EngineeringCheckpointStore:    storeDir,
		EngineeringResumeCheckpointID: checkpoint.ID,
		EngineeringResumeSourceTaskID: checkpoint.TaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.engineeringResume == nil || opts.engineeringResume.SourceTaskID != checkpoint.TaskID || opts.engineeringResume.StartAttempt != 2 {
		t.Fatalf("resume state did not bind the matching recoverable checkpoint: %#v", opts.engineeringResume)
	}

	wrongIdentity := checkpoint
	wrongIdentity.ID = "ecp_wrong_identity"
	wrongIdentity.TaskIdentitySHA256 = "sha256:wrong"
	if err := store.Save(wrongIdentity); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareEngineeringResume(envelope, Options{
		EngineeringCheckpointStore:    storeDir,
		EngineeringResumeCheckpointID: wrongIdentity.ID,
		EngineeringResumeSourceTaskID: wrongIdentity.TaskID,
	}); err == nil {
		t.Fatal("resume must reject a checkpoint from a different typed task identity")
	}

	if _, err := prepareEngineeringResume(taskEnvelope{Adapter: "shell", Payload: map[string]any{}}, Options{
		EngineeringCheckpointStore:    storeDir,
		EngineeringResumeCheckpointID: checkpoint.ID,
		EngineeringResumeSourceTaskID: checkpoint.TaskID,
	}); err == nil {
		t.Fatal("resume must require a typed coding task before loading a checkpoint")
	}
}

func TestEngineeringTaskFromEnvelopePreservesOnlyTypedContracts(t *testing.T) {
	task := contracts.EngineeringTask{SchemaVersion: contracts.EngineeringTaskSchemaVersion}
	if got, ok := engineeringTaskFromEnvelope(taskEnvelope{Payload: map[string]any{"engineering_task": task}}); !ok || got.SchemaVersion != task.SchemaVersion {
		t.Fatalf("value engineering contract was not preserved: %#v, %v", got, ok)
	}
	if _, ok := engineeringTaskFromEnvelope(taskEnvelope{Payload: map[string]any{"engineering_task": (*contracts.EngineeringTask)(nil)}}); ok {
		t.Fatal("nil engineering contract pointer must be rejected")
	}
	if _, ok := engineeringTaskFromEnvelope(taskEnvelope{Payload: map[string]any{"engineering_task": map[string]any{"schema_version": "invalid"}}}); ok {
		t.Fatal("malformed generic engineering contract must be rejected")
	}
	if _, ok := engineeringTaskFromEnvelope(taskEnvelope{Payload: map[string]any{}}); ok {
		t.Fatal("missing engineering contract must be rejected")
	}
}

func engineeringCheckpointFixture(taskID, checkpointID string) EngineeringCheckpoint {
	return EngineeringCheckpoint{
		SchemaVersion:      EngineeringCheckpointSchemaVersion,
		ID:                 checkpointID,
		TaskID:             taskID,
		AttemptID:          "attempt:" + taskID,
		TaskIdentitySHA256: "sha256:fixture",
		Phase:              EngineeringPhaseRepair,
		Attempt:            2,
		Recoverable:        true,
		CreatedAt:          time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
	}
}
