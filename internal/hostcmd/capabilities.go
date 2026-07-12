package hostcmd

import "strings"

// ConstrainCapabilities intersects detected host capabilities with a verified
// manifest ceiling. Direct local development can leave enforceCeiling false.
func ConstrainCapabilities(detected, ceiling []string, enforceCeiling bool) []string {
	allowed := capabilitySet(ceiling)
	result := make([]string, 0, len(detected))
	seen := map[string]bool{}
	for _, capability := range detected {
		capability = strings.TrimSpace(capability)
		if capability == "" || seen[capability] || (enforceCeiling && !allowed[capability]) {
			continue
		}
		seen[capability] = true
		result = append(result, capability)
	}
	return result
}

// CapabilitiesAllowed verifies a task against the same signed ceiling before
// any target-side adapter runs.
func CapabilitiesAllowed(requested, ceiling []string, enforceCeiling bool) bool {
	if !enforceCeiling {
		return true
	}
	allowed := capabilitySet(ceiling)
	for _, capability := range requested {
		capability = strings.TrimSpace(capability)
		if capability == "" || !allowed[capability] {
			return false
		}
	}
	return true
}

func capabilitySet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = true
		}
	}
	return result
}
