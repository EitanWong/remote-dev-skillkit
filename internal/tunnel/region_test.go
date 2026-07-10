package tunnel

import (
	"testing"
	"time"
)

func validMainlandSamples() []NetworkSample {
	return []NetworkSample{
		{Carrier: "china-telecom", Region: "east-cn", Success: true},
		{Carrier: "china-telecom", Region: "west-cn", Success: true},
		{Carrier: "china-unicom", Region: "north-cn", Success: true},
		{Carrier: "china-unicom", Region: "south-cn", Success: true},
		{Carrier: "china-mobile", Region: "central-cn", Success: true},
		{Carrier: "china-mobile", Region: "coastal-cn", Success: true},
	}
}

func TestEvaluateEligibilityRequiresFreshMainlandEvidence(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	meta := ProviderMetadata{ID: "provider-a", DefaultAutomatic: true}
	tests := []struct {
		name     string
		evidence []RegionalEvidence
		want     bool
		reason   string
	}{
		{name: "missing", want: false, reason: "regional-evidence-missing"},
		{name: "unknown", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceUnknown, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()}}, want: false, reason: "regional-evidence-not-verified"},
		{name: "expired", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Second), Samples: validMainlandSamples()}}, want: false, reason: "regional-evidence-expired"},
		{name: "hard failed", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceBlocked, Issuer: "probe", ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()}}, want: false, reason: "regional-evidence-blocked"},
		{name: "verified", evidence: []RegionalEvidence{{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(6 * 24 * time.Hour), Samples: validMainlandSamples()}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateEligibility(meta, Policy{Region: RegionCNMainland, Now: now}, tt.evidence)
			if got.Eligible != tt.want || got.Reason != tt.reason {
				t.Fatalf("EvaluateEligibility() = %#v, want eligible=%v reason=%q", got, tt.want, tt.reason)
			}
		})
	}
}

func TestEvaluateEligibilityRejectsStaleFutureAndWrongRegionEvidence(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	meta := ProviderMetadata{ID: "provider-a", DefaultAutomatic: true}
	tests := []struct {
		name     string
		evidence RegionalEvidence
		reason   string
	}{
		{
			name:     "stale",
			evidence: RegionalEvidence{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-8 * 24 * time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()},
			reason:   "regional-evidence-invalid",
		},
		{
			name:     "future issued",
			evidence: RegionalEvidence{ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(time.Second), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()},
			reason:   "regional-evidence-not-yet-valid",
		},
		{
			name:     "wrong region",
			evidence: RegionalEvidence{ProviderID: "provider-a", Region: RegionGlobal, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()},
			reason:   "regional-evidence-missing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateEligibility(meta, Policy{Region: RegionCNMainland, Now: now}, []RegionalEvidence{tt.evidence})
			if got.Eligible || got.Reason != tt.reason {
				t.Fatalf("EvaluateEligibility() = %#v, want ineligible reason=%q", got, tt.reason)
			}
		})
	}
}

func TestRegionalEvidenceValidateRejectsTargetIPAndLongTTL(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	base := RegionalEvidence{
		ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "project-probe", ObservedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour),
		Samples: validMainlandSamples(),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid evidence: %v", err)
	}
	tooLong := base
	tooLong.ExpiresAt = now.Add(7*24*time.Hour + time.Second)
	if err := tooLong.Validate(); err == nil {
		t.Fatal("expected TTL rejection")
	}
	withIP := base
	withIP.Samples = []NetworkSample{{Carrier: "china-telecom", Region: "203.0.113.4", Success: true}}
	if err := withIP.Validate(); err == nil {
		t.Fatal("expected IP-like region rejection")
	}
}

func TestRegionalEvidenceValidateRequiresMainlandCarrierCoverage(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	base := RegionalEvidence{
		ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "project-probe", ObservedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		Samples: validMainlandSamples(),
	}

	tests := []struct {
		name   string
		mutate func([]NetworkSample) []NetworkSample
	}{
		{name: "missing carrier", mutate: func(samples []NetworkSample) []NetworkSample { return samples[:4] }},
		{name: "one region per carrier", mutate: func(samples []NetworkSample) []NetworkSample {
			return []NetworkSample{samples[0], samples[2], samples[4]}
		}},
		{name: "failed second region", mutate: func(samples []NetworkSample) []NetworkSample { samples[1].Success = false; return samples }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := base
			evidence.Samples = tt.mutate(validMainlandSamples())
			if err := evidence.Validate(); err == nil {
				t.Fatal("expected mainland carrier coverage rejection")
			}
		})
	}
}

func TestRegionalEvidenceValidateDoesNotCountWhitespaceVariantsAsDistinctRegions(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	evidence := RegionalEvidence{
		ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "project-probe", ObservedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		Samples: []NetworkSample{
			{Carrier: "china-telecom", Region: "east-cn", Success: true},
			{Carrier: "china-telecom", Region: "east-cn ", Success: true},
			{Carrier: "china-unicom", Region: "north-cn", Success: true},
			{Carrier: "china-unicom", Region: "north-cn ", Success: true},
			{Carrier: "china-mobile", Region: "central-cn", Success: true},
			{Carrier: "china-mobile", Region: "central-cn ", Success: true},
		},
	}
	if err := evidence.Validate(); err == nil {
		t.Fatal("expected whitespace variants to count as the same coarse region")
	}
}

