package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyWindowsTemporaryPlan(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "plan")
	if _, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:             planOut,
		HandoffArchivePath: writeWindowsLayeredAcceptanceArchive(t),
		Now:                time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyWindowsTemporaryPlan(filepath.Join(planOut, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !verification.OK() {
		t.Fatalf("expected layered handoff plan verification to pass: %#v", verification.Checks)
	}
}

func TestVerifyWindowsTemporaryPlanRejectsTamperedHandoff(t *testing.T) {
	root := t.TempDir()
	planOut := filepath.Join(root, "plan")
	if _, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:             planOut,
		HandoffArchivePath: writeWindowsLayeredAcceptanceArchive(t),
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planOut, "Windows-ConnectionEntry.zip"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyWindowsTemporaryPlan(filepath.Join(planOut, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.OK() || !strings.Contains(failedCheckNames(verification.Checks), "handoff_archive") {
		t.Fatalf("tampered handoff must fail verification: %#v", verification.Checks)
	}
}

func TestPackageWindowsTemporaryEvidencePackagesAndRedacts(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true, "token": "sk-abcdefghijklmnop"}`)
	packageDir := filepath.Join(fixture.root, "package")

	pkg, err := PackageWindowsTemporaryEvidence(WindowsTemporaryPackageOptions{
		PlanPath:                 fixture.planPath,
		OutDir:                   packageDir,
		TranscriptPath:           fixture.transcriptPath,
		ReleaseVerificationPath:  fixture.releaseVerificationPath,
		AuditPath:                fixture.auditPath,
		NoPersistenceDir:         fixture.noPersistenceDir,
		DenialProbesDir:          fixture.denialProbesDir,
		ColdLayeredRunPath:       fixture.coldLayeredRunPath,
		WarmLayeredRunPath:       fixture.warmLayeredRunPath,
		LayeredEntryEvidencePath: fixture.layeredEntryEvidencePath,
		Now:                      time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.OK() {
		t.Fatalf("expected package checks to pass: %#v", pkg.Checks)
	}
	if pkg.SchemaVersion != WindowsTemporaryPackageSchemaVersion {
		t.Fatalf("unexpected schema %q", pkg.SchemaVersion)
	}
	for _, path := range []string{
		filepath.Join(packageDir, "package.json"),
		filepath.Join(packageDir, "checksums.txt"),
		filepath.Join(packageDir, "plan", "windows-temporary-plan.json"),
		filepath.Join(packageDir, "evidence", "no-persistence", "scheduled-tasks.txt"),
		filepath.Join(packageDir, "evidence", "denial-probes", "package-install.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected packaged file %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(packageDir, "Windows-ConnectionEntry.zip")); !os.IsNotExist(err) {
		t.Fatal("private handoff archive must not enter public acceptance evidence")
	}
	releaseEvidence, err := os.ReadFile(filepath.Join(packageDir, "evidence", "release-verification.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(releaseEvidence), "sk-abcdefghijklmnop") || !strings.Contains(string(releaseEvidence), "[REDACTED:") {
		t.Fatalf("expected release verification evidence to be redacted, got %s", string(releaseEvidence))
	}
	if len(pkg.RedactionRuleCounts) == 0 {
		t.Fatalf("expected redaction counts, got %#v", pkg.RedactionRuleCounts)
	}
}

func TestPackageWindowsTemporaryEvidenceRejectsFailedReleaseVerification(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": false}`)

	pkg, err := PackageWindowsTemporaryEvidence(WindowsTemporaryPackageOptions{
		PlanPath:                 fixture.planPath,
		OutDir:                   filepath.Join(fixture.root, "package"),
		TranscriptPath:           fixture.transcriptPath,
		ReleaseVerificationPath:  fixture.releaseVerificationPath,
		AuditPath:                fixture.auditPath,
		NoPersistenceDir:         fixture.noPersistenceDir,
		DenialProbesDir:          fixture.denialProbesDir,
		ColdLayeredRunPath:       fixture.coldLayeredRunPath,
		WarmLayeredRunPath:       fixture.warmLayeredRunPath,
		LayeredEntryEvidencePath: fixture.layeredEntryEvidencePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.OK() {
		t.Fatal("failed release verification must fail package checks")
	}
	if !strings.Contains(failedCheckNames(pkg.Checks), "release_verification_ok") {
		t.Fatalf("expected release_verification_ok failure: %#v", pkg.Checks)
	}
}

type windowsTemporaryPackageFixture struct {
	root                     string
	planPath                 string
	transcriptPath           string
	releaseVerificationPath  string
	auditPath                string
	noPersistenceDir         string
	denialProbesDir          string
	coldLayeredRunPath       string
	warmLayeredRunPath       string
	layeredEntryEvidencePath string
}

func writeWindowsTemporaryPackageFixture(t *testing.T, releaseVerification string) windowsTemporaryPackageFixture {
	t.Helper()
	root := t.TempDir()
	planOut := filepath.Join(root, "plan")
	plan, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:             planOut,
		HandoffArchivePath: writeWindowsLayeredAcceptanceArchive(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(root, "transcript.txt")
	releaseVerificationPath := filepath.Join(root, "rdev-verify.json")
	auditPath := filepath.Join(root, "audit.jsonl")
	noPersistenceDir := filepath.Join(root, "no-persistence")
	denialProbesDir := filepath.Join(root, "denial-probes")
	if err := os.WriteFile(transcriptPath, []byte("temporary host transcript\nAuthorization: Bearer abcdefghijklmnop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releaseVerificationPath, []byte(releaseVerification+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auditPath, []byte(`{"event":"session.joined"}`+"\n"+`{"event":"task.completed"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEvidenceFiles(t, noPersistenceDir, []string{
		"services.txt",
		"scheduled_tasks.txt",
		"hkcu_run_key.txt",
		"hklm_run_key.txt",
		"startup_folders.txt",
		"firewall_rules.txt",
	})
	writeEvidenceFiles(t, denialProbesDir, []string{
		"package.install.txt",
		"elevation.request.txt",
		"service.manage.txt",
		"gui.control.txt",
		"credential.change.txt",
	})
	coldLayeredRunPath := writeWindowsLayeredRunReportForTest(t, root, "cold-layered-run.json", windowsLayeredRunReportForTest(false))
	warmLayeredRunPath := writeWindowsLayeredRunReportForTest(t, root, "warm-layered-run.json", windowsLayeredRunReportForTest(true))
	layeredEntryEvidence := windowsLayeredEntryEvidenceForTest()
	layeredEntryEvidence["handoff_zip_size_bytes"] = float64(plan.HandoffArchiveSizeBytes)
	layeredEntryEvidence["handoff_zip_sha256"] = plan.HandoffArchiveSHA256
	layeredEntryEvidenceContent, err := json.Marshal(layeredEntryEvidence)
	if err != nil {
		t.Fatal(err)
	}
	layeredEntryEvidencePath := filepath.Join(root, "layered-entry-evidence.json")
	if err := os.WriteFile(layeredEntryEvidencePath, append(layeredEntryEvidenceContent, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return windowsTemporaryPackageFixture{
		root:                     root,
		planPath:                 filepath.Join(planOut, "windows-temporary-plan.json"),
		transcriptPath:           transcriptPath,
		releaseVerificationPath:  releaseVerificationPath,
		auditPath:                auditPath,
		noPersistenceDir:         noPersistenceDir,
		denialProbesDir:          denialProbesDir,
		coldLayeredRunPath:       coldLayeredRunPath,
		warmLayeredRunPath:       warmLayeredRunPath,
		layeredEntryEvidencePath: layeredEntryEvidencePath,
	}
}

func writeEvidenceFiles(t *testing.T, dir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+" ok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
