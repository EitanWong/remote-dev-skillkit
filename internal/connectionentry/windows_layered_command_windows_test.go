//go:build windows

package connectionentry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("RDEV_LAYERED_BOOTSTRAP_FIXTURE") == "1" && len(os.Args) > 1 {
		if os.Args[1] == "layered-run" {
			os.Exit(runLayeredBootstrapFixture(os.Args[2:]))
		}
		if os.Getenv("RDEV_LAYERED_POWERSHELL_FIXTURE") == "1" {
			os.Exit(runPowerShellFixture(os.Args[1:]))
		}
	}
	os.Exit(m.Run())
}

func TestWindowsLayeredBrokerFallbackExecution(t *testing.T) {
	tests := []struct {
		name          string
		scenario      string
		powerShell    string
		wantExitError bool
		wantLaunchers []string
	}{
		{name: "preferred PowerShell success", scenario: "success", wantLaunchers: []string{"powershell"}},
		{name: "current policy failure retries process-scoped policy", scenario: "policy", powerShell: "fixture", wantLaunchers: []string{"powershell-bypass"}},
		{name: "PowerShell runtime absence uses native CMD", scenario: "runtime-absence", powerShell: "missing", wantLaunchers: []string{"cmd"}},
		{name: "download failure retries before core", scenario: "download-failure", wantLaunchers: []string{"powershell", "powershell-bypass"}},
		{name: "core exit failure stops fallback", scenario: "core-exit", wantExitError: true, wantLaunchers: []string{"powershell"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutableWindowsLayeredFixture(t)
			launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
			if test.powerShell != "" {
				powerShellPath := filepath.Join(t.TempDir(), "powershell-fixture.exe")
				if test.powerShell == "missing" {
					powerShellPath = filepath.Join(t.TempDir(), "missing-powershell.exe")
				} else {
					copyCurrentTestExecutable(t, powerShellPath)
				}
				replaceLauncherLine(t, launcherPath, `set "POWERSHELL=`, `set "POWERSHELL=`+powerShellPath+`"`)
			}

			localAppData := filepath.Join(t.TempDir(), "local-app-data")
			markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
			eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
			attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
			command := exec.Command("cmd.exe", "/d", "/c", launcherPath)
			command.Env = append(os.Environ(),
				"LOCALAPPDATA="+localAppData,
				"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
				"RDEV_LAYERED_SCENARIO="+test.scenario,
				"RDEV_LAYERED_MARKER="+markerPath,
				"RDEV_LAYERED_EVENTS="+eventsPath,
				"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
			)
			if test.powerShell == "fixture" {
				command.Env = append(command.Env, "RDEV_LAYERED_POWERSHELL_FIXTURE=1")
			}
			output, err := command.CombinedOutput()
			if (err != nil) != test.wantExitError {
				t.Fatalf("broker error = %v, wantExitError=%t\n%s", err, test.wantExitError, output)
			}
			marker := readOptionalWindowsFixtureLines(t, markerPath)
			if len(marker) != 1 {
				t.Fatalf("core marker count = %d, want 1; marker=%q\n%s", len(marker), marker, output)
			}
			launchers := readOptionalWindowsFixtureLines(t, eventsPath)
			if strings.Join(launchers, ",") != strings.Join(test.wantLaunchers, ",") {
				t.Fatalf("launcher order = %q, want %q\n%s", launchers, test.wantLaunchers, output)
			}
			assertOneWindowsFixtureAttempt(t, attemptsPath, len(test.wantLaunchers))
			if test.scenario == "core-exit" && !strings.Contains(string(output), "core_started core_exited") {
				t.Fatalf("core-exit failure must refuse another path:\n%s", output)
			}
		})
	}
}

