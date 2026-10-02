#!/bin/bash
# OurWay Agent Installer
# Usage: curl -sL https://ourway.example.com/install.sh | bash -s -- --server wss://yourserver.com --key YOUR_DEVICE_KEY
# Or simply: ./install.sh (for local installation)
set -e

VERSION="1.0.0"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}[info]${NC} $1"; }
success() { echo -e "${GREEN}[ok]${NC} $1"; }
warn() { echo -e "${YELLOW}[warn]${NC} $1"; }
error() { echo -e "${RED}[error]${NC} $1"; exit 1; }

# Default values
SERVER="http://localhost:8080"
DEVICE_KEY=""
INSTALL_DIR="/opt/ourway"
BIN_NAME="ourway-agent"
SKIP_SERVICE=false
REGISTER=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --server)
            SERVER="$2"
            shift 2
            ;;
        --key)
            DEVICE_KEY="$2"
            shift 2
            ;;
        --install-dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        --skip-service)
            SKIP_SERVICE=true
            shift
            ;;
        --register)
            REGISTER=true
            shift
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        -h|--help)
            echo "OurWay Agent Installer"
            echo ""
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --server URL       Server URL (default: http://localhost:8080)"
            echo "  --key KEY          Device key for registration"
            echo "  --register         Register device with server and get key"
            echo "  --install-dir DIR  Installation directory (default: /opt/ourway)"
            echo "  --skip-service     Don't install as a service"
            echo "  --version VERSION  Agent version to install (default: $VERSION)"
            echo "  -h, --help         Show this help message"
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            ;;
    esac
done

echo "======================================"
echo "  OurWay RMM Agent Installer"
echo "======================================"
echo ""

# Detect OS
detect_os() {
    local os="$(uname -s)"
    case "${os}" in
        Linux*)     OS="linux" ;;
        Darwin*)    OS="darwin" ;;
        *)          error "Unsupported operating system: $os" ;;
    esac
    echo "$OS"
}

# Detect architecture
detect_arch() {
    local arch="$(uname -m)"
    case "${arch}" in
        x86_64)     ARCH="amd64" ;;
        amd64)      ARCH="amd64" ;;
        arm64)      ARCH="arm64" ;;
        aarch64)    ARCH="arm64" ;;
        *)          error "Unsupported architecture: $arch" ;;
    esac
    echo "$ARCH"
}

OS=$(detect_os)
ARCH=$(detect_arch)

info "Detected OS: ${OS}/${ARCH}"
info "Server: ${SERVER}"
[ -n "$DEVICE_KEY" ] && info "Device Key: ${DEVICE_KEY}"
echo ""

# Auto-register device if no key provided and --register flag is set
if [ -z "$DEVICE_KEY" ] && [ "$REGISTER" = true ]; then
    info "Registering device with server..."
    
    # Determine hostname
    HOSTNAME=$(hostname 2>/dev/null || cat /etc/hostname 2>/dev/null || echo "unknown")
    
    # Get IP addresses
    PUBLIC_IP=$(curl -s --max-time 5 https://api.ipify.org 2>/dev/null || echo "")
    PRIVATE_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "")
    
    # Build JSON payload. Append the IP fields to the base payload (not
    # a full rebuild) so detecting a private IP does not drop the public
    # one.
    PAYLOAD="{\"name\":\"${HOSTNAME}\",\"hostname\":\"${HOSTNAME}\",\"os\":\"${OS}\",\"arch\":\"${ARCH}\",\"agent_version\":\"${VERSION}\"}"
    
    if [ -n "$PUBLIC_IP" ]; then
        PAYLOAD="${PAYLOAD},\"public_ip\":\"${PUBLIC_IP}\""
    fi
    if [ -n "$PRIVATE_IP" ]; then
        PAYLOAD="${PAYLOAD},\"private_ip\":\"${PRIVATE_IP}\""
    fi
    
    # Register with server (|| true: set -e must not kill the script on a
    # failed call — the error is handled below and by the key gate)
    RESPONSE=$(curl -s --max-time 10 -X POST "${SERVER}/api/agent/register" -H "Content-Type: application/json" -d "$PAYLOAD" 2>&1) || true

    if [ -n "$RESPONSE" ]; then
        DEVICE_KEY=$(echo "$RESPONSE" | grep -o '"device_key":"[^"]*"' | head -1 | cut -d'"' -f4)
    fi
    if [ -n "$DEVICE_KEY" ]; then
        success "Device registered! Key: ${DEVICE_KEY}"
    else
        warn "Device registration failed: ${RESPONSE:-no response from server}"
        warn "You can manually set the key with --key option"
    fi
    echo ""
fi

# Hard gate: never install an agent with an empty device key — it would
# crash-loop on start with no way to recover.
if [ -z "$DEVICE_KEY" ]; then
    error "No device key available. Pass --key KEY, or fix server registration (is ${SERVER} reachable?) and retry with --register."
