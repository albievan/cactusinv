# cactusInv setup for Windows: downloads the newest release of cactusinv.exe for this computer
# (Intel/AMD or ARM 64-bit) into the current folder, checks it, and replaces an older
# cactusinv.exe there (kept as cactusinv.old.exe). Run it in PowerShell, in the folder
# cactusinv lives in:
#
#   irm https://raw.githubusercontent.com/albievan/cactusinv/main/setup.ps1 | iex
#
# Set $env:CACTUSINV_VERSION = "1.28.0" to fetch that release instead of the newest one.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is many times slower with the bar
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = 'https://github.com/albievan/cactusinv/releases'
$arch = $null
try { $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() } catch { }
if (-not $arch) { $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE } }
switch -Regex ($arch) {
    '^(X64|AMD64)$' { $arch = 'amd64' }
    '^ARM64$' { $arch = 'arm64' }
    default { throw "cactusinv setup: no release for the $arch processor (there are amd64 and arm64)" }
}
$asset = "cactusinv-windows-$arch.exe"
$base = if ($env:CACTUSINV_VERSION) { "$repo/download/v$($env:CACTUSINV_VERSION)" } else { "$repo/latest/download" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("cactusinv-setup-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading $asset ..."
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
    Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')

    # Windows PowerShell has no Ed25519, so the release signature (SHA256SUMS.sig) is not
    # checked here; the checksum list comes over HTTPS from GitHub.
    $want = $null
    foreach ($line in Get-Content (Join-Path $tmp 'SHA256SUMS')) {
        $f = $line -split '\s+'
        if ($f.Count -ge 2 -and $f[1] -eq $asset) { $want = $f[0] }
    }
    if (-not $want) { throw "cactusinv setup: $asset is not in SHA256SUMS" }
    $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
    if ($got -ne $want.ToLower()) { throw "cactusinv setup: the checksum of $asset does not match: not installing" }
    Write-Host 'Checksum: matches. (Release signature: not checked on Windows.)'

    $new = & (Join-Path $tmp $asset) version
    if ($LASTEXITCODE -ne 0) { throw 'cactusinv setup: the downloaded program does not run on this computer' }
    if (Test-Path cactusinv.exe) {
        Move-Item -Force cactusinv.exe cactusinv.old.exe
        Write-Host 'Kept the previous program as cactusinv.old.exe.'
    }
    Move-Item (Join-Path $tmp $asset) cactusinv.exe
    Write-Host "Installed cactusinv $new in $((Get-Location).Path)."
    Write-Host 'Restart cactusinv if it was running. A first install needs a MariaDB 11 database and a'
    Write-Host 'db_password secret: see https://github.com/albievan/cactusinv#installing-the-program'
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
