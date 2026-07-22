package acceptance

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateWindowsLayeredRunReportAcceptsColdAndWarmEvidence(t *testing.T) {
	for _, fromCache := range []bool{false, true} {
		report := windowsLayeredRunReportForTest(fromCache)
		content := marshalWindowsLayeredRunReportForTest(t, report)
		if err := validateWindowsLayeredRunReport(content, fromCache); err != nil {
			t.Fatalf("expected from_cache=%t report to validate: %v", fromCache, err)
		}
	}
}

func TestValidateWindowsLayeredRunReportAcceptsResumedColdEvidence(t *testing.T) {
	report := windowsLayeredRunReportForTest(false)
	report["resumed"] = true
	if err := validateWindowsLayeredRunReport(marshalWindowsLayeredRunReportForTest(t, report), false); err != nil {
		t.Fatalf("expected a resumed cold download report to validate: %v", err)
	}
}

func TestValidateWindowsLayeredRunReportRejectsResumedWarmEvidence(t *testing.T) {
	report := windowsLayeredRunReportForTest(true)
	report["resumed"] = true
	err := validateWindowsLayeredRunReport(marshalWindowsLayeredRunReportForTest(t, report), true)
	if err == nil {
		t.Fatal("expected a cache-hit report with resumed=true to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "resumed") {
		t.Fatalf("error %q does not explain resumed cache-hit failure", err)
	}
}

func TestWindowsTemporaryRequiredEvidenceRejectsArbitraryBaseEntries(t *testing.T) {
	evidence := []string{
		"arbitrary evidence one",
		"arbitrary evidence two",
		"arbitrary evidence three",
		"arbitrary evidence four",
		"arbitrary evidence five",
		windowsTemporaryColdLayeredRunEvidence,
		windowsTemporaryWarmLayeredRunEvidence,
	}
	if windowsTemporaryRequiredEvidenceComplete(evidence) {
		t.Fatal("expected arbitrary base evidence entries to fail the Windows temporary plan contract")
	}
}

func TestValidateWindowsLayeredRunReportRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name              string
		expectedFromCache bool
		mutate            func(map[string]any)
		wantError         string
	}{
		{
			name:              "cold report claims cache hit",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["from_cache"] = true
			},
			wantError: "from_cache",
		},
		{
			name:              "cold report omits cache field",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				delete(report, "from_cache")
			},
			wantError: "from_cache",
		},
		{
			name:              "warm report claims cache miss",
			expectedFromCache: true,
			mutate: func(report map[string]any) {
				report["from_cache"] = false
			},
			wantError: "from_cache",
		},
		{
			name:              "wrong schema",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["schema_version"] = "rdev.layered-run-report.v0"
			},
			wantError: "schema",
		},
		{
			name:              "unknown benign field",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["unexpected"] = "safe"
			},
			wantError: "unknown field",
		},
		{
			name:              "missing signature verification",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				removeWindowsLayeredRunStageForTest(report, "signature-verification")
			},
			wantError: "signature-verification",
		},
		{
			name:              "unexpected stage name",
			expectedFromCache: true,
			mutate: func(report map[string]any) {
				stages, _ := report["stages"].([]any)
				stage, _ := stages[2].(map[string]any)
				stage["name"] = "runtime-resolution"
			},
			wantError: "runtime-download",
		},
		{
			name:              "stages out of order",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				stages, _ := report["stages"].([]any)
				stages[0], stages[1] = stages[1], stages[0]
			},
			wantError: "manifest-fetch",
		},
		{
			name:              "negative duration",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				setWindowsLayeredRunStageDurationForTest(report, "manifest-fetch", -1)
			},
			wantError: "duration",
		},
		{
			name:              "missing duration",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				stages, _ := report["stages"].([]any)
				stage, _ := stages[0].(map[string]any)
				delete(stage, "duration_ms")
			},
			wantError: "duration",
		},
		{
			name:              "ticket text",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["note"] = "ticket_code=ABCD-1234"
			},
			wantError: "ticket",
		},
		{
			name:              "gateway text",
			expectedFromCache: true,
			mutate: func(report map[string]any) {
				report["note"] = "gateway_url=https://api.example.com/v1"
			},
			wantError: "gateway",
		},
		{
			name:              "token text",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["note"] = "token=secret-value"
			},
			wantError: "token",
		},
		{
			name:              "private Windows path",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["note"] = `C:\Users\Alice\AppData\Local\RemoteDevSkillkit`
			},
			wantError: "private path",
		},
		{
			name:              "pre-redacted content",
			expectedFromCache: false,
			mutate: func(report map[string]any) {
				report["note"] = "[REDACTED:secret_json]"
			},
			wantError: "redacted",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := windowsLayeredRunReportForTest(test.expectedFromCache)
			test.mutate(report)
			content := marshalWindowsLayeredRunReportForTest(t, report)
			err := validateWindowsLayeredRunReport(content, test.expectedFromCache)
			if err == nil {
				t.Fatal("expected layered run report validation to fail")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.wantError)) {
				t.Fatalf("error %q does not explain %q failure", err, test.wantError)
			}
		})
	}
}

func TestValidateWindowsLayeredRunReportRejectsDuplicateKeys(t *testing.T) {
	content := marshalWindowsLayeredRunReportForTest(t, windowsLayeredRunReportForTest(false))
	content = bytes.Replace(content, []byte(`"from_cache": false`), []byte(`"from_cache": true, "from_cache": false`), 1)
	err := validateWindowsLayeredRunReport(content, false)
	if err == nil {
		t.Fatal("expected duplicate from_cache keys to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("error %q does not explain duplicate key failure", err)
	}
}

func TestValidateWindowsLayeredRunReportRejectsCaseAliasFields(t *testing.T) {
	content := marshalWindowsLayeredRunReportForTest(t, windowsLayeredRunReportForTest(false))
	content = bytes.Replace(
		content,
		[]byte(`"asset_id": "rdev-host-windows-amd64"`),
		[]byte(`"asset_id": "https://private.example.invalid/session/ABCD-1234", "ASSET_ID": "rdev-host-windows-amd64"`),
		1,
	)
	err := validateWindowsLayeredRunReport(content, false)
	if err == nil {
		t.Fatal("expected case-aliased asset_id fields to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "field") {
		t.Fatalf("error %q does not explain exact field-name failure", err)
	}
}
