package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPackageWindowsTemporaryEvidenceIncludesColdAndWarmLayeredReports(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true}`)
	outDir := filepath.Join(fixture.root, "package")

	pkg, err := PackageWindowsTemporaryEvidence(windowsTemporaryLayeredPackageOptions(fixture, outDir, fixture.coldLayeredRunPath, fixture.warmLayeredRunPath))
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected layered evidence package checks to pass: %#v", pkg.Checks)
	}
	assertWindowsTemporaryPackageCheck(t, pkg.Checks, "cold_layered_run_valid", true)
	assertWindowsTemporaryPackageCheck(t, pkg.Checks, "warm_layered_run_valid", true)
	for _, check := range pkg.Checks {
		if strings.HasPrefix(check.Name, "cold_layered_run_") || strings.HasPrefix(check.Name, "warm_layered_run_") {
			if strings.Contains(check.Detail, fixture.root) {
				t.Fatalf("layered report check %s leaks its private source path: %q", check.Name, check.Detail)
			}
		}
	}

	for _, report := range []struct {
		path      string
		fromCache bool
	}{
		{path: "evidence/cold-layered-run.json", fromCache: false},
		{path: "evidence/warm-layered-run.json", fromCache: true},
	} {
		packagedPath := filepath.Join(outDir, filepath.FromSlash(report.path))
		content, err := os.ReadFile(packagedPath)
		if err != nil {
			t.Fatalf("read packaged report %s: %v", report.path, err)
		}
		if err := validateWindowsLayeredRunReport(content, report.fromCache); err != nil {
			t.Fatalf("packaged report %s is invalid: %v", report.path, err)
		}
		if !windowsTemporaryPackageContainsPath(pkg.Files, report.path) {
			t.Fatalf("package manifest does not include %s: %#v", report.path, pkg.Files)
		}
		if !windowsTemporaryRequiredEvidenceContains(pkg.RequiredEvidence, filepath.Base(report.path)) {
			t.Fatalf("required evidence does not include %s: %#v", filepath.Base(report.path), pkg.RequiredEvidence)
		}
	}

	checksums, err := os.ReadFile(filepath.Join(outDir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, reportPath := range []string{"evidence/cold-layered-run.json", "evidence/warm-layered-run.json"} {
		if !strings.Contains(string(checksums), reportPath) {
			t.Fatalf("checksums.txt does not include %s:\n%s", reportPath, checksums)
		}
	}
}

func TestPackageWindowsTemporaryEvidenceRejectsMissingLayeredReports(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true}`)
	outDir := filepath.Join(fixture.root, "package")

	pkg, err := PackageWindowsTemporaryEvidence(windowsTemporaryLayeredPackageOptions(fixture, outDir, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatal("expected missing cold and warm layered reports to fail package verification")
	}
	assertWindowsTemporaryPackageCheck(t, pkg.Checks, "cold_layered_run_present", false)
	assertWindowsTemporaryPackageCheck(t, pkg.Checks, "warm_layered_run_present", false)
	for _, reportPath := range []string{"evidence/cold-layered-run.json", "evidence/warm-layered-run.json"} {
		if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(reportPath))); !os.IsNotExist(err) {
			t.Fatalf("missing report unexpectedly copied to %s: %v", reportPath, err)
		}
	}
}

func TestPackageWindowsTemporaryEvidenceRejectsInvalidLayeredReports(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(cold, warm map[string]any)
		failedCheck string
		absentPath  string
	}{
		{
			name: "reversed cold cache semantics",
			mutate: func(cold, _ map[string]any) {
				cold["from_cache"] = true
			},
			failedCheck: "cold_layered_run_valid",
			absentPath:  "evidence/cold-layered-run.json",
		},
		{
			name: "reversed warm cache semantics",
			mutate: func(_, warm map[string]any) {
				warm["from_cache"] = false
			},
			failedCheck: "warm_layered_run_valid",
			absentPath:  "evidence/warm-layered-run.json",
		},
		{
			name: "missing signature verification stage",
			mutate: func(cold, _ map[string]any) {
				removeWindowsLayeredRunStageForTest(cold, "signature-verification")
			},
			failedCheck: "cold_layered_run_valid",
			absentPath:  "evidence/cold-layered-run.json",
		},
		{
			name: "wrong report schema",
			mutate: func(cold, _ map[string]any) {
				cold["schema_version"] = "rdev.layered-run-report.v0"
			},
			failedCheck: "cold_layered_run_valid",
			absentPath:  "evidence/cold-layered-run.json",
		},
		{
			name: "negative stage duration",
			mutate: func(_, warm map[string]any) {
				setWindowsLayeredRunStageDurationForTest(warm, "runtime-download", -1)
			},
			failedCheck: "warm_layered_run_valid",
			absentPath:  "evidence/warm-layered-run.json",
		},
		{
			name: "ticket text",
			mutate: func(cold, _ map[string]any) {
				cold["note"] = "ticket_code=ABCD-1234"
			},
			failedCheck: "cold_layered_run_valid",
			absentPath:  "evidence/cold-layered-run.json",
		},
		{
			name: "gateway text",
			mutate: func(_, warm map[string]any) {
				warm["note"] = "gateway_url=https://api.example.com/v1"
			},
			failedCheck: "warm_layered_run_valid",
			absentPath:  "evidence/warm-layered-run.json",
		},
		{
			name: "token text",
			mutate: func(cold, _ map[string]any) {
				cold["note"] = "token=secret-value"
			},
			failedCheck: "cold_layered_run_valid",
			absentPath:  "evidence/cold-layered-run.json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true}`)
			cold := windowsLayeredRunReportForTest(false)
			warm := windowsLayeredRunReportForTest(true)
			test.mutate(cold, warm)
			coldPath := writeWindowsLayeredRunReportForTest(t, fixture.root, "cold-layered-run.json", cold)
			warmPath := writeWindowsLayeredRunReportForTest(t, fixture.root, "warm-layered-run.json", warm)
			outDir := filepath.Join(fixture.root, "package")

			pkg, err := PackageWindowsTemporaryEvidence(windowsTemporaryLayeredPackageOptions(fixture, outDir, coldPath, warmPath))
			if err != nil {
				t.Fatal(err)
			}
			if pkg.OK() {
				t.Fatalf("expected invalid layered report to fail package verification: %#v", pkg.Checks)
			}
			assertWindowsTemporaryPackageCheck(t, pkg.Checks, test.failedCheck, false)
			if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(test.absentPath))); !os.IsNotExist(err) {
				t.Fatalf("invalid report was copied to %s: %v", test.absentPath, err)
			}
		})
	}
}

