package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/workspace"
)

func TestHostRunnerEngineeringTaskRepairsFailedVerificationWithinBoundedAttempts(t *testing.T) {
	requireEngineeringLoopTools(t, "git", "sh")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
count_file=.rdev-engineering-attempts
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$count_file"
printf '%s\n' "$@" > ".rdev-engineering-argv-$count"
if [ "$count" -eq 1 ]; then
  printf 'broken\n' > result.txt
else
  printf 'fixed\n' > result.txt
fi
`)
	lockStore := filepath.Join(t.TempDir(), "workspace-locks")

	result, err := RunSessionTaskWithOptionsContext(context.Background(), engineeringCodexTask(
		"task-engineering-repair",
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		2,
		[][]string{{"sh", "-c", `test "$(cat result.txt)" = fixed`}},
		[]string{"sh"},
	), time.Now().UTC(), Options{WorkspaceLockStore: lockStore})
	if err != nil {
		t.Fatal(err)
	}

	artifact := decodeEngineeringLoopArtifact(t, result.ArtifactContent)
	execution := artifact.EngineeringExecution
	if !execution.Completed || execution.MaxAttempts != 2 || execution.StoppedReason != "verification_passed" || len(execution.Attempts) != 2 {
		t.Fatalf("unexpected bounded repair execution evidence: %#v", execution)
	}
	if execution.Inspection.Status != "completed" || execution.Inspection.ArtifactSHA256 == "" || execution.Inspection.WorkspaceFingerprint == "" || execution.PlanSHA256 == "" || execution.AcceptanceSHA256 == "" {
		t.Fatalf("engineering execution must retain bounded inspect and plan evidence: %#v", execution)
	}
	assertEngineeringProgress(t, execution, lockStore, []string{
		EngineeringPhaseInspect,
		EngineeringPhasePlan,
		EngineeringPhaseEdit,
		EngineeringPhaseVerify,
		EngineeringPhaseDiagnose,
		EngineeringPhaseRepair,
		EngineeringPhaseEdit,
		EngineeringPhaseVerify,
		EngineeringPhaseFinalize,
	})
	if first, second := execution.Attempts[0], execution.Attempts[1]; first.Status != "verification_failed" || len(first.Verification) != 1 || first.Verification[0].ExitCode == 0 || second.Status != "verified" || second.ArtifactSHA256 == "" || second.ResultSchema != "rdev.codex-result.v1" {
		t.Fatalf("repair loop did not preserve failed and repaired attempt evidence: %#v", execution.Attempts)
	}
	if artifact.WorkspaceIsolation == nil || artifact.WorkspaceIsolation.WorktreePath == "" || !artifact.WorkspaceIsolation.LockRelease.Released {
		t.Fatalf("repair loop must retain worktree isolation evidence: %#v", artifact.WorkspaceIsolation)
	}
	worktree := artifact.WorkspaceIsolation.WorktreePath
	if got := readEngineeringAttemptCount(t, filepath.Join(worktree, ".rdev-engineering-attempts")); got != "2" {
		t.Fatalf("expected exactly two bounded coding attempts, got %q", got)
	}
	secondPrompt, err := os.ReadFile(filepath.Join(worktree, ".rdev-engineering-argv-2"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"This is repair attempt 2 of 2",
		"Previous structured diagnosis: Declared verification failed.",
		"Additional signed task instructions:",
		"Leave the workspace in a passing state.",
	} {
		if !strings.Contains(string(secondPrompt), expected) {
			t.Fatalf("repair prompt missing %q:\n%s", expected, secondPrompt)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("source checkout must remain isolated from repair attempts, stat err=%v", err)
	}
}

func TestHostRunnerEngineeringTaskKeepsCheckpointAfterProgressTransportFailure(t *testing.T) {
	requireEngineeringLoopTools(t, "git", "sh")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
printf 'ready\n' > result.txt
`)
	lockStore := filepath.Join(t.TempDir(), "workspace-locks")
	reporterCalls := 0
	result, err := RunSessionTaskWithOptionsContext(context.Background(), engineeringCodexTask(
		"task-engineering-progress-transport",
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		1,
		[][]string{{"sh", "-c", "test -f result.txt"}},
		[]string{"sh"},
	), time.Now().UTC(), Options{
		WorkspaceLockStore: lockStore,
		EngineeringProgressReporter: func(context.Context, EngineeringProgress) error {
			reporterCalls++
			return errors.New("transient gateway disconnect")
		},
	})
	if err != nil {
		t.Fatalf("progress delivery failure must not rerun or fail a completed engineering task: %v", err)
	}
	artifact := decodeEngineeringLoopArtifact(t, result.ArtifactContent)
	execution := artifact.EngineeringExecution
	if !execution.Completed || execution.ProgressDeliveryFailures != len(execution.Progress) || execution.ProgressDeliveryFailureClass != EngineeringFailureTransportReconnect || reporterCalls != len(execution.Progress) {
		t.Fatalf("transport interruption was not retained as recoverable execution evidence: %#v", execution)
	}
	store := fileEngineeringCheckpointStore{dir: filepath.Join(filepath.Dir(lockStore), "engineering-checkpoints")}
	for _, progress := range execution.Progress {
		if _, err := store.Load(progress.TaskID, progress.CheckpointID); err != nil {
			t.Fatalf("checkpoint must survive progress transport failure: %v", err)
		}
	}
}

