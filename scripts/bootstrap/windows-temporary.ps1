param(
  [Parameter(Mandatory = $true)]
  [string]$GatewayUrl,

  [Parameter(Mandatory = $true)]
  [string]$TicketCode,

  [Parameter(Mandatory = $true)]
  [string]$DownloadUrl,

  [Parameter(Mandatory = $true)]
  [string]$ExpectedSha256,

  [string]$ManifestUrl = "",

  [string]$ManifestRootPublicKey = "",

  [string]$ReleaseManifestUrl = "",

  [string]$ReleaseBundleUrl = "",

  [string]$ReleaseBundleRequiredArtifacts = "rdev-host.exe,rdev-verify.exe",

  [string]$ReleaseRootPublicKey = "",

  [string]$VerifierDownloadUrl = "",

  [string]$VerifierExpectedSha256 = "",

  [string]$TrustPin = "",

  [string]$HostName = $env:COMPUTERNAME,

  # Maximum number of times to re-register after an unexpected exit.
  # Set to 0 to disable the retry loop.
  [int]$MaxRetries = 5,

  # Seconds to wait between retry attempts.
  [int]$RetryDelaySeconds = 5
)

$ErrorActionPreference = "Stop"

function Write-Step {
  param([string]$Message)
  Write-Host "[rdev] $Message"
}

function Resolve-TempPath {
  $root = Join-Path ([System.IO.Path]::GetTempPath()) "rdev-host"
  New-Item -ItemType Directory -Force -Path $root | Out-Null
  return $root
}

function Invoke-Download {
  param(
    [string]$Url,
    [string]$OutFile
  )
  Write-Step "Downloading $Url to $OutFile"
  Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
}

function Assert-Required {
  param(
    [string]$Name,
    [string]$Value
  )
  if ([string]::IsNullOrWhiteSpace($Value)) {
    throw "$Name is required"
  }
}

function Assert-Sha256 {
  param(
    [string]$Path,
    [string]$Expected
  )
  $actual = (Get-FileHash -Algorithm SHA256 -Path $Path).Hash.ToLowerInvariant()
  $expectedLower = $Expected.ToLowerInvariant()
  if ($actual -ne $expectedLower) {
    throw "SHA256 mismatch. expected=$expectedLower actual=$actual"
  }
  Write-Step "SHA256 verified: $actual"
}

function Assert-ReleaseSignature {
  param(
    [string]$VerifierExe,
    [string]$ArtifactPath,
    [string]$ManifestPath,
    [string]$RootPublicKey
  )
  Write-Step "Verifying signed release manifest"
  $verifyOutput = & $VerifierExe --artifact $ArtifactPath --manifest $ManifestPath --root-public-key $RootPublicKey
  $verifyExit = $LASTEXITCODE
  if ($verifyOutput) {
    $verifyOutput | ForEach-Object { Write-Host $_ }
  }
  if ($verifyExit -ne 0) {
    throw "release signature verification failed with exit code $verifyExit"
  }
}

