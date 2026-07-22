package connectionentry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/acceptance"
	"github.com/EitanWong/remote-dev-skillkit/internal/agentinvite"
	"github.com/EitanWong/remote-dev-skillkit/internal/model"
	"github.com/EitanWong/remote-dev-skillkit/internal/release"
	"github.com/EitanWong/remote-dev-skillkit/internal/trustref"
)

const windowsLayeredHandoffSchemaVersion = "rdev.connection-entry.windows-layered-handoff.v1"

const (
	windowsLayeredDirName              = "windows-layered"
	windowsLayeredBootstrapName        = "rdev-bootstrap.exe"
	windowsLayeredReleaseManifestName  = "rdev-bootstrap.exe.rdev-release.json"
	windowsLayeredVerificationPlanName = "windows-layered-verification.json"
	windowsLayeredChecksumName         = "rdev-bootstrap.exe.sha256"
	windowsLayeredLauncherName         = "Start-ConnectionEntry.ps1"
	windowsLayeredCommandLauncherName  = "Start-ConnectionEntry.cmd"
	windowsLayeredArchiveName          = "Windows-ConnectionEntry.zip"
	windowsLayeredArchiveRecoveryName  = "ARCHIVE-RECOVERY.txt"
)

type windowsLayeredHandoff struct {
	bootstrap                []byte
	manifest                 release.Manifest
	layeredAssetsManifestURL string
	releaseVersion           string
	releaseRootPublicKey     string
	joinManifestURL          string
	joinManifestRoot         string
	generatedAt              time.Time
}

type windowsLayeredVerification struct {
	SchemaVersion string `json:"schema_version"`
	Platform      string `json:"platform"`
	Verification  string `json:"verification"`
}

func prepareWindowsLayeredHandoff(plan Plan, invite agentinvite.Invite, opts Options, outDir string) (*windowsLayeredHandoff, error) {
	bootstrapPath := strings.TrimSpace(opts.WindowsBootstrapBinaryPath)
	manifestPath := strings.TrimSpace(opts.WindowsBootstrapReleaseManifestPath)
	layeredManifestURL := strings.TrimSpace(opts.LayeredAssetsManifestURL)
	releaseVersion := strings.TrimSpace(opts.LayeredReleaseVersion)
	provided := 0
	for _, value := range []string{bootstrapPath, manifestPath, layeredManifestURL, releaseVersion} {
		if value != "" {
			provided++
		}
	}
	if provided < 4 {
		return nil, nil
	}

	rootPublicKey := strings.TrimSpace(opts.ReleaseRootPublicKey)
	if rootPublicKey == "" {
		return nil, fmt.Errorf("Windows layered handoff requires a release root public key")
	}
	if plan.TargetOS != "windows" || plan.SessionMode != string(model.HostModeAttendedTemporary) || normalizeWindowsLayeredArch(opts.TargetArch) != "amd64" {
		return nil, fmt.Errorf("Windows layered handoff requires windows/amd64 attended-temporary")
	}
	if outDir == "" {
		return nil, fmt.Errorf("Windows layered handoff requires out_dir")
	}
	if err := validateLayeredAssetsManifestURL(layeredManifestURL); err != nil {
		return nil, err
	}
	for _, argument := range []struct {
		name  string
		value string
	}{
		{name: "layered assets manifest URL", value: layeredManifestURL},
		{name: "layered release version", value: releaseVersion},
		{name: "release root public key", value: rootPublicKey},
		{name: "join manifest URL", value: strings.TrimSpace(invite.ManifestURL)},
		{name: "join manifest root public key", value: strings.TrimSpace(invite.ManifestRootPublicKey)},
	} {
		if err := validateWindowsLayeredCommandArgument(argument.name, argument.value); err != nil {
			return nil, err
		}
	}

	root, err := trustref.Parse(rootPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse Windows layered release root: %w", err)
	}
	manifest, err := release.ReadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read Windows bootstrap release manifest: %w", err)
	}
	if manifest.ArtifactName != windowsLayeredBootstrapName {
		return nil, fmt.Errorf("Windows bootstrap release manifest artifact must be %q", windowsLayeredBootstrapName)
	}
	if manifest.ReleaseVersion != releaseVersion {
		return nil, fmt.Errorf("Windows bootstrap release version %q does not match expected %q", manifest.ReleaseVersion, releaseVersion)
	}
	if manifest.TargetPlatform != "windows/amd64" {
		return nil, fmt.Errorf("Windows bootstrap target platform must be windows/amd64")
	}
	if err := manifest.VerifyArtifact(bootstrapPath, root); err != nil {
		return nil, fmt.Errorf("verify Windows bootstrap release artifact: %w", err)
	}

	bootstrap, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return nil, fmt.Errorf("read verified Windows bootstrap: %w", err)
	}
	digest := sha256.Sum256(bootstrap)
	if hex.EncodeToString(digest[:]) != manifest.ArtifactSHA256 || int64(len(bootstrap)) != manifest.ArtifactSize {
		return nil, fmt.Errorf("verified Windows bootstrap changed before packaging")
	}

	return &windowsLayeredHandoff{
		bootstrap:                bootstrap,
		manifest:                 manifest,
		layeredAssetsManifestURL: layeredManifestURL,
		releaseVersion:           releaseVersion,
		releaseRootPublicKey:     rootPublicKey,
		joinManifestURL:          strings.TrimSpace(invite.ManifestURL),
		joinManifestRoot:         strings.TrimSpace(invite.ManifestRootPublicKey),
		generatedAt:              plan.GeneratedAt,
	}, nil
}

