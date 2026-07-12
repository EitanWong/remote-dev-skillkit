package tunnel

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const (
	ProviderCloudflareQuick = "cloudflare-quick"
	ProviderLocalhostRun    = "localhost-run"
	ProviderPinggy          = "pinggy"
	ProviderTunn3l          = "tunn3l"
)

var canonicalProviderIDs = []string{
	ProviderCloudflareQuick,
	ProviderLocalhostRun,
	ProviderPinggy,
	ProviderTunn3l,
}

func CanonicalProviderIDs() []string {
	return append([]string(nil), canonicalProviderIDs...)
}

func IsCanonicalProviderID(id string) bool {
	for _, candidate := range canonicalProviderIDs {
		if id == candidate {
			return true
		}
	}
	return false
}

type Selection struct {
	Provider    Provider
	Metadata    ProviderMetadata
	Eligibility Eligibility
}

type Registry struct {
	providers []Provider
}

func NewRegistry(providers ...Provider) (Registry, error) {
	registered := make([]Provider, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for i, provider := range providers {
		if provider == nil || isNilProvider(provider) {
			return Registry{}, fmt.Errorf("provider %d is nil", i)
		}
		id := provider.ID()
		if strings.TrimSpace(id) == "" {
			return Registry{}, fmt.Errorf("provider %d has an empty ID", i)
		}
		meta := provider.Metadata()
		if meta.ID != id {
			return Registry{}, fmt.Errorf("provider %q metadata ID %q does not match", id, meta.ID)
		}
		if _, exists := seen[id]; exists {
			return Registry{}, fmt.Errorf("duplicate provider ID %q", id)
		}
		seen[id] = struct{}{}
		registered = append(registered, provider)
	}
	return Registry{providers: registered}, nil
}

func (r Registry) Providers() []ProviderMetadata {
	providers := make([]ProviderMetadata, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, cloneMetadata(provider.Metadata()))
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].ID < providers[j].ID
	})
	return providers
}

func (r Registry) Evaluate(policy Policy, evidence []RegionalEvidence) []Selection {
	items := make([]Selection, 0, len(r.providers))
	for _, provider := range r.providers {
		metadata := cloneMetadata(provider.Metadata())
		items = append(items, Selection{
			Provider:    provider,
			Metadata:    metadata,
			Eligibility: EvaluateEligibility(metadata, policy, evidence),
		})
	}
	sortSelections(items, policy, evidence)
	return items
}

func (r Registry) Select(policy Policy, evidence []RegionalEvidence) []Selection {
	evaluated := r.Evaluate(policy, evidence)
	selected := make([]Selection, 0, len(evaluated))
	for _, item := range evaluated {
		if item.Eligibility.Eligible {
			selected = append(selected, item)
		}
	}
	return selected
}

func sortSelections(items []Selection, policy Policy, evidence []RegionalEvidence) {
	sort.Slice(items, func(i, j int) bool {
		iVerified := hasFreshVerifiedEvidence(items[i].Metadata.ID, policy, evidence)
		jVerified := hasFreshVerifiedEvidence(items[j].Metadata.ID, policy, evidence)
		if iVerified != jVerified {
			return iVerified
		}
		if items[i].Metadata.DefaultAutomatic != items[j].Metadata.DefaultAutomatic {
			return items[i].Metadata.DefaultAutomatic
		}
		iPriority := items[i].Metadata.AutomaticPriority
		jPriority := items[j].Metadata.AutomaticPriority
		iPositive := iPriority > 0
		jPositive := jPriority > 0
		if iPositive != jPositive {
			return iPositive
		}
		if iPositive && iPriority != jPriority {
			return iPriority < jPriority
		}
		return items[i].Metadata.ID < items[j].Metadata.ID
	})
}

func isNilProvider(provider Provider) bool {
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func hasFreshVerifiedEvidence(providerID string, policy Policy, evidence []RegionalEvidence) bool {
	selected, ok := newestEvidence(providerID, policy.Region, evidence)
	if !ok || selected.Status != EvidenceVerified || policy.Now.IsZero() {
		return false
	}
	if selected.ObservedAt.After(policy.Now) || !selected.ExpiresAt.After(policy.Now) {
		return false
	}
	return selected.Validate() == nil
}
