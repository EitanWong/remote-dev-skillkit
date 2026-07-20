package connectionentry

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/signing"
)

func TestWindowsLayeredHandoffArchive(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)

	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryPackagePlan == nil {
		t.Fatalf("expected Windows layered entry package plan: %#v", plan)
	}
	archivePath := filepath.Join(fixture.options.OutDir, "Windows-ConnectionEntry.zip")
	archiveGeneratedFileFound := false
	for _, generatedFile := range plan.GeneratedFiles {
		if generatedFile.Path == archivePath {
			archiveGeneratedFileFound = true
			break
		}
	}
	if !archiveGeneratedFileFound {
		t.Fatalf("generated files do not include final archive path %q: %#v", archivePath, plan.GeneratedFiles)
	}
	for name, artifactPath := range map[string]string{
		"plan":     plan.EntryPackagePlan.PlanPath,
		"launcher": plan.EntryPackagePlan.LauncherPath,
		"archive":  plan.EntryPackagePlan.ArchivePath,
	} {
		if filepath.IsAbs(artifactPath) || artifactPath == "" || strings.Contains(artifactPath, `\`) {
			t.Fatalf("entry package %s path must be a non-empty artifact-relative slash path, got %q", name, artifactPath)
		}
	}
	archive := readTestFile(t, archivePath)
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	assertWindowsLayeredArchivePrivate(t, archivePath)
	if archiveInfo.Size() > maxWindowsLayeredHandoffBytes {
		t.Fatalf("Windows handoff archive exceeds 1 MiB: %d", archiveInfo.Size())
	}
	digest := sha256.Sum256(archive)
	wantSHA256 := hex.EncodeToString(digest[:])
	if plan.EntryPackagePlan.ArchiveSHA256 != wantSHA256 || plan.EntryPackagePlan.ArchiveSizeBytes != archiveInfo.Size() {
		t.Fatalf("archive report mismatch: %#v", plan.EntryPackagePlan)
	}
	assertLayeredArchiveCheck(t, plan.EntryPackagePlan, "windows_layered_archive_sha256", wantSHA256)
	assertLayeredArchiveCheck(t, plan.EntryPackagePlan, "windows_layered_archive_size", strconv.FormatInt(archiveInfo.Size(), 10))
	assertLayeredArchivePrivateCheck(t, plan.EntryPackagePlan)

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	wantNames := []string{
		"ARCHIVE-RECOVERY.txt",
		windowsLayeredCommandLauncherName,
		windowsLayeredLauncherName,
		windowsLayeredBootstrapName,
		windowsLayeredReleaseManifestName,
		windowsLayeredChecksumName,
		windowsLayeredVerificationPlanName,
	}
	if len(reader.File) != len(wantNames) {
		t.Fatalf("archive entries = %d, want %d", len(reader.File), len(wantNames))
	}
	for index, file := range reader.File {
		if file.Name != wantNames[index] {
			t.Errorf("archive entry %d = %q, want %q", index, file.Name, wantNames[index])
		}
		if file.Mode().Perm() != 0o600 {
			t.Errorf("archive entry %q must be private, got mode %o", file.Name, file.Mode().Perm())
		}
	}
	recovery := readZipEntryForTest(t, reader, "ARCHIVE-RECOVERY.txt")
	for _, want := range []string{"rdev-bootstrap.exe", "layered-run", "signed archive recovery profile"} {
		if !strings.Contains(strings.ToLower(string(recovery)), strings.ToLower(want)) {
			t.Errorf("archive recovery instruction missing %q:\n%s", want, recovery)
		}
	}
	for _, file := range reader.File {
		content := readZipEntryForTest(t, reader, file.Name)
		forbidden := []string{
			fixture.controllerDir,
			filepath.ToSlash(fixture.controllerDir),
			"controller-source-private-token-do-not-publish",
		}
		if file.Name != windowsLayeredLauncherName && file.Name != windowsLayeredCommandLauncherName {
			forbidden = append(forbidden, fixture.invite.Ticket.Code, fixture.invite.GatewayURL)
		}
		for _, marker := range forbidden {
			if bytes.Contains(content, []byte(marker)) {
				t.Errorf("archive entry %q leaked %q", file.Name, marker)
			}
		}
	}

	metadata, err := json.Marshal(plan.EntryPackagePlan)
	if err != nil {
		t.Fatal(err)
	}
	metadata = append(metadata, recovery...)
	for _, forbidden := range []string{
		fixture.invite.Ticket.Code,
		fixture.invite.GatewayURL,
		fixture.controllerDir,
		filepath.ToSlash(fixture.controllerDir),
		"controller-source-private-token-do-not-publish",
	} {
		if bytes.Contains(metadata, []byte(forbidden)) {
			t.Errorf("archive report or recovery metadata leaked %q: %s", forbidden, metadata)
		}
	}
	topLevelChecks, err := json.Marshal(plan.Checks)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		fixture.invite.ConnectionEntry.EntryURL,
		fixture.invite.ManifestURL,
		fixture.invite.ManifestRootPublicKey,
		fixture.invite.Ticket.Code,
		fixture.invite.GatewayURL,
		"controller-source-private-token-do-not-publish",
	} {
		if bytes.Contains(topLevelChecks, []byte(forbidden)) {
			t.Errorf("top-level plan checks leaked %q: %s", forbidden, topLevelChecks)
		}
	}
	packageMetadata, err := json.Marshal(struct {
		Package *EntryPackagePlan `json:"entry_package_plan"`
		Checks  []Check           `json:"checks"`
	}{Package: plan.EntryPackagePlan, Checks: plan.Checks})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		fixture.options.OutDir,
		fixture.controllerDir,
		filepath.ToSlash(fixture.options.OutDir),
		filepath.ToSlash(fixture.controllerDir),
	} {
		if bytes.Contains(packageMetadata, []byte(forbidden)) {
			t.Errorf("serialized entry package metadata leaked controller-local path %q: %s", forbidden, packageMetadata)
		}
	}
	for _, check := range plan.Checks {
		if check.Name == "entry_package_plan" && check.Detail != "ready" {
			t.Errorf("entry package plan check detail = %q, want status-only detail ready", check.Detail)
		}
	}
}

func TestWindowsLayeredCheckTreeRedactsPrivateDetails(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)

	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RunnerPlan == nil || plan.EntryPackagePlan == nil {
		t.Fatalf("expected complete Windows layered plan: %#v", plan)
	}

	checkTree, err := json.Marshal(struct {
		Checks             []Check `json:"checks"`
		RunnerPlanChecks   any     `json:"runner_plan_checks"`
		EntryPackageChecks any     `json:"entry_package_checks"`
	}{
		Checks:             plan.Checks,
		RunnerPlanChecks:   plan.RunnerPlan.Checks,
		EntryPackageChecks: plan.EntryPackagePlan.Checks,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		fixture.invite.ConnectionEntry.EntryURL,
		fixture.invite.ManifestURL,
		fixture.invite.ManifestRootPublicKey,
		fixture.invite.Ticket.Code,
		fixture.invite.GatewayURL,
		fixture.options.OutDir,
		filepath.ToSlash(fixture.options.OutDir),
		fixture.controllerDir,
		filepath.ToSlash(fixture.controllerDir),
		"controller-source-private-token-do-not-publish",
	} {
		if bytes.Contains(checkTree, []byte(forbidden)) {
			t.Errorf("serialized check tree leaked %q: %s", forbidden, checkTree)
		}
	}
	for _, check := range plan.RunnerPlan.Checks {
		want := "failed"
		if check.Passed {
			want = "ready"
		}
		if check.Detail != want {
			t.Errorf("runner check %q detail = %q, want status-only %q", check.Name, check.Detail, want)
		}
	}
}

func assertLayeredArchivePrivateCheck(t *testing.T, plan *EntryPackagePlan) {
	t.Helper()
	for _, check := range plan.Checks {
		if check.Name == "windows_layered_archive_private" {
			if !check.Passed || check.Detail == "" {
				t.Fatalf("archive private check = %#v, want verified detail", check)
			}
			return
		}
	}
	t.Fatalf("missing archive private check: %#v", plan.Checks)
}

func assertLayeredArchiveCheck(t *testing.T, plan *EntryPackagePlan, name, detail string) {
	t.Helper()
	for _, check := range plan.Checks {
		if check.Name == name {
			if !check.Passed || check.Detail != detail {
				t.Fatalf("archive check %q = %#v, want passed detail %q", name, check, detail)
			}
			return
		}
	}
	t.Fatalf("missing archive check %q: %#v", name, plan.Checks)
}

func readZipEntryForTest(t *testing.T, reader *zip.ReadCloser, name string) []byte {
	t.Helper()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer entry.Close()
		content := new(bytes.Buffer)
		if _, err := content.ReadFrom(entry); err != nil {
			t.Fatal(err)
		}
		return content.Bytes()
	}
	t.Fatalf("missing ZIP entry %q", name)
	return nil
}

func TestWindowsConnectionEntryPrefersLayeredBootstrapAndRetainsArchiveFallback(t *testing.T) {
	t.Run("verified layered bootstrap is the primary handoff", func(t *testing.T) {
		fixture := newWindowsLayeredFixture(t)

		plan, err := FromInvite(fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		if plan.EntryPackagePlan == nil {
			t.Fatalf("expected a layered entry package plan: %#v", plan)
		}
		if plan.EntryPackagePlan.PackageMode != "private-windows-layered-handoff" ||
			plan.EntryPackagePlan.PlatformPlanKind != "windows-layered-handoff" {
			t.Fatalf("expected the verified layered handoff to be preferred: %#v", plan.EntryPackagePlan)
		}

		legacyLauncherPath := filepath.Join(fixture.options.OutDir, "windows-layered", windowsLayeredLauncherName)
		launcher := readTestFile(t, legacyLauncherPath)
		normalizedLauncher := normalizePowerShellForTest(string(launcher))
		assertStringsInOrder(t, normalizedLauncher,
			"function Invoke-Layered",
			"layered-run",
			"--manifest-url", fixture.options.LayeredAssetsManifestURL,
			"--root-public-key", fixture.options.ReleaseRootPublicKey,
			"--expected-release-version", fixture.options.LayeredReleaseVersion,
			"--platform", "windows/amd64",
			"--cache-dir",
			"--mode", "temporary",
			"--", "serve",
			"--mode", "temporary",
			"--manifest-url", fixture.invite.ManifestURL,
			"--manifest-root-public-key", fixture.invite.ManifestRootPublicKey,
			"--transport", "auto",
			"--once=false",
			"--max-tasks", "0",
		)
		if !strings.Contains(normalizedLauncher, "LOCALAPPDATA") ||
			!strings.Contains(normalizedLauncher, "RemoteDevSkillkit") ||
			!strings.Contains(normalizedLauncher, "cache") {
			t.Fatalf("launcher must use the current user's LocalApplicationData cache:\n%s", launcher)
		}
		if !strings.Contains(normalizedLauncher, "SHA256") ||
			!strings.Contains(strings.ToLower(normalizedLauncher), fixture.bootstrapSHA256) {
			t.Fatalf("launcher must recheck the controller-verified bootstrap SHA-256:\n%s", launcher)
		}
		for _, want := range []string{
			"FileAttributes]::ReparsePoint",
			"FileShare]::Read",
			"ComputeHash($bootstrapLock)",
			"$bootstrapLock.Length",
			"WindowsIdentity]::GetCurrent().User.Value",
			"icacls.exe",
			"UNC paths are not allowed",
			"ACL grants access to an untrusted identity",
		} {
			if !strings.Contains(normalizedLauncher, want) {
				t.Fatalf("launcher must protect the bootstrap path and user cache with %q:\n%s", want, launcher)
			}
		}
		if strings.Contains(normalizedLauncher, "$writeMask") {
			t.Fatalf("private handoff ACL validation must reject untrusted read ACEs as well as write ACEs:\n%s", launcher)
		}
		if strings.Count(normalizedLauncher, "--transport") != 1 || strings.Contains(normalizedLauncher, "--transport long-poll") {
			t.Fatalf("layered launcher must preserve the runtime's transport fallback policy:\n%s", launcher)
		}
		for _, fallback := range []string{"signed archive recovery profile", "ARCHIVE-RECOVERY.txt"} {
			if !strings.Contains(normalizedLauncher, fallback) {
				t.Fatalf("layered failure must name the separately verified archive recovery command %q:\n%s", fallback, launcher)
			}
		}
		for _, forbidden := range []string{"Start-Process", "Start-Job", "WindowStyle Hidden", "--gateway", "--ticket-code"} {
			if strings.Contains(normalizedLauncher, forbidden) {
				t.Fatalf("layered launcher must stay foreground and obtain gateway data from the signed join manifest; found %q:\n%s", forbidden, launcher)
			}
		}

		handoffDir := filepath.Dir(legacyLauncherPath)
		if filepath.Base(handoffDir) != "windows-layered" {
			t.Fatalf("expected a focused windows-layered handoff, got %q", handoffDir)
		}
		fallbackPath := filepath.Join(fixture.options.OutDir, "windows-temporary", "Start-ConnectionEntry.ps1")
		fallback := readTestFile(t, fallbackPath)
		for _, want := range []string{"rdev-verify", fixture.options.ReleaseBundleURL, fixture.options.ReleaseRootPublicKey} {
			if !strings.Contains(string(fallback), want) {
				t.Fatalf("verified archive fallback missing %q:\n%s", want, fallback)
			}
		}
		if strings.Contains(normalizedLauncher, "& $fallbackPath") {
			t.Fatalf("layered verification/runtime failures must not automatically execute the archive fallback:\n%s", launcher)
		}
		assertStringsInOrder(t, normalizedLauncher,
			"try {",
			"ComputeHash($bootstrapLock)",
			"Invoke-Layered $Launcher",
			"exit $layeredExitCode",
			"} catch {",
			"Run the signed archive recovery profile",
			"exit 1",
		)
		packagedBootstrap := filepath.Join(handoffDir, "rdev-bootstrap.exe")
		if got := readTestFile(t, packagedBootstrap); !bytes.Equal(got, fixture.bootstrap) {
			t.Fatalf("packaged bootstrap is not an exact copy: got %d bytes, want %d", len(got), len(fixture.bootstrap))
		}
		assertPrivateLayeredHandoff(t, handoffDir, fixture)
	})

	t.Run("visible command broker falls back without automatic archive execution", func(t *testing.T) {
		fixture := newWindowsLayeredFixture(t)

		plan, err := FromInvite(fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		handoffDir := filepath.Join(fixture.options.OutDir, "windows-layered")
		cmdPath := filepath.Join(handoffDir, "Start-ConnectionEntry.cmd")
		if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.LauncherPath != windowsLayeredDirName+"/"+windowsLayeredLauncherName {
			t.Fatalf("PowerShell launcher must be the primary entry point: %#v", plan.EntryPackagePlan)
		}
		launcher := strings.ToLower(string(readTestFile(t, cmdPath)))
		for _, want := range []string{
			"start-connectionentry.ps1",
			"rdev-bootstrap.exe",
			"layered-run",
			"certutil.exe",
			"icacls.exe",
			"private-path-check",
			"whoami.exe",
			"/setowner",
			"--manifest-url",
			"--root-public-key",
			"--expected-release-version",
			"--platform",
			"windows/amd64",
			"--cache-dir",
			"--attempt-dir",
			"--launcher",
			"--mode",
			"temporary",
			"attempt-check",
			"--create",
			"powershell-bypass",
			"signed archive recovery profile",
		} {
			if !strings.Contains(launcher, want) {
				t.Fatalf("command broker missing %q:\n%s", want, launcher)
			}
		}
		assertStringsInOrder(t, launcher,
			":reject_unsafe_path",
			"icacls.exe",
			"private-path-check",
		)
		normalizedCMD := strings.ReplaceAll(launcher, "\r\n", "\n")
		rejectStart := strings.LastIndex(normalizedCMD, "\n:reject_unsafe_path\n")
		rejectEnd := strings.Index(normalizedCMD[rejectStart+1:], "\n:load_sid\n")
		if rejectStart < 0 || rejectEnd < 0 {
			t.Fatalf("command broker is missing the early path-validation block:\n%s", launcher)
		}
		if strings.Contains(normalizedCMD[rejectStart:rejectStart+1+rejectEnd], "private-path-check") {
			t.Fatalf("early CMD path checks must not execute the bootstrap before its digest check:\n%s", launcher)
		}
		rejectBlock := normalizedCMD[rejectStart : rejectStart+1+rejectEnd]
		for _, want := range []string{"%%~aI", "if not defined ATTRS", `if not "%ATTRS:l=%"=="%ATTRS%"`} {
			if !strings.Contains(rejectBlock, strings.ToLower(want)) {
				t.Fatalf("early CMD path checks must fail closed on unreadable or reparse attributes with %q:\n%s", want, launcher)
			}
		}
		if strings.Contains(rejectBlock, "fsutil") {
			t.Fatalf("CMD path checks must not classify every fsutil query error as a non-reparse path:\n%s", launcher)
		}
		assertStringsInOrder(t, launcher,
			"powershell.exe",
			"-file",
			"powershell",
			"-executionpolicy", "bypass",
			"-file",
			"powershell-bypass",
			"attempt-check", "--attempt-dir",
			"--launcher", "cmd",
		)
		if strings.Count(launcher, `--transport auto`) != 1 {
			t.Fatalf("the broker and direct-native paths must share exactly one core transport selection:\n%s", launcher)
		}
		for _, forbidden := range []string{"start-process", "start-job", "windowstyle hidden"} {
			if strings.Contains(launcher, forbidden) {
				t.Fatalf("command broker must keep every attempt visible and foreground; found %q:\n%s", forbidden, launcher)
			}
		}
		if reset := strings.Index(launcher, "/reset"); reset < 0 {
			t.Fatalf("command broker must clear unrelated explicit ACL entries before granting its exact private trustees:\n%s", launcher)
		} else if reject := strings.Index(launcher, ":reject_unsafe_path"); reject < 0 || reset < reject {
			t.Fatalf("command broker must reject reparse paths before ACL reset:\n%s", launcher)
		}
		for _, forbidden := range []string{`call "%fallback_path%"`, `start "" "%fallback_path%"`} {
			if strings.Contains(launcher, forbidden) {
				t.Fatalf("native command launcher must not automatically execute the archive fallback; found %q:\n%s", forbidden, launcher)
			}
		}
	})

	t.Run("missing layered prerequisites retains archive launcher", func(t *testing.T) {
		fixture := newWindowsLayeredFixture(t)
		fixture.options.WindowsBootstrapBinaryPath = ""
		fixture.options.WindowsBootstrapReleaseManifestPath = ""
		fixture.options.LayeredAssetsManifestURL = ""

		plan, err := FromInvite(fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.PlatformPlanKind != "windows-temporary-acceptance-plan" {
			t.Fatalf("expected the existing self-contained runner fallback: %#v", plan.EntryPackagePlan)
		}
		if filepath.Base(plan.EntryPackagePlan.LauncherPath) != "Start-ConnectionEntry.ps1" {
			t.Fatalf("expected Start-ConnectionEntry.ps1 fallback, got %#v", plan.EntryPackagePlan)
		}
		launcher := readTestFile(t, plan.EntryPackagePlan.LauncherPath)
		if strings.Contains(string(launcher), "layered-run") {
			t.Fatalf("missing layered inputs must select the existing fallback, not a partial layered launcher:\n%s", launcher)
		}
	})

	partial := []struct {
		name   string
		mutate func(*Options)
	}{
		{
			name: "only bootstrap binary",
			mutate: func(options *Options) {
				options.WindowsBootstrapReleaseManifestPath = ""
				options.LayeredAssetsManifestURL = ""
			},
		},
		{
			name: "bootstrap and release manifest without layered URL",
			mutate: func(options *Options) {
				options.LayeredAssetsManifestURL = ""
			},
		},
		{
			name: "layered inputs without expected release version",
			mutate: func(options *Options) {
				options.LayeredReleaseVersion = ""
			},
		},
		{
			name: "release manifest and layered URL without bootstrap",
			mutate: func(options *Options) {
				options.WindowsBootstrapBinaryPath = ""
			},
		},
	}
	for _, test := range partial {
		t.Run(test.name+" retains archive launcher", func(t *testing.T) {
			fixture := newWindowsLayeredFixture(t)
			test.mutate(&fixture.options)

			plan, err := FromInvite(fixture.options)
			if err != nil {
				t.Fatalf("partial layered inputs must use the verified archive fallback: %v", err)
			}
			if plan.EntryPackagePlan == nil || plan.EntryPackagePlan.PlatformPlanKind != "windows-temporary-acceptance-plan" {
				t.Fatalf("partial layered inputs did not select the verified archive fallback: %#v", plan.EntryPackagePlan)
			}
			assertNoPartialLayeredOutput(t, fixture.options.OutDir)
		})
	}
}

func TestWindowsConnectionEntryPrefersPowerShell(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)

	plan, err := FromInvite(fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryPackagePlan == nil {
		t.Fatal("expected Windows layered entry package plan")
	}
	wantLauncher := windowsLayeredDirName + "/" + windowsLayeredLauncherName
	if plan.EntryPackagePlan.LauncherPath != wantLauncher {
		t.Fatalf("preferred launcher = %q, want %q", plan.EntryPackagePlan.LauncherPath, wantLauncher)
	}
	if !strings.Contains(strings.ToLower(plan.EntryPackagePlan.HumanEntryPoint), "powershell") {
		t.Fatalf("human entry point must prefer the visible PowerShell launcher: %#v", plan.EntryPackagePlan)
	}
	generated := make(map[string]bool)
	for _, file := range plan.GeneratedFiles {
		generated[filepath.Base(file.Path)] = true
	}
	for _, name := range []string{windowsLayeredLauncherName, windowsLayeredCommandLauncherName} {
		if !generated[name] {
			t.Fatalf("package plan omitted visible launcher %q: %#v", name, plan.GeneratedFiles)
		}
	}

	powerShell := normalizePowerShellForTest(string(readTestFile(t, filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredLauncherName))))
	for _, want := range []string{
		"AttemptDir",
		"Launcher",
		"powershell",
		"--attempt-dir",
		"--launcher",
		"Invoke-AttemptCheck",
		"attempt-check",
		"--create",
		"rdev-bootstrap.exe",
		"layered-run",
		"signed archive recovery profile",
	} {
		if !strings.Contains(powerShell, want) {
			t.Fatalf("preferred PowerShell launcher missing %q:\n%s", want, powerShell)
		}
	}
	if strings.Count(powerShell, "--transport") != 1 {
		t.Fatalf("PowerShell path must preserve exactly one core transport selection:\n%s", powerShell)
	}
	for _, forbidden := range []string{"Start-Process", "Start-Job", "WindowStyle Hidden", "& $fallbackPath"} {
		if strings.Contains(powerShell, forbidden) {
			t.Fatalf("preferred launcher must stay foreground and must not execute archive recovery; found %q:\n%s", forbidden, powerShell)
		}
	}
}

func TestWindowsLayeredBrokerFallbackContract(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	if _, err := FromInvite(fixture.options); err != nil {
		t.Fatal(err)
	}

	launcher := strings.ToLower(string(readTestFile(t, filepath.Join(fixture.options.OutDir, windowsLayeredDirName, windowsLayeredCommandLauncherName))))
	for _, want := range []string{
		"attempt_dir",
		"attempt-check",
		"core_started",
		"--native",
		"--attempt-dir",
		"--launcher",
		"powershell-bypass",
		"signed archive recovery profile",
	} {
		if !strings.Contains(launcher, want) {
			t.Fatalf("broker fallback contract missing %q:\n%s", want, launcher)
		}
	}
	assertStringsInOrder(t, launcher,
		"powershell.exe",
		"-file",
		"powershell",
		"-executionpolicy", "bypass",
		"-file",
		"powershell-bypass",
		":native",
	)
	if strings.Count(launcher, `"%attempt_dir%"`) < 3 {
		t.Fatalf("all broker paths must share the one allocated attempt directory:\n%s", launcher)
	}
	if strings.Contains(launcher, `call "%fallback_path%"`) || strings.Contains(launcher, `start "" "%fallback_path%"`) {
		t.Fatalf("broker must never execute archive recovery:\n%s", launcher)
	}
}

func TestWindowsLayeredBrokerAuthenticatesPowerShellAndAvoidsRuntimeCallArguments(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	if _, err := FromInvite(fixture.options); err != nil {
		t.Fatal(err)
	}

	handoffDir := filepath.Join(fixture.options.OutDir, windowsLayeredDirName)
	powerShell := readTestFile(t, filepath.Join(handoffDir, windowsLayeredLauncherName))
	digest := sha256.Sum256(powerShell)
	wantDigest := hex.EncodeToString(digest[:])
	command := string(readTestFile(t, filepath.Join(handoffDir, windowsLayeredCommandLauncherName)))
	for _, want := range []string{
		`set "EXPECTED_PS_SHA256=` + wantDigest + `"`,
		`set "EXPECTED_PS_SIZE=` + strconv.Itoa(len(powerShell)) + `"`,
		":verify_powershell",
		`set "SOURCE_BOOTSTRAP=`,
		`set "STAGED_BOOTSTRAP=`,
		":prepare_bootstrap",
		`copy /b "%SOURCE_BOOTSTRAP%" "%STAGED_BOOTSTRAP%"`,
		":cleanup_bootstrap",
		`set "CLEANUP_EXIT=0"`,
		`if errorlevel 1 set "CLEANUP_EXIT=1"`,
		`if errorlevel 1 set "LAYERED_EXIT=1"`,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("command broker must authenticate the packaged PowerShell content with %q:\n%s", want, command)
		}
	}
	if strings.Count(command, "call :verify_powershell") < 2 {
		t.Fatalf("command broker must authenticate PowerShell before both policy attempts:\n%s", command)
	}
	normalizedCommand := strings.ReplaceAll(command, "\r\n", "\n")
	nativeStart := strings.Index(normalizedCommand, "\n:native\n")
	if nativeStart < 0 {
		t.Fatalf("command broker is missing the shared native block:\n%s", command)
	}
	nativeBlock := normalizedCommand[nativeStart:]
	assertStringsInOrder(t, nativeBlock,
		"call :verify_bootstrap",
		`"%BOOTSTRAP%" layered-run attempt-check`,
		`set "TARGET=%COMMAND_LAUNCHER%"`,
		"call :verify_private_target",
		"call :verify_bootstrap",
		"layered-run --manifest-url",
	)
	if strings.Contains(nativeBlock, `findstr.exe" /l /c:"\"stage\":\"pre_core\""`) {
		t.Fatalf("native entry must use the verified bootstrap parser instead of substring-classifying attempt state:\n%s", command)
	}
	for _, forbidden := range []string{
		`call "%~f0"`,
		`call :protect_directory "%`,
		`call :protect_file "%`,
		`call :reject_unsafe_path "%`,
	} {
		if strings.Contains(strings.ToLower(command), strings.ToLower(forbidden)) {
			t.Fatalf("command broker must not pass runtime paths through CALL reparsing; found %q:\n%s", forbidden, command)
		}
	}

	powerShellText := string(powerShell)
	for _, want := range []string{windowsLayeredCommandLauncherName, "$commandLauncher", "& $commandLauncher", "--native"} {
		if !strings.Contains(powerShellText, want) {
			t.Fatalf("direct PowerShell fallback must enter the shared native CMD block with %q:\n%s", want, powerShellText)
		}
	}
	assertStringsInOrder(t, powerShellText,
		"Invoke-Layered $Launcher",
		"if (-not $Brokered) {",
		"Invoke-AttemptCheck $AttemptDir 'cmd'",
		"& $commandLauncher '--native' '--attempt-dir' $AttemptDir",
	)
	if strings.Contains(powerShellText, "'--attempt-dir' $AttemptDir '--launcher' 'cmd'") {
		t.Fatalf("private native PowerShell handoff must use the exact --native --attempt-dir grammar:\n%s", powerShellText)
	}
	if strings.Contains(powerShellText, "$cmdPath") {
		t.Fatalf("direct PowerShell fallback must not maintain a second native execution block:\n%s", powerShellText)
	}
}

