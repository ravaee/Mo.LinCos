# Installs or updates the lincos command on Windows. Download it, read it, then
# run it, in PowerShell:
#
#   irm https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/local.ps1 -OutFile local.ps1
#   notepad local.ps1
#   powershell -ExecutionPolicy Bypass -File local.ps1
#
# It installs the program only if its SHA-256 matches the release's SHA256SUMS.
# Then, in a new terminal: lincos open <any server you can ssh to that runs the agent>
$ErrorActionPreference = 'Stop'

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$dir = Join-Path $env:LOCALAPPDATA 'Programs\lincos'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$exe = Join-Path $dir 'lincos.exe'
$release = 'https://github.com/ravaee/Mo.LinCos/releases/latest/download'
$bin = "lincos-windows-$arch.exe"
$tmp = Join-Path ([IO.Path]::GetTempPath()) "lincos-$([guid]::NewGuid())"
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Invoke-WebRequest -UseBasicParsing -OutFile (Join-Path $tmp 'SHA256SUMS') "$release/SHA256SUMS"
    Invoke-WebRequest -UseBasicParsing -OutFile (Join-Path $tmp $bin) "$release/$bin"

    # Installed only if it matches the release's SHA256SUMS, so a broken or swapped file never is
    $line = Get-Content (Join-Path $tmp 'SHA256SUMS') | Where-Object { ($_ -split '\s+')[1] -eq $bin }
    if (-not $line) { throw "$bin is not listed in SHA256SUMS" }
    $want = ($line -split '\s+')[0]
    $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $bin)).Hash
    if ($got -ne $want) { throw "$bin does not match its checksum; nothing was installed" }

    Move-Item -Force (Join-Path $tmp $bin) $exe
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# Put the folder on your PATH once
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not $userPath) { $userPath = '' }
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir").TrimStart(';'), 'User')
    $env:Path += ";$dir"
}

Write-Host "Installed $(& $exe version) to $exe"
if (-not (Get-Command ssh -ErrorAction SilentlyContinue)) {
    Write-Host 'Note: lincos needs ssh (Settings > Optional features > OpenSSH Client).'
}
Write-Host 'Next, in a new terminal: lincos open <ssh host>'
