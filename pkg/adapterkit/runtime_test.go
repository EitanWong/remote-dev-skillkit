package adapterkit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestRunLifecycleProducesVerifiableRuntimeFixture(t *testing.T) {
	var calls []string
	adapter := lifecycleTestAdapter(&calls, nil)
	fixture, err := RunLifecycle(context.Background(), adapter, RuntimeRequest{
		Adapter:       "fake",
		TaskID:        "task_123",
		WorkspaceRoot: "/tmp/repo",
		Intent:        "exercise runtime sdk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, RequiredLifecyclePhases) {
		t.Fatalf("unexpected lifecycle order: %#v", calls)
	}
	if !fixture.CleanupAttempted || !fixture.CleanupOK {
		t.Fatalf("expected cleanup success, got %#v", fixture)
	}
	if fixture.ResultArtifactSchema != "rdev.fake-result.v1" || len(fixture.ResultArtifact) == 0 {
		t.Fatalf("expected collected result artifact, got %#v", fixture)
	}
	content, err := fixture.JSON()
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyRuntimeFixtureJSON(content, RuntimeFixtureContract{
		Adapter:               "fake",
		RequireSuccessful:     true,
		RequireCleanup:        true,
		RequireResultArtifact: true,
	})
	if !report.OK {
		t.Fatalf("expected runtime fixture conformance success, got %#v\n%s", report, string(content))
	}
}

func TestRunLifecycleCleansUpAfterRunCancellation(t *testing.T) {
	var calls []string
	adapter := lifecycleTestAdapter(&calls, map[string]error{
		PhaseRun: context.Canceled,
	})
	fixture, err := RunLifecycle(context.Background(), adapter, RuntimeRequest{Adapter: "fake"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled lifecycle error, got %v", err)
	}
	if !fixture.Canceled || fixture.TimedOut {
		t.Fatalf("expected canceled fixture without timeout, got %#v", fixture)
	}
	if !fixture.CleanupAttempted || !fixture.CleanupOK {
		t.Fatalf("expected cleanup after canceled run, got %#v", fixture)
	}
	expected := []string{PhaseDetect, PhasePlan, PhasePrepare, PhaseRun, PhaseCleanup}
	if !reflect.DeepEqual(calls, expected) {
		t.Fatalf("unexpected lifecycle order after cancel: %#v", calls)
	}
	content, err := fixture.JSON()
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyRuntimeFixtureJSON(content, RuntimeFixtureContract{
		Adapter:             "fake",
		RequiredPhases:      []string{PhaseDetect, PhasePlan, PhasePrepare, PhaseRun, PhaseCleanup},
		RequireCleanup:      true,
		RequireCancellation: true,
	})
	if !report.OK {
		t.Fatalf("expected cancellation fixture conformance success, got %#v\n%s", report, string(content))
	}
}

func TestVerifyRuntimeFixtureJSONRejectsMissingCleanup(t *testing.T) {
	content := []byte(`{
  "schema_version": "rdev.adapter-runtime-fixture.v1",
  "adapter": "fake",
  "started_at": "2026-06-30T00:00:00Z",
  "ended_at": "2026-06-30T00:00:01Z",
  "duration_millis": 1000,
  "canceled": false,
  "timed_out": false,
  "cleanup_attempted": false,
  "cleanup_ok": false,
  "phases": [
    {"phase": "detect", "ok": true, "evidence": ["version"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "plan", "ok": true, "evidence": ["commands"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "prepare", "ok": true, "evidence": ["workspace"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "run", "ok": true, "evidence": ["process"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0},
    {"phase": "collect", "ok": true, "evidence": ["result"], "started_at": "2026-06-30T00:00:00Z", "ended_at": "2026-06-30T00:00:00Z", "duration_millis": 0}
  ]
}`)
	report := VerifyRuntimeFixtureJSON(content, RuntimeFixtureContract{
		Adapter:           "fake",
		RequireSuccessful: true,
		RequireCleanup:    true,
	})
	if report.OK {
		t.Fatalf("expected runtime fixture conformance failure, got %#v", report)
	}
}

func lifecycleTestAdapter(calls *[]string, phaseErrors map[string]error) RuntimeAdapter {
	output := func(phase string) (RuntimePhaseOutput, error) {
		*calls = append(*calls, phase)
		if err := phaseErrors[phase]; err != nil {
			return RuntimePhaseOutput{Evidence: []string{phase + "-evidence"}}, err
		}
		out := RuntimePhaseOutput{Evidence: []string{phase + "-evidence"}, Detail: phase + " ok"}
		if phase == PhaseCollect {
			artifact, _ := json.Marshal(map[string]any{
				"schema_version": "rdev.fake-result.v1",
				"adapter":        "fake",
				"workspace_root": "/tmp/repo",
			})
			out.ArtifactSchema = "rdev.fake-result.v1"
			out.ResultArtifact = artifact
		}
		return out, nil
	}
	return RuntimeAdapterFuncs{
		DetectFunc:  func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error) { return output(PhaseDetect) },
		PlanFunc:    func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error) { return output(PhasePlan) },
		PrepareFunc: func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error) { return output(PhasePrepare) },
		RunFunc:     func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error) { return output(PhaseRun) },
		CollectFunc: func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error) { return output(PhaseCollect) },
		CleanupFunc: func(context.Context, RuntimeRequest) (RuntimePhaseOutput, error) { return output(PhaseCleanup) },
	}
}
