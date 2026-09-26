# OurWay Agent Installer for Windows
# Usage: .\install.ps1 -Server "wss://yourserver.com" -Key "YOUR_DEVICE_KEY"
# Or from the web:
# Invoke-WebRequest -Uri "https://ourway.example.com/install.ps1" -UseBasicParsing | Invoke-Expression

param(
    [string]$Server = "http://localhost:8081",
    [string]$Key = "",
    [string]$InstallDir = "C:\Program Files\OurWay\Agent",
    [string]$Version = "1.0.0"
)

function Write-Info { param($msg) Write-Host "[info] $msg" -ForegroundColor Cyan }
function Write-Ok { param($msg) Write-Host "[ok] $msg" -ForegroundColor Green }
function Write-Warn { param($msg) Write-Host "[warn] $msg" -ForegroundColor Yellow }
function Write-ErrorAndExit { param($msg) Write-Host "[error] $msg" -ForegroundColor Red; exit 1 }

Write-Host "======================================"
Write-Host "  OurWay RMM Agent Installer"
Write-Host "======================================"
Write-Host ""

# Detect architecture
$arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
    $arch = "arm64"
}

Write-Info "Detected OS: windows/$arch"
Write-Info "Server: $Server"
if ($Key) { Write-Info "Device Key: $Key" }
Write-Host ""

# Determine binary path
$binaryPath = Join-Path $InstallDir "ourway-agent.exe"

# Download binary
Write-Info "Downloading agent binary..."
$url = "$Server/api/agent/binary?os=windows&arch=$arch&version=$Version"
Write-Info "URL: $url"

try {
    # Try local binary first (for development)
    if (Test-Path "./dist/agents/ourway-agent-windows-$arch.exe") {
        Write-Info "Using local binary"
        $binaryPath = "./dist/agents/ourway-agent-windows-$arch.exe"
    } elseif (Test-Path "./ourway-agent.exe") {
        Write-Info "Using binary in current directory"
        $binaryPath = "./ourway-agent.exe"
    } else {
        # Download from server
        if (!(Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        }
        Invoke-WebRequest -Uri $url -OutFile $binaryPath -UseBasicParsing
        Write-Ok "Binary downloaded to $binaryPath"
    }
} catch {
    Write-ErrorAndExit "Failed to download binary: $_"
}

# Create configuration directory
$configDir = Join-Path $InstallDir "config"
if (!(Test-Path $configDir)) {
    New-Item -ItemType Directory -Path $configDir -Force | Out-Null
}
Write-Ok "Installation directories created"

# Write configuration
$configFile = Join-Path $configDir "device.json"
$config = @{
    server_url = $Server
    device_key = $Key
    collect_interval = 30
    log_level = "info"
}
$config | ConvertTo-Json | Set-Content -Path $configFile -Encoding UTF8
Write-Ok "Configuration written to $configFile"

# Install as Windows service
Write-Host ""
Write-Info "Installing as Windows service..."

try {
    # Stop existing service if running
    $svc = Get-Service -Name "OurWayAgent" -ErrorAction SilentlyContinue
    if ($svc) {
        Write-Info "Service already exists, updating..."
        Stop-Service -Name "OurWayAgent" -Force -ErrorAction SilentlyContinue
        sc.exe delete OurWayAgent | Out-Null
        Start-Sleep -Seconds 2
    }
    
    # Create service
    $cmd = "sc.exe create OurWayAgent binPath=`"$binaryPath --server $Server --key $Key`" start=auto"
    Invoke-Expression $cmd | Out-Null
    
    # Start service
    Start-Service -Name "OurWayAgent"
    
    Write-Ok "Windows service installed and started"
} catch {
    Write-Warn "Failed to install as service (run as administrator?): $_"
}

Write-Host ""
Write-Host "======================================"
Write-Ok "OurWay Agent installed successfully!"
Write-Host "======================================"
Write-Host ""
Write-Host "  Binary:   $binaryPath"
Write-Host "  Config:   $configDir"
Write-Host "  Server:   $Server"
Write-Host ""
Write-Host "Service status: Get-Service OurWayAgent"
Write-Host "Logs: sc.exe query OurWayAgent"
Write-Host ""
Write-Host "To uninstall:"
Write-Host "  sc.exe stop OurWayAgent"
Write-Host "  sc.exe delete OurWayAgent"
Write-Host "  Remove-Item -Recurse -Force $InstallDir"
