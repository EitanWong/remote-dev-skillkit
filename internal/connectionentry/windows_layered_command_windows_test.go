//go:build windows

package connectionentry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("RDEV_LAYERED_FILE_LOCK_HELPER") == "1" {
		os.Exit(runWindowsFileLockHelper())
	}
	if marker := os.Getenv("RDEV_LAYERED_BOOTSTRAP_EXEC_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("executed\n"), 0o600)
		os.Exit(99)
	}
	if marker := os.Getenv("RDEV_LAYERED_EXECUTABLES"); marker != "" && os.Getenv("RDEV_LAYERED_BOOTSTRAP_FIXTURE") == "1" && len(os.Args) > 1 && os.Args[1] == "layered-run" {
		executable, err := os.Executable()
		if err != nil || appendWindowsFixtureLine(marker, executable) != nil {
			os.Exit(98)
		}
	}
	if os.Getenv("RDEV_LAYERED_BOOTSTRAP_FIXTURE") == "1" && len(os.Args) > 1 {
		if os.Args[1] == "layered-run" {
			if len(os.Args) > 2 && os.Args[2] == "attempt-check" {
				os.Exit(runAttemptCheckFixture(os.Args[3:]))
			}
			if len(os.Args) > 2 && os.Args[2] == "private-path-check" {
				os.Exit(runPrivatePathCheckFixture(os.Args[3:]))
			}
			os.Exit(runLayeredBootstrapFixture(os.Args[2:]))
		}
		if os.Getenv("RDEV_LAYERED_POWERSHELL_FIXTURE") == "1" {
			os.Exit(runPowerShellFixture(os.Args[1:]))
		}
	}
	os.Exit(m.Run())
}

func runPrivatePathCheckFixture(args []string) int {
	path := fixtureArgument(args, "--path")
	kind := fixtureArgument(args, "--kind")
	if len(args) != 4 || path == "" || kind != "file" && kind != "directory" || fixtureReparseAncestor(path) {
		return 90
	}
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() != (kind == "directory") {
		return 91
	}
	return 0
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
			powerShellScriptsPath := filepath.Join(t.TempDir(), "powershell-scripts.txt")
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
				"RDEV_LAYERED_POWERSHELL_SCRIPTS="+powerShellScriptsPath,
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
			if test.powerShell == "fixture" {
				scripts := readOptionalWindowsFixtureLines(t, powerShellScriptsPath)
				if len(scripts) != 2 {
					t.Fatalf("PowerShell fixture invocation count = %d, want 2: %q\n%s", len(scripts), scripts, output)
				}
				for _, path := range scripts {
					if !strings.HasPrefix(filepath.Base(path), ".Start-ConnectionEntry-") {
						t.Fatalf("broker executed a non-staged PowerShell path %q\n%s", path, output)
					}
				}
				if staged, err := filepath.Glob(filepath.Join(fixture.options.OutDir, windowsLayeredDirName, ".Start-ConnectionEntry-*.ps1")); err != nil {
					t.Fatal(err)
				} else if len(staged) != 0 {
					t.Fatalf("broker left staged PowerShell files behind: %q", staged)
				}
			}
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
	fixture := newExecutableWindowsLayeredFixtureAt(t, filepath.Join(t.TempDir(), "连接入口", "entry"))
	launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	command := exec.Command("cmd.exe", "/d", "/c", launcherPath)
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=success",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Unicode launcher path failed: %v\n%s", err, output)
	}
	if marker := readOptionalWindowsFixtureLines(t, markerPath); len(marker) != 1 {
		t.Fatalf("Unicode launcher core marker count = %d, want 1: %q\n%s", len(marker), marker, output)
	}
	assertOneWindowsFixtureAttempt(t, attemptsPath, 1)
}

