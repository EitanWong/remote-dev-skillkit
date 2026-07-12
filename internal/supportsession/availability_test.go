package supportsession

import (
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func TestDirectAvailabilityRequiresExplicitOverride(t *testing.T) {
	set := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionCNMainland,
		Candidates: []tunnel.Candidate{
			{ProviderID: "a", URL: "https://a.example"},
			{ProviderID: "b", URL: "https://b.example"},
		},
	}
	got := DirectAvailability(set, false)
	if got.ReadyToSend || !got.DegradedSingleEntry || got.State != "degraded-single-entry" {
		t.Fatalf("unexpected readiness: %#v", got)
	}
	overridden := DirectAvailability(set, true)
	if !overridden.ReadyToSend || overridden.ReadyToActivate || overridden.ReadyToExecute || !overridden.DegradedSingleEntry {
		t.Fatalf("unexpected override: %#v", overridden)
	}
}

func TestDirectAvailabilityFailsClosedWithoutPublicCandidate(t *testing.T) {
	tests := []struct {
		name string
		set  tunnel.AvailabilitySet
	}{
		{name: "zero value"},
		{
			name: "no candidates",
			set: tunnel.AvailabilitySet{
				SchemaVersion: tunnel.AvailabilitySchemaVersion,
				Region:        tunnel.RegionGlobal,
			},
		},
		{
			name: "LAN only",
			set: tunnel.AvailabilitySet{
				SchemaVersion: tunnel.AvailabilitySchemaVersion,
				Region:        tunnel.RegionCNMainland,
				Candidates: []tunnel.Candidate{
					{ProviderID: "lan", URL: "http://192.168.50.10:8787"},
					{ProviderID: "loopback", URL: "http://127.0.0.1:8787"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DirectAvailability(tt.set, true)
			if got.ReadyToSend || got.ReadyToActivate || got.ReadyToExecute || got.DegradedSingleEntry || got.State != "unavailable" {
				t.Fatalf("expected fail-closed unavailable readiness, got %#v", got)
			}
			if got.DegradedReason == "" || got.StandardNextAction == "" {
				t.Fatalf("expected actionable unavailable explanation, got %#v", got)
			}
		})
	}
}

func TestDirectAvailabilityOneCandidateRemainsDegraded(t *testing.T) {
	set := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates: []tunnel.Candidate{
			{ProviderID: "quick", URL: "https://temporary.example"},
		},
	}

	got := DirectAvailability(set, false)
	if got.ReadyToSend || !got.DegradedSingleEntry || got.State != "degraded-single-entry" {
		t.Fatalf("expected one direct entry to require override, got %#v", got)
	}
}

func TestNormalizeAvailabilityReadinessRejectsSendableStateWithoutValidAvailabilitySet(t *testing.T) {
	got := normalizeAvailabilityReadiness(AvailabilityReadiness{
		SchemaVersion: tunnel.ReadinessSchemaVersion,
		State:         "degraded-single-entry",
		ReadyToSend:   true,
	})

	if got.ReadyToSend || got.ReadyToActivate || got.ReadyToExecute || got.State != "unavailable" {
		t.Fatalf("expected malformed readiness to fail closed, got %#v", got)
	}
}

func TestDirectAvailabilityRejectsMalformedAvailabilitySet(t *testing.T) {
	valid := tunnel.AvailabilitySet{
		SchemaVersion: tunnel.AvailabilitySchemaVersion,
		Region:        tunnel.RegionGlobal,
		Candidates:    []tunnel.Candidate{{ProviderID: "provider", URL: "https://gateway.example"}},
	}
	tests := []struct {
		name   string
		mutate func(tunnel.AvailabilitySet) tunnel.AvailabilitySet
	}{
		{name: "unknown region", mutate: func(set tunnel.AvailabilitySet) tunnel.AvailabilitySet { set.Region = "unknown"; return set }},
		{name: "empty provider", mutate: func(set tunnel.AvailabilitySet) tunnel.AvailabilitySet {
			set.Candidates[0].ProviderID = " "
			return set
		}},
		{name: "ftp URL", mutate: func(set tunnel.AvailabilitySet) tunnel.AvailabilitySet {
			set.Candidates[0].URL = "ftp://gateway.example"
			return set
		}},
		{name: "userinfo", mutate: func(set tunnel.AvailabilitySet) tunnel.AvailabilitySet {
			set.Candidates[0].URL = "https://user@gateway.example"
			return set
		}},
		{name: "explicit port", mutate: func(set tunnel.AvailabilitySet) tunnel.AvailabilitySet {
			set.Candidates[0].URL = "https://gateway.example:444"
			return set
		}},
		{name: "private candidate", mutate: func(set tunnel.AvailabilitySet) tunnel.AvailabilitySet {
			set.Candidates[0].URL = "https://10.0.0.1"
			return set
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := cloneAvailabilitySet(valid)
			got := DirectAvailability(tt.mutate(set), true)
			if got.ReadyToSend || got.State != "unavailable" {
				t.Fatalf("expected malformed availability set to fail closed, got %#v", got)
			}
		})
	}
}