func TestWindowsLayeredConnectionEntryRejectsUnverifiedControllerHandoff(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *windowsLayeredFixture)
	}{
		{
			name: "release key ID mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.SigningKeyID = "other-release-root"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "invalid release signature",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.Signature = "not-a-valid-ed25519-signature"
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "bootstrap digest mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ArtifactSHA256 = strings.Repeat("0", 64)
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "bootstrap size mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ArtifactSize++
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "wrong artifact name",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ArtifactName = "not-rdev-bootstrap.exe"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "signed bootstrap release version mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.ReleaseVersion = "v0.1.0"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "signed bootstrap target platform mismatch",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				manifest := fixture.manifest
				manifest.TargetPlatform = "windows/arm64"
				manifest = signReleaseManifestForTest(t, manifest, fixture.key)
				writeReleaseManifestForTest(t, fixture.options.WindowsBootstrapReleaseManifestPath, manifest)
			},
		},
		{
			name: "HTTP layered manifest URL",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.LayeredAssetsManifestURL = "http://downloads.example.com/layered-assets.json"
			},
		},
		{
			name: "layered manifest URL with query",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.LayeredAssetsManifestURL = "https://downloads.example.com/layered-assets.json?channel=test"
			},
		},
		{
			name: "layered manifest URL with fragment",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.LayeredAssetsManifestURL = "https://downloads.example.com/layered-assets.json#latest"
			},
		},
		{
			name: "layered inputs supplied without release root",
			mutate: func(t *testing.T, fixture *windowsLayeredFixture) {
				fixture.options.ReleaseRootPublicKey = ""
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWindowsLayeredFixture(t)
			test.mutate(t, &fixture)

			if _, err := FromInvite(fixture.options); err == nil {
				t.Fatal("expected controller-side layered handoff verification to fail closed")
			}
			assertNoPartialLayeredOutput(t, fixture.options.OutDir)
		})
	}
}

