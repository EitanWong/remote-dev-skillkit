package cdnopt

import "strings"

const PlanSchemaVersion = "rdev.cdn-optimizer-plan.v1"

type Options struct {
	Provider       string
	MaxConcurrency int
	SampleBytes    int
	TimeoutSeconds int
}

type Plan struct {
	SchemaVersion          string   `json:"schema_version"`
	Provider               string   `json:"provider"`
	Status                 string   `json:"status"`
	DryRun                 bool     `json:"dry_run"`
	EnabledByDefault       bool     `json:"enabled_by_default"`
	RequiresExplicitEnable bool     `json:"requires_explicit_enable"`
	AssetDownloadsOnly     bool     `json:"asset_downloads_only"`
	MaxConcurrency         int      `json:"max_concurrency"`
	SampleBytes            int      `json:"sample_bytes"`
	TimeoutSeconds         int      `json:"timeout_seconds"`
	CandidateSources       []string `json:"candidate_sources"`
	ForbiddenSideEffects   []string `json:"forbidden_side_effects"`
	SafetyRules            []string `json:"safety_rules"`
	AgentRule              string   `json:"agent_rule"`
}

func BuildPlan(opts Options) Plan {
	provider := strings.ToLower(strings.TrimSpace(opts.Provider))
	if provider == "" {
		provider = "cloudflare"
	}
	return Plan{
		SchemaVersion:          PlanSchemaVersion,
		Provider:               provider,
		Status:                 "dry-run-only",
		DryRun:                 true,
		EnabledByDefault:       false,
		RequiresExplicitEnable: true,
		AssetDownloadsOnly:     true,
		MaxConcurrency:         bounded(opts.MaxConcurrency, 8, 1, 16),
		SampleBytes:            bounded(opts.SampleBytes, 128*1024, 16*1024, 256*1024),
		TimeoutSeconds:         bounded(opts.TimeoutSeconds, 20, 5, 30),
		CandidateSources: []string{
			"signed helper asset mirrors from join manifest/package catalog",
			"operator-authorized CDN candidate list",
			"future Cloudflare IP range source after explicit enablement",
		},
		ForbiddenSideEffects: []string{
			"do not change DNS settings",
			"do not write hosts files",
			"do not change proxy settings",
			"do not change firewall settings",
			"do not change route tables",
			"do not install services, drivers, or hidden persistence",
		},
		SafetyRules: []string{
			"optimizer is for verified core asset downloads only and is initiated by rdev-bootstrap",
			"keep TLS SNI and HTTP Host bound to the original signed asset host",
			"use low bounded concurrency and small byte-range samples",
			"record selected candidate and timing evidence in preconnect status before rdev-bootstrap downloads the signed core runtime",
			"fall back to normal signed mirror order when optimization fails",
		},
		AgentRule: "Treat this as a future explicit optimizer gate, not permission to mutate system networking or bypass local policy.",
	}
}

func bounded(value, fallback, min, max int) int {
	if value <= 0 {
		value = fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
