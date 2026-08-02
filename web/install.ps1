$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$Domain = "ssh.swag.best"
$BaseUrl = "https://$Domain"

if ([Environment]::Is64BitProcess) {
    $Arch = "amd64"
} else {
    $Arch = "386"
}

$InstallDir = Join-Path $env:LOCALAPPDATA "swagssh"
$Binary = "swagssh-windows-$Arch.exe"
$Url = "$BaseUrl/releases/$Binary"
$ExePath = Join-Path $InstallDir "swagssh.exe"

Write-Host ""
Write-Host "  [+] swagSSH Installer" -ForegroundColor Cyan
Write-Host "  [+] Platform: windows/$Arch" -ForegroundColor Cyan

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

Write-Host "  [+] Downloading: $Url" -ForegroundColor Cyan
try {
    Invoke-WebRequest -Uri $Url -OutFile $ExePath -UseBasicParsing
} catch {
    Write-Host "[-] Failed to download: $_" -ForegroundColor Red
    exit 1
}

$currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($currentPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$currentPath;$InstallDir", "User")
    $env:PATH = "$env:PATH;$InstallDir"
    Write-Host "  [+] Added to user PATH: $InstallDir" -ForegroundColor Green
}

Write-Host "  [+] Installed to: $ExePath" -ForegroundColor Green
Write-Host "  [+] To connect from another terminal, open a NEW window and run:" -ForegroundColor Yellow
Write-Host "        swagssh connect <session-id>" -ForegroundColor Yellow
Write-Host ""
Write-Host "  [+] Initializing Reverse SSH Tunnel..." -ForegroundColor Green
Write-Host ""

& $ExePath share --server "${Domain}:2222"