func TestWindowsLayeredCommandArgumentRejectsShellSyntax(t *testing.T) {
	for _, value := range []string{
		"https://downloads.example.com/layered-assets.json&whoami",
		"v0.2.0|whoami",
		"release-root:abc%PATH%",
		"https://gateway.example.com/manifest\" --unexpected",
		"line\r\nbreak",
	} {
		if err := validateWindowsLayeredCommandArgument("test", value); err == nil {
			t.Fatalf("expected Windows command argument validation to reject %q", value)
		}
	}
	if err := validateWindowsLayeredCommandArgument("test", "https://downloads.example.com/layered-assets.json"); err != nil {
		t.Fatalf("expected a safe Windows command argument to be accepted: %v", err)
	}
}

func TestWindowsLayeredConnectionEntryRejectsCommandSyntaxBeforeMaterialization(t *testing.T) {
	fixture := newWindowsLayeredFixture(t)
	fixture.options.ReleaseRootPublicKey += "&whoami"

	_, err := FromInvite(fixture.options)
	if err == nil || !strings.Contains(err.Error(), "unsupported Windows command syntax") {
		t.Fatalf("expected Windows command argument rejection, got %v", err)
	}
	assertNoPartialLayeredOutput(t, fixture.options.OutDir)
}

type windowsLayeredFixture struct {
	options         Options
	invite          agentinvite.Invite
	key             signing.Key
	manifest        release.Manifest
	controllerDir   string
	bootstrap       []byte
	bootstrapSHA256 string
}