func normalizeWindowsLayeredArch(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "x64", "x86_64":
		return "amd64"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func validateLayeredAssetsManifestURL(value string) error {
	if strings.ContainsAny(value, "?#") {
		return fmt.Errorf("layered assets manifest URL must not contain a query or fragment")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("parse layered assets manifest URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" {
		return fmt.Errorf("layered assets manifest URL must be an HTTPS URL without credentials")
	}
	if parsed.Path != "/layered-assets.json" || parsed.EscapedPath() != "/layered-assets.json" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery {
		return fmt.Errorf("layered assets manifest URL path must be exactly /layered-assets.json")
	}
	return nil
}

func validateWindowsLayeredCommandArgument(name, value string) error {
	if strings.ContainsAny(value, "\r\n\"%&|<>()^!") {
		return fmt.Errorf("%s contains unsupported Windows command syntax", name)
	}
	return nil
}

func materializeWindowsLayeredHandoff(plan *Plan, handoff *windowsLayeredHandoff, outDir string) (*pendingWindowsLayeredArchive, error) {
	handoffDir := filepath.Join(outDir, windowsLayeredDirName)
	stagingDir, err := os.MkdirTemp(outDir, ".windows-layered-")
	if err != nil {
		return nil, fmt.Errorf("create Windows layered handoff staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	if err := os.Chmod(stagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure Windows layered handoff staging directory: %w", err)
	}

	manifestJSON, err := json.Marshal(handoff.manifest)
	if err != nil {
		return nil, fmt.Errorf("encode Windows bootstrap release manifest: %w", err)
	}
	verification := windowsLayeredVerification{
		SchemaVersion: windowsLayeredHandoffSchemaVersion,
		Platform:      "windows/amd64",
		Verification:  "verified",
	}
	verificationJSON, err := json.Marshal(verification)
	if err != nil {
		return nil, fmt.Errorf("encode Windows layered verification plan: %w", err)
	}
	checksum := []byte(handoff.manifest.ArtifactSHA256 + "  " + windowsLayeredBootstrapName)
	launcher := []byte(renderWindowsLayeredLauncher(handoff))
	launcherDigest := sha256.Sum256(launcher)
	commandLauncher := []byte(renderWindowsLayeredCommandLauncher(
		handoff,
		hex.EncodeToString(launcherDigest[:]),
		len(launcher),
	))

	files := []windowsLayeredArchiveFile{
		{name: windowsLayeredBootstrapName, content: handoff.bootstrap},
		{name: windowsLayeredReleaseManifestName, content: manifestJSON},
		{name: windowsLayeredVerificationPlanName, content: verificationJSON},
		{name: windowsLayeredChecksumName, content: checksum},
		{name: windowsLayeredLauncherName, content: launcher},
		{name: windowsLayeredCommandLauncherName, content: commandLauncher},
	}
	for _, file := range files {
		if err := writePrivateWindowsLayeredFile(filepath.Join(stagingDir, file.name), file.content); err != nil {
			return nil, err
		}
	}

	if _, err := os.Lstat(handoffDir); err == nil {
		return nil, fmt.Errorf("Windows layered handoff directory already exists: %s", handoffDir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect Windows layered handoff destination: %w", err)
	}
	archiveFiles := append([]windowsLayeredArchiveFile(nil), files...)
	archiveFiles = append(archiveFiles, windowsLayeredArchiveFile{
		name:    windowsLayeredArchiveRecoveryName,
		content: []byte(renderWindowsLayeredArchiveRecovery()),
	})
	pendingArchive, archiveReport, err := prepareWindowsLayeredArchive(filepath.Dir(plan.OutDir), windowsLayeredArchiveName, handoff.generatedAt, archiveFiles)
	if err != nil {
		return nil, err
	}
	archiveReport.Path = filepath.Join(plan.OutDir, windowsLayeredArchiveName)
	if err := os.Rename(stagingDir, handoffDir); err != nil {
		publishErr := fmt.Errorf("publish Windows layered handoff: %w", err)
		cleanupErr := pendingArchive.discard()
		if cleanupErr != nil {
			cleanupErr = fmt.Errorf("clean up archive after handoff publication failure: %w", cleanupErr)
		}
		return nil, errors.Join(publishErr, cleanupErr)
	}

	verificationPath := filepath.Join(handoffDir, windowsLayeredVerificationPlanName)
	launcherPath := filepath.Join(handoffDir, windowsLayeredLauncherName)
	verificationArtifactPath := windowsLayeredDirName + "/" + windowsLayeredVerificationPlanName
	launcherArtifactPath := windowsLayeredDirName + "/" + windowsLayeredLauncherName
	checks := []acceptance.Check{
		{Name: "windows_layered_platform", Passed: true, Detail: "windows/amd64 attended-temporary"},
		{Name: "windows_layered_release_version", Passed: true, Detail: handoff.releaseVersion},
		{Name: "windows_layered_manifest_url", Passed: true, Detail: "valid"},
		{Name: "windows_bootstrap_release_verification", Passed: true, Detail: handoff.manifest.ArtifactSHA256},
		{Name: "windows_layered_archive_private", Passed: archiveReport.Private, Detail: archiveReport.PrivacyDetail},
		{Name: "windows_layered_archive_sha256", Passed: archiveReport.SHA256 != "", Detail: archiveReport.SHA256},
		{Name: "windows_layered_archive_size", Passed: archiveReport.SizeBytes <= maxWindowsLayeredHandoffBytes, Detail: strconv.FormatInt(archiveReport.SizeBytes, 10)},
	}
	plan.EntryPackagePlan = &EntryPackagePlan{
		SchemaVersion:      EntryPackagePlanSchemaVersion,
		TargetOS:           plan.TargetOS,
		SessionMode:        plan.SessionMode,
		PackageMode:        "private-windows-layered-handoff",
		OK:                 allAcceptanceChecksPassed(checks),
		PlanPath:           verificationArtifactPath,
		LauncherPath:       launcherArtifactPath,
		ArchivePath:        windowsLayeredArchiveName,
		ArchiveSHA256:      archiveReport.SHA256,
		ArchiveSizeBytes:   archiveReport.SizeBytes,
		PlatformPlanSchema: windowsLayeredHandoffSchemaVersion,
		PlatformPlanKind:   "windows-layered-handoff",
		HumanEntryPoint:    "run the visible PowerShell launcher from the private Windows layered handoff; use the visible Command Prompt broker when needed",
		AgentOnlyParameters: []string{
			"layered_assets_manifest_url",
			"expected_release_version",
			"release_root_public_key",
			"manifest_url",
			"manifest_root_public_key",
		},
		Checks: checks,
	}
	plan.GeneratedFiles = append(plan.GeneratedFiles,
		GeneratedFile{Path: filepath.Join(handoffDir, windowsLayeredBootstrapName), Purpose: "controller-verified Windows bootstrap trust anchor"},
		GeneratedFile{Path: filepath.Join(handoffDir, windowsLayeredReleaseManifestName), Purpose: "signed non-sensitive bootstrap release manifest"},
		GeneratedFile{Path: verificationPath, Purpose: "non-sensitive Windows layered handoff verification record"},
		GeneratedFile{Path: filepath.Join(handoffDir, windowsLayeredChecksumName), Purpose: "bootstrap SHA-256 pin checked again by the launcher"},
		GeneratedFile{Path: launcherPath, Purpose: "preferred visible foreground-only PowerShell connection entry launcher"},
		GeneratedFile{Path: filepath.Join(handoffDir, windowsLayeredCommandLauncherName), Purpose: "visible Command Prompt broker and native fallback launcher"},
		GeneratedFile{Path: archiveReport.Path, Purpose: "deterministic private Windows Connection Entry delivery archive"},
	)
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: "ready"})
	return pendingArchive, nil
}

func renderWindowsLayeredArchiveRecovery() string {
	return "Run rdev-bootstrap.exe layered-run: signed archive recovery profile only.\n"
}

func writePrivateWindowsLayeredFile(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write private Windows layered handoff file %s: %w", filepath.Base(path), err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure Windows layered handoff file %s: %w", filepath.Base(path), err)
	}
	return nil
}

