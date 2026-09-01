[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string] $LauncherPath,

    [Parameter(Mandatory)]
    [string] $SetupPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$files = @(
    (Get-Item -LiteralPath $LauncherPath),
    (Get-Item -LiteralPath $SetupPath)
)

foreach ($file in $files) {
    $signature = Get-AuthenticodeSignature -LiteralPath $file.FullName
    if ($signature.Status -ne 'Valid') {
        throw "$($file.Name) has no valid Authenticode signature (status: $($signature.Status))."
    }
    if (-not $signature.SignerCertificate.Subject) {
        throw "$($file.Name) has no identifiable signer subject."
    }

    $version = $file.VersionInfo
    foreach ($property in @('FileDescription', 'ProductName', 'FileVersion', 'ProductVersion', 'OriginalFilename')) {
        if ([string]::IsNullOrWhiteSpace($version.$property)) {
            throw "$($file.Name) has an empty $property version resource."
        }
    }

    [pscustomobject]@{
        File = $file.Name
        SHA256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        Signer = $signature.SignerCertificate.Subject
        FileVersion = $version.FileVersion
        ProductVersion = $version.ProductVersion
    }
}