fi

# Determine download URL
if [ -f "./dist/agents/${BIN_NAME}-${OS}-${ARCH}" ]; then
    # Use local binary for development
    BINARY="./dist/agents/${BIN_NAME}-${OS}-${ARCH}"
    info "Using local binary: ${BINARY}"
elif [ -f "./${BIN_NAME}" ]; then
    # Use binary in current directory
    BINARY="./${BIN_NAME}"
    info "Using binary in current directory: ${BINARY}"
else
    # Download from GitHub releases
    BINARY="${INSTALL_DIR}/${BIN_NAME}"
    SUFFIX=""
    DOWNLOAD_URL="https://github.com/WelcomeToTheWeb/OurWay/releases/download/v${VERSION}/ourway-agent-${OS}-${ARCH}${SUFFIX}"
    
    info "Downloading agent binary from GitHub releases..."
    info "URL: ${DOWNLOAD_URL}"
    
    if command -v curl >/dev/null 2>&1; then
        if ! curl -fsSL -o "${BINARY}" "${DOWNLOAD_URL}"; then
            error "Failed to download binary. Check version: ${VERSION}"
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -q -O "${BINARY}" "${DOWNLOAD_URL}"; then
            error "Failed to download binary. Check version: ${VERSION}"
        fi
    else
        error "Neither curl nor wget found. Install one of them."
    fi
    
    success "Binary downloaded to ${BINARY}"
fi

# Make binary executable
chmod +x "${BINARY}"

# Create installation directories
if [ "${BINARY}" != "${INSTALL_DIR}/${BIN_NAME}" ]; then
    mkdir -p "${INSTALL_DIR}"
    cp "${BINARY}" "${INSTALL_DIR}/${BIN_NAME}"
    chmod +x "${INSTALL_DIR}/${BIN_NAME}"
fi

mkdir -p "${INSTALL_DIR}/config"
mkdir -p "${INSTALL_DIR}/logs"

success "Installation directories created"

# Write configuration
CONFIG_FILE="${INSTALL_DIR}/config/device.json"
cat > "${CONFIG_FILE}" << EOF
{
    "server_url": "${SERVER}",
    "device_key": "${DEVICE_KEY}",
    "collect_interval": 30,
    "log_level": "info"
}
EOF

success "Configuration written to ${CONFIG_FILE}"

# Install as service (if not skipped)
if [ "$SKIP_SERVICE" != true ]; then
    echo ""
    info "Installing as service..."
    
    case "$OS" in
        linux)
            UNIT_FILE="/etc/systemd/system/ourway-agent.service"
            
            cat > "${UNIT_FILE}" << EOF
[Unit]
Description=OurWay RMM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=${INSTALL_DIR}/${BIN_NAME} --server ${SERVER} --key ${DEVICE_KEY}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

            systemctl daemon-reload
            systemctl enable ourway-agent
            systemctl start ourway-agent
            
            success "Systemd service installed and started"
            ;;
        darwin)
            PLIST_FILE="/Library/LaunchDaemons/com.ourway.agent.plist"
            
            cat > "${PLIST_FILE}" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ourway.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/${BIN_NAME}</string>
        <string>--server</string>
        <string>${SERVER}</string>
        <string>--key</string>
        <string>${DEVICE_KEY}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/ourway-agent.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/ourway-agent.err.log</string>
</dict>
</plist>
EOF

            launchctl load -w "${PLIST_FILE}"
            success "Launchd service installed and started"
            ;;
    esac
fi

echo ""
echo "======================================"
success "OurWay Agent installed successfully!"
echo "======================================"
echo ""
echo "  Binary:   ${INSTALL_DIR}/${BIN_NAME}"
echo "  Config:   ${INSTALL_DIR}/config"
echo "  Logs:     ${INSTALL_DIR}/logs"
echo "  Server:   ${SERVER}"
echo ""

if [ "$SKIP_SERVICE" != true ]; then
    echo "Service is running. Check status with:"
    if [ "$OS" = "linux" ]; then
        echo "  sudo systemctl status ourway-agent"
        echo "  sudo journalctl -u ourway-agent -f"
    else
        echo "  sudo launchctl list com.ourway.agent"
        echo "  cat /tmp/ourway-agent.out.log"
    fi
fi

echo ""
echo "To uninstall, run:"
if [ "$OS" = "linux" ]; then
    echo "  sudo systemctl stop ourway-agent"
    echo "  sudo systemctl disable ourway-agent"
    echo "  sudo rm /etc/systemd/system/ourway-agent.service"
    echo "  sudo systemctl daemon-reload"
    echo "  sudo rm -rf ${INSTALL_DIR}"
else
    echo "  sudo launchctl unload -w /Library/LaunchDaemons/com.ourway.agent.plist"
    echo "  sudo rm /Library/LaunchDaemons/com.ourway.agent.plist"
    echo "  sudo rm -rf ${INSTALL_DIR}"
fi
