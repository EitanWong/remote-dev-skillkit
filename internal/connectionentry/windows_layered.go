package connectionentry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	SchemaVersion            string    `json:"schema_version"`
	GeneratedAt              time.Time `json:"generated_at"`
	Platform                 string    `json:"platform"`
	ArtifactName             string    `json:"artifact_name"`
	ArtifactSHA256           string    `json:"artifact_sha256"`
	ArtifactSize             int64     `json:"artifact_size"`
	ReleaseManifest          string    `json:"release_manifest"`
	LayeredAssetsManifestURL string    `json:"layered_assets_manifest_url"`
	ReleaseVersion           string    `json:"release_version"`
	Verification             string    `json:"verification"`
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

func materializeWindowsLayeredHandoff(plan *Plan, handoff *windowsLayeredHandoff, outDir, fallbackLauncherPath string) error {
	fallbackInfo, err := os.Stat(fallbackLauncherPath)
	if err != nil || !fallbackInfo.Mode().IsRegular() {
		return fmt.Errorf("verified Windows archive fallback is unavailable")
	}
	handoffDir := filepath.Join(outDir, windowsLayeredDirName)
	fallbackRelative, err := filepath.Rel(handoffDir, fallbackLauncherPath)
	if err != nil || filepath.IsAbs(fallbackRelative) || strings.HasPrefix(filepath.ToSlash(fallbackRelative), "../../") {
		return fmt.Errorf("verified Windows archive fallback path is invalid")
	}
	fallbackRelative = strings.ReplaceAll(filepath.ToSlash(fallbackRelative), "/", `\`)

	stagingDir, err := os.MkdirTemp(outDir, ".windows-layered-")
	if err != nil {
		return fmt.Errorf("create Windows layered handoff staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	if err := os.Chmod(stagingDir, 0o700); err != nil {
		return fmt.Errorf("secure Windows layered handoff staging directory: %w", err)
	}

	manifestJSON, err := json.MarshalIndent(handoff.manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Windows bootstrap release manifest: %w", err)
	}
	manifestJSON = append(manifestJSON, '\n')
	verification := windowsLayeredVerification{
		SchemaVersion:            windowsLayeredHandoffSchemaVersion,
		GeneratedAt:              handoff.generatedAt.UTC(),
		Platform:                 "windows/amd64",
		ArtifactName:             windowsLayeredBootstrapName,
		ArtifactSHA256:           handoff.manifest.ArtifactSHA256,
		ArtifactSize:             handoff.manifest.ArtifactSize,
		ReleaseManifest:          windowsLayeredReleaseManifestName,
		LayeredAssetsManifestURL: handoff.layeredAssetsManifestURL,
		ReleaseVersion:           handoff.releaseVersion,
		Verification:             "controller-verified-before-materialization",
	}
	verificationJSON, err := json.MarshalIndent(verification, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Windows layered verification plan: %w", err)
	}
	verificationJSON = append(verificationJSON, '\n')
	checksum := []byte(handoff.manifest.ArtifactSHA256 + "  " + windowsLayeredBootstrapName + "\n")
	launcher := []byte(renderWindowsLayeredLauncher(handoff, fallbackRelative))
	commandLauncher := []byte(renderWindowsLayeredCommandLauncher(handoff, fallbackRelative))

	files := []struct {
		name    string
		content []byte
	}{
		{name: windowsLayeredBootstrapName, content: handoff.bootstrap},
		{name: windowsLayeredReleaseManifestName, content: manifestJSON},
		{name: windowsLayeredVerificationPlanName, content: verificationJSON},
		{name: windowsLayeredChecksumName, content: checksum},
		{name: windowsLayeredLauncherName, content: launcher},
		{name: windowsLayeredCommandLauncherName, content: commandLauncher},
	}
	for _, file := range files {
		if err := writePrivateWindowsLayeredFile(filepath.Join(stagingDir, file.name), file.content); err != nil {
			return err
		}
	}

	if _, err := os.Lstat(handoffDir); err == nil {
		return fmt.Errorf("Windows layered handoff directory already exists: %s", handoffDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect Windows layered handoff destination: %w", err)
	}
	if err := os.Rename(stagingDir, handoffDir); err != nil {
		return fmt.Errorf("publish Windows layered handoff: %w", err)
	}

	verificationPath := filepath.Join(handoffDir, windowsLayeredVerificationPlanName)
	launcherPath := filepath.Join(handoffDir, windowsLayeredCommandLauncherName)
	checks := []acceptance.Check{
		{Name: "windows_layered_platform", Passed: true, Detail: "windows/amd64 attended-temporary"},
		{Name: "windows_layered_release_version", Passed: true, Detail: handoff.releaseVersion},
		{Name: "windows_layered_manifest_url", Passed: true, Detail: handoff.layeredAssetsManifestURL},
		{Name: "windows_bootstrap_release_verification", Passed: true, Detail: handoff.manifest.ArtifactSHA256},
		{Name: "windows_layered_private_handoff", Passed: true, Detail: "directory mode 0700; file modes 0600"},
	}
	plan.EntryPackagePlan = &EntryPackagePlan{
		SchemaVersion:      EntryPackagePlanSchemaVersion,
		TargetOS:           plan.TargetOS,
		SessionMode:        plan.SessionMode,
		PackageMode:        "private-windows-layered-handoff",
		OK:                 allAcceptanceChecksPassed(checks),
		PlanPath:           verificationPath,
		LauncherPath:       launcherPath,
		PlatformPlanSchema: windowsLayeredHandoffSchemaVersion,
		PlatformPlanKind:   "windows-layered-handoff",
		HumanEntryPoint:    "run the visible native Command Prompt launcher from the private Windows layered handoff",
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
		GeneratedFile{Path: launcherPath, Purpose: "visible foreground-only Windows layered connection entry launcher"},
		GeneratedFile{Path: filepath.Join(handoffDir, windowsLayeredLauncherName), Purpose: "PowerShell review launcher with equivalent Windows ACL and reparse-point checks"},
	)
	plan.Checks = append(plan.Checks, Check{Name: "entry_package_plan", Passed: plan.EntryPackagePlan.OK, Detail: verificationPath})
	return nil
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

func renderWindowsLayeredLauncher(handoff *windowsLayeredHandoff, fallbackRelative string) string {
	return fmt.Sprintf(`Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-NoReparsePoint([string] $Path) {
    if ($Path.StartsWith('\\')) {
        throw "UNC paths are not allowed for layered handoff files: $Path"
    }
    $cursor = [IO.Path]::GetFullPath($Path)
    while ($true) {
        $item = Get-Item -LiteralPath $cursor -Force -ErrorAction Stop
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Reparse points are not allowed in the layered handoff path: $cursor"
        }
        $parent = [IO.Directory]::GetParent($cursor)
        if ($null -eq $parent) { break }
        $cursor = $parent.FullName
    }
}

function Protect-PrivatePath([string] $Path) {
    Assert-NoReparsePoint $Path
    $userSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $systemSid = 'S-1-5-18'
    $administratorsSid = 'S-1-5-32-544'
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    $permissions = if ($item.PSIsContainer) { '(OI)(CI)F' } else { 'F' }
    $icacls = Join-Path $env:SystemRoot 'System32\icacls.exe'
    & $icacls $Path '/inheritance:r' '/grant:r' "*${userSid}:$permissions" "*${systemSid}:$permissions" "*${administratorsSid}:$permissions" | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to secure layered path ACL: $Path"
    }
    $trustedSids = @($userSid, $systemSid, $administratorsSid)
    $acl = Get-Acl -LiteralPath $Path
    $ownerSid = $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
    if ($trustedSids -notcontains $ownerSid) {
        throw "Layered path owner is not trusted: $Path"
    }
    foreach ($rule in $acl.Access) {
        if ($rule.AccessControlType -ne [Security.AccessControl.AccessControlType]::Allow) {
            continue
        }
        try {
            $sid = $rule.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value
        } catch {
            throw "Layered path ACL contains an unresolvable identity: $Path"
        }
        if ($trustedSids -notcontains $sid) {
            throw "Layered path ACL grants access to an untrusted identity: $Path"
        }
    }
}