func TestWindowsLayeredBrokerAuthenticatesPowerShellBeforeBypass(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	commandPath := filepath.Join(handoffDir, windowsLayeredCommandLauncherName)
	powerShellPath := filepath.Join(handoffDir, windowsLayeredLauncherName)
	tamperMarker := filepath.Join(t.TempDir(), "tampered-powershell.txt")
	tampered := "Set-Content -LiteralPath '" + strings.ReplaceAll(tamperMarker, "'", "''") + "' -Value tampered\nexit 19\n"
	if err := os.WriteFile(powerShellPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	command := exec.Command("cmd.exe", "/d", "/c", commandPath)
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=runtime-absence",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native fallback after PowerShell authentication failure failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(tamperMarker); !os.IsNotExist(err) {
		t.Fatalf("tampered PowerShell executed before native fallback: err=%v\n%s", err, output)
	}
	if got := readOptionalWindowsFixtureLines(t, eventsPath); strings.Join(got, ",") != "cmd" {
		t.Fatalf("launcher events = %q, want authenticated native-only fallback\n%s", got, output)
	}
	if got := readOptionalWindowsFixtureLines(t, markerPath); len(got) != 1 {
		t.Fatalf("core marker count = %d, want 1; marker=%q\n%s", len(got), got, output)
	}
}

func TestWindowsLayeredBrokerRejectsWrongDigestBootstrapBeforeExecution(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	bootstrapPath := filepath.Join(handoffDir, windowsLayeredBootstrapName)
	bootstrap, err := os.OpenFile(bootstrapPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Write([]byte{0}); err != nil {
		bootstrap.Close()
		t.Fatal(err)
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}

	executionMarker := filepath.Join(t.TempDir(), "bootstrap-executed.txt")
	coreMarker := filepath.Join(t.TempDir(), "core-marker.txt")
	command := exec.Command("cmd.exe", "/d", "/c", filepath.Join(handoffDir, windowsLayeredCommandLauncherName))
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_BOOTSTRAP_EXEC_MARKER="+executionMarker,
		"RDEV_LAYERED_SCENARIO=success",
		"RDEV_LAYERED_MARKER="+coreMarker,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("wrong-digest bootstrap was accepted:\n%s", output)
	}
	if _, err := os.Stat(executionMarker); !os.IsNotExist(err) {
		t.Fatalf("wrong-digest bootstrap executed before verification: %v\n%s", err, output)
	}
	if got := readOptionalWindowsFixtureLines(t, coreMarker); len(got) != 0 {
		t.Fatalf("wrong-digest bootstrap started a core: %q\n%s", got, output)
	}
}

func TestWindowsLayeredBrokerRemovesUntrustedExplicitACEBeforeBootstrap(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	bootstrapPath := filepath.Join(handoffDir, windowsLayeredBootstrapName)
	aclTool := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	if output, err := exec.Command(aclTool, bootstrapPath, "/grant", "*S-1-5-32-545:R").CombinedOutput(); err != nil {
		t.Fatalf("add untrusted fixture ACE: %v\n%s", err, output)
	}
	if protection, err := inspectWindowsLayeredArchiveProtection(bootstrapPath); err != nil {
		t.Fatal(err)
	} else if err := validateWindowsLayeredArchiveProtectionState(protection); err == nil {
		t.Fatal("fixture bootstrap unexpectedly retained an exact private DACL after adding an untrusted ACE")
	}

	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	command := exec.Command("cmd.exe", "/d", "/c", filepath.Join(handoffDir, windowsLayeredCommandLauncherName))
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=success",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("broker did not normalize the untrusted explicit ACE: %v\n%s", err, output)
	}
	if got := readOptionalWindowsFixtureLines(t, markerPath); len(got) != 1 {
		t.Fatalf("core marker count = %d, want 1 after DACL normalization: %q\n%s", len(got), got, output)
	}
	if protection, err := inspectWindowsLayeredArchiveProtection(bootstrapPath); err != nil {
		t.Fatal(err)
	} else if err := validateWindowsLayeredArchiveProtectionState(protection); err != nil {
		t.Fatalf("broker did not install the exact protected bootstrap DACL: %v", err)
	}
}

