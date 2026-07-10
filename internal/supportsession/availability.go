package supportsession

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

type AvailabilityReadiness struct {
	SchemaVersion       string                 `json:"schema_version"`
	State               string                 `json:"state"`
	Region              tunnel.RegionProfile   `json:"region"`
	ReadyToSend         bool                   `json:"ready_to_send"`
	ReadyToActivate     bool                   `json:"ready_to_activate"`
	ReadyToExecute      bool                   `json:"ready_to_execute"`
	DegradedSingleEntry bool                   `json:"degraded_single_entry"`
	DegradedReason      string                 `json:"degraded_reason,omitempty"`
	StandardNextAction  string                 `json:"standard_next_action,omitempty"`
	AvailabilitySet     tunnel.AvailabilitySet `json:"availability_set"`
}

func DirectAvailability(set tunnel.AvailabilitySet, allowOverride bool) AvailabilityReadiness {
	cloned := cloneAvailabilitySet(set)
	readiness := AvailabilityReadiness{
		SchemaVersion:      tunnel.ReadinessSchemaVersion,
		State:              "unavailable",
		Region:             set.Region,
		DegradedReason:     "no public gateway candidate is available for a remote target",
		StandardNextAction: "start or configure a policy-approved public tunnel and verify its gateway instance marker",
		AvailabilitySet:    cloned,
	}
	if set.SchemaVersion != tunnel.AvailabilitySchemaVersion || !hasPublicCandidate(set.Candidates) {
		return readiness
	}

	readiness.State = "degraded-single-entry"
	readiness.DegradedSingleEntry = true
	readiness.DegradedReason = "direct script-first entry does not yet provide the signed connector and rendezvous path required for standard readiness"
	readiness.StandardNextAction = "use the explicit degraded override only for an attended session after reviewing the direct-entry limitation"
	readiness.ReadyToSend = allowOverride
	return readiness
}

func normalizeAvailabilityReadiness(readiness AvailabilityReadiness) AvailabilityReadiness {
	derived := DirectAvailability(readiness.AvailabilitySet, readiness.ReadyToSend)
	if readiness.SchemaVersion != tunnel.ReadinessSchemaVersion || readiness.State != derived.State {
		return DirectAvailability(readiness.AvailabilitySet, false)
	}
	return derived
}

func hasPublicCandidate(candidates []tunnel.Candidate) bool {
	for _, candidate := range candidates {
		parsed, err := url.Parse(strings.TrimSpace(candidate.URL))
		if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
			continue
		}
		host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		if host == "localhost" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
			continue
		}
		return true
	}
	return false
}

func cloneAvailabilitySet(set tunnel.AvailabilitySet) tunnel.AvailabilitySet {
	cloned := set
	cloned.Candidates = append([]tunnel.Candidate(nil), set.Candidates...)
	cloned.Attempts = append([]tunnel.Attempt(nil), set.Attempts...)
	return cloned
}

func availabilityReadinessFromMap(payload map[string]any) AvailabilityReadiness {
	value := payload["availability_readiness"]
	if readiness, ok := value.(AvailabilityReadiness); ok {
		return normalizeAvailabilityReadiness(readiness)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return normalizeAvailabilityReadiness(AvailabilityReadiness{})
	}
	var readiness AvailabilityReadiness
	if err := json.Unmarshal(encoded, &readiness); err != nil {
		return normalizeAvailabilityReadiness(AvailabilityReadiness{})
	}
	return normalizeAvailabilityReadiness(readiness)
}
