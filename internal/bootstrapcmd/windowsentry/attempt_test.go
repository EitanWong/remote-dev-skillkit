//go:build !rdev_bootstrap_focused

package windowsentry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

func TestAttemptStrictJSON(t *testing.T) {
	valid := `{"schema_version":"rdev.windows-layered-attempt.v1","attempt_id":"opaque-id","stage":"pre_core","launcher":"powershell","updated_at":"2026-07-17T08:00:00Z"}`
	state, err := decodeAttemptState([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if state.AttemptID != "opaque-id" || state.Stage != attemptStagePreCore || state.Launcher != launcherPowerShell {
		t.Fatalf("unexpected decoded attempt state: %#v", state)
	}
	for _, content := range []string{
		strings.Replace(valid, `"stage":"pre_core"`, `"stage":"pre_core","selected_route":"direct"`, 1),
		strings.Replace(valid, `"stage":"pre_core"`, `"stage":"pre_core","stage":"core_started"`, 1),
		valid + `{}`,
		strings.Replace(valid, `,"launcher":"powershell"`, "", 1),
		strings.Replace(valid, "rdev.windows-layered-attempt.v1", "rdev.windows-layered-attempt.v0", 1),
		strings.Replace(valid, "pre_core", "invalid", 1),
		strings.Replace(valid, "powershell", "pwsh", 1),
		strings.Replace(valid, "2026-07-17T08:00:00Z", "not-a-time", 1),
		"\v" + valid,
		valid + "\f",
		"\u00a0" + valid,
	} {
		if _, err := decodeAttemptState([]byte(content)); err == nil {
			t.Fatalf("strict attempt decoder accepted %s", content)
		}
	}
}

func TestAttemptTransitionsAreForwardOnly(t *testing.T) {
	allowed := map[[2]attemptStage]bool{
		{attemptStagePreCore, attemptStageCoreStarted}:    true,
		{attemptStageCoreStarted, attemptStageCoreExited}: true,
	}
	for _, from := range []attemptStage{attemptStagePreCore, attemptStageCoreStarted, attemptStageCoreExited} {
		for _, to := range []attemptStage{attemptStagePreCore, attemptStageCoreStarted, attemptStageCoreExited} {
			if got := validAttemptTransition(from, to); got != allowed[[2]attemptStage{from, to}] {
				t.Errorf("transition %s -> %s = %t", from, to, got)
			}
		}
	}
}

func TestAttemptStateIsPrivateAndAtomicallyReplaced(t *testing.T) {
	directory := privateAttemptDirForTest(t)
	guard, err := acquireAttempt(directory, launcherPowerShell, time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer guard.close()

	statePath := filepath.Join(directory, attemptStateFilename)
	before, err := os.Lstat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateAttemptPathForTest(t, directory, true)
	assertPrivateAttemptPathForTest(t, statePath, false)
	assertPrivateAttemptPathForTest(t, filepath.Join(directory, attemptLockFilename), false)
	if err := guard.transition(attemptStageCoreStarted, time.Date(2026, 7, 17, 8, 0, 1, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("attempt state was updated in place instead of atomically replaced")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("attempt transition left temporary state file %q", entry.Name())
		}
	}
}

func TestAttemptRejectsUnsafePathsAndManagedFiles(t *testing.T) {
	for _, directory := range []string{`\\server\share\attempt`, `//server/share/attempt`, "relative-attempt"} {
		if _, err := acquireAttempt(directory, launcherPowerShell, time.Now()); err == nil {
			t.Fatalf("unsafe attempt path %q was accepted", directory)
		}
	}

	root := canonicalWindowsEntryTestTempDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("create attempt symlink fixture: %v", err)
	}
	if _, err := acquireAttempt(link, launcherPowerShell, time.Now()); err == nil {
		t.Fatal("attempt directory symlink was accepted")
	}

	directory := filepath.Join(root, "state-symlink-attempt")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	targetState := filepath.Join(root, "target-state")
	if err := os.WriteFile(targetState, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetState, filepath.Join(directory, attemptStateFilename)); err != nil {
		t.Skipf("create state symlink fixture: %v", err)
	}
	if _, err := acquireAttempt(directory, launcherPowerShell, time.Now()); err == nil {
		t.Fatal("attempt state symlink was accepted")
	}
}

func TestLayeredRunStartsCoreOnceWithFreshAttempt(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	transport := &recordingTransport{responses: map[string]transportFixture{
		fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
		fixture.coreURL:     {status: 200, content: fixture.core},
	}}
	attemptDir := privateAttemptDirForTest(t)
	args := windowsEntryAttemptArgs(fixture, windowsEntryTestCacheDir(t), attemptDir, launcherPowerShell)

	var stdout bytes.Buffer
	var launchedArgs []string
	runnerCount := 0
	app := App{
		Stdout:    &stdout,
		Stderr:    io.Discard,
		Transport: transport,
		Now:       fixture.now,
		CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
			runnerCount++
			launchedArgs = append([]string(nil), args...)
			return successfulTestCommand(ctx, path, args...)
		},
	}
	if err := app.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if runnerCount != 1 {
		t.Fatalf("core runner count = %d, want 1", runnerCount)
	}
	if !slices.Contains(launchedArgs, "--transport") || !slices.Contains(launchedArgs, "auto") {
		t.Fatalf("core args do not preserve --transport auto: %q", launchedArgs)
	}

	content, err := os.ReadFile(filepath.Join(attemptDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(content, &state); err != nil {
		t.Fatal(err)
	}
	if state["schema_version"] != "rdev.windows-layered-attempt.v1" || state["stage"] != "core_exited" || state["launcher"] != "powershell" {
		t.Fatalf("unexpected final attempt state: %s", content)
	}
	if _, found := state["selected_route"]; found {
		t.Fatalf("attempt state persisted route selection: %s", content)
	}
}

func TestLayeredRunStartsCoreOnceAcrossLaunchers(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	attemptDir := privateAttemptDirForTest(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var runnerCount atomic.Int32
	for _, launcher := range []attemptLauncher{launcherPowerShell, launcherCMD} {
		launcher := launcher
		transport := &recordingTransport{responses: map[string]transportFixture{
			fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
			fixture.coreURL:     {status: 200, content: fixture.core},
		}}
		app := App{
			Stdout:    io.Discard,
			Stderr:    io.Discard,
			Transport: transport,
			Now:       fixture.now,
			CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
				runnerCount.Add(1)
				return successfulTestCommand(ctx, path, args...)
			},
		}
		go func() {
			<-start
			results <- app.Run(t.Context(), windowsEntryAttemptArgs(fixture, windowsEntryTestCacheDir(t), attemptDir, launcher))
		}()
	}
	close(start)
	err1, err2 := <-results, <-results
	if err1 != nil && err2 != nil {
		t.Fatalf("both launchers failed: %v; %v", err1, err2)
	}
	if runnerCount.Load() != 1 {
		t.Fatalf("concurrent core runner count = %d, want 1", runnerCount.Load())
	}
}

func TestLayeredRunStartsCoreOnceRejectsSecondStart(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	attemptDir := privateAttemptDirForTest(t)
	var runnerCount atomic.Int32
	newApp := func() App {
		return App{
			Stdout: io.Discard,
			Stderr: io.Discard,
			Transport: &recordingTransport{responses: map[string]transportFixture{
				fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
				fixture.coreURL:     {status: 200, content: fixture.core},
			}},
			Now: fixture.now,
			CommandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
				runnerCount.Add(1)
				return successfulTestCommand(ctx, path, args...)
			},
		}
	}
	if err := newApp().Run(t.Context(), windowsEntryAttemptArgs(fixture, windowsEntryTestCacheDir(t), attemptDir, launcherPowerShell)); err != nil {
		t.Fatal(err)
	}
	err := newApp().Run(t.Context(), windowsEntryAttemptArgs(fixture, windowsEntryTestCacheDir(t), attemptDir, launcherPowerShellBypass))
	if err == nil {
		t.Fatal("second core start was accepted")
	}
	if runnerCount.Load() != 1 {
		t.Fatalf("core runner count after second launch = %d, want 1", runnerCount.Load())
	}
}

func TestLayeredRunCancellationReapsChildBeforeCoreExited(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	attemptDir := privateAttemptDirForTest(t)
	readyPath := filepath.Join(t.TempDir(), "child-ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var command *exec.Cmd
	app := App{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Transport: &recordingTransport{responses: map[string]transportFixture{
			fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
			fixture.coreURL:     {status: 200, content: fixture.core},
		}},
		Now: fixture.now,
		CommandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			command = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAttemptBlockingHelperProcess$")
			command.Env = append(os.Environ(), "RDEV_ATTEMPT_BLOCKING_HELPER=1", "RDEV_ATTEMPT_READY="+readyPath)
			return command
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx, windowsEntryAttemptArgs(fixture, windowsEntryTestCacheDir(t), attemptDir, launcherPowerShell))
	}()
	waitForAttemptTest(t, func() bool {
		content, err := os.ReadFile(filepath.Join(attemptDir, attemptStateFilename))
		if err != nil {
			return false
		}
		state, err := decodeAttemptState(content)
		return err == nil && state.Stage == attemptStageCoreStarted && fileExistsForAttemptTest(readyPath)
	})
	if command == nil {
		t.Fatal("child command was not created")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled layered run error = %v, want context canceled", err)
	}
	if command.ProcessState == nil {
		t.Fatal("layered run returned before canceled child was reaped")
	}
	content, err := os.ReadFile(filepath.Join(attemptDir, attemptStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeAttemptState(content)
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != attemptStageCoreExited {
		t.Fatalf("attempt stage after reaping = %s, want core_exited", state.Stage)
	}
}

func TestLayeredRunPreCoreErrorIsClassifiedAndRedacted(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	attemptDir := privateAttemptDirForTest(t)
	cacheDir := windowsEntryTestCacheDir(t)
	manifestURL := "https://downloads.example.test/releases/layered.json?token=TOP_SECRET"
	args := windowsEntryAttemptArgs(fixture, cacheDir, attemptDir, launcherCMD)
	args = replaceWindowsEntryFlag(t, args, "--manifest-url", manifestURL)
	var stdout, stderr bytes.Buffer
	err := (App{Stdout: &stdout, Stderr: &stderr, Transport: &recordingTransport{}}).Run(t.Context(), args)
	if err == nil || !strings.HasPrefix(err.Error(), "layered pre-core failure:") {
		t.Fatalf("pre-core error was not stably classified: %T %v", err, err)
	}
	combined := err.Error() + stdout.String() + stderr.String()
	for _, private := range []string{manifestURL, fixture.root, attemptDir, cacheDir, "TOP_SECRET"} {
		if strings.Contains(combined, private) {
			t.Fatalf("pre-core output exposed private value %q: %s", private, combined)
		}
	}
	for _, forbidden := range []string{"ticket", "gateway", "token", "credential"} {
		if strings.Contains(strings.ToLower(combined), forbidden) {
			t.Fatalf("pre-core output exposed forbidden category %q: %s", forbidden, combined)
		}
	}
}

func TestLayeredRunPreCoreCancellationPreservesContext(t *testing.T) {
	fixture := newWindowsEntryFixture(t)
	for _, testCase := range []struct {
		name      string
		transport assetdownload.Transport
		download  func(context.Context, assetdownload.Options) (assetdownload.Result, error)
	}{
		{
			name: "manifest fetch",
			transport: transportFunc(func(ctx context.Context, _ assetdownload.TransportRequest) (assetdownload.TransportResponse, error) {
				<-ctx.Done()
				return assetdownload.TransportResponse{}, ctx.Err()
			}),
		},
		{
			name: "runtime download",
			transport: &recordingTransport{responses: map[string]transportFixture{
				fixture.manifestURL: {status: 200, content: fixture.manifestJSON},
			}},
			download: func(ctx context.Context, _ assetdownload.Options) (assetdownload.Result, error) {
				<-ctx.Done()
				return assetdownload.Result{}, ctx.Err()
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			attemptDir := privateAttemptDirForTest(t)
			err := (App{
				Stdout:    io.Discard,
				Stderr:    io.Discard,
				Transport: testCase.transport,
				Now:       fixture.now,
				download:  testCase.download,
			}).Run(ctx, windowsEntryAttemptArgs(fixture, windowsEntryTestCacheDir(t), attemptDir, launcherPowerShell))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("pre-core cancellation error = %v, want context canceled", err)
			}
			content, readErr := os.ReadFile(filepath.Join(attemptDir, attemptStateFilename))
			if readErr != nil {
				t.Fatal(readErr)
			}
			state, decodeErr := decodeAttemptState(content)
			if decodeErr != nil || state.Stage != attemptStagePreCore {
				t.Fatalf("canceled pre-core state = %#v, %v", state, decodeErr)
			}
		})
	}
}

func TestAttemptProcessLockAcrossProcesses(t *testing.T) {
	if os.Getenv("RDEV_ATTEMPT_PROCESS_HELPER") == "1" {
		guard, err := acquireAttempt(os.Getenv("RDEV_ATTEMPT_DIR"), launcherPowerShell, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		defer guard.close()
		if err := os.WriteFile(os.Getenv("RDEV_ATTEMPT_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		for !fileExistsForAttemptTest(os.Getenv("RDEV_ATTEMPT_RELEASE")) {
			time.Sleep(5 * time.Millisecond)
		}
		return
	}
	directory := privateAttemptDirForTest(t)
	readyPath := filepath.Join(t.TempDir(), "lock-ready")
	releasePath := filepath.Join(t.TempDir(), "lock-release")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAttemptProcessLockAcrossProcesses$")
	cmd.Env = append(os.Environ(),
		"RDEV_ATTEMPT_PROCESS_HELPER=1",
		"RDEV_ATTEMPT_DIR="+directory,
		"RDEV_ATTEMPT_READY="+readyPath,
		"RDEV_ATTEMPT_RELEASE="+releasePath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForAttemptTest(t, func() bool { return fileExistsForAttemptTest(readyPath) })
	if guard, err := acquireAttempt(directory, launcherCMD, time.Now()); err == nil {
		guard.close()
		t.Fatal("second process acquired an active attempt lock")
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestAttemptBlockingHelperProcess(t *testing.T) {
	if os.Getenv("RDEV_ATTEMPT_BLOCKING_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("RDEV_ATTEMPT_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func windowsEntryAttemptArgs(fixture windowsEntryFixture, cacheDir, attemptDir string, launcher attemptLauncher) []string {
	args := fixture.baseArgs(cacheDir)
	separator := slices.Index(args, "--")
	return slices.Concat(args[:separator], []string{"--attempt-dir", attemptDir, "--launcher", string(launcher)}, args[separator:])
}

func waitForAttemptTest(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for attempt test condition")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func fileExistsForAttemptTest(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
