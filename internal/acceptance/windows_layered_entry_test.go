package acceptance

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunWindowsTemporaryPlanUsesLayeredHandoffOnly(t *testing.T) {
	archivePath := writeWindowsLayeredAcceptanceArchive(t)
	outDir := filepath.Join(t.TempDir(), "acceptance")
	plan, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:             outDir,
		HandoffArchivePath: archivePath,
		Now:                time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HandoffArchiveSHA256 == "" || plan.HandoffArchiveSizeBytes <= 0 || plan.HandoffArchiveSizeBytes > 1<<20 {
		t.Fatalf("invalid measured handoff summary: %#v", plan)
	}
	if plan.PowerShellLauncher != "Start-ConnectionEntry.ps1" || plan.CommandLauncher != "Start-ConnectionEntry.cmd" {
		t.Fatalf("unexpected visible launchers: %#v", plan)
	}
	if !slices.Equal(plan.FallbackOrder, []string{"powershell", "powershell-bypass", "cmd"}) {
		t.Fatalf("fallback order = %#v", plan.FallbackOrder)
	}
	content, err := os.ReadFile(filepath.Join(outDir, "windows-temporary-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(content))
	for _, forbidden := range []string{"rdev-host.exe", "host_download_url", "ticket_code", "gateway_url", filepath.ToSlash(filepath.Dir(archivePath))} {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Fatalf("layered acceptance plan leaked legacy/private value %q:\n%s", forbidden, content)
		}
	}
	if !strings.Contains(lower, "rdev-bootstrap") {
		t.Fatalf("layered acceptance plan omitted bootstrap boundary:\n%s", content)
	}
}

func TestPackageWindowsTemporaryEvidenceDoesNotPublishPrivateSourcePaths(t *testing.T) {
	fixture := writeWindowsTemporaryPackageFixture(t, `{"ok": true}`)
	outDir := filepath.Join(fixture.root, "package")
	pkg, err := PackageWindowsTemporaryEvidence(windowsTemporaryLayeredPackageOptions(fixture, outDir, fixture.coldLayeredRunPath, fixture.warmLayeredRunPath))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(outDir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	public := string(content)
	for _, privatePath := range []string{fixture.root, fixture.planPath, fixture.transcriptPath, fixture.releaseVerificationPath, fixture.auditPath} {
		if strings.Contains(public, privatePath) {
			t.Fatalf("public package leaked private source path %q:\n%s", privatePath, public)
		}
	}
	if filepath.IsAbs(pkg.OutDir) || filepath.IsAbs(pkg.PlanPath) || filepath.IsAbs(pkg.PlanVerification.PlanPath) {
		t.Fatalf("package result contains absolute public paths: %#v", pkg)
	}
	for _, file := range pkg.Files {
		if file.Source != "" {
			t.Fatalf("package file retained private source path: %#v", file)
		}
	}
}

func TestRunWindowsTemporaryPlanRejectsOversizedUncompressedEntry(t *testing.T) {
	archivePath := writeWindowsLayeredAcceptanceArchiveWithBootstrapSize(t, 9<<20)
	_, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:             filepath.Join(t.TempDir(), "acceptance"),
		HandoffArchivePath: archivePath,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "uncompressed") {
		t.Fatalf("expected oversized uncompressed entry rejection, got %v", err)
	}
}

func TestRunWindowsTemporaryPlanRejectsLegacyHelperAfterLauncherScanPrefix(t *testing.T) {
	powerShell := "& $PSScriptRoot\\rdev-bootstrap.exe layered-run\n# " + strings.Repeat("x", 1<<20) + "\nrdev-host.exe\n"
	archivePath := writeWindowsLayeredAcceptanceArchiveWithPowerShell(t, powerShell)
	_, err := RunWindowsTemporaryPlan(WindowsTemporaryOptions{
		OutDir:             filepath.Join(t.TempDir(), "acceptance"),
		HandoffArchivePath: archivePath,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "legacy") {
		t.Fatalf("expected hidden legacy helper rejection, got %v", err)
	}
}

func writeWindowsLayeredAcceptanceArchive(t *testing.T) string {
	return writeWindowsLayeredAcceptanceArchiveWithBootstrapSize(t, len("bootstrap fixture"))
}

func writeWindowsLayeredAcceptanceArchiveWithBootstrapSize(t *testing.T, bootstrapSize int) string {
	return writeWindowsLayeredAcceptanceArchiveFixture(t, bootstrapSize, "& $PSScriptRoot\\rdev-bootstrap.exe layered-run\n")
}

func writeWindowsLayeredAcceptanceArchiveWithPowerShell(t *testing.T, powerShell string) string {
	return writeWindowsLayeredAcceptanceArchiveFixture(t, len("bootstrap fixture"), powerShell)
}

func writeWindowsLayeredAcceptanceArchiveFixture(t *testing.T, bootstrapSize int, powerShell string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Windows-ConnectionEntry.zip")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	files := map[string]string{
		"Start-ConnectionEntry.ps1":            powerShell,
		"Start-ConnectionEntry.cmd":            "@echo off\r\n%~dp0rdev-bootstrap.exe layered-run\r\n",
		"rdev-bootstrap.exe":                   strings.Repeat("x", bootstrapSize),
		"rdev-bootstrap.exe.rdev-release.json": "{}",
		"rdev-bootstrap.exe.sha256":            strings.Repeat("a", 64),
		"windows-layered-verification.json":    "{}",
		"ARCHIVE-RECOVERY.txt":                 "rdev-bootstrap.exe layered-run\n",
	}
	for _, name := range []string{
		"Start-ConnectionEntry.ps1",
		"Start-ConnectionEntry.cmd",
		"rdev-bootstrap.exe",
		"rdev-bootstrap.exe.rdev-release.json",
		"rdev-bootstrap.exe.sha256",
		"windows-layered-verification.json",
		"ARCHIVE-RECOVERY.txt",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