func TestHostRunnerEngineeringTaskResumesFromRepairCheckpointInSameWorktree(t *testing.T) {
	requireEngineeringLoopTools(t, "git", "sh")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
count_file=.rdev-engineering-attempts
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$count_file"
if [ "$count" -eq 1 ]; then
  printf 'broken\n' > result.txt
else
  printf 'fixed\n' > result.txt
fi
`)
	lockStore := filepath.Join(t.TempDir(), "workspace-locks")
	sourceTaskID := "task-engineering-resume-source"
	ctx, cancel := context.WithCancel(context.Background())
	initialResult, initialErr := RunSessionTaskWithOptionsContext(ctx, engineeringCodexTask(
		sourceTaskID,
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		2,
		[][]string{{"sh", "-c", `test "$(cat result.txt)" = fixed`}},
		[]string{"sh"},
	), time.Now().UTC(), Options{
		WorkspaceLockStore: lockStore,
		EngineeringProgressReporter: func(_ context.Context, progress EngineeringProgress) error {
			if progress.Phase == EngineeringPhaseDiagnose && progress.Attempt == 1 {
				cancel()
			}
			return nil
		},
	})
	defer cancel()
	if initialErr == nil {
		t.Fatal("interrupted source execution must not report success")
	}
	initialArtifact := decodeEngineeringLoopArtifact(t, initialResult.ArtifactContent)
	checkpointID := ""
	for _, progress := range initialArtifact.EngineeringExecution.Progress {
		if progress.Phase == EngineeringPhaseRepair && progress.Attempt == 2 && progress.Recoverable {
			checkpointID = progress.CheckpointID
			break
		}
	}
	if checkpointID == "" || initialArtifact.WorkspaceIsolation == nil {
		t.Fatalf("interrupted task must preserve a recoverable repair checkpoint and worktree evidence: %#v", initialArtifact)
	}

	resumeTaskID := "task-engineering-resume-next"
	resumeSpec := engineeringCodexTask(
		resumeTaskID,
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		2,
		[][]string{{"sh", "-c", `test "$(cat result.txt)" = fixed`}},
		[]string{"sh"},
	)
	resumeTask := resumeSpec.Payload["engineering_task"].(map[string]any)
	resumeTask["idempotency_key"] = sourceTaskID + "-idempotency"
	resumeTask["workspace"].(map[string]any)["branch"] = "rdev/" + sourceTaskID
	resumedResult, resumedErr := RunSessionTaskWithOptionsContext(context.Background(), resumeSpec, time.Now().UTC(), Options{
		WorkspaceLockStore:            lockStore,
		EngineeringResumeCheckpointID: checkpointID,
		EngineeringResumeSourceTaskID: sourceTaskID,
	})
	if resumedErr != nil {
		t.Fatal(resumedErr)
	}
	resumedArtifact := decodeEngineeringLoopArtifact(t, resumedResult.ArtifactContent)
	execution := resumedArtifact.EngineeringExecution
	if !execution.Resumed || execution.ResumedFromCheckpoint != checkpointID || !execution.Completed || len(execution.Attempts) != 1 || execution.Attempts[0].Attempt != 2 || execution.Attempts[0].Status != "verified" {
		t.Fatalf("resume did not continue from the checkpointed repair attempt: %#v", execution)
	}
	if resumedArtifact.WorkspaceIsolation == nil || resumedArtifact.WorkspaceIsolation.WorktreePath != initialArtifact.WorkspaceIsolation.WorktreePath {
		t.Fatalf("resume must reuse the original isolated worktree: initial=%#v resumed=%#v", initialArtifact.WorkspaceIsolation, resumedArtifact.WorkspaceIsolation)
	}
	if got := readEngineeringAttemptCount(t, filepath.Join(resumedArtifact.WorkspaceIsolation.WorktreePath, ".rdev-engineering-attempts")); got != "2" {
		t.Fatalf("resume must continue with the second bounded attempt, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("resume must preserve source workspace isolation, stat err=%v", err)
	}
}

func TestHostRunnerEngineeringTaskHonorsAttemptLimit(t *testing.T) {
	requireEngineeringLoopTools(t, "git", "sh")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
count_file=.rdev-engineering-attempts
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$count_file"
printf 'broken\n' > result.txt
`)

	result, err := RunSessionTaskWithOptionsContext(context.Background(), engineeringCodexTask(
		"task-engineering-limit",
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		2,
		[][]string{{"sh", "-c", `test "$(cat result.txt)" = fixed`}},
		[]string{"sh"},
	), time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	if err == nil {
		t.Fatal("unrepaired verification failure must fail at its attempt limit")
	}
	artifact := decodeEngineeringLoopArtifact(t, result.ArtifactContent)
	execution := artifact.EngineeringExecution
	if execution.Completed || !execution.AutoRepairAllowed || execution.StoppedReason != "attempt_limit_reached" || len(execution.Attempts) != 2 || execution.Attempts[0].Status != "verification_failed" || execution.Attempts[1].Status != "verification_failed" {
		t.Fatalf("attempt limit was not enforced: %#v", execution)
	}
	if artifact.WorkspaceIsolation == nil {
		t.Fatalf("attempt-limit failure must retain worktree evidence: %s", result.ArtifactContent)
	}
	if got := readEngineeringAttemptCount(t, filepath.Join(artifact.WorkspaceIsolation.WorktreePath, ".rdev-engineering-attempts")); got != "2" {
		t.Fatalf("expected exactly the declared number of attempts, got %q", got)
	}
}

