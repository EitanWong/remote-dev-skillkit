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
const LifecycleManifestSchemaVersion = "rdev.adapter-lifecycle.v1"

type ResultArtifactContract struct {
	Adapter                 string
	SchemaVersion           string
	CommandFields           []string
	RequiredStringFields    []string
	RequireTiming           bool
	RequireRedaction        bool
	RejectUnredactedSecrets bool
}

type CancellationContract struct {
	Adapter                 string
	SchemaVersion           string
	CommandFields           []string
	RequiredStringFields    []string
	RequireTiming           bool
	RequireRedaction        bool
	RejectUnredactedSecrets bool
}

type LifecycleContract struct {
	Adapter                 string
	SchemaVersion           string
	RequiredPhases          []string
	RequireSafety           bool
	RequireCancellation     bool
	RequireResultSchema     bool
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

func VerifyCancellationArtifactJSON(content []byte, contract CancellationContract) ConformanceReport {
	report := VerifyResultArtifactJSON(content, ResultArtifactContract{
		Adapter:                 contract.Adapter,
		SchemaVersion:           contract.SchemaVersion,
		CommandFields:           contract.CommandFields,
		RequiredStringFields:    contract.RequiredStringFields,
		RequireTiming:           contract.RequireTiming,
		RequireRedaction:        contract.RequireRedaction,
		RejectUnredactedSecrets: contract.RejectUnredactedSecrets,
	})
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	var artifact map[string]any
	if err := json.Unmarshal(content, &artifact); err != nil {
		report.OK = report.allChecksPassed()
		return report
	}
	commandFields := contract.CommandFields
	if len(commandFields) == 0 {
		commandFields = []string{"."}
	}
	for _, field := range commandFields {
		command, ok := commandObject(artifact, field)
		add("cancellation_command_field_present:"+field, ok, "")
		if !ok {
			continue
		}
		canceled, canceledOK := boolField(command, "canceled")
		add("cancellation_canceled_true:"+field, canceledOK && canceled, boolDetail(canceled, canceledOK))
		timedOut, timedOutOK := boolField(command, "timed_out")
		add("cancellation_not_timed_out:"+field, timedOutOK && !timedOut, boolDetail(timedOut, timedOutOK))
		exitCode, exitOK := numericField(command, "exit_code")
		add("cancellation_exit_code_present:"+field, exitOK, numericDetail(exitCode, exitOK))
		outputTruncated, truncatedOK := boolField(command, "output_truncated")
		add("cancellation_output_truncated_present:"+field, truncatedOK, boolDetail(outputTruncated, truncatedOK))
	}
	report.OK = report.allChecksPassed()
	return report
}

func VerifyLifecycleManifestJSON(content []byte, contract LifecycleContract) ConformanceReport {
	if strings.TrimSpace(contract.SchemaVersion) == "" {
		contract.SchemaVersion = LifecycleManifestSchemaVersion
	}
	if len(contract.RequiredPhases) == 0 {
		contract.RequiredPhases = []string{"detect", "plan", "prepare", "run", "collect", "cleanup"}
	}
	report := ConformanceReport{
		SchemaVersion:  ConformanceReportSchemaVersion,
		Adapter:        contract.Adapter,
		ArtifactSchema: contract.SchemaVersion,
	}
	add := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	var manifest map[string]any
	if err := json.Unmarshal(content, &manifest); err != nil {
		add("json_valid", false, err.Error())
		report.OK = report.allChecksPassed()
		return report
	}
	add("json_valid", true, "")
	add("schema_version", stringField(manifest, "schema_version") == contract.SchemaVersion, stringField(manifest, "schema_version"))
	add("adapter", stringField(manifest, "adapter") == contract.Adapter, stringField(manifest, "adapter"))

	phases, phasesOK := objectField(manifest, "phases")
	add("phases_object", phasesOK, "")
	for _, phaseName := range contract.RequiredPhases {
		phase, ok := objectField(phases, phaseName)
		add("phase_present:"+phaseName, phasesOK && ok, "")
		if !ok {
			continue
		}
		implemented, implementedOK := boolField(phase, "implemented")
		add("phase_implemented:"+phaseName, implementedOK && implemented, "")
		add("phase_evidence:"+phaseName, nonEmptyStringArrayField(phase, "evidence"), strings.Join(stringArrayField(phase, "evidence"), ","))
	}
	if plan, ok := objectField(phases, "plan"); ok {
		add("plan_declares_external_consequences", boolFieldEquals(plan, "declares_external_consequences", true), "")
		add("plan_declares_required_authorizations", boolFieldEquals(plan, "declares_required_authorizations", true), "")
	}
	if prepare, ok := objectField(phases, "prepare"); ok {
		add("prepare_enforces_workspace_boundary", boolFieldEquals(prepare, "enforces_workspace_boundary", true), "")
		add("prepare_uses_workspace_lock", boolFieldEquals(prepare, "uses_workspace_lock", true), "")
	}
	if run, ok := objectField(phases, "run"); ok {
		add("run_supports_timeout", boolFieldEquals(run, "supports_timeout", true), "")
		if contract.RequireCancellation {
			add("run_supports_cancellation", boolFieldEquals(run, "supports_cancellation", true), "")
		}
	}
	if collect, ok := objectField(phases, "collect"); ok {
		add("collect_emits_result_artifact", boolFieldEquals(collect, "emits_result_artifact", true), "")
		if contract.RequireResultSchema {
			add("collect_result_schema", strings.TrimSpace(stringField(collect, "result_schema")) != "", stringField(collect, "result_schema"))
		}
	}
	if cleanup, ok := objectField(phases, "cleanup"); ok {
		add("cleanup_idempotent", boolFieldEquals(cleanup, "idempotent", true), "")
		add("cleanup_releases_locks", boolFieldEquals(cleanup, "releases_locks", true), "")
	}
	if contract.RequireSafety {
		safety, safetyOK := objectField(manifest, "safety")
		add("safety_object", safetyOK, "")
		add("safety_adapter_does_not_authorize_tasks", safetyOK && boolFieldEquals(safety, "adapter_authorizes_tasks", false), "")
		add("safety_adapter_does_not_self_authorize", safetyOK && boolFieldEquals(safety, "adapter_authorizes_dangerous_actions", false), "")
		add("safety_no_hidden_persistence", safetyOK && boolFieldEquals(safety, "adapter_installs_persistence", false), "")
		add("safety_host_validates_before_run", safetyOK && boolFieldEquals(safety, "host_validates_before_run", true), "")
		add("safety_redacts_outputs", safetyOK && boolFieldEquals(safety, "redacts_outputs", true), "")
	}
	if contract.RequireCancellation {
		cancellation, cancellationOK := objectField(manifest, "cancellation")
		add("cancellation_object", cancellationOK, "")
		add("cancellation_supported", cancellationOK && boolFieldEquals(cancellation, "supported", true), "")
		add("cancellation_evidence_field", cancellationOK && strings.TrimSpace(stringField(cancellation, "evidence_field")) != "", stringField(cancellation, "evidence_field"))
		add("cancellation_timeout_exclusive", cancellationOK && boolFieldEquals(cancellation, "timeout_exclusive", true), "")
		add("cancellation_cleanup_on_cancel", cancellationOK && boolFieldEquals(cancellation, "cleanup_on_cancel", true), "")
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

func objectField(values map[string]any, key string) (map[string]any, bool) {
	if values == nil {
		return nil, false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return nil, false
	}
	typed, ok := value.(map[string]any)
	return typed, ok
}

func boolFieldEquals(values map[string]any, key string, expected bool) bool {
	actual, ok := boolField(values, key)
	return ok && actual == expected
}

func stringArrayField(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	raw, ok := values[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func nonEmptyStringArrayField(values map[string]any, key string) bool {
	return len(stringArrayField(values, key)) > 0
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

func boolDetail(value bool, ok bool) string {
	if !ok {
		return ""
	}
	return strconv.FormatBool(value)
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
