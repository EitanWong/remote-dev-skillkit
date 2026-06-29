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

  [string]$ReleaseRootPublicKey = "",

  [string]$VerifierDownloadUrl = "",

  [string]$VerifierExpectedSha256 = "",

  [string]$TrustPin = "",

  [string]$HostName = $env:COMPUTERNAME
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
Write-Host "Mode:    attended temporary foreground"
Write-Host ""
Write-Host "This script does not install a Windows Service and does not create hidden persistence."
Write-Host "Close this window to stop the foreground host process."
Write-Host ""

$tempDir = Resolve-TempPath
$hostExe = Join-Path $tempDir "rdev-host.exe"

if ($ReleaseManifestUrl -ne "") {
  Assert-Required -Name "ReleaseRootPublicKey" -Value $ReleaseRootPublicKey
  Assert-Required -Name "VerifierDownloadUrl" -Value $VerifierDownloadUrl
  Assert-Required -Name "VerifierExpectedSha256" -Value $VerifierExpectedSha256
}

Invoke-Download -Url $DownloadUrl -OutFile $hostExe
Assert-Sha256 -Path $hostExe -Expected $ExpectedSha256

if ($ReleaseManifestUrl -ne "") {
  $releaseManifest = Join-Path $tempDir "rdev-host.exe.rdev-release.json"
  $verifierExe = Join-Path $tempDir "rdev-verify.exe"
  Invoke-Download -Url $ReleaseManifestUrl -OutFile $releaseManifest
  Invoke-Download -Url $VerifierDownloadUrl -OutFile $verifierExe
  Assert-Sha256 -Path $verifierExe -Expected $VerifierExpectedSha256
  Assert-ReleaseSignature -VerifierExe $verifierExe -ArtifactPath $hostExe -ManifestPath $releaseManifest -RootPublicKey $ReleaseRootPublicKey
}

Write-Step "Starting foreground temporary host"
$hostArgs = @("host", "serve", "--mode", "temporary", "--name", $HostName)
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
& $hostExe @hostArgs

$exitCode = $LASTEXITCODE
Write-Step "rdev-host exited with code $exitCode"
exit $exitCode
