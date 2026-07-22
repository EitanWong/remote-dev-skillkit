package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateWindowsLayeredEntryEvidence(t *testing.T) {
	plan := WindowsTemporaryPlan{
		HandoffArchiveSizeBytes: 1045000,
		HandoffArchiveSHA256:    strings.Repeat("a", 64),
	}
	content := marshalWindowsLayeredEntryEvidenceForTest(t, windowsLayeredEntryEvidenceForTest())
	if err := validateWindowsLayeredEntryEvidence(content, plan); err != nil {
		t.Fatalf("expected complete layered entry evidence to pass: %v", err)
	}
}

func TestValidateWindowsLayeredEntryEvidenceRejectsInvalidContracts(t *testing.T) {
	plan := WindowsTemporaryPlan{
		HandoffArchiveSizeBytes: 1045000,
		HandoffArchiveSHA256:    strings.Repeat("a", 64),
	}
	tests := []struct {
		name string
		want string
		edit func(map[string]any)
	}{
		{name: "oversized zip", want: "size", edit: func(v map[string]any) { v["handoff_zip_size_bytes"] = float64((1 << 20) + 1) }},
		{name: "unordered fallback", want: "fallback", edit: func(v map[string]any) { v["fallback_attempts"] = []any{"cmd", "powershell"} }},
		{name: "duplicate core", want: "core", edit: func(v map[string]any) { v["core_start_count"] = float64(2) }},
		{name: "negative duration", want: "duration", edit: func(v map[string]any) { v["registration_duration_ms"] = float64(-1) }},
		{name: "negative bytes", want: "network", edit: func(v map[string]any) { v["network_bytes"] = float64(-1) }},
		{name: "range not resumed", want: "range", edit: func(v map[string]any) { v["range_resumed"] = false }},
		{name: "private acl missing", want: "acl", edit: func(v map[string]any) { v["private_acl"] = false }},
		{name: "reparse accepted", want: "reparse", edit: func(v map[string]any) { v["reparse_rejected"] = false }},
		{name: "defender lock missing", want: "defender", edit: func(v map[string]any) { v["defender_lock_verified"] = false }},
		{name: "route not reselected", want: "route", edit: func(v map[string]any) { v["route_reselected"] = false }},
		{name: "duplicate registration", want: "registration", edit: func(v map[string]any) { v["registration_count"] = float64(2) }},
		{name: "automatic archive execution", want: "archive", edit: func(v map[string]any) { v["archive_recovery_executed"] = true }},
		{name: "legacy route", want: "bootstrap", edit: func(v map[string]any) { v["bootstrap_only"] = false }},
		{name: "persistence residue", want: "persistence", edit: func(v map[string]any) { v["persistence_residue"] = []any{"service"} }},
		{name: "cleanup missing", want: "cleanup", edit: func(v map[string]any) { v["cleanup_complete"] = false }},
		{name: "private path", want: "private path", edit: func(v map[string]any) { v["note"] = `C:\Users\fixture\private` }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := windowsLayeredEntryEvidenceForTest()
			test.edit(value)
			err := validateWindowsLayeredEntryEvidence(marshalWindowsLayeredEntryEvidenceForTest(t, value), plan)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func windowsLayeredEntryEvidenceForTest() map[string]any {
	return map[string]any{
		"schema_version":            WindowsLayeredEntryEvidenceSchemaVersion,
		"windows_release":           "11",
		"architecture":              "amd64",
		"handoff_zip_size_bytes":    float64(1045000),
		"handoff_zip_sha256":        strings.Repeat("a", 64),
		"selected_launcher":         "cmd",
		"fallback_attempts":         []any{"powershell", "powershell-bypass", "cmd"},
		"core_start_count":          float64(1),
		"network_bytes":             float64(2048),
		"registration_duration_ms":  float64(750),
		"cache_hit":                 false,
		"range_interrupted":         true,
		"range_resumed":             true,
		"range_bytes":               float64(1024),
		"private_acl":               true,
		"unc_rejected":              true,
		"reparse_rejected":          true,
		"defender_lock_verified":    true,
		"active_route_failed":       true,
		"route_reselected":          true,
		"registration_count":        float64(1),
		"session_identity_stable":   true,
		"event_cursor_stable":       true,
		"archive_recovery_executed": false,
		"bootstrap_only":            true,
		"persistence_residue":       []any{},
		"cleanup_complete":          true,
	}
}

func marshalWindowsLayeredEntryEvidenceForTest(t *testing.T, value map[string]any) []byte {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
