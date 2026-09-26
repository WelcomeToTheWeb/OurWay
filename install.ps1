# OurWay Agent Installer for Windows
# Usage: .\install.ps1 -Server "wss://yourserver.com" -Register
# Or: .\install.ps1 -Server "wss://yourserver.com" -Key "YOUR_DEVICE_KEY"

param(
    [string]$Server = "http://localhost:8081",
    [string]$Key = "",
    [string]$InstallDir = "C:\Program Files\OurWay\Agent",
    [string]$Version = "1.0.0",
    [switch]$Register,
    [switch]$SkipService
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

# Auto-register device if no key provided and -Register is set
if (-not $Key -and $Register) {
    Write-Info "Registering device with server..."
    
    $hostname = $env:COMPUTERNAME
    $publicIP = ""
    try {
        $publicIP = (Invoke-RestMethod -Uri "https://api.ipify.org" -TimeoutSec 5 -UseBasicParsing).ToString()
    } catch { }
    
    $payload = @{
        name = $hostname
        hostname = $hostname
        os = "windows"
        arch = $arch
        agent_version = $Version
    }
    if ($publicIP) { $payload["public_ip"] = $publicIP }
    
    try {
        $response = Invoke-RestMethod -Uri "$Server/api/agent/register" -Method POST -Body ($payload | ConvertTo-Json) -ContentType "application/json" -UseBasicParsing
        if ($response.device_key) {
            $Key = $response.device_key
            Write-Ok "Device registered! Key: $Key"
        } else {
            Write-Warn "Device registered but could not extract key."
        }
    } catch {
        Write-Warn "Failed to register device: $_"
        Write-Warn "You can manually set the key with -Key option"
    }
    Write-Host ""
}

# Create install directory
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Determine binary path
$binaryPath = Join-Path $InstallDir "ourway-agent.exe"
$localBinaryFound = $false

Write-Info "Downloading agent binary from GitHub releases..."
$url = "https://github.com/WelcomeToTheWeb/OurWay/releases/download/v$Version/ourway-agent-windows-$arch.exe"
Write-Info "URL: $url"

try {
    if (Test-Path "./dist/agents/ourway-agent-windows-$arch.exe") {
        Write-Info "Using local binary: ./dist/agents/ourway-agent-windows-$arch.exe"
        Copy-Item "./dist/agents/ourway-agent-windows-$arch.exe" $binaryPath -Force
        $localBinaryFound = $true
    } elseif (Test-Path "./ourway-agent.exe") {
        Write-Info "Using binary in current directory: ./ourway-agent.exe"
        Copy-Item "./ourway-agent.exe" $binaryPath -Force
        $localBinaryFound = $true
    } else {
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
if (-not $SkipService) {
    Write-Host ""
    Write-Info "Installing as Windows service..."
    
    try {
        $svc = Get-Service -Name "OurWayAgent" -ErrorAction SilentlyContinue
        if ($svc) {
            Write-Info "Service already exists, updating..."
            Stop-Service -Name "OurWayAgent" -Force -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 2
            Remove-Service -Name "OurWayAgent" -Force -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 2
        }
        
        $display = "OurWay Agent"
        $description = "OurWay RMM monitoring agent"
        New-Service -Name "OurWayAgent" -DisplayName $display -BinaryPathName "`"$binaryPath`" --server $Server --key $Key" -StartupType Automatic -Description $description | Out-Null
        Write-Ok "Windows service installed"
        
        Start-Service -Name "OurWayAgent"
        Start-Sleep -Seconds 3
        
        $status = Get-Service -Name "OurWayAgent"
        if ($status.Status -eq "Running") {
            Write-Ok "Service is running"
        } else {
            Write-Warn "Service status: $($status.Status). Check Event Viewer for errors."
        }
    } catch {
        Write-Warn "Failed to install as service: $_"
        Write-Warn "Try running PowerShell as Administrator"
    }
}

Write-Host ""
Write-Host "======================================"
Write-Ok "OurWay Agent installed successfully!"
Write-Host "======================================"
Write-Host ""
Write-Host "  Binary:   $binaryPath"
Write-Host "  Config:   $configFile"
Write-Host "  Server:   $Server"
if ($Key) { Write-Host "  Key:      $Key" }
Write-Host ""

if (-not $SkipService) {
    Write-Host "Check service: Get-Service OurWayAgent"
    Write-Host "View logs: sc.exe query OurWayAgent"
}
Write-Host ""
Write-Host "To uninstall:"
Write-Host "  sc.exe stop OurWayAgent"
Write-Host "  sc.exe delete OurWayAgent"
Write-Host "  Remove-Item -Recurse -Force $InstallDir"