func renderWindowsLayeredLauncher(handoff *windowsLayeredHandoff) string {
	return fmt.Sprintf(`param(
    [string] $AttemptDir = '',
    [string] $Launcher = 'powershell',
    [switch] $Brokered
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-NoReparsePoint([string] $Path) {
    if ([string]::IsNullOrWhiteSpace($Path) -or -not [IO.Path]::IsPathRooted($Path)) { throw 1 }
    $cursor = [IO.Path]::GetFullPath($Path)
    if ($cursor.StartsWith('\\') -or $cursor.StartsWith('//')) { throw 'UNC paths are not allowed.' }
    $root = [IO.Path]::GetPathRoot($cursor)
    if (([IO.DriveInfo]::new($root)).DriveType -ne [IO.DriveType]::Fixed) { throw 1 }
    while ($true) {
        $item = Get-Item -LiteralPath $cursor -Force -ErrorAction Stop
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 1 }
        $parent = [IO.Directory]::GetParent($cursor)
        if ($null -eq $parent) { break }
        $cursor = $parent.FullName
    }
}

function Protect-PrivatePath([string] $Path) {
    Assert-NoReparsePoint $Path
    $user = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $sys = 'S-1-5-18'
    $admin = 'S-1-5-32-544'
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    $perm = if ($item.PSIsContainer) { '(OI)(CI)F' } else { 'F' }
    $aclTool = "$env:SystemRoot\System32\icacls.exe"
    & $aclTool $Path '/reset' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 1 }
    & $aclTool $Path '/inheritance:r' '/grant:r' "*${user}:$perm" "*${sys}:$perm" "*${admin}:$perm" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 1 }
    $trusted = @($user, $sys, $admin)
    $acl = Get-Acl -LiteralPath $Path
    $owner = $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
    if ($trusted -notcontains $owner) { throw 1 }
    foreach ($rule in $acl.Access) {
        if ($rule.AccessControlType -ne [Security.AccessControl.AccessControlType]::Allow) { continue }
        try { $sid = $rule.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value } catch { throw 1 }
        if ($trusted -notcontains $sid) { throw 'ACL grants access to an untrusted identity.' }
    }
}

function Invoke-AttemptCheck([string] $Path, [string] $Launch, [switch] $Create) {
    $check = @('layered-run', 'attempt-check', '--attempt-dir', $Path, '--launcher', $Launch)
    if ($Create) { $check += '--create' }
    & $bootstrapPath @check
    $LASTEXITCODE
}

function Assert-PrivatePath([string] $Path, [string] $Kind) {
    & $bootstrapPath 'layered-run' 'private-path-check' '--path' $Path '--kind' $Kind
    if ($LASTEXITCODE -ne 0) { throw 1 }
}

function Invoke-Layered([string] $Launch) {
    $run = @('layered-run', '--manifest-url', %s, '--root-public-key', %s, '--expected-release-version', %s, '--platform', 'windows/amd64', '--cache-dir', $cacheDir, '--attempt-dir', $AttemptDir, '--launcher', $Launch, '--mode', 'temporary', '--', 'serve', '--mode', 'temporary', '--manifest-url', %s, '--manifest-root-public-key', %s, '--transport', 'auto', '--once=false', '--max-tasks', '0')
    & $bootstrapPath @run
    $script:layeredExitCode = $LASTEXITCODE
}

$bootstrapPath = "$PSScriptRoot\%s"
$commandLauncher = "$PSScriptRoot\%s"
$powerShellLauncher = $PSCommandPath
$attemptRoot = [IO.Path]::GetFullPath("$env:LOCALAPPDATA\RemoteDevSkillkit\attempts")
$cacheDir = [IO.Path]::GetFullPath("$env:LOCALAPPDATA\RemoteDevSkillkit\cache")
$layeredExitCode = 1
try {
    if (-not (Test-Path -LiteralPath $bootstrapPath -PathType Leaf) -or -not (Test-Path -LiteralPath $commandLauncher -PathType Leaf)) { throw 1 }
    Protect-PrivatePath $PSScriptRoot
    Protect-PrivatePath $bootstrapPath
    Protect-PrivatePath $commandLauncher
    Protect-PrivatePath $powerShellLauncher
    if ([string]::IsNullOrWhiteSpace($AttemptDir)) {
        $createAttempt = $true
    } else {
        $AttemptDir = [IO.Path]::GetFullPath($AttemptDir)
        $createAttempt = $false
    }
    $bootstrapLock = [IO.File]::Open($bootstrapPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        $sha = [Security.Cryptography.SHA256]::Create()
        try { $actualSHA256 = ([BitConverter]::ToString($sha.ComputeHash($bootstrapLock)) -replace '-', '').ToLowerInvariant() } finally { $sha.Dispose() }
        if ($actualSHA256 -ne '%s' -or $bootstrapLock.Length -ne %d) { throw 1 }
        Assert-PrivatePath $PSScriptRoot 'directory'
        Assert-PrivatePath $bootstrapPath 'file'
        Assert-PrivatePath $commandLauncher 'file'
        Assert-PrivatePath $powerShellLauncher 'file'
        if ($createAttempt) {
            $created = $false
            for ($tries = 0; $tries -lt 32; $tries++) {
                $AttemptDir = [IO.Path]::Combine($attemptRoot, [IO.Path]::GetRandomFileName())
                $checkExit = Invoke-AttemptCheck $AttemptDir $Launcher -Create
                if ($checkExit -eq 0) { $created = $true; break }
                if ($checkExit -ne 2) { throw 1 }
            }
            if (-not $created) { throw 1 }
        } elseif ((Invoke-AttemptCheck $AttemptDir $Launcher) -ne 0) { throw 1 }
        Invoke-Layered $Launcher
        $layeredExitCode = $script:layeredExitCode
        if ($layeredExitCode -eq 0) { exit 0 }
        if (-not $Brokered) {
            if ((Invoke-AttemptCheck $AttemptDir 'cmd') -eq 0) {
                & $commandLauncher '--native' '--attempt-dir' $AttemptDir
                exit $LASTEXITCODE
            }
        }
    } finally { $bootstrapLock.Dispose() }
    Write-Warning 'Run the signed archive recovery profile: ARCHIVE-RECOVERY.txt.'
    exit $layeredExitCode
} catch {
    Write-Warning 'Layered bootstrap preparation failed; refusing automatic archive fallback.'
    Write-Warning 'Run the signed archive recovery profile: ARCHIVE-RECOVERY.txt.'
    exit 1
}
`, powerShellSingleQuoted(handoff.layeredAssetsManifestURL),
		powerShellSingleQuoted(handoff.releaseRootPublicKey),
		powerShellSingleQuoted(handoff.releaseVersion),
		powerShellSingleQuoted(handoff.joinManifestURL),
		powerShellSingleQuoted(handoff.joinManifestRoot),
		windowsLayeredBootstrapName,
		windowsLayeredCommandLauncherName,
		handoff.manifest.ArtifactSHA256,
		handoff.manifest.ArtifactSize,
	)
}

