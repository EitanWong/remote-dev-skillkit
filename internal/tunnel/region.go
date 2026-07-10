package tunnel

import (
	"errors"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

const MaxRegionalEvidenceTTL = 7 * 24 * time.Hour

var coarseRegionTokenPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type NetworkSample struct {
	Carrier      string `json:"carrier"`
	Region       string `json:"region"`
	ResolverType string `json:"resolver_type,omitempty"`
	Success      bool   `json:"success"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
}

type RegionalEvidence struct {
	ProviderID string          `json:"provider_id"`
	Region     RegionProfile   `json:"region"`
	Status     EvidenceStatus  `json:"status"`
	Issuer     string          `json:"issuer"`
	ObservedAt time.Time       `json:"observed_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
	Samples    []NetworkSample `json:"samples"`
}

func (e RegionalEvidence) Validate() error {
	if strings.TrimSpace(e.ProviderID) == "" {
		return errors.New("provider ID is required")
	}
	if strings.TrimSpace(e.Issuer) == "" {
		return errors.New("issuer is required")
	}
	if !supportedRegion(e.Region) {
		return errors.New("unsupported region")
	}
	if !supportedEvidenceStatus(e.Status) {
		return errors.New("unsupported evidence status")
	}
	if e.ObservedAt.IsZero() || e.ExpiresAt.IsZero() {
		return errors.New("evidence timestamps are required")
	}
	if !e.ExpiresAt.After(e.ObservedAt) {
		return errors.New("evidence expiry must follow observation")
	}
	if e.ExpiresAt.Sub(e.ObservedAt) > MaxRegionalEvidenceTTL {
		return errors.New("regional evidence TTL exceeds maximum")
	}
	for _, sample := range e.Samples {
		if strings.TrimSpace(sample.Carrier) == "" || strings.TrimSpace(sample.Region) == "" {
			return errors.New("sample carrier and region are required")
		}
		if isIPLiteral(sample.Region) {
			return errors.New("sample region must not be an IP literal")
		}
		if _, ok := canonicalCoarseRegion(sample.Region); !ok {
			return errors.New("sample region must be a canonical coarse-region token")
		}
	}
	if e.Region == RegionCNMainland && e.Status == EvidenceVerified && !hasMainlandCarrierCoverage(e.Samples) {
		return errors.New("verified mainland evidence lacks carrier coverage")
	}
	return nil
}

func EvaluateEligibility(meta ProviderMetadata, policy Policy, evidence []RegionalEvidence) Eligibility {
	if !providerAllowed(meta.ID, policy.AllowedProviderIDs) {
		return Eligibility{Reason: "provider-not-allowed"}
	}

	switch policy.Region {
	case RegionGlobal:
		if !meta.DefaultAutomatic && !policy.AllowNonDefault {
			return Eligibility{Reason: "provider-not-default"}
		}
		return Eligibility{Eligible: true}
	case RegionCNMainland:
		return evaluateMainlandEligibility(meta.ID, policy.Now, evidence)
	default:
		return Eligibility{Reason: "region-unsupported"}
	}
}

func evaluateMainlandEligibility(providerID string, now time.Time, evidence []RegionalEvidence) Eligibility {
	selected, ok := newestEvidence(providerID, RegionCNMainland, evidence)
	if !ok {
		return Eligibility{Reason: "regional-evidence-missing"}
	}
	cloned := cloneEvidence(selected)
	result := Eligibility{Evidence: &cloned}
	if selected.ObservedAt.After(now) {
		result.Reason = "regional-evidence-not-yet-valid"
		return result
	}
	if !selected.ExpiresAt.After(now) {
		result.Reason = "regional-evidence-expired"
		return result
	}
	if selected.Status == EvidenceBlocked {
		result.Reason = "regional-evidence-blocked"
		return result
	}
	if selected.Status != EvidenceVerified {
		result.Reason = "regional-evidence-not-verified"
		return result
	}
	if err := selected.Validate(); err != nil {
		result.Reason = "regional-evidence-invalid"
		return result
	}
	result.Eligible = true
	result.Reason = ""
	return result
}

func newestEvidence(providerID string, region RegionProfile, evidence []RegionalEvidence) (RegionalEvidence, bool) {
	var selected RegionalEvidence
	found := false
	for _, item := range evidence {
		if item.ProviderID != providerID || item.Region != region {
			continue
		}
		if !found || item.ObservedAt.After(selected.ObservedAt) {
			selected = item
			found = true
		}
	}
	return selected, found
}

func providerAllowed(providerID string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, allowedID := range allowed {
		if providerID == allowedID {
			return true
		}
	}
	return false
}

func supportedRegion(region RegionProfile) bool {
	return region == RegionGlobal || region == RegionCNMainland
}

func supportedEvidenceStatus(status EvidenceStatus) bool {
	switch status {
	case EvidenceUnknown, EvidenceCandidate, EvidenceVerified, EvidenceDegraded, EvidenceBlocked:
		return true
	default:
		return false
	}
}

func hasMainlandCarrierCoverage(samples []NetworkSample) bool {
	required := map[string]struct{}{
		"china-telecom": {},
		"china-unicom":  {},
		"china-mobile":  {},
	}
	regionsByCarrier := make(map[string]map[string]struct{}, len(required))
	for _, sample := range samples {
		if !sample.Success {
			continue
		}
		carrier := strings.TrimSpace(sample.Carrier)
		if _, ok := required[carrier]; !ok {
			continue
		}
		if regionsByCarrier[carrier] == nil {
			regionsByCarrier[carrier] = make(map[string]struct{})
		}
		region, ok := canonicalCoarseRegion(sample.Region)
		if !ok {
			continue
		}
		regionsByCarrier[carrier][region] = struct{}{}
	}
	for carrier := range required {
		if len(regionsByCarrier[carrier]) < 2 {
			return false
		}
	}
	return true
}

func canonicalCoarseRegion(value string) (string, bool) {
	if strings.TrimSpace(value) != value || !coarseRegionTokenPattern.MatchString(value) {
		return "", false
	}
	return value, true
}

func isIPLiteral(value string) bool {
	trimmed := strings.TrimSpace(value)
	if _, err := netip.ParseAddr(strings.Trim(trimmed, "[]")); err == nil {
		return true
	}
	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		return false
	}
	_, err = netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil
}

func cloneEvidence(evidence RegionalEvidence) RegionalEvidence {
	cloned := evidence
	if evidence.Samples == nil {
		cloned.Samples = nil
	} else {
		cloned.Samples = append([]NetworkSample(nil), evidence.Samples...)
	}
	return cloned
}
