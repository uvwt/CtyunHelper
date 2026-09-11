[CmdletBinding()]
param(
    [string]$Version,
    [string]$OutputDirectory
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repositoryRoot 'dist'
}

if ([string]::IsNullOrWhiteSpace($Version)) {
    $buildInfoPath = Join-Path $repositoryRoot 'internal\buildinfo\info.go'
    $buildInfo = [System.IO.File]::ReadAllText($buildInfoPath)
    $match = [regex]::Match($buildInfo, 'var\s+Version\s*=\s*"([^"]+)"')
    if (-not $match.Success) {
        throw "Cannot read version from $buildInfoPath"
    }
    $Version = $match.Groups[1].Value
}

$Version = $Version.Trim()
if ($Version.StartsWith('v', [StringComparison]::OrdinalIgnoreCase)) {
    $Version = $Version.Substring(1)
}
if ($Version -notmatch '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
    throw "Invalid release version: $Version"
}

New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$outputPath = Join-Path $OutputDirectory "CtyunHelper-v$Version.exe"
$ldflags = "-H=windowsgui -X github.com/uvwt/CtyunHelper/internal/buildinfo.Version=$Version"

Push-Location $repositoryRoot
try {
    & go build -ldflags $ldflags -o $outputPath ./cmd/ctyun-helper
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }

    & (Join-Path $PSScriptRoot 'embed-icon-windows.ps1') -Executable $outputPath
    if ($LASTEXITCODE -ne 0) {
        throw "embedding Windows resources failed with exit code $LASTEXITCODE"
    }
}
finally {
    Pop-Location
}

$hash = Get-FileHash -Algorithm SHA256 -Path $outputPath
Write-Host "Built release asset: $outputPath"
Write-Host "SHA256: $($hash.Hash)"
Write-Output $outputPath