func TestWindowsLayeredPowerShellDirectFallsBackToNativeCMD(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredLauncherName)
	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := exec.Command(powerShell, "-NoLogo", "-NoProfile", "-File", launcherPath)
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=direct-fallback",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("direct PowerShell fallback failed: %v\n%s", err, output)
	}
	if marker := readOptionalWindowsFixtureLines(t, markerPath); len(marker) != 1 {
		t.Fatalf("core marker count = %d, want 1; marker=%q\n%s", len(marker), marker, output)
	}
	wantLaunchers := []string{"powershell", "cmd"}
	if launchers := readOptionalWindowsFixtureLines(t, eventsPath); strings.Join(launchers, ",") != strings.Join(wantLaunchers, ",") {
		t.Fatalf("launcher order = %q, want %q\n%s", launchers, wantLaunchers, output)
	}
	assertOneWindowsFixtureAttempt(t, attemptsPath, len(wantLaunchers))
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
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != windowsLayeredDirName+"/"+windowsLayeredLauncherName {
		t.Fatalf("expected the PowerShell launcher to be primary: %#v", plan.EntryPackagePlan)
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
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != windowsLayeredDirName+"/"+windowsLayeredLauncherName {
		t.Fatalf("expected the PowerShell launcher to be primary: %#v", plan.EntryPackagePlan)
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

func newExecutableWindowsLayeredFixture(t *testing.T) windowsLayeredFixture {
	t.Helper()
	fixture := newWindowsLayeredFixture(t)
	fallback := filepath.Join(t.TempDir(), "windows-temporary.ps1")
	if err := os.WriteFile(fallback, []byte("Write-Output 'archive fixture must stay explicit'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.options.WindowsBootstrapScriptPath = fallback
	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != windowsLayeredDirName+"/"+windowsLayeredLauncherName {
		t.Fatalf("expected preferred PowerShell launcher: %#v", plan.EntryPackagePlan)
	}

	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	bootstrapPath := filepath.Join(handoffDir, windowsLayeredBootstrapName)
	copyCurrentTestExecutable(t, bootstrapPath)
	bootstrap, err := os.ReadFile(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bootstrap)
	wantDigest := hex.EncodeToString(digest[:])
	wantSize := strconv.Itoa(len(bootstrap))
	oldSize := strconv.Itoa(len(fixture.bootstrap))
	for _, name := range []string{windowsLayeredLauncherName, windowsLayeredCommandLauncherName} {
		path := filepath.Join(handoffDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(content), fixture.bootstrapSHA256, wantDigest)
		if name == windowsLayeredLauncherName {
			text = strings.Replace(text, "$expectedSize = "+oldSize, "$expectedSize = "+wantSize, 1)
		} else {
			text = strings.Replace(text, `set "EXPECTED_SIZE=`+oldSize+`"`, `set "EXPECTED_SIZE=`+wantSize+`"`, 1)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func copyCurrentTestExecutable(t *testing.T, destination string) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, content, 0o700); err != nil {
		t.Fatal(err)
	}
}

func replaceLauncherLine(t *testing.T, path, prefix, replacement string) {
	t.Helper()
	if strings.ContainsAny(replacement, "%!^&|<>") {
		t.Fatalf("unsafe fixture launcher replacement %q", replacement)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(content), "\n")
	replaced := false
	for index, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[index] = replacement
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatalf("launcher line with prefix %q was not found", prefix)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runLayeredBootstrapFixture(args []string) int {
	attemptDir := fixtureArgument(args, "--attempt-dir")
	launcher := fixtureArgument(args, "--launcher")
	if attemptDir == "" || launcher == "" {
		return 90
	}
	if err := appendWindowsFixtureLine(os.Getenv("RDEV_LAYERED_EVENTS"), launcher); err != nil {
		return 91
	}
	if err := appendWindowsFixtureLine(os.Getenv("RDEV_LAYERED_ATTEMPTS"), filepath.Base(attemptDir)); err != nil {
		return 96
	}
	scenario := os.Getenv("RDEV_LAYERED_SCENARIO")
	if (scenario == "download-failure" || scenario == "direct-fallback") && launcher == "powershell" {
		if err := writeWindowsFixtureAttemptState(attemptDir, launcher, "pre_core"); err != nil {
			return 92
		}
		return 17
	}
	start := scenario == "success" && launcher == "powershell" ||
		scenario == "policy" && launcher == "powershell-bypass" ||
		scenario == "runtime-absence" && launcher == "cmd" ||
		scenario == "download-failure" && launcher == "powershell-bypass" ||
		scenario == "direct-fallback" && launcher == "cmd" ||
		scenario == "core-exit" && launcher == "powershell"
	if !start {
		_ = writeWindowsFixtureAttemptState(attemptDir, launcher, "pre_core")
		return 18
	}
	if err := writeWindowsFixtureAttemptState(attemptDir, launcher, "core_started"); err != nil {
		return 93
	}
	if err := appendWindowsFixtureLine(os.Getenv("RDEV_LAYERED_MARKER"), launcher); err != nil {
		return 94
	}
	if err := writeWindowsFixtureAttemptState(attemptDir, launcher, "core_exited"); err != nil {
		return 95
	}
	if scenario == "core-exit" {
		return 23
	}
	return 0
}

func fixtureArgument(args []string, name string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}

func writeWindowsFixtureAttemptState(directory, launcher, stage string) error {
	attemptID := filepath.Base(directory)
	content := fmt.Sprintf("{\"schema_version\":\"rdev.windows-layered-attempt.v1\",\"attempt_id\":%q,\"stage\":%q,\"launcher\":%q,\"updated_at\":\"2026-07-17T00:00:00Z\"}\n", attemptID, stage, launcher)
	return os.WriteFile(filepath.Join(directory, "state.json"), []byte(content), 0o600)
}

func appendWindowsFixtureLine(path, value string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(file, value)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func runPowerShellFixture(args []string) int {
	bypass := false
	for index := 0; index+1 < len(args); index++ {
		if strings.EqualFold(args[index], "-ExecutionPolicy") && strings.EqualFold(args[index+1], "Bypass") {
			bypass = true
			break
		}
	}
	if !bypass {
		return 19
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := exec.Command(powerShell, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 20
	}
	return 0
}

func readOptionalWindowsFixtureLines(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(content))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func assertOneWindowsFixtureAttempt(t *testing.T, path string, wantEvents int) {
	t.Helper()
	attempts := readOptionalWindowsFixtureLines(t, path)
	if len(attempts) != wantEvents || len(attempts) == 0 {
		t.Fatalf("attempt event count = %d, want %d: %q", len(attempts), wantEvents, attempts)
	}
	for _, attempt := range attempts[1:] {
		if attempt != attempts[0] {
			t.Fatalf("launcher paths used different attempts: %q", attempts)
		}
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
