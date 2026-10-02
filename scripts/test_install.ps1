Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$installer = Join-Path (Split-Path $PSScriptRoot -Parent) 'install.ps1'
$parseTokens = $null
$parseErrors = $null
$null = [Management.Automation.Language.Parser]::ParseFile($installer, [ref]$parseTokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) {
    throw ($parseErrors | Out-String)
}

$fixtureDirectory = Join-Path ([IO.Path]::GetTempPath()) ('aiquokka-test-' + [Guid]::NewGuid().ToString('N'))
$assetsDirectory = Join-Path $fixtureDirectory 'assets'
$binDirectory = Join-Path $fixtureDirectory 'installed binaries'
$null = New-Item -ItemType Directory -Path $assetsDirectory -Force
$originalUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$originalProcessPath = $env:Path
$originalArchitecture = $env:PROCESSOR_ARCHITECTURE
$originalNativeArchitecture = $env:PROCESSOR_ARCHITEW6432

function Invoke-RestMethod {
    param($Uri, $Headers)
    if ($Uri -ne 'https://api.github.com/repos/McKean/aiquokka/releases/latest') {
        throw "Unexpected URL: $Uri"
    }
    return @{ tag_name = 'v1.2.3' }
}

function Invoke-WebRequest {
    param($Uri, $OutFile, $Headers, [switch]$UseBasicParsing)
    if (-not $Uri.StartsWith('https://github.com/McKean/aiquokka/releases/download/v1.2.3/')) {
        throw "Unexpected URL: $Uri"
    }
    $name = $Uri.Substring($Uri.LastIndexOf('/') + 1)
    Copy-Item -LiteralPath (Join-Path $assetsDirectory $name) -Destination $OutFile
}

function New-TestRelease {
    param([string]$Architecture, [switch]$Corrupt, [switch]$MissingBinary)
    Get-ChildItem -LiteralPath $assetsDirectory | Remove-Item -Force
    $name = if ($MissingBinary) { 'other.exe' } else { 'aiquokka.exe' }
    $binary = Join-Path $assetsDirectory $name
    Set-Content -LiteralPath $binary -Value 'test release binary' -Encoding ascii
    $archiveName = "aiquokka_windows_${Architecture}.zip"
    $archive = Join-Path $assetsDirectory $archiveName
    Compress-Archive -LiteralPath $binary -DestinationPath $archive
    $checksum = if ($Corrupt) { '0' * 64 } else { (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash }
    Set-Content -LiteralPath (Join-Path $assetsDirectory 'checksums.txt') -Value "$checksum  $archiveName" -Encoding ascii
}

function Assert-InstallFails {
    param([string]$Message)
    $failure = $null
    try {
        & $installer -Repository McKean/aiquokka -Version v1.2.3 -BinDir $binDirectory
    } catch {
        $failure = $_.Exception.Message
    }
    if (-not $failure -or -not $failure.Contains($Message)) {
        throw "Expected failure containing '$Message', received '$failure'"
    }
    if ((Get-Content -LiteralPath (Join-Path $binDirectory 'aiquokka.exe') -Raw).Trim() -ne 'test release binary') {
        throw 'Failed installation changed the existing binary'
    }
}

try {
    foreach ($architecture in @('AMD64', 'ARM64')) {
        $env:PROCESSOR_ARCHITECTURE = $architecture
        $env:PROCESSOR_ARCHITEW6432 = $null
        New-TestRelease -Architecture ($architecture.ToLowerInvariant())
        & $installer -Repository McKean/aiquokka -Version latest -BinDir $binDirectory
        if ((Get-Content -LiteralPath (Join-Path $binDirectory 'aiquokka.exe') -Raw).Trim() -ne 'test release binary') {
            throw "Incorrect installed binary for $architecture"
        }
        if (($env:Path -split ';') -notcontains $binDirectory) {
            throw 'Install directory was not added to PATH'
        }
    }

    New-TestRelease -Architecture arm64 -Corrupt
    Assert-InstallFails -Message 'SHA-256 verification failed'

    New-TestRelease -Architecture arm64
    Set-Content -LiteralPath (Join-Path $assetsDirectory 'checksums.txt') -Value '' -Encoding ascii
    Assert-InstallFails -Message 'Missing or invalid checksum'

    New-TestRelease -Architecture arm64 -MissingBinary
    Assert-InstallFails -Message 'Archive does not contain aiquokka.exe'

    if (@(Get-ChildItem -LiteralPath $binDirectory -Filter '.aiquokka-*' -Force).Count -ne 0) {
        throw 'Installer left staged files behind'
    }
    Write-Host 'Windows installer tests passed.'
} finally {
    [Environment]::SetEnvironmentVariable('Path', $originalUserPath, 'User')
    $env:Path = $originalProcessPath
    $env:PROCESSOR_ARCHITECTURE = $originalArchitecture
    $env:PROCESSOR_ARCHITEW6432 = $originalNativeArchitecture
    Remove-Item -LiteralPath $fixtureDirectory -Recurse -Force
}