$bootstrapPath = Join-Path $PSScriptRoot '%s'
$fallbackPath = Join-Path $PSScriptRoot %s
$expectedSHA256 = '%s'
$expectedSize = %d
$layeredExitCode = 1
try {
    if (-not (Test-Path -LiteralPath $bootstrapPath -PathType Leaf)) {
        throw 'Packaged rdev-bootstrap.exe is unavailable.'
    }
    $cacheDir = Join-Path (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'RemoteDevSkillkit') 'cache'
    [IO.Directory]::CreateDirectory($cacheDir) | Out-Null
    Protect-PrivatePath $PSScriptRoot
    Protect-PrivatePath $bootstrapPath
    Protect-PrivatePath $cacheDir
    Assert-NoReparsePoint $bootstrapPath
    $bootstrapLock = [IO.File]::Open($bootstrapPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        Assert-NoReparsePoint $bootstrapPath
        $sha = [Security.Cryptography.SHA256]::Create()
        try {
            $actualSHA256 = ([BitConverter]::ToString($sha.ComputeHash($bootstrapLock)) -replace '-', '').ToLowerInvariant()
        } finally {
            $sha.Dispose()
        }
        if ($actualSHA256 -ne $expectedSHA256 -or $bootstrapLock.Length -ne $expectedSize) {
            throw 'Packaged rdev-bootstrap.exe failed SHA-256 or size verification.'
        }
        & $bootstrapPath 'layered-run' '--manifest-url' %s '--root-public-key' %s '--expected-release-version' %s '--platform' 'windows/amd64' '--cache-dir' $cacheDir '--mode' 'temporary' '--' 'serve' '--mode' 'temporary' '--manifest-url' %s '--manifest-root-public-key' %s '--transport' 'auto' '--once=false' '--max-tasks' '0'
        $layeredExitCode = $LASTEXITCODE
    } finally {
        $bootstrapLock.Dispose()
    }
    if ($layeredExitCode -eq 0) {
        exit 0
    }
    Write-Warning 'Layered bootstrap failed verification or execution; refusing automatic archive fallback.'
    exit $layeredExitCode
} catch {
    Write-Warning 'Layered bootstrap preparation failed; refusing automatic archive fallback.'
    Write-Warning ('Run the verified archive fallback explicitly: & "{0}"' -f $fallbackPath)
    exit 1
}
`, windowsLayeredBootstrapName,
		powerShellSingleQuoted(fallbackRelative),
		handoff.manifest.ArtifactSHA256,
		handoff.manifest.ArtifactSize,
		powerShellSingleQuoted(handoff.layeredAssetsManifestURL),
		powerShellSingleQuoted(handoff.releaseRootPublicKey),
		powerShellSingleQuoted(handoff.releaseVersion),
		powerShellSingleQuoted(handoff.joinManifestURL),
		powerShellSingleQuoted(handoff.joinManifestRoot),
	)
}

func renderWindowsLayeredCommandLauncher(handoff *windowsLayeredHandoff, fallbackRelative string) string {
	launcher := `@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "HANDOFF_DIR=%~dp0"
