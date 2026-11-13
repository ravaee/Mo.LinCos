# Installs or updates the lincos command on Windows, in PowerShell:
#
#   irm https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/local.ps1 | iex
#
# Then, in a new terminal: lincos open <any server you can ssh to that runs the agent>
$ErrorActionPreference = 'Stop'

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$dir = Join-Path $env:LOCALAPPDATA 'Programs\lincos'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$exe = Join-Path $dir 'lincos.exe'
Invoke-WebRequest -UseBasicParsing -OutFile $exe "https://github.com/ravaee/Mo.LinCos/releases/latest/download/lincos-windows-$arch.exe"

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
