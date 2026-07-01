package skillkit

import "strings"

const AdaptiveConfigurationSchemaVersion = "rdev.adaptive-configuration-contract.v1"

type AdaptiveConfigurationContract struct {
	SchemaVersion     string   `json:"schema_version"`
	Required          bool     `json:"required"`
	ProbeBeforeActing []string `json:"probe_before_acting"`
	AskIfUnclear      []string `json:"ask_if_unclear"`
	Placeholders      []string `json:"placeholders"`
}

func defaultAdaptiveConfigurationContract() AdaptiveConfigurationContract {
	return AdaptiveConfigurationContract{
		SchemaVersion: AdaptiveConfigurationSchemaVersion,
		Required:      true,
		ProbeBeforeActing: []string{
			"rdev doctor",
			"rdev mcp tools",
			"target OS and shell",
			"service manager",
			"gateway configuration",
			"available connection modes",
			"workspace path",
			"installed agent adapters",
			"framework install path",
			"current permissions",
		},
		AskIfUnclear: []string{
			"gateway URL",
			"ticket code",
			"root key",
			"release URL",
			"checksum",
			"framework install path",
			"workspace root",
			"adapter choice",
			"approval policy",
		},
		Placeholders: []string{
			"https://api.example.com/v1",
			"/Users/example",
			"/home/example",
			`C:\Users\Alice`,
		},
	}
}

func adaptiveContractFailure(contract AdaptiveConfigurationContract) string {
	required := defaultAdaptiveConfigurationContract()
	var failures []string
	if contract.SchemaVersion != AdaptiveConfigurationSchemaVersion {
		failures = append(failures, "schema_version")
	}
	if !contract.Required {
		failures = append(failures, "required")
	}
	failures = appendMissingContractValues(failures, "probe:", required.ProbeBeforeActing, contract.ProbeBeforeActing)
	failures = appendMissingContractValues(failures, "ask:", required.AskIfUnclear, contract.AskIfUnclear)
	failures = appendMissingContractValues(failures, "placeholder:", required.Placeholders, contract.Placeholders)
	return strings.Join(failures, ",")
}

func appendMissingContractValues(failures []string, prefix string, required, actual []string) []string {
	seen := map[string]bool{}
	for _, value := range actual {
		seen[value] = true
	}
	for _, value := range required {
		if !seen[value] {
			failures = append(failures, prefix+value)
		}
	}
	return failures
}

func textKeepsAdaptiveContract(content string) bool {
	required := []string{
		"Adaptive Configuration Contract",
		"rdev doctor",
		"rdev mcp tools",
		"gateway",
		"connection modes",
		"workspace",
		"framework install path",
		"ask",
		"inventing",
		"https://api.example.com/v1",
		"/Users/example",
		"/home/example",
		`C:\Users\Alice`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			return false
		}
	}
	return true
}

func skillKeepsAdaptiveContract(content string) bool {
	required := []string{
		"probe",
		"gateway",
		"workspace",
		"ask",
		"unclear",
		"invent",
	}
	lower := strings.ToLower(content)
	for _, needle := range required {
		if !strings.Contains(lower, needle) {
			return false
		}
	}
	return true
}
