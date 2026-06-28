param(
  [Parameter(Mandatory = $true)]
  [string]$GatewayUrl,

  [Parameter(Mandatory = $true)]
  [string]$TicketCode,

  [Parameter(Mandatory = $true)]
  [string]$DownloadUrl,

  [Parameter(Mandatory = $true)]
  [string]$ExpectedSha256,

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
  Write-Step "Downloading rdev-host to $OutFile"
  Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
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

Write-Host ""
Write-Host "Remote Dev Skillkit temporary support session"
Write-Host "Gateway: $GatewayUrl"
Write-Host "Ticket:  $TicketCode"
Write-Host "Mode:    attended temporary foreground"
Write-Host ""
Write-Host "This script does not install a Windows Service and does not create hidden persistence."
Write-Host "Close this window to stop the foreground host process."
Write-Host ""

$tempDir = Resolve-TempPath
$hostExe = Join-Path $tempDir "rdev-host.exe"

Invoke-Download -Url $DownloadUrl -OutFile $hostExe
Assert-Sha256 -Path $hostExe -Expected $ExpectedSha256

Write-Step "Starting foreground temporary host"
& $hostExe host serve `
  --mode temporary `
  --gateway $GatewayUrl `
  --ticket-code $TicketCode `
  --name $HostName

$exitCode = $LASTEXITCODE
Write-Step "rdev-host exited with code $exitCode"
exit $exitCode
