package hostrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/contracts"
	"github.com/EitanWong/remote-dev-skillkit/internal/fileadapter"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
)

const EngineeringExecutionSchemaVersion = "rdev.engineering-execution.v1"

// engineeringExecutionEvidence records the bounded host-side execution loop
// without copying raw adapter output a second time. The final adapter artifact
// remains the primary result; prior attempts are represented by redacted,
// structured command outcomes and content hashes.
type engineeringExecutionEvidence struct {
	SchemaVersion                string                        `json:"schema_version"`
	Workflow                     []string                      `json:"workflow"`
	Inspection                   engineeringInspectionEvidence `json:"inspection"`
	PlanSHA256                   string                        `json:"plan_sha256"`
	AcceptanceSHA256             string                        `json:"acceptance_sha256"`
	MaxAttempts                  int                           `json:"max_attempts"`
	AutoRepairAllowed            bool                          `json:"auto_repair_allowed"`
	Resumed                      bool                          `json:"resumed"`
	ResumedFromCheckpoint        string                        `json:"resumed_from_checkpoint,omitempty"`
	Progress                     []EngineeringProgress         `json:"progress,omitempty"`
	ProgressDeliveryFailures     int                           `json:"progress_delivery_failures,omitempty"`
	ProgressDeliveryFailureClass string                        `json:"progress_delivery_failure_class,omitempty"`
	Attempts                     []engineeringAttemptEvidence  `json:"attempts"`
	Completed                    bool                          `json:"completed"`
	StoppedReason                string                        `json:"stopped_reason"`
}

type engineeringInspectionEvidence struct {
	Status               string   `json:"status"`
	ArtifactSHA256       string   `json:"artifact_sha256,omitempty"`
	WorkspaceFingerprint string   `json:"workspace_fingerprint,omitempty"`
	HeadSHA              string   `json:"head_sha,omitempty"`
	BaseSHA              string   `json:"base_sha,omitempty"`
	Dirty                bool     `json:"dirty"`
	Languages            []string `json:"languages,omitempty"`
	BuildSystems         []string `json:"build_systems,omitempty"`
}

type engineeringAttemptEvidence struct {
	Attempt        int                          `json:"attempt"`
	Status         string                       `json:"status"`
	ArtifactSHA256 string                       `json:"artifact_sha256,omitempty"`
	ResultSchema   string                       `json:"result_schema,omitempty"`
	Coding         *engineeringCommandEvidence  `json:"coding,omitempty"`
	Verification   []engineeringCommandEvidence `json:"verification,omitempty"`
	Diagnosis      engineeringAttemptDiagnosis  `json:"diagnosis"`
}

type engineeringCommandEvidence struct {
	ExitCode        int  `json:"exit_code"`
	Canceled        bool `json:"canceled"`
	TimedOut        bool `json:"timed_out"`
	OutputTruncated bool `json:"output_truncated"`
	TestFailures    int  `json:"test_failures,omitempty"`
	TestIncomplete  bool `json:"test_incomplete"`
}

type engineeringAttemptDiagnosis struct {
	Class   string `json:"class"`
	Summary string `json:"summary"`
}

func isEngineeringRepairAdapter(adapter string) bool {
	switch adapter {
	case "codex", "claude-code", "acpx":
		return true
	default:
		return false
	}
}

func engineeringTaskFromEnvelope(envelope taskEnvelope) (contracts.EngineeringTask, bool) {
	raw, ok := envelope.Payload["engineering_task"]
	if !ok || raw == nil {
		return contracts.EngineeringTask{}, false
	}
	switch task := raw.(type) {
	case contracts.EngineeringTask:
		return task, true
	case *contracts.EngineeringTask:
		if task == nil {
			return contracts.EngineeringTask{}, false
		}
		return *task, true
	default:
		decoded, err := contracts.DecodeEngineeringTask(raw)
		if err != nil {
			return contracts.EngineeringTask{}, false
		}
		return decoded, true
	}
}

