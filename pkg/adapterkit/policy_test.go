package adapterkit

import (
	"encoding/json"
	"testing"
	"time"
)

var policyNow = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

func TestNewPolicyPlanRequiresAdapter(t *testing.T) {
	_, err := NewPolicyPlan("", "", nil, nil, nil, nil, 0, policyNow)
	if err == nil {
		t.Fatal("expected error for missing adapter")
	}
}

func TestNewPolicyPlanSetsSchemaAndTimestamp(t *testing.T) {
	plan, err := NewPolicyPlan("my-adapter", "job_1", []string{"network call"}, []string{"requires approval"}, nil, nil, 60, policyNow)
	if err != nil {
		t.Fatalf("NewPolicyPlan: %v", err)
	}
	if plan.SchemaVersion != PolicyPlanSchemaVersion {
		t.Fatalf("schema = %q", plan.SchemaVersion)
	}
	if plan.Adapter != "my-adapter" {
		t.Fatalf("adapter = %q", plan.Adapter)
	}
	if plan.GeneratedAt == "" {
		t.Fatal("generated_at must be set")
	}
	if len(plan.ExternalConsequences) != 1 {
		t.Fatalf("expected 1 consequence, got %d", len(plan.ExternalConsequences))
	}
	if len(plan.RequiredApprovals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(plan.RequiredApprovals))
	}
}

func TestNewPolicyPlanStripsBlankEntries(t *testing.T) {
	plan, err := NewPolicyPlan("my-adapter", "", []string{"  ", "network call", ""}, nil, nil, nil, 0, policyNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ExternalConsequences) != 1 || plan.ExternalConsequences[0] != "network call" {
		t.Fatalf("unexpected consequences: %v", plan.ExternalConsequences)
	}
	if len(plan.RequiredApprovals) != 0 {
		t.Fatalf("expected no approvals, got %v", plan.RequiredApprovals)
	}
}

func TestVerifyPolicyPlanJSONAcceptsValidPlan(t *testing.T) {
	plan, err := NewPolicyPlan("my-adapter", "job_1", []string{"network call"}, []string{"approval needed"}, []string{"/workspace"}, nil, 60, policyNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyPolicyPlanJSON(content, PolicyPlanContract{
		Adapter:                     "my-adapter",
		RequireExternalConsequences: true,
		RequireApprovals:            true,
		RequireWorkspaceBoundaries:  true,
	})
	if !report.OK {
		t.Fatalf("expected OK, checks: %#v", report.Checks)
	}
}

func TestVerifyPolicyPlanJSONRejectsWrongAdapter(t *testing.T) {
	plan, err := NewPolicyPlan("my-adapter", "", nil, nil, nil, nil, 0, policyNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyPolicyPlanJSON(content, PolicyPlanContract{Adapter: "other-adapter"})
	if report.OK {
		t.Fatal("expected failure for wrong adapter")
	}
}

func TestVerifyPolicyPlanJSONFailsWhenConsequencesMissing(t *testing.T) {
	plan, err := NewPolicyPlan("my-adapter", "", nil, nil, nil, nil, 0, policyNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyPolicyPlanJSON(content, PolicyPlanContract{
		Adapter:                     "my-adapter",
		RequireExternalConsequences: true,
	})
	if report.OK {
		t.Fatal("expected failure when external consequences required but absent")
	}
}

func TestVerifyPolicyPlanJSONPassesWhenConsequencesNotRequired(t *testing.T) {
	plan, err := NewPolicyPlan("my-adapter", "", nil, nil, nil, nil, 0, policyNow)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	// RequireExternalConsequences=false, so empty is OK.
	report := VerifyPolicyPlanJSON(content, PolicyPlanContract{Adapter: "my-adapter"})
	if !report.OK {
		t.Fatalf("expected OK, checks: %#v", report.Checks)
	}
}

func TestVerifyPolicyPlanJSONRejectsInvalidJSON(t *testing.T) {
	report := VerifyPolicyPlanJSON([]byte("{bad json"), PolicyPlanContract{Adapter: "x"})
	if report.OK {
		t.Fatal("expected failure for invalid JSON")
	}
}
