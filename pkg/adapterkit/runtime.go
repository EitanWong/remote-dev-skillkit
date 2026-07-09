package adapterkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const RuntimeFixtureSchemaVersion = "rdev.adapter-runtime-fixture.v1"

const (
	PhaseDetect  = "detect"
	PhasePlan    = "plan"
	PhasePrepare = "prepare"
	PhaseRun     = "run"
	PhaseCollect = "collect"
	PhaseCleanup = "cleanup"
)

var RequiredLifecyclePhases = []string{PhaseDetect, PhasePlan, PhasePrepare, PhaseRun, PhaseCollect, PhaseCleanup}

type RuntimeRequest struct {
	Adapter        string         `json:"adapter"`
	TaskID         string         `json:"task_id,omitempty"`
	WorkspaceRoot  string         `json:"workspace_root,omitempty"`
	Intent         string         `json:"intent,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	Limits         RuntimeLimits  `json:"limits,omitempty"`
	Authorizations []string       `json:"authorizations,omitempty"`
}

type RuntimeLimits struct {
	MaxDurationSeconds int `json:"max_duration_seconds,omitempty"`
	MaxOutputBytes     int `json:"max_output_bytes,omitempty"`
}

type RuntimePhaseOutput struct {
	Evidence       []string        `json:"evidence,omitempty"`
	Detail         string          `json:"detail,omitempty"`
	ArtifactSchema string          `json:"artifact_schema,omitempty"`
	ResultArtifact json.RawMessage `json:"result_artifact,omitempty"`
}

type RuntimePhaseResult struct {
	Phase          string   `json:"phase"`
	OK             bool     `json:"ok"`
	Evidence       []string `json:"evidence,omitempty"`
	Detail         string   `json:"detail,omitempty"`
	Error          string   `json:"error,omitempty"`
	StartedAt      string   `json:"started_at"`
	EndedAt        string   `json:"ended_at"`
	DurationMillis int64    `json:"duration_millis"`
}

type RuntimeFixture struct {
	SchemaVersion        string               `json:"schema_version"`
	Adapter              string               `json:"adapter"`
	TaskID               string               `json:"task_id,omitempty"`
	WorkspaceRoot        string               `json:"workspace_root,omitempty"`
	StartedAt            string               `json:"started_at"`
	EndedAt              string               `json:"ended_at"`
	DurationMillis       int64                `json:"duration_millis"`
	Canceled             bool                 `json:"canceled"`
	TimedOut             bool                 `json:"timed_out"`
	CleanupAttempted     bool                 `json:"cleanup_attempted"`
	CleanupOK            bool                 `json:"cleanup_ok"`
	ResultArtifactSchema string               `json:"result_artifact_schema,omitempty"`
	ResultArtifact       json.RawMessage      `json:"result_artifact,omitempty"`
	Phases               []RuntimePhaseResult `json:"phases"`
}

type RuntimeRunnerOptions struct {
	CleanupTimeout time.Duration
}

type RuntimeAdapter interface {
	Detect(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	Plan(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	Prepare(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	Run(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	Collect(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	Cleanup(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
}

type RuntimeAdapterFuncs struct {
	DetectFunc  func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	PlanFunc    func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	PrepareFunc func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	RunFunc     func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	CollectFunc func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	CleanupFunc func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
}

func (f RuntimeAdapterFuncs) Detect(ctx context.Context, request RuntimeRequest) (RuntimePhaseOutput, error) {
	return callRuntimeFunc(f.DetectFunc, ctx, request, PhaseDetect)
}

func (f RuntimeAdapterFuncs) Plan(ctx context.Context, request RuntimeRequest) (RuntimePhaseOutput, error) {
	return callRuntimeFunc(f.PlanFunc, ctx, request, PhasePlan)
}

func (f RuntimeAdapterFuncs) Prepare(ctx context.Context, request RuntimeRequest) (RuntimePhaseOutput, error) {
	return callRuntimeFunc(f.PrepareFunc, ctx, request, PhasePrepare)
}

func (f RuntimeAdapterFuncs) Run(ctx context.Context, request RuntimeRequest) (RuntimePhaseOutput, error) {
	return callRuntimeFunc(f.RunFunc, ctx, request, PhaseRun)
}

func (f RuntimeAdapterFuncs) Collect(ctx context.Context, request RuntimeRequest) (RuntimePhaseOutput, error) {
	return callRuntimeFunc(f.CollectFunc, ctx, request, PhaseCollect)
}

func (f RuntimeAdapterFuncs) Cleanup(ctx context.Context, request RuntimeRequest) (RuntimePhaseOutput, error) {
	return callRuntimeFunc(f.CleanupFunc, ctx, request, PhaseCleanup)
}

func RunLifecycle(ctx context.Context, adapter RuntimeAdapter, request RuntimeRequest) (RuntimeFixture, error) {
	return RunLifecycleWithOptions(ctx, adapter, request, RuntimeRunnerOptions{})
}

func RunLifecycleWithOptions(ctx context.Context, adapter RuntimeAdapter, request RuntimeRequest, opts RuntimeRunnerOptions) (RuntimeFixture, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if adapter == nil {
		return RuntimeFixture{}, fmt.Errorf("adapter runtime is required")
	}
	if strings.TrimSpace(request.Adapter) == "" {
		return RuntimeFixture{}, fmt.Errorf("adapter is required")
	}
	started := time.Now().UTC()
	fixture := RuntimeFixture{
		SchemaVersion: RuntimeFixtureSchemaVersion,
		Adapter:       request.Adapter,
		TaskID:        request.TaskID,
		WorkspaceRoot: request.WorkspaceRoot,
		StartedAt:     started.Format(time.RFC3339Nano),
	}
	var lifecycleErr error
	cleanupNeeded := false
	for _, call := range []struct {
		phase string
		fn    func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)
	}{
		{PhaseDetect, adapter.Detect},
		{PhasePlan, adapter.Plan},
		{PhasePrepare, adapter.Prepare},
		{PhaseRun, adapter.Run},
		{PhaseCollect, adapter.Collect},
	} {
		if call.phase == PhasePrepare {
			cleanupNeeded = true
		}
		output, err := runRuntimePhase(ctx, &fixture, call.phase, request, call.fn)
		if len(output.ResultArtifact) > 0 {
			fixture.ResultArtifact = append(json.RawMessage(nil), output.ResultArtifact...)
			fixture.ResultArtifactSchema = output.ArtifactSchema
		}
		if err != nil {
			lifecycleErr = err
			markRuntimeError(&fixture, err)
			break
		}
	}
	if cleanupNeeded {
		fixture.CleanupAttempted = true
		cleanupCtx, cleanupCancel := cleanupContext(opts)
		cleanupOutput, cleanupErr := runRuntimePhase(cleanupCtx, &fixture, PhaseCleanup, request, adapter.Cleanup)
		cleanupCancel()
		fixture.CleanupOK = cleanupErr == nil
		if len(cleanupOutput.ResultArtifact) > 0 {
			fixture.ResultArtifact = append(json.RawMessage(nil), cleanupOutput.ResultArtifact...)
			fixture.ResultArtifactSchema = cleanupOutput.ArtifactSchema
		}
		if cleanupErr != nil {
			markRuntimeError(&fixture, cleanupErr)
			lifecycleErr = errors.Join(lifecycleErr, cleanupErr)
		}
	}
	ended := time.Now().UTC()
	fixture.EndedAt = ended.Format(time.RFC3339Nano)
	fixture.DurationMillis = ended.Sub(started).Milliseconds()
	return fixture, lifecycleErr
}

func (f RuntimeFixture) JSON() ([]byte, error) {
	return json.MarshalIndent(f, "", "  ")
}

type RuntimeFixtureContract struct {
	Adapter               string
	RequiredPhases        []string
	RequireSuccessful     bool
	RequireCleanup        bool
	RequireResultArtifact bool
	RequireCancellation   bool
}

func VerifyRuntimeFixtureJSON(content []byte, contract RuntimeFixtureContract) ConformanceReport {
	if len(contract.RequiredPhases) == 0 {
		contract.RequiredPhases = append([]string(nil), RequiredLifecyclePhases...)
	}
	report := ConformanceReport{
		SchemaVersion:  ConformanceReportSchemaVersion,
		Adapter:        contract.Adapter,
		ArtifactSchema: RuntimeFixtureSchemaVersion,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	var fixture map[string]any
	if err := json.Unmarshal(content, &fixture); err != nil {
		add("json_valid", false, err.Error())
		report.OK = report.allChecksPassed()
		return report
	}
	add("json_valid", true, "")
	add("schema_version", stringField(fixture, "schema_version") == RuntimeFixtureSchemaVersion, stringField(fixture, "schema_version"))
	add("adapter", stringField(fixture, "adapter") == contract.Adapter, stringField(fixture, "adapter"))
	add("started_at_valid", validRFC3339(stringField(fixture, "started_at")), stringField(fixture, "started_at"))
	add("ended_at_valid", validRFC3339(stringField(fixture, "ended_at")), stringField(fixture, "ended_at"))
	duration, durationOK := numericField(fixture, "duration_millis")
	add("duration_millis_nonnegative", durationOK && duration >= 0, numericDetail(duration, durationOK))
	phases, phasesOK := fixture["phases"].([]any)
	add("phases_array", phasesOK && len(phases) > 0, fmt.Sprintf("%d", len(phases)))
	phaseMap := map[string]map[string]any{}
	phaseOrder := make([]string, 0, len(phases))
	for _, raw := range phases {
		phase, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := stringField(phase, "phase")
		if name == "" {
			continue
		}
		phaseMap[name] = phase
		phaseOrder = append(phaseOrder, name)
	}
	add("phase_order", expectedPhaseOrder(phaseOrder, contract.RequiredPhases), strings.Join(phaseOrder, ","))
	for _, required := range contract.RequiredPhases {
		phase, ok := phaseMap[required]
		add("phase_present:"+required, ok, "")
		if !ok {
			continue
		}
		if contract.RequireSuccessful {
			add("phase_ok:"+required, boolFieldEquals(phase, "ok", true), "")
		}
		add("phase_evidence:"+required, nonEmptyStringArrayField(phase, "evidence"), strings.Join(stringArrayField(phase, "evidence"), ","))
		add("phase_started_at_valid:"+required, validRFC3339(stringField(phase, "started_at")), stringField(phase, "started_at"))
		add("phase_ended_at_valid:"+required, validRFC3339(stringField(phase, "ended_at")), stringField(phase, "ended_at"))
		phaseDuration, ok := numericField(phase, "duration_millis")
		add("phase_duration_nonnegative:"+required, ok && phaseDuration >= 0, numericDetail(phaseDuration, ok))
	}
	if contract.RequireCleanup {
		add("cleanup_attempted", boolFieldEquals(fixture, "cleanup_attempted", true), "")
		add("cleanup_ok", boolFieldEquals(fixture, "cleanup_ok", true), "")
	}
	if contract.RequireResultArtifact {
		add("result_artifact_schema", strings.TrimSpace(stringField(fixture, "result_artifact_schema")) != "", stringField(fixture, "result_artifact_schema"))
		_, artifactOK := objectField(fixture, "result_artifact")
		add("result_artifact_object", artifactOK, "")
	}
	if contract.RequireCancellation {
		add("fixture_canceled", boolFieldEquals(fixture, "canceled", true), "")
		add("fixture_not_timed_out", boolFieldEquals(fixture, "timed_out", false), "")
		add("cleanup_after_cancel", boolFieldEquals(fixture, "cleanup_attempted", true) && boolFieldEquals(fixture, "cleanup_ok", true), "")
	}
	report.OK = report.allChecksPassed()
	return report
}

func callRuntimeFunc(fn func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error), ctx context.Context, request RuntimeRequest, phase string) (RuntimePhaseOutput, error) {
	if fn == nil {
		return RuntimePhaseOutput{}, fmt.Errorf("%s phase is not implemented", phase)
	}
	return fn(ctx, request)
}

func runRuntimePhase(ctx context.Context, fixture *RuntimeFixture, phase string, request RuntimeRequest, fn func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error)) (RuntimePhaseOutput, error) {
	started := time.Now().UTC()
	output, err := fn(ctx, request)
	ended := time.Now().UTC()
	result := RuntimePhaseResult{
		Phase:          phase,
		OK:             err == nil,
		Evidence:       append([]string(nil), output.Evidence...),
		Detail:         output.Detail,
		StartedAt:      started.Format(time.RFC3339Nano),
		EndedAt:        ended.Format(time.RFC3339Nano),
		DurationMillis: ended.Sub(started).Milliseconds(),
	}
	if err != nil {
		result.Error = err.Error()
	}
	fixture.Phases = append(fixture.Phases, result)
	return output, err
}

func cleanupContext(opts RuntimeRunnerOptions) (context.Context, context.CancelFunc) {
	timeout := opts.CleanupTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func markRuntimeError(fixture *RuntimeFixture, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		fixture.Canceled = true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fixture.TimedOut = true
	}
}

func expectedPhaseOrder(actual, expected []string) bool {
	if len(actual) < len(expected) {
		return false
	}
	for i, phase := range expected {
		if i >= len(actual) || actual[i] != phase {
			return false
		}
	}
	return true
}