func TestWindowsLayeredNativeStagesAndCleansBootstrap(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	sourceBootstrap := filepath.Join(handoffDir, windowsLayeredBootstrapName)
	commandPath := filepath.Join(handoffDir, windowsLayeredCommandLauncherName)
	replaceLauncherLine(t, commandPath, `set "POWERSHELL=`, `set "POWERSHELL=`+filepath.Join(t.TempDir(), "missing-powershell.exe")+`"`)

	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	executablesPath := filepath.Join(t.TempDir(), "bootstrap-executables.txt")
	command := exec.Command("cmd.exe", "/d", "/c", commandPath)
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=runtime-absence",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
		"RDEV_LAYERED_EXECUTABLES="+executablesPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native staged bootstrap failed: %v\n%s", err, output)
	}
	paths := readOptionalWindowsFixtureLines(t, executablesPath)
	if len(paths) == 0 {
		t.Fatalf("native entry did not execute the bootstrap fixture:\n%s", output)
	}
	for _, path := range paths {
		if strings.EqualFold(path, sourceBootstrap) || !strings.HasPrefix(strings.ToLower(filepath.Base(path)), ".rdev-bootstrap-") {
			t.Fatalf("native entry executed a non-staged bootstrap path %q; source=%q\n%s", path, sourceBootstrap, output)
		}
	}
	if staged, err := filepath.Glob(filepath.Join(handoffDir, ".rdev-bootstrap-*.exe")); err != nil {
		t.Fatal(err)
	} else if len(staged) != 0 {
		t.Fatalf("native entry left staged bootstrap files behind: %q", staged)
	}
	if staged, err := filepath.Glob(filepath.Join(handoffDir, ".Start-ConnectionEntry-*.ps1")); err != nil {
		t.Fatal(err)
	} else if len(staged) != 0 {
		t.Fatalf("native entry left staged PowerShell files behind: %q", staged)
	}
	if got := readOptionalWindowsFixtureLines(t, markerPath); len(got) != 1 {
		t.Fatalf("core marker count = %d, want 1: %q\n%s", len(got), got, output)
	}
}

