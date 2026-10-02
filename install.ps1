param(
    [string]$Repository = $(if ($env:AIQUOKKA_REPO) { $env:AIQUOKKA_REPO } else { 'McKean/aiquokka' }),
    [string]$Version = $(if ($env:VERSION) { $env:VERSION } else { 'latest' }),
    [string]$BinDir = $(if ($env:BIN_DIR) { $env:BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'aiquokka\bin' })
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

if ($env:OS -ne 'Windows_NT') {
    throw 'This installer requires Windows; on Linux or macOS, use install.sh.'
}
if ($Repository -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') {
    throw "Invalid repository: $Repository"
}

$architecture = $env:PROCESSOR_ARCHITEW6432
if (-not $architecture) {
    $architecture = $env:PROCESSOR_ARCHITECTURE
}
switch ($architecture) {
    'AMD64' { $goarch = 'amd64' }
    'ARM64' { $goarch = 'arm64' }
    default { throw "Unsupported architecture: $architecture" }
}

$headers = @{ 'User-Agent' = 'aiquokka-installer' }
if ($Version -eq 'latest') {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repository/releases/latest" -Headers $headers
        $Version = $release.tag_name
    } catch {
        throw "Could not find the latest release in ${Repository}; a published release is required. $($_.Exception.Message)"
    }
}
if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9._+-]*$') {
    throw "Invalid release version: $Version"
}

$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("aiquokka-install-" + [Guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $temporaryDirectory
$staged = $null
try {
    $asset = "aiquokka_windows_${goarch}.zip"
    $downloadUrl = "https://github.com/$Repository/releases/download/$Version"
    $archive = Join-Path $temporaryDirectory $asset
    $checksums = Join-Path $temporaryDirectory 'checksums.txt'
    Write-Host "Downloading aiquokka $Version for windows/$goarch"
    Invoke-WebRequest -Uri "$downloadUrl/$asset" -OutFile $archive -Headers $headers -UseBasicParsing
    Invoke-WebRequest -Uri "$downloadUrl/checksums.txt" -OutFile $checksums -Headers $headers -UseBasicParsing

    $pattern = '^([a-fA-F0-9]{64})\s+\*?' + [Regex]::Escape($asset) + '$'
    $entries = @(Get-Content -LiteralPath $checksums | Where-Object { $_ -match $pattern })
    if ($entries.Count -ne 1) {
        throw "Missing or invalid checksum for $asset"
    }
    $null = $entries[0] -match $pattern
    $expected = $Matches[1]
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
    if ($actual -ne $expected) {
        throw "SHA-256 verification failed for $asset"
    }

    $extracted = Join-Path $temporaryDirectory 'extracted'
    Expand-Archive -LiteralPath $archive -DestinationPath $extracted
    $binary = Join-Path $extracted 'aiquokka.exe'
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
        throw 'Archive does not contain aiquokka.exe'
    }
    $null = New-Item -ItemType Directory -Path $BinDir -Force
    $destination = Join-Path $BinDir 'aiquokka.exe'
    $staged = Join-Path $BinDir ('.aiquokka-' + [Guid]::NewGuid().ToString('N') + '.exe')
    Copy-Item -LiteralPath $binary -Destination $staged
    if (Test-Path -LiteralPath $destination) {
        [IO.File]::Replace($staged, $destination, $null)
    } else {
        [IO.File]::Move($staged, $destination)
    }
    $staged = $null

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $BinDir) {
        $updatedPath = if ($userPath) { "$userPath;$BinDir" } else { $BinDir }
        [Environment]::SetEnvironmentVariable('Path', $updatedPath, 'User')
    }
    if (($env:Path -split ';') -notcontains $BinDir) {
        $env:Path = "$($env:Path);$BinDir"
    }
    Write-Host "Installed $destination. Open a new terminal to use aiquokka."
} finally {
    if ($staged -and (Test-Path -LiteralPath $staged)) {
        Remove-Item -LiteralPath $staged -Force
    }
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
}