func newWindowsLayeredFixture(t *testing.T) windowsLayeredFixture {
	t.Helper()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	key, err := signing.Generate("release-root")
	if err != nil {
		t.Fatal(err)
	}
	controllerDir := filepath.Join(t.TempDir(), "controller-source-private-token-do-not-publish")
	if err := os.MkdirAll(controllerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bootstrap := []byte("fixture rdev bootstrap executable bytes\n")
	bootstrapPath := filepath.Join(controllerDir, "rdev-bootstrap.exe")
	if err := os.WriteFile(bootstrapPath, bootstrap, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := release.SignArtifactForRelease(bootstrapPath, key, now, "v0.2.0", "windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(controllerDir, "rdev-bootstrap.exe.rdev-release.json")
	writeReleaseManifestForTest(t, manifestPath, manifest)
	root := key.ID + ":" + base64.RawURLEncoding.EncodeToString(key.PublicKey)
	invite := testInvite(t, model.HostModeAttendedTemporary)
	digest := sha256.Sum256(bootstrap)

	return windowsLayeredFixture{
		options: Options{
			InviteJSON:                          mustJSON(t, invite),
			OutDir:                              filepath.Join(t.TempDir(), "entry"),
			TargetOS:                            "windows",
			TargetArch:                          "amd64",
			Ownership:                           "third-party",
			SessionMode:                         string(model.HostModeAttendedTemporary),
			WindowsBootstrapBinaryPath:          bootstrapPath,
			WindowsBootstrapReleaseManifestPath: manifestPath,
			LayeredAssetsManifestURL:            "https://downloads.example.com/layered-assets.json",
			LayeredReleaseVersion:               "v0.2.0",
			WindowsBootstrapScriptPath:          filepath.Join("..", "..", "scripts", "bootstrap", "windows-temporary.ps1"),
			WindowsHostDownloadURL:              "https://downloads.example.com/rdev-host.exe",
			WindowsHostExpectedSHA256:           strings.Repeat("a", 64),
			ReleaseBundleURL:                    "https://downloads.example.com/release-bundle.json",
			ReleaseRootPublicKey:                root,
			WindowsVerifierDownloadURL:          "https://downloads.example.com/rdev-verify.exe",
			WindowsVerifierExpectedSHA256:       strings.Repeat("b", 64),
			Now:                                 now,
		},
		invite:          invite,
		key:             key,
		manifest:        manifest,
		controllerDir:   controllerDir,
		bootstrap:       bootstrap,
		bootstrapSHA256: hex.EncodeToString(digest[:]),
	}
}

func signReleaseManifestForTest(t *testing.T, manifest release.Manifest, key signing.Key) release.Manifest {
	t.Helper()
	signed, err := manifest.Sign(key.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func writeReleaseManifestForTest(t *testing.T, path string, manifest release.Manifest) {
	t.Helper()
	if err := release.WriteManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
}

func normalizePowerShellForTest(content string) string {
	normalized := strings.NewReplacer("'", "", "\"", "", "`", "").Replace(content)
	return strings.Join(strings.Fields(normalized), " ")
}

func assertStringsInOrder(t *testing.T, content string, expected ...string) {
	t.Helper()
	offset := 0
	for _, value := range expected {
		index := strings.Index(content[offset:], value)
		if index < 0 {
			t.Fatalf("expected %q after byte %d in launcher:\n%s", value, offset, content)
		}
		offset += index + len(value)
	}
}

func assertPrivateLayeredHandoff(t *testing.T, handoffDir string, fixture windowsLayeredFixture) {
	t.Helper()
	metadataFound := false
	checksumFound := false
	err := filepath.Walk(handoffDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("layered handoff path must be private, got mode %o for %s", info.Mode().Perm(), path)
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".sha256") {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, forbidden := range []string{
				fixture.controllerDir,
				filepath.ToSlash(fixture.controllerDir),
				fixture.invite.Ticket.Code,
				fixture.invite.GatewayURL,
				"controller-source-private-token-do-not-publish",
			} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("public/release-like handoff metadata %s leaked %q", path, forbidden)
				}
			}
			metadataFound = metadataFound || strings.HasSuffix(name, ".json")
			if strings.HasSuffix(name, ".sha256") {
				checksumFound = true
				if !strings.Contains(strings.ToLower(string(content)), fixture.bootstrapSHA256) {
					t.Errorf("checksum file %s does not pin the packaged bootstrap digest", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !metadataFound || !checksumFound {
		t.Fatalf("layered handoff must include non-sensitive release verification metadata and a checksum file; metadata=%t checksum=%t", metadataFound, checksumFound)
	}
}

func assertNoPartialLayeredOutput(t *testing.T, outDir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(outDir, "windows-layered")); err == nil {
		t.Fatalf("verification failure left a partial windows-layered handoff in %s", outDir)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), "rdev-bootstrap.exe") {
			t.Errorf("verification failure copied a bootstrap to %s", path)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(bytes.ToLower(content), []byte("rdev-bootstrap.exe")) && bytes.Contains(content, []byte("layered-run")) {
			t.Errorf("verification failure wrote a layered launcher to %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
