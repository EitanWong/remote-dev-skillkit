package connectionhealth

import (
	"testing"
	"time"
)

func TestPlanDeduplicatesCandidatesAndPreservesOrder(t *testing.T) {
	plan := NewPlan([]Candidate{
		{URL: " https://primary.example.test/rdev ", Kind: "host", Priority: 20},
		{URL: "https://relay.example.test/rdev", Kind: "relay", Priority: 10},
		{URL: "https://primary.example.test/rdev", Kind: "duplicate", Priority: 1},
		{URL: " ", Kind: "empty", Priority: 0},
	})

	if plan.SchemaVersion != PlanSchemaVersion {
		t.Fatalf("unexpected schema version %q", plan.SchemaVersion)
	}
	if len(plan.Candidates) != 2 {
		t.Fatalf("expected two unique candidates, got %#v", plan.Candidates)
	}
	if plan.Candidates[0].URL != "https://primary.example.test/rdev" ||
		plan.Candidates[0].Kind != "host" ||
		plan.Candidates[1].URL != "https://relay.example.test/rdev" ||
		plan.Candidates[1].Kind != "relay" {
		t.Fatalf("expected candidate order to be preserved, got %#v", plan.Candidates)
	}
	if plan.Status != "planned" ||
		plan.AgentNextAction != "try the first reachable gateway candidate" {
		t.Fatalf("expected planned status, got %#v", plan)
	}
}

func TestPlanReportsGatewaySwitchingBeforeSuccess(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	plan := NewPlan([]Candidate{
		{URL: "https://primary.example.test/rdev", Kind: "host"},
		{URL: "https://relay.example.test/rdev", Kind: "relay"},
	}).WithAttempt(Attempt{
		Phase: "register",
		URL:   "https://primary.example.test/rdev",
		OK:    false,
		Error: "connect timeout",
		At:    now,
	})

	if plan.Status != "gateway-switching" {
		t.Fatalf("expected gateway-switching, got %#v", plan)
	}
	if plan.SelectedGatewayURL != "" {
		t.Fatalf("expected no selected gateway before success, got %q", plan.SelectedGatewayURL)
	}
	if plan.AgentNextAction != "continue with the next signed gateway candidate" {
		t.Fatalf("unexpected next action %q", plan.AgentNextAction)
	}
}

func TestPlanReportsRecoveredAfterFailureThenSuccess(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	plan := NewPlan([]Candidate{
		{URL: "https://primary.example.test/rdev", Kind: "host"},
		{URL: "https://relay.example.test/rdev", Kind: "relay"},
	}).WithAttempt(Attempt{
		Phase: "register",
		URL:   "https://primary.example.test/rdev",
		OK:    false,
		Error: "502 Bad Gateway",
		At:    now,
	}).WithAttempt(Attempt{
		Phase: "register",
		URL:   "https://relay.example.test/rdev",
		OK:    true,
		At:    now.Add(time.Second),
	})

	if plan.Status != "recovered" {
		t.Fatalf("expected recovered, got %#v", plan)
	}
	if plan.SelectedGatewayURL != "https://relay.example.test/rdev" {
		t.Fatalf("expected relay gateway selection, got %q", plan.SelectedGatewayURL)
	}
	if plan.AgentNextAction != "connection recovered; continue normal session join or task transport" {
		t.Fatalf("unexpected next action %q", plan.AgentNextAction)
	}
}
