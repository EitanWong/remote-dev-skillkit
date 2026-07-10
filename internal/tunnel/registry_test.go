package tunnel

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

type registryFakeProvider struct {
	id       string
	meta     ProviderMetadata
	startErr error
}

func newRegistryFakeProvider(id string, automatic bool) registryFakeProvider {
	return registryFakeProvider{
		id: id,
		meta: ProviderMetadata{
			ID:               id,
			DisplayName:      strings.ToUpper(id),
			Protocols:        []string{"https"},
			DefaultAutomatic: automatic,
		},
	}
}

func (p registryFakeProvider) ID() string { return p.id }

func (p registryFakeProvider) Metadata() ProviderMetadata { return p.meta }

func (p registryFakeProvider) Start(context.Context, StartRequest) (Handle, error) {
	return nil, p.startErr
}

func TestRegistryRejectsInvalidProviders(t *testing.T) {
	tests := []struct {
		name      string
		providers []Provider
	}{
		{name: "nil", providers: []Provider{nil}},
		{name: "empty ID", providers: []Provider{newRegistryFakeProvider("", true)}},
		{name: "duplicate ID", providers: []Provider{newRegistryFakeProvider("same", true), newRegistryFakeProvider("same", false)}},
		{name: "metadata mismatch", providers: []Provider{registryFakeProvider{id: "one", meta: ProviderMetadata{ID: "two"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRegistry(tt.providers...); err == nil {
				t.Fatal("expected invalid provider registry to be rejected")
			}
		})
	}
}

func TestRegistrySelectUsesDeterministicPolicyOrder(t *testing.T) {
	r, err := NewRegistry(
		newRegistryFakeProvider("zeta", true),
		newRegistryFakeProvider("beta", false),
		newRegistryFakeProvider("alpha", true),
	)
	if err != nil {
		t.Fatal(err)
	}

	got := r.Select(Policy{Region: RegionGlobal, AllowNonDefault: true}, nil)
	if len(got) != 3 {
		t.Fatalf("got %d selections, want 3", len(got))
	}
	want := []string{"alpha", "zeta", "beta"}
	for i := range want {
		if got[i].Provider.ID() != want[i] {
			t.Fatalf("selection %d = %q, want %q; full selection %#v", i, got[i].Provider.ID(), want[i], got)
		}
	}

	allowed := r.Select(Policy{Region: RegionGlobal, AllowNonDefault: true, AllowedProviderIDs: []string{"beta"}}, nil)
	if len(allowed) != 1 || allowed[0].Provider.ID() != "beta" {
		t.Fatalf("allowlist selection = %#v, want beta only", allowed)
	}
}

func TestRegistrySelectPrioritizesFreshVerifiedEvidence(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	r, err := NewRegistry(
		newRegistryFakeProvider("automatic", true),
		newRegistryFakeProvider("verified", false),
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence := []RegionalEvidence{{
		ProviderID: "verified", Region: RegionGlobal, Status: EvidenceVerified,
		Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Samples: []NetworkSample{{Carrier: "probe-network", Region: "global", Success: true}},
	}}
	got := r.Select(Policy{Region: RegionGlobal, Now: now, AllowNonDefault: true}, evidence)
	if len(got) != 2 || got[0].Provider.ID() != "verified" {
		t.Fatalf("fresh verified provider was not prioritized: %#v", got)
	}
}

func TestRegistrySelectMainlandUsesFreshEvidenceOnly(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	r, err := NewRegistry(newRegistryFakeProvider("cloudflare", true), newRegistryFakeProvider("cpolar", false))
	if err != nil {
		t.Fatal(err)
	}
	got := r.Select(Policy{Region: RegionCNMainland, Now: now, AllowNonDefault: true}, []RegionalEvidence{
		{ProviderID: "cpolar", Region: RegionCNMainland, Status: EvidenceVerified, Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples()},
	})
	if len(got) != 1 || got[0].Provider.ID() != "cpolar" {
		t.Fatalf("unexpected selection: %#v", got)
	}
}

func TestRegistryReturnedSlicesAreIndependent(t *testing.T) {
	provider := newRegistryFakeProvider("provider-a", true)
	r, err := NewRegistry(provider)
	if err != nil {
		t.Fatal(err)
	}

	providers := r.Providers()
	providers[0].ID = "mutated"
	providers[0].Protocols[0] = "mutated"
	providersAgain := r.Providers()
	if providersAgain[0].ID != "provider-a" || providersAgain[0].Protocols[0] != "https" {
		t.Fatalf("Providers retained returned data: %#v", providersAgain)
	}

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	evidence := []RegionalEvidence{{
		ProviderID: "provider-a", Region: RegionCNMainland, Status: EvidenceVerified,
		Issuer: "probe", ObservedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Samples: validMainlandSamples(),
	}}
	selected := r.Select(Policy{Region: RegionCNMainland, Now: now}, evidence)
	selected[0].Metadata.Protocols[0] = "mutated"
	selected[0].Eligibility.Evidence.Samples[0].Carrier = "mutated"
	selectedAgain := r.Select(Policy{Region: RegionCNMainland, Now: now}, evidence)
	if selectedAgain[0].Metadata.Protocols[0] != "https" || selectedAgain[0].Eligibility.Evidence.Samples[0].Carrier != "china-telecom" {
		t.Fatalf("Select retained returned data: %#v", selectedAgain)
	}
}

func TestCanonicalProviderIDsAreFixedAndReturnedAsCopy(t *testing.T) {
	got := CanonicalProviderIDs()
	want := []string{"cloudflare-quick", "localhost-run", "pinggy"}
	if !slices.Equal(got, want) {
		t.Fatalf("CanonicalProviderIDs() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if CanonicalProviderIDs()[0] != "cloudflare-quick" {
		t.Fatal("CanonicalProviderIDs returned mutable shared storage")
	}
	for _, id := range want {
		if !IsCanonicalProviderID(id) {
			t.Fatalf("expected canonical provider %q", id)
		}
	}
	for _, id := range []string{"", "configured-gateway", "https://provider.example", "198.51.100.9", "super-secret-token"} {
		if IsCanonicalProviderID(id) {
			t.Fatalf("unexpected canonical provider %q", id)
		}
	}
}