set "HANDOFF_DIR=%HANDOFF_DIR:~0,-1%"
set "BOOTSTRAP=%HANDOFF_DIR%\__BOOTSTRAP__"
set "FALLBACK_PATH=%HANDOFF_DIR%\__FALLBACK__"
set "EXPECTED_SHA256=__SHA256__"
set "EXPECTED_SIZE=__SIZE__"
if not defined LOCALAPPDATA goto failure
set "CACHE_DIR=%LOCALAPPDATA%\RemoteDevSkillkit\cache"

call :protect_directory "%HANDOFF_DIR%" || goto failure
if not exist "%CACHE_DIR%" mkdir "%CACHE_DIR%" >nul 2>&1
if not exist "%CACHE_DIR%" goto failure
call :protect_directory "%CACHE_DIR%" || goto failure
if not exist "%BOOTSTRAP%" goto failure
call :protect_file "%BOOTSTRAP%" || goto failure

for %%I in ("%BOOTSTRAP%") do set "ACTUAL_SIZE=%%~zI"
if not "%ACTUAL_SIZE%"=="%EXPECTED_SIZE%" goto failure
set "ACTUAL_SHA256="
for /f "skip=1 tokens=1" %%H in ('%SystemRoot%\System32\certutil.exe -hashfile "%BOOTSTRAP%" SHA256') do if not defined ACTUAL_SHA256 set "ACTUAL_SHA256=%%H"
if not defined ACTUAL_SHA256 goto failure
if /I not "%ACTUAL_SHA256%"=="%EXPECTED_SHA256%" goto failure