func TestHostRunnerEngineeringTaskDoesNotRetryPolicyRejectedVerification(t *testing.T) {
	requireEngineeringLoopTools(t, "git")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
count_file=.rdev-engineering-attempts
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$count_file"
printf 'ready\n' > result.txt
`)

	result, err := RunSessionTaskWithOptionsContext(context.Background(), engineeringCodexTask(
		"task-engineering-policy",
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		3,
		[][]string{{"sh", "-c", "exit 0"}},
		nil,
	), time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	if err == nil {
		t.Fatal("policy-rejected verification must fail")
	}

	artifact := decodeEngineeringLoopArtifact(t, result.ArtifactContent)
	execution := artifact.EngineeringExecution
	if execution.Completed || execution.StoppedReason != "policy_failed" || len(execution.Attempts) != 1 || execution.Attempts[0].Status != "policy_failed" {
		t.Fatalf("policy failure must stop without repair retries: %#v", execution)
	}
	if artifact.WorkspaceIsolation == nil {
		t.Fatalf("policy failure must retain worktree evidence: %s", result.ArtifactContent)
	}
	if got := readEngineeringAttemptCount(t, filepath.Join(artifact.WorkspaceIsolation.WorktreePath, ".rdev-engineering-attempts")); got != "1" {
		t.Fatalf("policy failure must not retry coding adapter, got %q attempts", got)
	}
}

func TestHostRunnerEngineeringTaskDoesNotAutoRepairAfterInterruptRequired(t *testing.T) {
	requireEngineeringLoopTools(t, "git", "sh")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
count_file=.rdev-engineering-attempts
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$count_file"
printf 'broken\n' > result.txt
`)
	spec := engineeringCodexTask(
		"task-engineering-interrupt",
		repo,
		engineeringGitHead(t, repo),
		fakeCodex,
		contracts.EngineeringIsolationGitWorktree,
		3,
		[][]string{{"sh", "-c", `test "$(cat result.txt)" = fixed`}},
		[]string{"sh"},
	)
	spec.Payload["engineering_task"].(map[string]any)["interrupts_required"] = []string{"interrupt.push"}

	result, err := RunSessionTaskWithOptionsContext(context.Background(), spec, time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
	if err == nil {
		t.Fatal("failed verification with an explicit interrupt requirement must fail")
	}
	artifact := decodeEngineeringLoopArtifact(t, result.ArtifactContent)
	execution := artifact.EngineeringExecution
	if execution.AutoRepairAllowed || execution.Completed || execution.StoppedReason != "interrupt_required" || len(execution.Attempts) != 1 || execution.Attempts[0].Status != "verification_failed" {
		t.Fatalf("interrupt-required task must not receive automatic repair attempts: %#v", execution)
	}
	if artifact.WorkspaceIsolation == nil {
		t.Fatalf("interrupt-required failure must retain worktree evidence: %s", result.ArtifactContent)
	}
	if got := readEngineeringAttemptCount(t, filepath.Join(artifact.WorkspaceIsolation.WorktreePath, ".rdev-engineering-attempts")); got != "1" {
		t.Fatalf("interrupt-required task must not retry coding adapter, got %q attempts", got)
	}
}