func TestWindowsLayeredCleanupFailureChangesSuccessExit(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	commandPath := filepath.Join(handoffDir, windowsLayeredCommandLauncherName)
	replaceLauncherLine(t, commandPath, `set "POWERSHELL=`, `set "POWERSHELL=`+filepath.Join(t.TempDir(), "missing-powershell.exe")+`"`)

	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	readyPath := filepath.Join(t.TempDir(), "lock-ready.txt")
	stopPath := filepath.Join(t.TempDir(), "lock-stop.txt")
	command := exec.Command("cmd.exe", "/d", "/c", commandPath)
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+filepath.Join(t.TempDir(), "local-app-data"),
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=runtime-absence",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
		"RDEV_LAYERED_LOCK_STAGED_BOOTSTRAP=1",
		"RDEV_LAYERED_LOCK_READY="+readyPath,
		"RDEV_LAYERED_LOCK_STOP="+stopPath,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("native entry hid a staged-bootstrap cleanup failure:\n%s", output)
	}
	if _, err := os.Stat(readyPath); err != nil {
		t.Fatalf("staged-bootstrap lock helper did not become ready: %v\n%s", err, output)
	}
	if got := readOptionalWindowsFixtureLines(t, markerPath); len(got) != 1 {
		t.Fatalf("core marker count = %d, want 1 before cleanup failure: %q\n%s", len(got), got, output)
	}
	if err := os.WriteFile(stopPath, []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		staged, globErr := filepath.Glob(filepath.Join(handoffDir, ".rdev-bootstrap-*.exe"))
		if globErr != nil {
			t.Fatal(globErr)
		}
		if len(staged) == 0 {
			break
		}
		for _, path := range staged {
			_ = os.Remove(path)
		}
		if time.Now().After(deadline) {
			t.Fatalf("locked staged bootstrap did not become removable: %q", staged)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestWindowsLayeredNativeRejectsInvalidAttemptBeforeBootstrap(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string, string) string
	}{
		{
			name: "outside attempts root",
			setup: func(t *testing.T, _ string, _ string) string {
				path := filepath.Join(t.TempDir(), "outside-attempt")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return path
			},
		},
		{
			name: "active lock",
			setup: func(t *testing.T, _ string, attempt string) string {
				if err := os.WriteFile(filepath.Join(attempt, "attempt.lock"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return attempt
			},
		},
		{
			name: "core started",
			setup: func(t *testing.T, _ string, attempt string) string {
				if err := writeWindowsFixtureAttemptState(attempt, "powershell", "core_started"); err != nil {
					t.Fatal(err)
				}
				return attempt
			},
		},
		{
			name: "core exited",
			setup: func(t *testing.T, _ string, attempt string) string {
				if err := writeWindowsFixtureAttemptState(attempt, "powershell", "core_exited"); err != nil {
					t.Fatal(err)
				}
				return attempt
			},
		},
		{
			name: "malformed pre core",
			setup: func(t *testing.T, _ string, attempt string) string {
				content := `{"schema_version":"rdev.windows-layered-attempt.v1","attempt_id":"attempt-test","stage":"pre_core","launcher":"powershell","updated_at":"2026-07-17T00:00:00Z","extra":true}`
				if err := os.WriteFile(filepath.Join(attempt, "state.json"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				return attempt
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutableWindowsLayeredFixture(t)
			localAppData := filepath.Join(t.TempDir(), "local-app-data")
			attemptRoot := filepath.Join(localAppData, "RemoteDevSkillkit", "attempts")
			attempt := filepath.Join(attemptRoot, "attempt-test")
			if err := os.MkdirAll(attempt, 0o700); err != nil {
				t.Fatal(err)
			}
			attempt = test.setup(t, attemptRoot, attempt)
			markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
			eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
			launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
			command := exec.Command("cmd.exe", "/d", "/c", launcherPath, "--native", "--attempt-dir", attempt)
			command.Env = append(os.Environ(),
				"LOCALAPPDATA="+localAppData,
				"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
				"RDEV_LAYERED_SCENARIO=runtime-absence",
				"RDEV_LAYERED_MARKER="+markerPath,
				"RDEV_LAYERED_EVENTS="+eventsPath,
			)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("invalid native attempt was accepted:\n%s", output)
			}
			if got := readOptionalWindowsFixtureLines(t, markerPath); len(got) != 0 {
				t.Fatalf("invalid attempt started a core: %q\n%s", got, output)
			}
			if got := readOptionalWindowsFixtureLines(t, eventsPath); len(got) != 0 {
				t.Fatalf("invalid attempt reached bootstrap: %q\n%s", got, output)
			}
		})
	}
}

func TestWindowsLayeredLaunchersRejectReparseBeforeCreatingStateRoots(t *testing.T) {
	for _, launcher := range []string{"cmd", "powershell"} {
		t.Run(launcher, func(t *testing.T) {
			fixture := newExecutableWindowsLayeredFixture(t)
			target := filepath.Join(t.TempDir(), "local-app-target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			junction := filepath.Join(t.TempDir(), "local-app-junction")
			createWindowsJunction(t, junction, target)
			stateRoot := filepath.Join(target, "RemoteDevSkillkit")
			handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
			var command *exec.Cmd
			if launcher == "cmd" {
				command = exec.Command("cmd.exe", "/d", "/c", filepath.Join(handoffDir, windowsLayeredCommandLauncherName))
				command.Env = append(os.Environ(),
					"LOCALAPPDATA="+junction,
					"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
				)
			} else {
				path := filepath.Join(handoffDir, windowsLayeredLauncherName)
				powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
				command = exec.Command(powerShell, "-NoLogo", "-NoProfile", "-File", path)
				command.Env = append(os.Environ(),
					"LOCALAPPDATA="+junction,
					"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
				)
			}
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("reparse-backed state root was accepted:\n%s", output)
			}
			if _, err := os.Stat(stateRoot); !os.IsNotExist(err) {
				t.Fatalf("launcher mutated a reparse-backed state root before rejection: err=%v\n%s", err, output)
			}
		})
	}
}

func TestWindowsLayeredBrokerDoesNotReparsePercentPayload(t *testing.T) {
	fixture := newExecutableWindowsLayeredFixture(t)
	injectionMarker := filepath.Join(t.TempDir(), "injected.txt")
	payload := `x" & echo injected>"` + injectionMarker + `" & rem "`
	localAppData := filepath.Join(t.TempDir(), "%RDEV_LAYERED_INJECT%")
	markerPath := filepath.Join(t.TempDir(), "core-marker.txt")
	eventsPath := filepath.Join(t.TempDir(), "launcher-events.txt")
	attemptsPath := filepath.Join(t.TempDir(), "attempt-events.txt")
	launcherPath := filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName)
	command := exec.Command("cmd.exe", "/d", "/c", launcherPath)
	command.Env = append(os.Environ(),
		"LOCALAPPDATA="+localAppData,
		"RDEV_LAYERED_INJECT="+payload,
		"RDEV_LAYERED_BOOTSTRAP_FIXTURE=1",
		"RDEV_LAYERED_SCENARIO=runtime-absence",
		"RDEV_LAYERED_MARKER="+markerPath,
		"RDEV_LAYERED_EVENTS="+eventsPath,
		"RDEV_LAYERED_ATTEMPTS="+attemptsPath,
	)
	output, _ := command.CombinedOutput()
	if _, err := os.Stat(injectionMarker); !os.IsNotExist(err) {
		t.Fatalf("CALL reparsed a percent payload from a runtime path: err=%v\n%s", err, output)
	}
	if got := readOptionalWindowsFixtureLines(t, markerPath); len(got) != 1 {
		t.Fatalf("percent-bearing runtime path did not preserve one native core start: %q\n%s", got, output)
	}
	if got := readOptionalWindowsFixtureLines(t, eventsPath); strings.Join(got, ",") != "powershell,powershell-bypass,cmd" {
		t.Fatalf("launcher order = %q, want shared-attempt fallback through native CMD\n%s", got, output)
	}
	assertOneWindowsFixtureAttempt(t, attemptsPath, 3)
}

func newExecutableWindowsLayeredFixture(t *testing.T) windowsLayeredFixture {
	return newExecutableWindowsLayeredFixtureAt(t, "")
}

func newExecutableWindowsLayeredFixtureAt(t *testing.T, outDir string) windowsLayeredFixture {
	t.Helper()
	fixture := newWindowsLayeredFixture(t)
	if outDir == "" {
		shortRoot, err := os.MkdirTemp("", "rdev-entry-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(shortRoot) })
		outDir = filepath.Join(shortRoot, "entry")
	}
	fixture.options.OutDir = outDir
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
			text = strings.Replace(text, "$bootstrapLock.Length -ne "+oldSize, "$bootstrapLock.Length -ne "+wantSize, 1)
		} else {
			text = strings.Replace(text, `set "EXPECTED_SIZE=`+oldSize+`"`, `set "EXPECTED_SIZE=`+wantSize+`"`, 1)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	powerShellPath := filepath.Join(handoffDir, windowsLayeredLauncherName)
	powerShell, err := os.ReadFile(powerShellPath)
	if err != nil {
		t.Fatal(err)
	}
	powerShellDigest := sha256.Sum256(powerShell)
	commandPath := filepath.Join(handoffDir, windowsLayeredCommandLauncherName)
	replaceLauncherLine(t, commandPath, `set "EXPECTED_PS_SHA256=`, `set "EXPECTED_PS_SHA256=`+hex.EncodeToString(powerShellDigest[:])+`"`)
	replaceLauncherLine(t, commandPath, `set "EXPECTED_PS_SIZE=`, `set "EXPECTED_PS_SIZE=`+strconv.Itoa(len(powerShell))+`"`)
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
	if !validAttemptFixture(attemptDir) {
		return 97
	}
	if os.Getenv("RDEV_LAYERED_LOCK_STAGED_BOOTSTRAP") == "1" {
		if err := startWindowsFileLockHelper(); err != nil {
			return 99
		}
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

func startWindowsFileLockHelper() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable)
	command.Env = append(os.Environ(),
		"RDEV_LAYERED_FILE_LOCK_HELPER=1",
		"RDEV_LAYERED_LOCK_PATH="+executable,
	)
	if err := command.Start(); err != nil {
		return err
	}
	if err := command.Process.Release(); err != nil {
		return err
	}
	readyPath := os.Getenv("RDEV_LAYERED_LOCK_READY")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("file lock helper readiness timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runWindowsFileLockHelper() int {
	pointer, err := syscall.UTF16PtrFromString(os.Getenv("RDEV_LAYERED_LOCK_PATH"))
	if err != nil {
		return 1
	}
	handle, err := syscall.CreateFile(pointer, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 2
	}
	defer syscall.CloseHandle(handle)
	if err := os.WriteFile(os.Getenv("RDEV_LAYERED_LOCK_READY"), []byte("ready\n"), 0o600); err != nil {
		return 3
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("RDEV_LAYERED_LOCK_STOP")); err == nil {
			return 0
		} else if !os.IsNotExist(err) {
			return 4
		}
		if time.Now().After(deadline) {
			return 5
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func runAttemptCheckFixture(args []string) int {
	attemptDir := fixtureArgument(args, "--attempt-dir")
	launcher := fixtureArgument(args, "--launcher")
	if attemptDir == "" || launcher == "" || !validFixtureLauncher(launcher) {
		return 1
	}
	create := false
	for _, arg := range args {
		if arg == "--create" {
			create = true
		}
	}
	root := filepath.Join(os.Getenv("LOCALAPPDATA"), "RemoteDevSkillkit", "attempts")
	if parent := filepath.Dir(attemptDir); !strings.EqualFold(parent, root) || filepath.Base(attemptDir) == "." {
		return 1
	}
	if fixtureReparseAncestor(attemptDir) {
		return 1
	}
	if create {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return 1
		}
		if err := os.Mkdir(attemptDir, 0o700); err != nil {
			if os.IsExist(err) {
				return 2
			}
			return 1
		}
	} else if info, err := os.Stat(attemptDir); err != nil || !info.IsDir() {
		return 1
	}
	if !validAttemptFixture(attemptDir) {
		return 1
	}
	statePath := filepath.Join(attemptDir, "state.json")
	content, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		if err := writeWindowsFixtureAttemptState(attemptDir, launcher, "pre_core"); err != nil {
			return 1
		}
		return 0
	}
	if err != nil || !validAttemptFixtureState(content, filepath.Base(attemptDir)) {
		return 1
	}
	return 0
}

func fixtureReparseAncestor(path string) bool {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			data, ok := info.Sys().(*syscall.Win32FileAttributeData)
			if ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return true
			}
		} else if !os.IsNotExist(err) {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func validAttemptFixture(attemptDir string) bool {
	if _, err := os.Stat(filepath.Join(attemptDir, "attempt.lock")); err == nil || !os.IsNotExist(err) {
		return false
	}
	content, err := os.ReadFile(filepath.Join(attemptDir, "state.json"))
	return os.IsNotExist(err) || err == nil && validAttemptFixtureState(content, filepath.Base(attemptDir))
}

func validAttemptFixtureState(content []byte, attemptID string) bool {
	var state map[string]any
	if err := json.Unmarshal(content, &state); err != nil || len(state) != 5 {
		return false
	}
	return state["schema_version"] == "rdev.windows-layered-attempt.v1" &&
		state["attempt_id"] == attemptID && state["stage"] == "pre_core" &&
		validFixtureLauncher(fmt.Sprint(state["launcher"])) && strings.HasSuffix(fmt.Sprint(state["updated_at"]), "Z")
}

func validFixtureLauncher(value string) bool {
	return value == "powershell" || value == "powershell-bypass" || value == "cmd"
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
	for index := 0; index+1 < len(args); index++ {
		if strings.EqualFold(args[index], "-File") {
			if marker := os.Getenv("RDEV_LAYERED_POWERSHELL_SCRIPTS"); marker != "" {
				if err := appendWindowsFixtureLine(marker, args[index+1]); err != nil {
					return 21
				}
			}
			break
		}
	}
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