func executeEngineeringTask(ctx context.Context, envelope taskEnvelope, captureRuntimeFixture bool, opts Options) (adapterExecution, error) {
	task, ok := engineeringTaskFromEnvelope(envelope)
	if !ok || !isEngineeringRepairAdapter(envelope.Adapter) {
		return executeJobAdapterWithToolchainRoot(ctx, envelope, captureRuntimeFixture, nil, opts.ToolchainRoot)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	maxAttempts := task.Limits.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	maxDuration := envelope.Limits.MaxDurationSeconds
	if maxDuration < 1 {
		maxDuration = model.DefaultTaskTTLSeconds
	}
	resumeStartAttempt := 1
	resumedFromCheckpoint := ""
	if opts.engineeringResume != nil {
		resumeStartAttempt = opts.engineeringResume.StartAttempt
		resumedFromCheckpoint = opts.engineeringResume.Checkpoint.ID
	}
	loopCtx, cancel := context.WithTimeout(ctx, time.Duration(maxDuration)*time.Second)
	defer cancel()

	evidence := engineeringExecutionEvidence{
		SchemaVersion:         EngineeringExecutionSchemaVersion,
		Workflow:              []string{EngineeringPhaseInspect, EngineeringPhasePlan, EngineeringPhaseEdit, EngineeringPhaseVerify, EngineeringPhaseDiagnose, EngineeringPhaseRepair, EngineeringPhaseFinalize},
		PlanSHA256:            engineeringStringsSHA256(task.Plan),
		AcceptanceSHA256:      engineeringStringsSHA256(task.Acceptance),
		MaxAttempts:           maxAttempts,
		AutoRepairAllowed:     len(envelope.InterruptsRequired) == 0,
		Resumed:               resumedFromCheckpoint != "",
		ResumedFromCheckpoint: resumedFromCheckpoint,
	}
	emitter, err := newEngineeringProgressEmitter(envelope, task, opts)
	if err != nil {
		evidence.StoppedReason = "checkpoint_unavailable"
		return adapterExecution{ArtifactContent: attachEngineeringExecutionEvidence("", evidence)}, err
	}
	if resumeStartAttempt < 1 || resumeStartAttempt > maxAttempts {
		evidence.StoppedReason = "resume_attempt_exhausted"
		return adapterExecution{ArtifactContent: attachEngineeringExecutionEvidence("", evidence)}, fmt.Errorf("engineering resume checkpoint has no remaining attempt budget")
	}
	var final adapterExecution
	stopForCheckpointError := func(checkpointErr error) (adapterExecution, error) {
		evidence.StoppedReason = "checkpoint_unavailable"
		evidence.Progress = emitter.Values()
		evidence.ProgressDeliveryFailures = emitter.DeliveryFailures()
		if evidence.ProgressDeliveryFailures > 0 {
			evidence.ProgressDeliveryFailureClass = EngineeringFailureTransportReconnect
		}
		final.ArtifactContent = attachEngineeringExecutionEvidence(final.ArtifactContent, evidence)
		return final, checkpointErr
	}
	emit := func(phase string, attempt int, summary string, refs []EngineeringArtifactReference, recoverable bool, failureClass string) error {
		if _, err := emitter.emit(loopCtx, phase, attempt, summary, refs, recoverable, failureClass); err != nil {
			return err
		}
		evidence.Progress = emitter.Values()
		evidence.ProgressDeliveryFailures = emitter.DeliveryFailures()
		if evidence.ProgressDeliveryFailures > 0 {
			evidence.ProgressDeliveryFailureClass = EngineeringFailureTransportReconnect
		}
		return nil
	}

	evidence.Inspection = inspectEngineeringWorkspace(loopCtx, envelope)
	inspectionSummary := "Host project context inspection completed."
	if evidence.Inspection.Status != "completed" {
		inspectionSummary = "Host project context inspection is unavailable; the coding adapter must inspect the declared workspace directly."
	}
	if err := emit(EngineeringPhaseInspect, 0, inspectionSummary, engineeringArtifactRef("project_context", evidence.Inspection.ArtifactSHA256), evidence.AutoRepairAllowed, ""); err != nil {
		return stopForCheckpointError(err)
	}
	if err := emit(EngineeringPhasePlan, 0, "The signed engineering plan and acceptance criteria were prepared for bounded execution.", append(engineeringArtifactRef("engineering_plan", evidence.PlanSHA256), engineeringArtifactRef("engineering_acceptance", evidence.AcceptanceSHA256)...), evidence.AutoRepairAllowed, ""); err != nil {
		return stopForCheckpointError(err)
	}

	var finalErr error
	var previous engineeringAttemptDiagnosis
	if opts.engineeringResume != nil {
		previous = engineeringResumeDiagnosis(opts.engineeringResume.Checkpoint)
	}
	for attempt := resumeStartAttempt; attempt <= maxAttempts; attempt++ {
		if err := loopCtx.Err(); err != nil {
			finalErr = err
			evidence.StoppedReason = engineeringStopReason(err)
			break
		}
		if err := emit(EngineeringPhaseEdit, attempt, "Coding adapter execution started inside the declared task workspace.", nil, attempt < maxAttempts && evidence.AutoRepairAllowed, ""); err != nil {
			return stopForCheckpointError(err)
		}
		attemptEnvelope := envelope
		attemptEnvelope.Payload = cloneMap(envelope.Payload)
		attemptEnvelope.Payload["prompt"] = engineeringAttemptPrompt(task, envelope, attempt, previous)
		attemptEnvelope.Limits = engineeringAttemptLimits(envelope.Limits, maxAttempts)

		execution, executionErr := executeJobAdapterWithToolchainRoot(loopCtx, attemptEnvelope, captureRuntimeFixture, nil, opts.ToolchainRoot)
		final = execution
		attemptEvidence := summarizeEngineeringAttempt(attempt, envelope.Adapter, execution.ArtifactContent, executionErr)
		evidence.Attempts = append(evidence.Attempts, attemptEvidence)
		failureClass := engineeringFailureClassFromAttempt(attemptEvidence)
		recoverable := retryableEngineeringAttempt(attemptEvidence) && evidence.AutoRepairAllowed && attempt < maxAttempts
		if err := emit(EngineeringPhaseVerify, attempt, attemptEvidence.Diagnosis.Summary, engineeringAttemptArtifactRefs(attemptEvidence), recoverable, failureClass); err != nil {
			return stopForCheckpointError(err)
		}
		if executionErr == nil {
			evidence.Completed = true
			evidence.StoppedReason = "verification_passed"
			finalErr = nil
			break
		}
		finalErr = executionErr
		previous = attemptEvidence.Diagnosis
		if err := emit(EngineeringPhaseDiagnose, attempt, attemptEvidence.Diagnosis.Summary, engineeringAttemptArtifactRefs(attemptEvidence), recoverable, failureClass); err != nil {
			return stopForCheckpointError(err)
		}
		if !retryableEngineeringAttempt(attemptEvidence) {
			evidence.StoppedReason = engineeringStopReasonForAttempt(attemptEvidence)
			break
		}
		if !evidence.AutoRepairAllowed {
			evidence.StoppedReason = "interrupt_required"
			break
		}
		if attempt == maxAttempts {
			evidence.StoppedReason = "attempt_limit_reached"
			break
		}
		if err := emit(EngineeringPhaseRepair, attempt+1, "A bounded repair attempt will review the existing isolated worktree and rerun only declared verification.", engineeringAttemptArtifactRefs(attemptEvidence), true, failureClass); err != nil {
			return stopForCheckpointError(err)
		}
	}
	if evidence.StoppedReason == "" {
		evidence.StoppedReason = "attempt_limit_reached"
	}
	finalFailureClass := engineeringFailureClassForExecution(evidence, finalErr)
	if err := emit(EngineeringPhaseFinalize, len(evidence.Attempts), "Engineering execution reached a bounded terminal result; workspace evidence and task result will be finalized next.", engineeringArtifactRef("adapter_result", final.ArtifactContent), false, finalFailureClass); err != nil {
		return stopForCheckpointError(err)
	}
	final.ArtifactContent = attachEngineeringExecutionEvidence(final.ArtifactContent, evidence)
	return final, finalErr
}

func engineeringAttemptLimits(limits model.TaskLimits, maxAttempts int) model.TaskLimits {
	out := limits
	if maxAttempts <= 1 {
		return out
	}
	if out.MaxDurationSeconds > 0 {
		out.MaxDurationSeconds = max(1, out.MaxDurationSeconds/maxAttempts)
	}
	if out.MaxOutputBytes > 0 {
		out.MaxOutputBytes = max(1, out.MaxOutputBytes/maxAttempts)
	}
	return out
}

// inspectEngineeringWorkspace uses the existing bounded, redacted project
// context implementation. It is host-side adapter preflight metadata, not a
// separate file-transfer action, and only its non-content summary is retained.
func inspectEngineeringWorkspace(ctx context.Context, envelope taskEnvelope) engineeringInspectionEvidence {
	maxDuration := envelope.Limits.MaxDurationSeconds
	if maxDuration <= 0 || maxDuration > 10 {
		maxDuration = 10
	}
	maxOutput := envelope.Limits.MaxOutputBytes
	if maxOutput <= 0 || maxOutput > 64*1024 {
		maxOutput = 64 * 1024
	}
	artifact, err := fileadapter.ExecuteContext(ctx, fileadapter.Spec{
		WorkspaceRoot:      envelope.Workspace.Root,
		ReadScope:          stringSliceValue(envelope.Payload, "read_scope"),
		Action:             "describe",
		AllowGitInspection: hasCapability(envelope.Capabilities, "git.diff"),
		MaxDurationSeconds: maxDuration,
		MaxOutputBytes:     maxOutput,
	})
	if err != nil || artifact.ProjectContext == nil {
		return engineeringInspectionEvidence{Status: "unavailable"}
	}
	content := artifact.ArtifactContent()
	sum := sha256.Sum256([]byte(content))
	evidence := engineeringInspectionEvidence{
		Status:               "completed",
		ArtifactSHA256:       "sha256:" + hex.EncodeToString(sum[:]),
		WorkspaceFingerprint: artifact.ProjectContext.Freshness.WorkspaceFingerprint,
	}
	if description := artifact.ProjectContext.Description; description != nil {
		evidence.HeadSHA = description.Git.HeadSHA
		evidence.BaseSHA = description.Git.BaseSHA
		evidence.Dirty = description.Git.Dirty
		evidence.Languages = append([]string(nil), description.Languages...)
		evidence.BuildSystems = append([]string(nil), description.BuildSystems...)
	}
	return evidence
}

func engineeringStringsSHA256(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(value), value)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func engineeringAttemptPrompt(task contracts.EngineeringTask, envelope taskEnvelope, attempt int, previous engineeringAttemptDiagnosis) string {
	var builder strings.Builder
	builder.WriteString("You are executing a bounded engineering task inside the declared workspace. ")
	builder.WriteString("First inspect the relevant project context, then follow the approved plan, edit only within the declared write scope, and run the declared verification commands. ")
	builder.WriteString("Do not perform network, credential, push, merge, deploy, publish, service, or other external-consequence actions unless the signed task separately authorizes them.\n\n")
	builder.WriteString("Goal:\n")
	builder.WriteString(task.Goal)
	builder.WriteString("\n\nApproved plan:\n")
	for _, step := range task.Plan {
		builder.WriteString("- ")
		builder.WriteString(step)
		builder.WriteByte('\n')
	}
	builder.WriteString("\nAcceptance criteria:\n")
	for _, criterion := range task.Acceptance {
		builder.WriteString("- ")
		builder.WriteString(criterion)
		builder.WriteByte('\n')
	}
	if additional := strings.TrimSpace(stringValue(envelope.Payload, "prompt", "")); additional != "" && additional != task.Goal {
		builder.WriteString("\nAdditional signed task instructions:\n")
		builder.WriteString(additional)
		builder.WriteByte('\n')
	}
	if attempt > 1 {
		builder.WriteString("\nThis is repair attempt ")
		builder.WriteString(fmt.Sprintf("%d of %d", attempt, task.Limits.MaxAttempts))
		builder.WriteString(". Review the existing worktree changes before editing again.\n")
		builder.WriteString("Previous structured diagnosis: ")
		builder.WriteString(previous.Summary)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func summarizeEngineeringAttempt(attempt int, adapter, artifactContent string, executionErr error) engineeringAttemptEvidence {
	evidence := engineeringAttemptEvidence{
		Attempt: attempt,
		Status:  "failed",
		Diagnosis: engineeringAttemptDiagnosis{
			Class:   "adapter_failed",
			Summary: "The coding adapter did not complete successfully; inspect the worktree and declared verification results before retrying.",
		},
	}
	if strings.TrimSpace(artifactContent) != "" {
		sum := sha256.Sum256([]byte(artifactContent))
		evidence.ArtifactSHA256 = "sha256:" + hex.EncodeToString(sum[:])
	}
	var artifact map[string]any
	if json.Unmarshal([]byte(artifactContent), &artifact) == nil {
		evidence.ResultSchema, _ = artifact["schema_version"].(string)
		codingKey := engineeringCodingCommandKey(adapter)
		if command, ok := engineeringCommandFromArtifact(artifact[codingKey]); ok {
			evidence.Coding = &command
		}
		if raw, ok := artifact["verification_results"].([]any); ok {
			for _, value := range raw {
				if command, ok := engineeringCommandFromArtifact(value); ok {
					evidence.Verification = append(evidence.Verification, command)
				}
			}
		}
	}
	if executionErr == nil {
		evidence.Status = "verified"
		evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "verified", Summary: "The coding adapter and every declared verification command completed successfully."}
		return evidence
	}
	if engineeringAttemptCanceled(evidence) {
		evidence.Status = "canceled"
		evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "canceled", Summary: "The task was canceled; no automatic repair attempt is permitted."}
		return evidence
	}
	if engineeringAttemptTimedOut(evidence) {
		evidence.Status = "timed_out"
		evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "timed_out", Summary: "The task exceeded its bounded execution time; no automatic repair attempt is permitted."}
		return evidence
	}
	if failed, policyFailure := engineeringVerificationFailure(evidence.Verification); failed {
		if policyFailure {
			evidence.Status = "policy_failed"
			evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "policy_failed", Summary: "A declared verification command was rejected by host policy; no automatic repair attempt is permitted."}
			return evidence
		}
		evidence.Status = "verification_failed"
		evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "verification_failed", Summary: "Declared verification failed. Inspect the failing command and repair the worktree without widening the task scope."}
		return evidence
	}
	if evidence.Coding != nil && evidence.Coding.ExitCode != 0 {
		if evidence.Coding.ExitCode < 0 {
			evidence.Status = "infrastructure_failed"
			evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "infrastructure_failed", Summary: "The coding adapter could not be started or produced no executable result; no automatic repair attempt is permitted."}
			return evidence
		}
		evidence.Status = "adapter_failed"
		evidence.Diagnosis = engineeringAttemptDiagnosis{Class: "adapter_failed", Summary: "The coding adapter exited unsuccessfully. Inspect the current worktree and repair only if the declared task still applies."}
	}
	return evidence
}