func TestRegionalEvidenceValidateRejectsIPLiteralVariants(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	for _, region := range []string{"[2001:db8::1]", "fe80::1%en0", "203.0.113.4:443", "[2001:db8::1]:443"} {
		t.Run(region, func(t *testing.T) {
			evidence := RegionalEvidence{
				ProviderID: "provider-a", Region: RegionGlobal, Status: EvidenceVerified,
				Issuer: "project-probe", ObservedAt: now, ExpiresAt: now.Add(24 * time.Hour),
				Samples: []NetworkSample{{Carrier: "probe-network", Region: region, Success: true}},
			}
			if err := evidence.Validate(); err == nil {
				t.Fatalf("expected IP-like region %q to be rejected", region)
			}
		})
	}
}

func TestRegionalEvidenceValidateRejectsInvalidContracts(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	base := RegionalEvidence{
		ProviderID: "provider-a", Region: RegionGlobal, Status: EvidenceVerified,
		Issuer: "project-probe", ObservedAt: now, ExpiresAt: now.Add(time.Hour),
		Samples: []NetworkSample{{Carrier: "probe-network", Region: "east-cn", Success: true}},
	}
	tests := []struct {
		name   string
		mutate func(RegionalEvidence) RegionalEvidence
	}{
		{name: "empty provider ID", mutate: func(e RegionalEvidence) RegionalEvidence { e.ProviderID = " "; return e }},
		{name: "empty issuer", mutate: func(e RegionalEvidence) RegionalEvidence { e.Issuer = " "; return e }},
		{name: "unsupported region", mutate: func(e RegionalEvidence) RegionalEvidence { e.Region = "unsupported"; return e }},
		{name: "unsupported status", mutate: func(e RegionalEvidence) RegionalEvidence { e.Status = "unsupported"; return e }},
		{name: "zero observed timestamp", mutate: func(e RegionalEvidence) RegionalEvidence { e.ObservedAt = time.Time{}; return e }},
		{name: "zero expiry timestamp", mutate: func(e RegionalEvidence) RegionalEvidence { e.ExpiresAt = time.Time{}; return e }},
		{name: "reversed timestamps", mutate: func(e RegionalEvidence) RegionalEvidence { e.ExpiresAt = e.ObservedAt.Add(-time.Second); return e }},
		{name: "empty sample carrier", mutate: func(e RegionalEvidence) RegionalEvidence { e.Samples[0].Carrier = " "; return e }},
		{name: "empty sample region", mutate: func(e RegionalEvidence) RegionalEvidence { e.Samples[0].Region = " "; return e }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.mutate(cloneEvidence(base)).Validate(); err == nil {
				t.Fatal("expected invalid evidence to be rejected")
			}
		})
	}
}

func TestEvaluateEligibilityIgnoresWrongProviderEvidence(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	evidence := RegionalEvidence{
		ProviderID: "provider-b", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples(),
	}
	got := EvaluateEligibility(
		ProviderMetadata{ID: "provider-a", DefaultAutomatic: true},
		Policy{Region: RegionCNMainland, Now: now},
		[]RegionalEvidence{evidence},
	)
	if got.Eligible || got.Reason != "regional-evidence-missing" {
		t.Fatalf("EvaluateEligibility() = %#v, want wrong-provider evidence ignored", got)
	}
}

func TestEvaluateEligibilityClonesInputsAndResult(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	protocols := []string{"https"}
	meta := ProviderMetadata{ID: "provider-a", Protocols: protocols, DefaultAutomatic: true}
	samples := validMainlandSamples()
	evidence := []RegionalEvidence{{
		ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: samples,
	}}

	clonedMeta := cloneMetadata(meta)
	got := EvaluateEligibility(meta, Policy{Region: RegionCNMainland, Now: now}, evidence)
	protocols[0] = "mutated"
	samples[0].Carrier = "mutated"
	evidence[0].Samples[1].Region = "mutated"
	if clonedMeta.Protocols[0] != "https" {
		t.Fatalf("cloneMetadata retained input slice: %q", clonedMeta.Protocols[0])
	}
	if got.Evidence == nil || got.Evidence.Samples[0].Carrier != "china-telecom" || got.Evidence.Samples[1].Region != "west-cn" {
		t.Fatalf("EvaluateEligibility retained evidence input: %#v", got.Evidence)
	}

	clonedEvidence := cloneEvidence(*got.Evidence)
	got.Evidence.Samples[0].Region = "changed-return"
	if clonedEvidence.Samples[0].Region != "east-cn" {
		t.Fatalf("cloneEvidence retained returned slice: %q", clonedEvidence.Samples[0].Region)
	}
}

func TestEvaluateEligibilityGlobalPolicy(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		meta   ProviderMetadata
		policy Policy
		want   bool
		reason string
	}{
		{name: "default", meta: ProviderMetadata{ID: "provider-a", DefaultAutomatic: true}, policy: Policy{Region: RegionGlobal, Now: now}, want: true},
		{name: "non default denied", meta: ProviderMetadata{ID: "provider-a"}, policy: Policy{Region: RegionGlobal, Now: now}, reason: "provider-not-default"},
		{name: "non default allowed", meta: ProviderMetadata{ID: "provider-a"}, policy: Policy{Region: RegionGlobal, Now: now, AllowNonDefault: true}, want: true},
		{name: "not in allowlist", meta: ProviderMetadata{ID: "provider-a", DefaultAutomatic: true}, policy: Policy{Region: RegionGlobal, Now: now, AllowedProviderIDs: []string{"provider-b"}}, reason: "provider-not-allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateEligibility(tt.meta, tt.policy, nil)
			if got.Eligible != tt.want || got.Reason != tt.reason {
				t.Fatalf("EvaluateEligibility() = %#v, want eligible=%v reason=%q", got, tt.want, tt.reason)
			}
		})
	}
}
