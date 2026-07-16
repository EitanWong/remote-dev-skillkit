//go:build windows

package connectionentry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsLayeredCommandLauncherFailsClosedWithoutArchiveExecution(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	fallbackBootstrap := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(fallbackBootstrap, []byte("Write-Output 'fixture archive fallback'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.options.WindowsBootstrapScriptPath = fallbackBootstrap
	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}

	launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != launcherPath {
		t.Fatalf("expected the native command launcher to be primary: %#v", plan.EntryPackagePlan)
	}

	localAppData := filepath.Join(t.TempDir(), "local-app-data")
	command := exec.Command("cmd.exe", "/d", "/c", launcherPath)
	command.Env = append(os.Environ(), "LOCALAPPDATA="+localAppData)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("expected fixture bootstrap execution to fail")
	}
	text := string(output)
	if !strings.Contains(text, "Layered bootstrap failed verification or execution; refusing automatic archive fallback.") {
		t.Fatalf("expected bootstrap verification and execution path, got:\n%s", text)
	}
	if !strings.Contains(text, "Run the verified archive fallback explicitly:") {
		t.Fatalf("expected an explicit archive fallback instruction, got:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "powershell") {
		t.Fatalf("native launcher must not invoke PowerShell: %s", text)
	}
	if _, statErr := os.Stat(filepath.Join(localAppData, "RemoteDevSkillkit", "cache")); statErr != nil {
		t.Fatalf("native launcher did not create the user-level cache: %v", statErr)
	}
}

func TestWindowsLayeredCommandLauncherRejectsReparseAncestor(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	fallbackBootstrap := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(fallbackBootstrap, []byte("Write-Output 'fixture archive fallback'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.options.WindowsBootstrapScriptPath = fallbackBootstrap

	junctionTarget := filepath.Join(t.TempDir(), "junction-target")
	if err := os.Mkdir(junctionTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(t.TempDir(), "junction")
	createWindowsJunction(t, junction, junctionTarget)
	fixture.options.OutDir = filepath.Join(junction, "entry")

	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != launcherPath {
		t.Fatalf("expected the native command launcher to be primary: %#v", plan.EntryPackagePlan)
	}

	command := exec.Command("cmd.exe", "/d", "/c", launcherPath)
	command.Env = append(os.Environ(), "LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("expected a reparse-point ancestor to fail closed")
	}
	text := string(output)
	if !strings.Contains(text, "Layered bootstrap preparation failed; refusing automatic archive fallback.") {
		t.Fatalf("expected reparse-point preparation failure, got:\n%s", text)
	}
	if strings.Contains(text, "Layered bootstrap failed verification or execution") {
		t.Fatalf("reparse-point ancestor must stop before bootstrap execution:\n%s", text)
	}
}

func TestWindowsLayeredCommandLauncherHandlesUnicodePath(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	fallbackBootstrap := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(fallbackBootstrap, []byte("Write-Output 'fixture archive fallback'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.options.WindowsBootstrapScriptPath = fallbackBootstrap
	fixture.options.OutDir = filepath.Join(t.TempDir(), "连接入口", "entry")

	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != launcherPath {
		t.Fatalf("expected the native command launcher to be primary: %#v", plan.EntryPackagePlan)
	}

	command := exec.Command("cmd.exe", "/d", "/c", launcherPath)
	command.Env = append(os.Environ(), "LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("expected fixture bootstrap execution to fail")
	}
	text := string(output)
	if !strings.Contains(text, "Layered bootstrap failed verification or execution; refusing automatic archive fallback.") {
		t.Fatalf("expected bootstrap verification and execution path for a Unicode path, got:\n%s", text)
	}
}

func createWindowsJunction(t *testing.T, junction, target string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction setup is unavailable on this Windows host: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("cmd.exe", "/d", "/c", "rmdir", junction).Run()
	})
}
