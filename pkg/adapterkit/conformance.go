package adapterkit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ConformanceReportSchemaVersion = "rdev.adapter-conformance-report.v1"

type ResultArtifactContract struct {
	Adapter                 string
	SchemaVersion           string
	CommandFields           []string
	RequiredStringFields    []string
	RequireTiming           bool
	RequireRedaction        bool
	RejectUnredactedSecrets bool
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type ConformanceReport struct {
	SchemaVersion  string  `json:"schema_version"`
	Adapter        string  `json:"adapter"`
	ArtifactSchema string  `json:"artifact_schema"`
	OK             bool    `json:"ok"`
	Checks         []Check `json:"checks"`
}

func VerifyResultArtifactJSON(content []byte, contract ResultArtifactContract) ConformanceReport {
	report := ConformanceReport{
		SchemaVersion:  ConformanceReportSchemaVersion,
		Adapter:        contract.Adapter,
		ArtifactSchema: contract.SchemaVersion,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	var artifact map[string]any
	if err := json.Unmarshal(content, &artifact); err != nil {
		add("json_valid", false, err.Error())
		report.OK = report.allChecksPassed()
		return report
	}
	add("json_valid", true, "")
	add("schema_version", stringField(artifact, "schema_version") == contract.SchemaVersion, stringField(artifact, "schema_version"))
	add("adapter", stringField(artifact, "adapter") == contract.Adapter, stringField(artifact, "adapter"))

	for _, field := range contract.RequiredStringFields {
		add("required_string_field:"+field, strings.TrimSpace(stringField(artifact, field)) != "", stringField(artifact, field))
	}
	if contract.RequireTiming {
		add("started_at_valid", validRFC3339(stringField(artifact, "started_at")), stringField(artifact, "started_at"))
		add("ended_at_valid", validRFC3339(stringField(artifact, "ended_at")), stringField(artifact, "ended_at"))
		duration, ok := numericField(artifact, "duration_millis")
		add("duration_millis_nonnegative", ok && duration >= 0, numericDetail(duration, ok))
	}
	if contract.RequireRedaction {
		_, redactedOK := boolField(artifact, "redacted")
		add("redacted_flag_present", redactedOK, "")
		rules, rulesOK := artifact["redaction_rules"].([]any)
		add("redaction_rules_present", rulesOK && len(rules) > 0, fmt.Sprintf("%d", len(rules)))
		if counts, ok := artifact["redaction_counts"]; ok {
			_, countsOK := counts.(map[string]any)
			add("redaction_counts_object", countsOK, "")
		}
	}
	commandFields := contract.CommandFields
	if len(commandFields) == 0 {
		commandFields = []string{"."}
	}
	for _, field := range commandFields {
		command, ok := commandObject(artifact, field)
		add("command_field_present:"+field, ok, "")
		if !ok {
			continue
		}
		_, exitOK := numericField(command, "exit_code")
		add("command_exit_code:"+field, exitOK, "")
		timedOut, timedOutOK := boolField(command, "timed_out")
		add("command_timed_out:"+field, timedOutOK, "")
		canceled, canceledOK := boolField(command, "canceled")
		add("command_canceled:"+field, canceledOK, "")
		_, truncatedOK := boolField(command, "output_truncated")
		add("command_output_truncated:"+field, truncatedOK, "")
		add("command_cancel_timeout_exclusive:"+field, !(timedOutOK && canceledOK && timedOut && canceled), "")
	}
	if contract.RejectUnredactedSecrets {
		add("no_unredacted_secret_patterns", !containsSecretPattern(string(content)), "")
	}
	report.OK = report.allChecksPassed()
	return report
}

func (r ConformanceReport) allChecksPassed() bool {
	if len(r.Checks) == 0 {
		return false
	}
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

func stringField(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func numericField(values map[string]any, key string) (float64, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func boolField(values map[string]any, key string) (bool, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func commandObject(artifact map[string]any, field string) (map[string]any, bool) {
	if field == "." {
		return artifact, true
	}
	value, ok := artifact[field]
	if !ok || value == nil {
		return nil, false
	}
	typed, ok := value.(map[string]any)
	return typed, ok
}

func validRFC3339(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return true
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func numericDetail(value float64, ok bool) string {
	if !ok {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
}

func containsSecretPattern(content string) bool {
	for _, pattern := range secretPatterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}