func TestHostRunnerEngineeringTaskDoesNotRetryCancellation(t *testing.T) {
	requireEngineeringLoopTools(t, "git", "sleep")
	repo := initEngineeringLoopGitRepo(t)
	fakeCodex := writeEngineeringLoopProgram(t, `#!/bin/sh
set -eu
printf '1\n' > .rdev-engineering-attempts
exec sleep 5
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outcome := make(chan struct {
		result Result
		err    error
	}, 1)

	go func() {
		result, err := RunSessionTaskWithOptionsContext(ctx, engineeringCodexTask(
			"task-engineering-cancel",
			repo,
			engineeringGitHead(t, repo),
			fakeCodex,
			contracts.EngineeringIsolationWorkspaceLock,
			3,
			[][]string{{"sh", "-c", "exit 0"}},
			[]string{"sh"},
		), time.Now().UTC(), Options{WorkspaceLockStore: filepath.Join(t.TempDir(), "workspace-locks")})
		outcome <- struct {
			result Result
			err    error
		}{result: result, err: err}
	}()

	attemptPath := filepath.Join(repo, ".rdev-engineering-attempts")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(attemptPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for engineering adapter attempt")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case received := <-outcome:
		if received.err == nil {
			t.Fatal("canceled engineering task must fail")
		}
		artifact := decodeEngineeringLoopArtifact(t, received.result.ArtifactContent)
		execution := artifact.EngineeringExecution
		if execution.Completed || execution.StoppedReason != "canceled" || len(execution.Attempts) != 1 || execution.Attempts[0].Status != "canceled" {
			t.Fatalf("cancellation must stop without repair retries: %#v", execution)
		}
		if got := readEngineeringAttemptCount(t, attemptPath); got != "1" {
			t.Fatalf("cancellation must not retry coding adapter, got %q attempts", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled engineering task did not finish")
	}
}

type engineeringLoopArtifact struct {
	EngineeringExecution engineeringExecutionEvidence   `json:"engineering_execution"`
	WorkspaceIsolation   *workspace.GitWorktreeEvidence `json:"workspace_isolation"`
}

func decodeEngineeringLoopArtifact(t *testing.T, content string) engineeringLoopArtifact {
	t.Helper()
	var artifact engineeringLoopArtifact
	if err := json.Unmarshal([]byte(content), &artifact); err != nil {
		t.Fatalf("decode engineering loop artifact: %v\n%s", err, content)
	}
	return artifact
}

func engineeringCodexTask(taskID, repo, baseSHA, command, isolation string, maxAttempts int, verification [][]string, allowCommands []string) SessionTaskSpec {
	return SessionTaskSpec{
		TaskID:     taskID,
		EndpointID: "endpoint-engineering",
		Adapter:    "codex",
		Payload: map[string]any{
			"codex_command": command,
			"prompt":        "Leave the workspace in a passing state.",
			"engineering_task": map[string]any{
				"schema_version": contracts.EngineeringTaskSchemaVersion,
				"goal":           "Repair the scoped fixture through declared verification.",
				"workspace": map[string]any{
					"root":         repo,
					"base_sha":     baseSHA,
					"branch":       "rdev/" + taskID,
					"isolation":    isolation,
					"dirty_policy": contracts.EngineeringDirtyPolicyPreserve,
					"cleanup":      contracts.EngineeringWorktreeCleanupPreserve,
					"read_scope":   []string{"."},
					"write_scope":  []string{"."},
				},
				"plan":       []string{"Inspect the existing fixture.", "Apply the narrow repair."},
				"acceptance": []string{"The declared verification command passes."},
				"verification": map[string]any{
					"commands":       verification,
					"allow_commands": allowCommands,
				},
				"limits": map[string]any{
					"max_duration_seconds": 30,
					"max_output_bytes":     64 * 1024,
					"max_attempts":         maxAttempts,
				},
				"network_policy":        contracts.EngineeringNetworkDefaultDeny,
				"required_capabilities": []string{"codex.run", "git.diff"},
				"idempotency_key":       taskID + "-idempotency",
			},
		},
	}
}

func assertEngineeringProgress(t *testing.T, execution engineeringExecutionEvidence, lockStore string, phases []string) {
	t.Helper()
	if execution.ProgressDeliveryFailures != 0 || len(execution.Progress) != len(phases) {
		t.Fatalf("unexpected engineering progress evidence: %#v", execution.Progress)
	}
	store := fileEngineeringCheckpointStore{dir: filepath.Join(filepath.Dir(lockStore), "engineering-checkpoints")}
	for index, progress := range execution.Progress {
		if progress.SchemaVersion != EngineeringProgressSchemaVersion || progress.Phase != phases[index] || progress.TaskID == "" || progress.AttemptID == "" || progress.Summary == "" || progress.CheckpointID == "" {
			t.Fatalf("invalid engineering progress at index %d: %#v", index, progress)
		}
		checkpoint, err := store.Load(progress.TaskID, progress.CheckpointID)
		if err != nil {
			t.Fatalf("load checkpoint for progress %d: %v", index, err)
		}
		if checkpoint.Phase != progress.Phase || checkpoint.Attempt != progress.Attempt || checkpoint.Recoverable != progress.Recoverable || checkpoint.FailureClass != progress.FailureClass {
			t.Fatalf("checkpoint did not preserve progress metadata: checkpoint=%#v progress=%#v", checkpoint, progress)
		}
	}
	if first := execution.Progress[0]; first.Phase != EngineeringPhaseInspect || len(first.ArtifactRefs) != 1 || first.ArtifactRefs[0].Kind != "project_context" {
		t.Fatalf("inspect progress must retain bounded project-context evidence: %#v", first)
	}
	if diagnose := execution.Progress[4]; diagnose.FailureClass != EngineeringFailureCodeTest || !diagnose.Recoverable {
		t.Fatalf("diagnose progress must classify a repairable verification failure: %#v", diagnose)
	}
}

func initEngineeringLoopGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# engineering loop fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runnerGit(t, repo, "init")
	runnerGit(t, repo, "config", "user.email", "rdev@example.test")
	runnerGit(t, repo, "config", "user.name", "Remote Dev")
	runnerGit(t, repo, "add", ".")
	runnerGit(t, repo, "commit", "-m", "engineering loop fixture")
	return repo
}

func engineeringGitHead(t *testing.T, repo string) string {
	t.Helper()
	output, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func writeEngineeringLoopProgram(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-codex")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func readEngineeringAttemptCount(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}

func requireEngineeringLoopTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required for engineering loop test: %v", tool, err)
		}
	}
}
