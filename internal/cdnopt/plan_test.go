package cdnopt

import (
	"strings"
	"testing"
)

func TestPlanDefaultsAreSafeAndDryRunOnly(t *testing.T) {
	plan := BuildPlan(Options{})

	if plan.SchemaVersion != PlanSchemaVersion ||
		plan.Provider != "cloudflare" ||
		plan.Status != "dry-run-only" ||
		plan.EnabledByDefault ||
		!plan.RequiresExplicitEnable ||
		!plan.AssetDownloadsOnly ||
		!plan.DryRun {
		t.Fatalf("unexpected safe default plan: %#v", plan)
	}
	if plan.MaxConcurrency <= 0 || plan.MaxConcurrency > 16 {
		t.Fatalf("expected bounded low concurrency, got %d", plan.MaxConcurrency)
	}
	if plan.SampleBytes <= 0 || plan.SampleBytes > 256*1024 {
		t.Fatalf("expected small sample bytes, got %d", plan.SampleBytes)
	}
	for _, forbidden := range []string{"DNS", "hosts", "proxy", "firewall", "route"} {
		if !containsText(plan.ForbiddenSideEffects, forbidden) {
			t.Fatalf("expected forbidden side effect mentioning %q, got %#v", forbidden, plan.ForbiddenSideEffects)
		}
	}
}

func TestPlanNormalizesUnsafeInputsToSafeBounds(t *testing.T) {
	plan := BuildPlan(Options{
		Provider:       "cloudflare",
		MaxConcurrency: 500,
		SampleBytes:    8 * 1024 * 1024,
		TimeoutSeconds: 600,
	})

	if plan.MaxConcurrency != 16 {
		t.Fatalf("expected concurrency capped at 16, got %d", plan.MaxConcurrency)
	}
	if plan.SampleBytes != 256*1024 {
		t.Fatalf("expected sample bytes capped at 256KiB, got %d", plan.SampleBytes)
	}
	if plan.TimeoutSeconds != 30 {
		t.Fatalf("expected timeout capped at 30 seconds, got %d", plan.TimeoutSeconds)
	}
	if !containsText(plan.SafetyRules, "asset downloads only") ||
		!containsText(plan.SafetyRules, "SNI") ||
		!containsText(plan.SafetyRules, "rdev-bootstrap") ||
		containsText(plan.SafetyRules, "full helper") {
		t.Fatalf("expected asset-only and SNI safety rules, got %#v", plan.SafetyRules)
	}
}

func containsText(values []string, needle string) bool {
	needle = strings.ToLower(needle)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}
