# OurWay Agent Installer for Windows
# Usage: .\install.ps1 -Server "wss://yourserver.com" -Register
# Or: .\install.ps1 -Server "wss://yourserver.com" -Key "YOUR_DEVICE_KEY"

param(
    [string]$Server = "http://localhost:8080",
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
    
    $privateIP = ""
    try {
        $addr = [System.Net.NetworkInformation.NetworkInterface]::GetAllNetworkInterfaces() |
            Where-Object { $_.OperationalStatus -eq 'Up' -and $_.NetworkInterfaceType -ne 'Tunnel' } |
            ForEach-Object { $_.GetIPAddresses() } |
            Where-Object { $_.AddressFamily -eq 'InterNetwork' -and -not $_.IsDnsLinkLocal } |
            Select-Object -First 1
        if ($addr) { $privateIP = $addr.IPAddress.ToString() }
    } catch { }
    
    $payload = @{
        name = $hostname
        hostname = $hostname
        os = "windows"
        arch = $arch
        agent_version = $Version
    }
    if ($publicIP) { $payload["public_ip"] = $publicIP }
    if ($privateIP) { $payload["private_ip"] = $privateIP }
    
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

# Hard gate: never install an agent with an empty device key — it would
# crash-loop on start with no way to recover.
if (-not $Key) {
    Write-ErrorAndExit "No device key available. Pass -Key KEY, or fix server registration (is $Server reachable?) and retry with -Register."
}

# Create install directory
$createdInstallDir = $false
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $createdInstallDir = $true
}

# Roll back partial state (install dir we created) when the install fails
# partway through, so a failed run does not require manual cleanup.
function Remove-PartialState {
    if ($createdInstallDir -and (Test-Path $InstallDir)) {
        Write-Info "Removing partially created $InstallDir..."
        Remove-Item -Recurse -Force $InstallDir -ErrorAction SilentlyContinue
    }
}

# Determine binary path
$binaryPath = Join-Path $InstallDir "ourway-agent.exe"
$localBinaryFound = $false

# Prefer a local dev build, then the server's own installer endpoint (which
# always serves a binary matching the running server), and only fall back
# to GitHub releases. Old release binaries predate the WebSocket
# subprotocol auth (C5) and their connection is rejected with HTTP 400.
Write-Info "Sourcing agent binary..."
$serverBinaryUrl = "$Server/api/v2/installers/ourway-agent-windows-$arch.exe"
$releaseUrl = "https://github.com/WelcomeToTheWeb/OurWay/releases/download/v$Version/ourway-agent-windows-$arch.exe"

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
        try {
            Write-Info "Downloading agent from server: $serverBinaryUrl"
            Invoke-WebRequest -Uri $serverBinaryUrl -OutFile $binaryPath -UseBasicParsing
        } catch {
            Write-Warn "Server installer endpoint unavailable ($_); falling back to GitHub releases"
            Invoke-WebRequest -Uri $releaseUrl -OutFile $binaryPath -UseBasicParsing
        }
        Write-Ok "Binary downloaded to $binaryPath"
    }
} catch {
    Remove-PartialState
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
        $logFile = Join-Path $InstallDir "agent.log"
        # Quote the log-file path: it contains spaces (C:\Program Files\...),
        # and the SCM/CRT splits the service command line on unquoted spaces —
        # the agent would receive --log-file C:\Program and log nowhere useful.
        $binPath = "`"$binaryPath`" --server $Server --key $Key --log-file `"$logFile`""
        Write-Info "Service command: $binPath"
        
        # Stop and remove existing service
        Stop-Service -Name "OurWayAgent" -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 1
        $scDelete = sc.exe delete OurWayAgent 2>&1
        Write-Info "sc.exe delete: $scDelete"
        Start-Sleep -Seconds 2
        
        # Create the service with New-Service: it takes the binary path
        # as a parameter, so PowerShell's native-argument quoting cannot
        # mangle the embedded quotes the way sc.exe does (exit 1639).
        New-Service -Name "OurWayAgent" -DisplayName "OurWay Agent" `
            -BinaryPathName $binPath -StartupType Automatic | Out-Null
        Write-Info "Service created: OurWayAgent"

        # Automatic recovery: restart on failure (5s/10s/30s), matching
        # the Go installer (agent/install/windows.go).
        $null = & sc.exe failure OurWayAgent reset= 86400 actions= restart/5000/restart/10000/restart/30000
        if ($LASTEXITCODE -ne 0) {
            Write-Warn "Could not set service recovery actions (exit $LASTEXITCODE)"
        }
        Start-Sleep -Seconds 2
        
        Start-Service -Name "OurWayAgent"
        Start-Sleep -Seconds 3
        
        $status = Get-Service -Name "OurWayAgent"
        if ($status.Status -eq "Running") {
            Write-Ok "Service is running"
            Write-Info "Logs: $logFile"
        } else {
            Write-Warn "Service status: $($status.Status). Check Event Viewer."
            Write-Info "Logs: $logFile"
        }
    } catch {
        # Remove the (possibly half-created) service definition so a
        # failed install does not leave a broken service behind that
        # needs manual cleanup before re-running.
        Stop-Service -Name "OurWayAgent" -Force -ErrorAction SilentlyContinue
        sc.exe delete OurWayAgent 2>&1 | Out-Null
        Write-Warn "Failed to install as service: $_"
        Write-Warn "The agent binary is installed; re-run as Administrator to register the service."
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
# Uninstall entry points next to the agent: uninstall.cmd and an
# Add/Remove Programs entry that run "ourway-agent.exe --uninstall".
$agentExe = Join-Path $InstallDir "ourway-agent.exe"
try {
    Set-Content -Path (Join-Path $InstallDir "uninstall.cmd") -Value "@echo off`r`n`"%~dp0ourway-agent.exe`" --uninstall --pause`r`n" -Encoding ASCII
    $arp = "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\OurWayAgent"
    New-Item -Path $arp -Force | Out-Null
    Set-ItemProperty -Path $arp -Name DisplayName -Value "OurWay Agent"
    Set-ItemProperty -Path $arp -Name Publisher -Value "OurWay"
    Set-ItemProperty -Path $arp -Name InstallLocation -Value $InstallDir
    Set-ItemProperty -Path $arp -Name UninstallString -Value "`"$agentExe`" --uninstall --pause"
    Set-ItemProperty -Path $arp -Name NoModify -Value 1 -Type DWord
    Set-ItemProperty -Path $arp -Name NoRepair -Value 1 -Type DWord
} catch {
    Write-Host "Warning: could not register the uninstaller: $_"
}
Write-Host "To uninstall: Settings > Apps > OurWay Agent, or run:"
Write-Host "  $InstallDir\uninstall.cmd"