func engineeringCodingCommandKey(adapter string) string {
	switch adapter {
	case "claude-code":
		return "claude_code_command"
	case "acpx":
		return "acpx_command"
	default:
		return "codex_command"
	}
}

func engineeringCommandFromArtifact(value any) (engineeringCommandEvidence, bool) {
	command, ok := value.(map[string]any)
	if !ok {
		return engineeringCommandEvidence{}, false
	}
	exitCode, ok := engineeringInt(command["exit_code"])
	if !ok {
		return engineeringCommandEvidence{}, false
	}
	evidence := engineeringCommandEvidence{
		ExitCode:        exitCode,
		Canceled:        engineeringBool(command["canceled"]),
		TimedOut:        engineeringBool(command["timed_out"]),
		OutputTruncated: engineeringBool(command["output_truncated"]),
	}
	if report, ok := command["test_report"].(map[string]any); ok {
		if failed, ok := engineeringInt(report["failed"]); ok {
			evidence.TestFailures = failed
		}
		evidence.TestIncomplete = engineeringBool(report["incomplete"])
	}
	return evidence, true
}

func engineeringInt(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func engineeringBool(value any) bool {
	result, _ := value.(bool)
	return result
}

func engineeringAttemptCanceled(evidence engineeringAttemptEvidence) bool {
	if evidence.Coding != nil && evidence.Coding.Canceled {
		return true
	}
	for _, command := range evidence.Verification {
		if command.Canceled {
			return true
		}
	}
	return false
}

func engineeringAttemptTimedOut(evidence engineeringAttemptEvidence) bool {
	if evidence.Coding != nil && evidence.Coding.TimedOut {
		return true
	}
	for _, command := range evidence.Verification {
		if command.TimedOut {
			return true
		}
	}
	return false
}

func engineeringVerificationFailure(commands []engineeringCommandEvidence) (failed bool, policyFailure bool) {
	for _, command := range commands {
		if command.ExitCode == 0 {
			continue
		}
		return true, command.ExitCode < 0
	}
	return false, false
}

func retryableEngineeringAttempt(evidence engineeringAttemptEvidence) bool {
	return evidence.Status == "verification_failed" || evidence.Status == "adapter_failed"
}

func engineeringAttemptArtifactRefs(evidence engineeringAttemptEvidence) []EngineeringArtifactReference {
	return engineeringArtifactRef("adapter_result", evidence.ArtifactSHA256)
}

func engineeringFailureClassFromAttempt(evidence engineeringAttemptEvidence) string {
	switch evidence.Status {
	case "verified":
		return ""
	case "verification_failed":
		return EngineeringFailureCodeTest
	case "policy_failed":
		return EngineeringFailurePolicyDenial
	case "timed_out":
		return EngineeringFailureTimeout
	case "canceled":
		return EngineeringFailureCancellation
	case "infrastructure_failed", "adapter_failed":
		return EngineeringFailureEnvironmentTool
	default:
		return EngineeringFailureEnvironmentTool
	}
}

func engineeringFailureClassForExecution(evidence engineeringExecutionEvidence, executionErr error) string {
	if executionErr == nil {
		return ""
	}
	if len(evidence.Attempts) > 0 {
		return engineeringFailureClassFromAttempt(evidence.Attempts[len(evidence.Attempts)-1])
	}
	if executionErr == context.Canceled {
		return EngineeringFailureCancellation
	}
	if executionErr == context.DeadlineExceeded {
		return EngineeringFailureTimeout
	}
	return EngineeringFailureEnvironmentTool
}

func engineeringResumeDiagnosis(checkpoint EngineeringCheckpoint) engineeringAttemptDiagnosis {
	switch checkpoint.FailureClass {
	case EngineeringFailureCodeTest:
		return engineeringAttemptDiagnosis{Class: "verification_failed", Summary: "Resume checkpoint recorded a declared verification failure. Inspect the preserved worktree and repair only within the declared scope."}
	case EngineeringFailureEnvironmentTool:
		return engineeringAttemptDiagnosis{Class: "infrastructure_failed", Summary: "Resume checkpoint recorded an environment or tool failure. Reinspect the preserved worktree and task runtime before editing."}
	case EngineeringFailureTimeout:
		return engineeringAttemptDiagnosis{Class: "timed_out", Summary: "Resume checkpoint recorded a bounded timeout. Review the preserved worktree and use the remaining task budget carefully."}
	case EngineeringFailureTransportReconnect:
		return engineeringAttemptDiagnosis{Class: "transport_reconnect_failure", Summary: "Resume checkpoint was preserved across a transport interruption. Reinspect the existing worktree before continuing."}
	default:
		return engineeringAttemptDiagnosis{Class: "resume", Summary: "Resume checkpoint is active. Review the preserved worktree before continuing the bounded task."}
	}
}

func engineeringStopReasonForAttempt(evidence engineeringAttemptEvidence) string {
	switch evidence.Status {
	case "verified":
		return "verification_passed"
	case "canceled", "timed_out", "policy_failed", "infrastructure_failed":
		return evidence.Status
	default:
		return "non_retryable_failure"
	}
}

func engineeringStopReason(err error) string {
	if err == nil {
		return "verification_passed"
	}
	if err == context.Canceled {
		return "canceled"
	}
	if err == context.DeadlineExceeded {
		return "timed_out"
	}
	return "non_retryable_failure"
}

func attachEngineeringExecutionEvidence(content string, evidence engineeringExecutionEvidence) string {
	artifact := make(map[string]any)
	if strings.TrimSpace(content) != "" && json.Unmarshal([]byte(content), &artifact) == nil {
		artifact["engineering_execution"] = evidence
	} else {
		artifact = map[string]any{
			"schema_version":        "rdev.engineering-execution-result.v1",
			"engineering_execution": evidence,
		}
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return content
	}
	return string(encoded)
}
