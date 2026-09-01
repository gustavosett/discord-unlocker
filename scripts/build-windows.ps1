[CmdletBinding()]
param(
    [Parameter()]
    [string] $Version = '0.1.5-dev',

    [Parameter()]
    [string] $GoExe = 'go',

    [Parameter()]
    [string] $WinResExe = 'go-winres',

    [Parameter()]
    [string] $IsccExe = "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",

    [Parameter()]
    [switch] $KeepResourceObject
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$outputDirectory = Join-Path $repositoryRoot 'dist'
$resourceConfig = Join-Path $repositoryRoot 'build\windows\winres.json'
$resourcePrefix = Join-Path $repositoryRoot 'cmd\discord-unlocker\rsrc'
$resourceObject = "${resourcePrefix}_windows_amd64.syso"
$launcherPath = Join-Path $outputDirectory 'discord-unlocker.exe'
$compiledSetupPath = Join-Path $outputDirectory 'discord-unlocker-setup-unsigned.exe'
$setupPath = Join-Path $outputDirectory 'discord-unlocker-setup.exe'

function ConvertTo-NumericVersion {
    param([Parameter(Mandatory)][string] $Value)

    if ($Value -notmatch '^v?(?<major>\d+)\.(?<minor>\d+)\.(?<patch>\d+)(?:[-.]ci[.-]?(?<build>\d+))?(?:[-+].*)?$') {
        throw "Version '$Value' must begin with major.minor.patch (for example 0.1.5 or 0.1.5-ci.27)."
    }

    $parts = @(
        [uint32] $Matches.major,
        [uint32] $Matches.minor,
        [uint32] $Matches.patch,
        $(if ($Matches.ContainsKey('build') -and $Matches['build']) {
            [uint32] $Matches['build']
        } else {
            [uint32] 0
        })
    )
    foreach ($part in $parts) {
        if ($part -gt 65535) {
            throw "Every numeric version component must be between 0 and 65535."
        }
    }
    return ($parts -join '.')
}

$numericVersion = ConvertTo-NumericVersion $Version

if (-not (Get-Command $GoExe -ErrorAction SilentlyContinue)) {
    throw "Go executable not found: $GoExe"
}
if (-not (Get-Command $WinResExe -ErrorAction SilentlyContinue)) {
    throw "go-winres executable not found: $WinResExe"
}
if (-not (Test-Path -LiteralPath $IsccExe -PathType Leaf)) {
    throw "Inno Setup compiler not found: $IsccExe"
}

New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null

Push-Location $repositoryRoot
try {
    & $GoExe test -count=1 ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }

    & $GoExe vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed.' }

    & $WinResExe make `
        --in $resourceConfig `
        --arch amd64 `
        --out $resourcePrefix `
        --file-version $numericVersion `
        --product-version $numericVersion
    if ($LASTEXITCODE -ne 0) { throw 'Windows resource generation failed.' }

    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & $GoExe build -trimpath `
        -ldflags "-H=windowsgui -X main.version=$Version" `
        -o $launcherPath `
        ./cmd/discord-unlocker
    if ($LASTEXITCODE -ne 0) { throw 'Launcher build failed.' }

    & $IsccExe "/DMyAppVersion=$Version" "/DMyAppNumericVersion=$numericVersion" installer\discord-unlocker.iss
    if ($LASTEXITCODE -ne 0) { throw 'Installer build failed.' }

    # The suffix is only an internal compiler identity. Renaming after the
    # compiler exits preserves the exact bytes while keeping the public name.
    Move-Item -LiteralPath $compiledSetupPath -Destination $setupPath -Force

    Get-Item -LiteralPath $launcherPath, $setupPath |
        Sort-Object Name |
        ForEach-Object {
            $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
            "$hash *$($_.Name)"
        } |
        Set-Content -LiteralPath (Join-Path $outputDirectory 'SHA256SUMS.txt') -Encoding ascii
}
finally {
    Pop-Location
    if (-not $KeepResourceObject) {
        Remove-Item -LiteralPath $resourceObject -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "Built Discord Unlocker $Version ($numericVersion)."
Write-Host 'This build is unsigned and is for local validation only.'