func TestPackageWindowsTemporaryEvidenceRejectsMismatchedLayeredRuntimePair(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true}`)
	cold := windowsLayeredRunReportForTest(false)
	warm := windowsLayeredRunReportForTest(true)
	warm["bytes"] = int64(8192)
	coldPath := writeWindowsLayeredRunReportForTest(t, fixture.root, "pair-cold-layered-run.json", cold)
	warmPath := writeWindowsLayeredRunReportForTest(t, fixture.root, "pair-warm-layered-run.json", warm)
	outDir := filepath.Join(fixture.root, "package")

	pkg, err := PackageWindowsTemporaryEvidence(windowsTemporaryLayeredPackageOptions(fixture, outDir, coldPath, warmPath))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatalf("expected mismatched cold/warm runtime reports to fail: %#v", pkg.Checks)
	}
	assertWindowsTemporaryPackageCheck(t, pkg.Checks, "layered_run_pair_valid", false)
	for _, reportPath := range []string{"evidence/cold-layered-run.json", "evidence/warm-layered-run.json"} {
		if _, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(reportPath))); !os.IsNotExist(err) {
			t.Fatalf("mismatched report pair was copied to %s: %v", reportPath, err)
		}
	}
}

func windowsTemporaryLayeredPackageOptions(fixture windowsTemporaryPackageFixture, outDir, coldPath, warmPath string) WindowsTemporaryPackageOptions {
	return WindowsTemporaryPackageOptions{
		PlanPath:                fixture.planPath,
		OutDir:                  outDir,
		TranscriptPath:          fixture.transcriptPath,
		ReleaseVerificationPath: fixture.releaseVerificationPath,
		AuditPath:               fixture.auditPath,
		NoPersistenceDir:        fixture.noPersistenceDir,
		DenialProbesDir:         fixture.denialProbesDir,
		ColdLayeredRunPath:      coldPath,
		WarmLayeredRunPath:      warmPath,
		Now:                     time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC),
	}
}

func windowsLayeredRunReportForTest(fromCache bool) map[string]any {
	return map[string]any{
		"schema_version": "rdev.layered-run-report.v1",
		"asset_id":       "rdev-host-windows-amd64",
		"from_cache":     fromCache,
		"resumed":        false,
		"bytes":          int64(4096),
		"stages": []any{
			map[string]any{"name": "manifest-fetch", "duration_ms": int64(4)},
			map[string]any{"name": "signature-verification", "duration_ms": int64(2)},
			map[string]any{"name": "runtime-download", "duration_ms": int64(8)},
			map[string]any{"name": "runtime-launch-preparation", "duration_ms": int64(1)},
		},
	}
}

func writeWindowsLayeredRunReportForTest(t *testing.T, dir, name string, report map[string]any) string {
	t.Helper()
	content := marshalWindowsLayeredRunReportForTest(t, report)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func marshalWindowsLayeredRunReportForTest(t *testing.T, report map[string]any) []byte {
	t.Helper()
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(content, '\n')
}

func removeWindowsLayeredRunStageForTest(report map[string]any, name string) {
	stages, _ := report["stages"].([]any)
	filtered := make([]any, 0, len(stages))
	for _, value := range stages {
		stage, _ := value.(map[string]any)
		if stage["name"] != name {
			filtered = append(filtered, value)
		}
	}
	report["stages"] = filtered
}

func setWindowsLayeredRunStageDurationForTest(report map[string]any, name string, duration int64) {
	stages, _ := report["stages"].([]any)
	for _, value := range stages {
		stage, _ := value.(map[string]any)
		if stage["name"] == name {
			stage["duration_ms"] = duration
			return
		}
	}
}

func assertWindowsTemporaryPackageCheck(t *testing.T, checks []Check, name string, passed bool) {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			if check.Passed != passed {
				t.Fatalf("check %s passed=%t, want %t: %#v", name, check.Passed, passed, check)
			}
			return
		}
	}
	t.Fatalf("missing package check %s: %#v", name, checks)
}

func windowsTemporaryPackageContainsPath(files []WindowsTemporaryPackageFile, path string) bool {
	for _, file := range files {
		if file.Path == path && file.SizeBytes > 0 && strings.HasPrefix(file.SHA256, "sha256:") {
			return true
		}
	}
	return false
}

func windowsTemporaryRequiredEvidenceContains(required []string, name string) bool {
	for _, value := range required {
		if strings.Contains(value, name) {
			return true
		}
	}
	return false
}