func renderWindowsLayeredCommandLauncher(handoff *windowsLayeredHandoff, expectedPowerShellSHA256 string, expectedPowerShellSize int) string {
	launcher := `@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "HANDOFF_DIR=%~dp0"
set "HANDOFF_DIR=%HANDOFF_DIR:~0,-1%"
set "SOURCE_BOOTSTRAP=%HANDOFF_DIR%\__BOOTSTRAP__"
set "BOOTSTRAP="
set "STAGED_BOOTSTRAP="
set "SOURCE_PS_LAUNCHER=%HANDOFF_DIR%\__POWERSHELL_LAUNCHER__"
set "PS_LAUNCHER="
set "STAGED_PS_LAUNCHER="
set "POWERSHELL=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
set "EXPECTED_SHA256=__SHA256__"
set "EXPECTED_SIZE=__SIZE__"
set "EXPECTED_PS_SHA256=__PS_SHA256__"
set "EXPECTED_PS_SIZE=__PS_SIZE__"
set "COMMAND_LAUNCHER=%HANDOFF_DIR%\__COMMAND_LAUNCHER__"
if not defined LOCALAPPDATA goto failure
set "CACHE_DIR=%LOCALAPPDATA%\RemoteDevSkillkit\cache"
set "ATTEMPT_ROOT=%LOCALAPPDATA%\RemoteDevSkillkit\attempts"
call :load_sid || goto failure

if /I "%~1"=="--native" goto native_arguments
if not "%~1"=="" goto failure
goto broker

:native_arguments
if /I not "%~2"=="--attempt-dir" goto failure
set "ATTEMPT_DIR=%~3"
if not defined ATTEMPT_DIR goto failure
if not "%~4"=="" goto failure
goto native

:broker
call :prepare_bootstrap || goto failure
set "TARGET=%COMMAND_LAUNCHER%"
set "TARGET_KIND=file"
call :reject_unsafe_path || goto failure
call :protect_file || goto failure
call :verify_private_target || goto failure
set "ATTEMPT_TRIES=0"

:create_attempt
set /a ATTEMPT_TRIES+=1 >nul
if %ATTEMPT_TRIES% GTR 32 goto failure
set "ATTEMPT_DIR=%ATTEMPT_ROOT%\%RANDOM%-%RANDOM%-%RANDOM%"
"%BOOTSTRAP%" layered-run attempt-check --attempt-dir "%ATTEMPT_DIR%" --launcher powershell --create >nul 2>&1
if errorlevel 2 goto create_attempt
if errorlevel 1 goto failure
set "BROKER_EXIT=1"
set "SKIP_PS="
call :prepare_powershell
if errorlevel 1 set "SKIP_PS=1"
if not exist "%POWERSHELL%" set "SKIP_PS=1"
if not defined SKIP_PS call :verify_powershell
if not defined SKIP_PS if errorlevel 1 set "SKIP_PS=1"

if not defined SKIP_PS "%POWERSHELL%" -NoLogo -NoProfile -File "%PS_LAUNCHER%" -AttemptDir "%ATTEMPT_DIR%" -Launcher "powershell" -Brokered
if not defined SKIP_PS set "BROKER_EXIT=%ERRORLEVEL%"
if not defined SKIP_PS if "%BROKER_EXIT%"=="0" goto success
if not defined SKIP_PS call :verify_bootstrap
if not defined SKIP_PS if errorlevel 1 goto broker_exhausted
if not defined SKIP_PS "%BOOTSTRAP%" layered-run attempt-check --attempt-dir "%ATTEMPT_DIR%" --launcher powershell-bypass >nul 2>&1
if not defined SKIP_PS if errorlevel 1 goto broker_exhausted

if not defined SKIP_PS call :verify_powershell
if not defined SKIP_PS if errorlevel 1 set "SKIP_PS=1"
if not defined SKIP_PS "%POWERSHELL%" -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%PS_LAUNCHER%" -AttemptDir "%ATTEMPT_DIR%" -Launcher "powershell-bypass" -Brokered
if not defined SKIP_PS set "BROKER_EXIT=%ERRORLEVEL%"
if not defined SKIP_PS if "%BROKER_EXIT%"=="0" goto success
call :verify_bootstrap || goto broker_exhausted
goto native

:broker_exhausted
call :cleanup_bootstrap >nul 2>&1
if errorlevel 1 set "BROKER_EXIT=1"
echo [rdev] core_started core_exited; no second core.
echo [rdev] Run the signed archive recovery profile: ARCHIVE-RECOVERY.txt.
exit /b %BROKER_EXIT%

:native
call :prepare_bootstrap || goto failure
call :verify_bootstrap || goto failure
"%BOOTSTRAP%" layered-run attempt-check --attempt-dir "%ATTEMPT_DIR%" --launcher cmd >nul 2>&1 || goto failure
set "TARGET=%HANDOFF_DIR%"
set "TARGET_KIND=directory"
call :reject_unsafe_path || goto failure
call :protect_directory || goto failure
call :verify_bootstrap || goto failure
set "TARGET=%COMMAND_LAUNCHER%"
set "TARGET_KIND=file"
call :reject_unsafe_path || goto failure
call :protect_file || goto failure
call :verify_private_target || goto failure
call :verify_bootstrap || goto failure
"%BOOTSTRAP%" layered-run attempt-check --attempt-dir "%ATTEMPT_DIR%" --launcher cmd >nul 2>&1 || goto failure
"%BOOTSTRAP%" layered-run --manifest-url __LAYERED_MANIFEST_URL__ --root-public-key __RELEASE_ROOT__ --expected-release-version __RELEASE_VERSION__ --platform windows/amd64 --cache-dir "%CACHE_DIR%" --attempt-dir "%ATTEMPT_DIR%" --launcher cmd --mode temporary -- serve --mode temporary --manifest-url __JOIN_MANIFEST_URL__ --manifest-root-public-key __JOIN_MANIFEST_ROOT__ --transport auto --once=false --max-tasks 0
set "LAYERED_EXIT=%ERRORLEVEL%"
call :cleanup_bootstrap >nul 2>&1
if errorlevel 1 set "LAYERED_EXIT=1"
if not "%LAYERED_EXIT%"=="0" echo [rdev] Run the signed archive recovery profile: ARCHIVE-RECOVERY.txt.
exit /b %LAYERED_EXIT%

:success
call :cleanup_bootstrap >nul 2>&1 || goto failure
exit /b 0

:failure
call :cleanup_bootstrap >nul 2>&1
echo [rdev] Layered bootstrap preparation failed; refusing automatic archive fallback.
echo [rdev] Run the signed archive recovery profile: ARCHIVE-RECOVERY.txt.
exit /b 1

:prepare_bootstrap
if defined BOOTSTRAP exit /b 0
set "TARGET=%HANDOFF_DIR%"
set "TARGET_KIND=directory"
call :protect_directory || exit /b 1
set "TARGET=%SOURCE_BOOTSTRAP%"
set "EXPECTED_FILE_SHA=%EXPECTED_SHA256%"
set "EXPECTED_FILE_SIZE=%EXPECTED_SIZE%"
call :verify_digest || exit /b 1
set "STAGE_TRIES=0"
:stage_bootstrap
set /a STAGE_TRIES+=1 >nul
if %STAGE_TRIES% GTR 32 exit /b 1
set "STAGED_BOOTSTRAP=%HANDOFF_DIR%\.rdev-bootstrap-%RANDOM%-%RANDOM%.exe"
if exist "%STAGED_BOOTSTRAP%" goto stage_bootstrap
copy /b "%SOURCE_BOOTSTRAP%" "%STAGED_BOOTSTRAP%" >nul 2>&1 || goto prepare_bootstrap_failure
set "TARGET=%STAGED_BOOTSTRAP%"
set "EXPECTED_FILE_SHA=%EXPECTED_SHA256%"
set "EXPECTED_FILE_SIZE=%EXPECTED_SIZE%"
call :verify_digest || goto prepare_bootstrap_failure
set "BOOTSTRAP=%STAGED_BOOTSTRAP%"
set "TARGET=%BOOTSTRAP%"
call :verify_private_target || goto prepare_bootstrap_failure
exit /b 0

:prepare_bootstrap_failure
call :cleanup_bootstrap >nul 2>&1
exit /b 1

:cleanup_bootstrap
set "CLEANUP_EXIT=0"
call :cleanup_powershell >nul 2>&1
if errorlevel 1 set "CLEANUP_EXIT=1"
if not defined STAGED_BOOTSTRAP exit /b %CLEANUP_EXIT%
del /f /q "%STAGED_BOOTSTRAP%" >nul 2>&1
if exist "%STAGED_BOOTSTRAP%" exit /b 1
set "BOOTSTRAP="
set "STAGED_BOOTSTRAP="
exit /b %CLEANUP_EXIT%

:prepare_powershell
if defined PS_LAUNCHER exit /b 0
set "TARGET=%SOURCE_PS_LAUNCHER%"
set "EXPECTED_FILE_SHA=%EXPECTED_PS_SHA256%"
set "EXPECTED_FILE_SIZE=%EXPECTED_PS_SIZE%"
call :verify_digest || exit /b 1
set "PS_STAGE_TRIES=0"
:stage_powershell
set /a PS_STAGE_TRIES+=1 >nul
if %PS_STAGE_TRIES% GTR 32 exit /b 1
set "STAGED_PS_LAUNCHER=%HANDOFF_DIR%\.Start-ConnectionEntry-%RANDOM%-%RANDOM%.ps1"
if exist "%STAGED_PS_LAUNCHER%" goto stage_powershell
copy /b "%SOURCE_PS_LAUNCHER%" "%STAGED_PS_LAUNCHER%" >nul 2>&1 || goto prepare_powershell_failure
set "TARGET=%STAGED_PS_LAUNCHER%"
set "EXPECTED_FILE_SHA=%EXPECTED_PS_SHA256%"
set "EXPECTED_FILE_SIZE=%EXPECTED_PS_SIZE%"
call :verify_digest || goto prepare_powershell_failure
set "PS_LAUNCHER=%STAGED_PS_LAUNCHER%"
exit /b 0

:prepare_powershell_failure
call :cleanup_powershell >nul 2>&1
exit /b 1

:cleanup_powershell
if not defined STAGED_PS_LAUNCHER exit /b 0
del /f /q "%STAGED_PS_LAUNCHER%" >nul 2>&1
if exist "%STAGED_PS_LAUNCHER%" exit /b 1
set "PS_LAUNCHER="
set "STAGED_PS_LAUNCHER="
exit /b 0

:verify_bootstrap
set "TARGET=%BOOTSTRAP%"
set "EXPECTED_FILE_SHA=%EXPECTED_SHA256%"
set "EXPECTED_FILE_SIZE=%EXPECTED_SIZE%"
call :verify_file
exit /b %ERRORLEVEL%

:verify_powershell
set "TARGET=%PS_LAUNCHER%"
set "EXPECTED_FILE_SHA=%EXPECTED_PS_SHA256%"
set "EXPECTED_FILE_SIZE=%EXPECTED_PS_SIZE%"
call :verify_file
exit /b %ERRORLEVEL%

:verify_file
call :verify_digest || exit /b 1
call :verify_private_target || exit /b 1
exit /b 0

:verify_digest
set "TARGET_KIND=file"
call :reject_unsafe_path || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /reset >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /inheritance:r /grant:r "*%CURRENT_SID%:F" "*S-1-5-18:F" "*S-1-5-32-544:F" >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /setowner "*%CURRENT_SID%" >nul || exit /b 1
set "ACTUAL_SIZE="
for %%I in ("%TARGET%") do set "ACTUAL_SIZE=%%~zI"
if not "%ACTUAL_SIZE%"=="%EXPECTED_FILE_SIZE%" exit /b 1
"%SystemRoot%\System32\certutil.exe" -hashfile "%TARGET%" SHA256 2>nul | "%SystemRoot%\System32\findstr.exe" /i /x /c:"%EXPECTED_FILE_SHA%" >nul
if errorlevel 1 exit /b 1
exit /b 0

:protect_directory
set "TARGET_KIND=directory"
call :reject_unsafe_path || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /reset >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /inheritance:r /grant:r "*%CURRENT_SID%:(OI)(CI)F" "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /setowner "*%CURRENT_SID%" >nul || exit /b 1
exit /b 0

:protect_file
set "TARGET_KIND=file"
call :reject_unsafe_path || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /reset >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /inheritance:r /grant:r "*%CURRENT_SID%:F" "*S-1-5-18:F" "*S-1-5-32-544:F" >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /setowner "*%CURRENT_SID%" >nul || exit /b 1
exit /b 0

:verify_private_target
"%BOOTSTRAP%" layered-run private-path-check --path "%TARGET%" --kind file >nul 2>&1 || exit /b 1
if /I not "%TARGET%"=="%BOOTSTRAP%" exit /b 0
"%BOOTSTRAP%" layered-run private-path-check --path "%HANDOFF_DIR%" --kind directory >nul 2>&1 || exit /b 1
exit /b 0

:reject_unsafe_path
if "%TARGET:~0,2%"=="\\" exit /b 1
if /I not "%TARGET_KIND%"=="file" if /I not "%TARGET_KIND%"=="directory" exit /b 1
if not exist "%TARGET%" exit /b 1
set "CURSOR="
set "ATTRS="
for %%I in ("%TARGET%") do set "CURSOR=%%~fI"& set "ATTRS=%%~aI"
if not defined CURSOR exit /b 1
if not defined ATTRS exit /b 1
if not "%ATTRS:l=%"=="%ATTRS%" exit /b 1
if /I "%TARGET_KIND%"=="directory" if /I not "%ATTRS:~0,1%"=="d" exit /b 1
if /I "%TARGET_KIND%"=="file" if /I "%ATTRS:~0,1%"=="d" exit /b 1
:reject_unsafe_path_loop
set "PARENT="
for %%I in ("%CURSOR%\..") do set "PARENT=%%~fI"
if not defined PARENT exit /b 1
if /I "%PARENT%"=="%CURSOR%" exit /b 0
set "CURSOR=%PARENT%"
set "ATTRS="
for %%I in ("%CURSOR%") do set "ATTRS=%%~aI"
if not defined ATTRS exit /b 1
if not "%ATTRS:l=%"=="%ATTRS%" exit /b 1
goto :reject_unsafe_path_loop

:load_sid
set "CURRENT_SID="
for /f "tokens=2 delims=," %%S in ('"%SystemRoot%\System32\whoami.exe" /user /fo csv /nh') do if not defined CURRENT_SID set "CURRENT_SID=%%~S"
if not defined CURRENT_SID exit /b 1
if not "%CURRENT_SID:~0,4%"=="S-1-" exit /b 1
exit /b 0
`
	rendered := strings.NewReplacer(
		"__BOOTSTRAP__", windowsLayeredBootstrapName,
		"__POWERSHELL_LAUNCHER__", windowsLayeredLauncherName,
		"__COMMAND_LAUNCHER__", windowsLayeredCommandLauncherName,
		"__SHA256__", handoff.manifest.ArtifactSHA256,
		"__SIZE__", strconv.FormatInt(handoff.manifest.ArtifactSize, 10),
		"__PS_SHA256__", expectedPowerShellSHA256,
		"__PS_SIZE__", strconv.Itoa(expectedPowerShellSize),
		"__LAYERED_MANIFEST_URL__", quoteValidatedWindowsCommandArgument(handoff.layeredAssetsManifestURL),
		"__RELEASE_ROOT__", quoteValidatedWindowsCommandArgument(handoff.releaseRootPublicKey),
		"__RELEASE_VERSION__", quoteValidatedWindowsCommandArgument(handoff.releaseVersion),
		"__JOIN_MANIFEST_URL__", quoteValidatedWindowsCommandArgument(handoff.joinManifestURL),
		"__JOIN_MANIFEST_ROOT__", quoteValidatedWindowsCommandArgument(handoff.joinManifestRoot),
	).Replace(launcher)
	return strings.ReplaceAll(rendered, "\n", "\r\n")
}

func quoteValidatedWindowsCommandArgument(value string) string {
	return `"` + value + `"`
}

func powerShellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