function Assert-BundleRelativePath {
  param(
    [string]$Root,
    [string]$Path
  )
  if ([string]::IsNullOrWhiteSpace($Path)) {
    throw "bundle path is required"
  }
  if ($Path.Contains("://") -or $Path.StartsWith("/") -or $Path.StartsWith('\')) {
    throw "bundle path must be relative: $Path"
  }
  if ([System.IO.Path]::IsPathRooted($Path)) {
    throw "bundle path must be relative: $Path"
  }
  $rootFull = [System.IO.Path]::GetFullPath($Root)
  $targetFull = [System.IO.Path]::GetFullPath((Join-Path $rootFull $Path))
  $rootPrefix = $rootFull.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
  if (-not $targetFull.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "bundle path escapes temp directory: $Path"
  }
  return $targetFull
}

function Resolve-BundleRelativeUrl {
  param(
    [string]$BaseUrl,
    [string]$RelativePath
  )
  $baseUri = [System.Uri]::new($BaseUrl)
  return ([System.Uri]::new($baseUri, $RelativePath)).AbsoluteUri
}

function Invoke-DownloadBundleManifests {
  param(
    [string]$BundlePath,
    [string]$BundleUrl,
    [string]$OutDir
  )
  $bundle = Get-Content -LiteralPath $BundlePath -Raw | ConvertFrom-Json
  if ($null -eq $bundle.artifacts) {
    throw "release bundle missing artifacts"
  }
  foreach ($artifact in $bundle.artifacts) {
    $manifestRel = [string]$artifact.manifest_path
    $manifestPath = Assert-BundleRelativePath -Root $OutDir -Path $manifestRel
    $manifestDir = Split-Path -Parent $manifestPath
    New-Item -ItemType Directory -Force -Path $manifestDir | Out-Null
    $manifestUrl = Resolve-BundleRelativeUrl -BaseUrl $BundleUrl -RelativePath $manifestRel
    Invoke-Download -Url $manifestUrl -OutFile $manifestPath
  }
}

function Assert-ReleaseBundle {
  param(
    [string]$VerifierExe,
    [string]$BundlePath,
    [string]$RootPublicKey,
    [string]$RequiredArtifacts
  )
  Write-Step "Verifying signed release bundle"
  $args = @("--bundle", $BundlePath, "--root-public-key", $RootPublicKey)
  if (-not [string]::IsNullOrWhiteSpace($RequiredArtifacts)) {
    $args += @("--require-artifacts", $RequiredArtifacts)
  }
  $verifyOutput = & $VerifierExe @args
  $verifyExit = $LASTEXITCODE
  if ($verifyOutput) {
    $verifyOutput | ForEach-Object { Write-Host $_ }
  }
  if ($verifyExit -ne 0) {
    throw "release bundle verification failed with exit code $verifyExit"
  }
}

Write-Host ""
Write-Host "Remote Dev Skillkit temporary support session"
Write-Host "Gateway: $GatewayUrl"
Write-Host "Ticket:  $TicketCode"
if ($ManifestUrl -ne "") {
  Write-Host "Manifest: $ManifestUrl"
}
if ($ManifestRootPublicKey -ne "") {
  Write-Host "Manifest root: configured"
}
if ($ReleaseManifestUrl -ne "") {
  Write-Host "Release manifest: $ReleaseManifestUrl"
}
if ($ReleaseBundleUrl -ne "") {
  Write-Host "Release bundle: $ReleaseBundleUrl"
}
Write-Host "Mode:    attended temporary foreground"
Write-Host "Retries: $MaxRetries"
Write-Host ""
Write-Host "This script does not install a Windows Service and does not create hidden persistence."
Write-Host "Close this window to stop the foreground host process."
Write-Host ""

$tempDir = Resolve-TempPath
$hostExe = Join-Path $tempDir "rdev-host.exe"

if (($ReleaseManifestUrl -ne "") -or ($ReleaseBundleUrl -ne "")) {
  Assert-Required -Name "ReleaseRootPublicKey" -Value $ReleaseRootPublicKey
  Assert-Required -Name "VerifierDownloadUrl" -Value $VerifierDownloadUrl
  Assert-Required -Name "VerifierExpectedSha256" -Value $VerifierExpectedSha256
}

Invoke-Download -Url $DownloadUrl -OutFile $hostExe
Assert-Sha256 -Path $hostExe -Expected $ExpectedSha256

if (($ReleaseManifestUrl -ne "") -or ($ReleaseBundleUrl -ne "")) {
  $verifierExe = Join-Path $tempDir "rdev-verify.exe"
  Invoke-Download -Url $VerifierDownloadUrl -OutFile $verifierExe
  Assert-Sha256 -Path $verifierExe -Expected $VerifierExpectedSha256
}

if ($ReleaseManifestUrl -ne "") {
  $releaseManifest = Join-Path $tempDir "rdev-host.exe.rdev-release.json"
  Invoke-Download -Url $ReleaseManifestUrl -OutFile $releaseManifest
  Assert-ReleaseSignature -VerifierExe $verifierExe -ArtifactPath $hostExe -ManifestPath $releaseManifest -RootPublicKey $ReleaseRootPublicKey
}

if ($ReleaseBundleUrl -ne "") {
  $releaseBundle = Join-Path $tempDir "release-bundle.json"
  Invoke-Download -Url $ReleaseBundleUrl -OutFile $releaseBundle
  Invoke-DownloadBundleManifests -BundlePath $releaseBundle -BundleUrl $ReleaseBundleUrl -OutDir $tempDir
  Assert-ReleaseBundle -VerifierExe $verifierExe -BundlePath $releaseBundle -RootPublicKey $ReleaseRootPublicKey -RequiredArtifacts $ReleaseBundleRequiredArtifacts
}

Write-Step "Starting foreground temporary host"
$hostArgs = @(
  "host", "serve",
  "--mode", "temporary",
  "--name", $HostName,
  "--once=false"
)
if ($ManifestUrl -ne "") {
  $hostArgs += @("--manifest-url", $ManifestUrl)
} else {
  $hostArgs += @("--gateway", $GatewayUrl, "--ticket-code", $TicketCode)
}
if ($ManifestRootPublicKey -ne "") {
  $hostArgs += @("--manifest-root-public-key", $ManifestRootPublicKey)
}
if ($TrustPin -ne "") {
  $hostArgs += @("--trust-pin", $TrustPin)
}

# Retry loop: re-register if host exits due to a transient error.
$attempt = 0
do {
  if ($attempt -gt 0) {
    Write-Step "Retrying host registration (attempt $($attempt + 1) of $($MaxRetries + 1)) after ${RetryDelaySeconds}s..."
    Start-Sleep -Seconds $RetryDelaySeconds
  }
  & $hostExe @hostArgs
  $exitCode = $LASTEXITCODE
  $attempt++
  Write-Step "rdev-host exited with code $exitCode"
} while ($exitCode -ne 0 -and $attempt -le $MaxRetries)

exit $exitCode
