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
)

var canonicalProviderIDs = []string{
	ProviderCloudflareQuick,
	ProviderLocalhostRun,
	ProviderPinggy,
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

func (r Registry) Select(policy Policy, evidence []RegionalEvidence) []Selection {
	selected := make([]Selection, 0, len(r.providers))
	for _, provider := range r.providers {
		metadata := cloneMetadata(provider.Metadata())
		eligibility := EvaluateEligibility(metadata, policy, evidence)
		if !eligibility.Eligible {
			continue
		}
		selected = append(selected, Selection{
			Provider:    provider,
			Metadata:    metadata,
			Eligibility: eligibility,
		})
	}
	sort.Slice(selected, func(i, j int) bool {
		iVerified := hasVerifiedEvidence(selected[i].Eligibility) || hasFreshVerifiedEvidence(selected[i].Metadata.ID, policy, evidence)
		jVerified := hasVerifiedEvidence(selected[j].Eligibility) || hasFreshVerifiedEvidence(selected[j].Metadata.ID, policy, evidence)
		if iVerified != jVerified {
			return iVerified
		}
		if selected[i].Metadata.DefaultAutomatic != selected[j].Metadata.DefaultAutomatic {
			return selected[i].Metadata.DefaultAutomatic
		}
		return selected[i].Metadata.ID < selected[j].Metadata.ID
	})
	return selected
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

func hasVerifiedEvidence(eligibility Eligibility) bool {
	return eligibility.Evidence != nil && eligibility.Evidence.Status == EvidenceVerified
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
