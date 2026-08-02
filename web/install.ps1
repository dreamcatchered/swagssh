$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$Domain = "ssh.swag.best"
$BaseUrl = "https://$Domain"

if ([Environment]::Is64BitProcess) {
    $Arch = "amd64"
} else {
    $Arch = "386"
}

$Binary = "swagssh-windows-$Arch.exe"
$Url = "$BaseUrl/releases/$Binary"
$Dest = Join-Path $env:TEMP "swagssh.exe"

Write-Host ""
Write-Host "  [+] swagSSH Installer" -ForegroundColor Cyan
Write-Host "  [+] Platform: windows/$Arch" -ForegroundColor Cyan
Write-Host "  [+] Downloading: $Url" -ForegroundColor Cyan
Write-Host ""

try {
    Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing
} catch {
    Write-Host "[-] Failed to download: $_" -ForegroundColor Red
    exit 1
}

Write-Host "  [+] Initializing Reverse SSH Tunnel..." -ForegroundColor Green
Write-Host ""

& $Dest share --server "${Domain}:2222"