"%BOOTSTRAP%" "layered-run" "--manifest-url" __LAYERED_MANIFEST_URL__ "--root-public-key" __RELEASE_ROOT__ "--expected-release-version" __RELEASE_VERSION__ "--platform" "windows/amd64" "--cache-dir" "%CACHE_DIR%" "--mode" "temporary" "--" "serve" "--mode" "temporary" "--manifest-url" __JOIN_MANIFEST_URL__ "--manifest-root-public-key" __JOIN_MANIFEST_ROOT__ "--transport" "auto" "--once=false" "--max-tasks" "0"
set "LAYERED_EXIT=%ERRORLEVEL%"
if "%LAYERED_EXIT%"=="0" exit /b 0
echo [rdev] Layered bootstrap failed verification or execution; refusing automatic archive fallback.
echo [rdev] Run the verified archive fallback explicitly: "%FALLBACK_PATH%"
exit /b %LAYERED_EXIT%

:failure
echo [rdev] Layered bootstrap preparation failed; refusing automatic archive fallback.
echo [rdev] Run the verified archive fallback explicitly: "%FALLBACK_PATH%"
exit /b 1

:protect_directory
set "TARGET=%~1"
call :reject_unsafe_path "%TARGET%" || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /reset >nul || exit /b 1
call :reject_unsafe_path "%TARGET%" || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /inheritance:r /grant:r "%USERNAME%:(OI)(CI)F" "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /setowner "%USERNAME%" >nul
exit /b %ERRORLEVEL%

:protect_file
set "TARGET=%~1"
call :reject_unsafe_path "%TARGET%" || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /reset >nul || exit /b 1
call :reject_unsafe_path "%TARGET%" || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /inheritance:r /grant:r "%USERNAME%:F" "*S-1-5-18:F" "*S-1-5-32-544:F" >nul || exit /b 1
"%SystemRoot%\System32\icacls.exe" "%TARGET%" /setowner "%USERNAME%" >nul
exit /b %ERRORLEVEL%

:reject_unsafe_path
set "TARGET=%~1"
if "%TARGET:~0,2%"=="\\" exit /b 1
if not exist "%SystemRoot%\System32\fsutil.exe" exit /b 1
for %%I in ("%TARGET%") do set "CURSOR=%%~fI"

:reject_unsafe_path_loop
"%SystemRoot%\System32\fsutil.exe" reparsepoint query "%CURSOR%" >nul 2>&1
if not errorlevel 1 exit /b 1
for %%I in ("%CURSOR%\..") do set "PARENT=%%~fI"
if /I "%PARENT%"=="%CURSOR%" exit /b 0
set "CURSOR=%PARENT%"
goto :reject_unsafe_path_loop
`
	return strings.NewReplacer(
		"__BOOTSTRAP__", windowsLayeredBootstrapName,
		"__FALLBACK__", fallbackRelative,
		"__SHA256__", handoff.manifest.ArtifactSHA256,
		"__SIZE__", strconv.FormatInt(handoff.manifest.ArtifactSize, 10),
		"__LAYERED_MANIFEST_URL__", quoteValidatedWindowsCommandArgument(handoff.layeredAssetsManifestURL),
		"__RELEASE_ROOT__", quoteValidatedWindowsCommandArgument(handoff.releaseRootPublicKey),
		"__RELEASE_VERSION__", quoteValidatedWindowsCommandArgument(handoff.releaseVersion),
		"__JOIN_MANIFEST_URL__", quoteValidatedWindowsCommandArgument(handoff.joinManifestURL),
		"__JOIN_MANIFEST_ROOT__", quoteValidatedWindowsCommandArgument(handoff.joinManifestRoot),
	).Replace(launcher)
}

func quoteValidatedWindowsCommandArgument(value string) string {
	return `"` + value + `"`
}

func powerShellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
