#!/usr/bin/env bash
# OurWay Agent installer for Linux and macOS
# Usage: ./install.sh [--server ws://host:port] [--key device_key] [--user]

set -euo pipefail

VERSION="1.0.0"
SERVER="ws://localhost:8081"
KEY=""
INSTALL_MODE="system"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --server)
            SERVER="$2"
            shift 2
            ;;
        --key)
            KEY="$2"
            shift 2
            ;;
        --user)
            INSTALL_MODE="user"
            shift
            ;;
        --version)
            echo "$VERSION"
            exit 0
            ;;
        --help|-h)
            echo "Usage: $0 [--server ws://host:port] [--key device_key] [--user]"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Detect OS and architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        GOARCH="amd64"
        ;;
    arm64|aarch64)
        GOARCH="arm64"
        ;;
    armv7l)
        GOARCH="arm"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

case "$OS" in
    Linux)
        GOOS="linux"
        ;;
    Darwin)
        GOOS="darwin"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

BINARY="ourway-agent"
URL="https://releases.ourway.io/agent/v${VERSION}/${BINARY}-${GOOS}-${GOARCH}"

echo "=== OurWay Agent Installer ==="
echo "OS: ${GOOS}"
echo "Architecture: ${GOARCH}"
echo "Server: ${SERVER}"
echo "Key: ${KEY}"
echo ""

# Download binary
echo "Downloading ${BINARY}..."
if command -v curl &> /dev/null; then
    curl -L -o "/tmp/${BINARY}" "$URL"
elif command -v wget &> /dev/null; then
    wget -O "/tmp/${BINARY}" "$URL"
else
    echo "Error: curl or wget required"
    exit 1
fi

chmod +x "/tmp/${BINARY}"

# Install binary
if [[ "$OS" == "Linux" ]]; then
    install -m 755 "/tmp/${BINARY}" "/usr/local/bin/${BINARY}"
else
    install -m 755 "/tmp/${BINARY}" "/usr/local/bin/${BINARY}"
fi

echo "Installed binary to /usr/local/bin/${BINARY}"

# Create config file
CONFIG_DIR="/etc/ourway"
CONFIG_FILE="${CONFIG_DIR}/agent.conf"

if [[ "$OS" == "Darwin" ]]; then
    CONFIG_DIR="$HOME/.ourway"
    CONFIG_FILE="${CONFIG_DIR}/agent.conf"
fi

mkdir -p "$CONFIG_DIR"

cat > "$CONFIG_FILE" << EOF
# OurWay Agent Configuration
OURWAY_SERVER="${SERVER}"
OURWAY_DEVICE_KEY="${KEY}"
EOF

echo "Created config at ${CONFIG_FILE}"

# Install as service
if [[ "$OS" == "Linux" ]]; then
    echo "Installing systemd service..."
    "${BINARY}" --install --server "$SERVER" --key "$KEY"
    systemctl enable ourway-agent
    systemctl start ourway-agent
    echo "Service installed and started"
else
    echo "Installing launchd agent..."
    "${BINARY}" --install --server "$SERVER" --key "$KEY"
    echo "Agent installed"
fi

echo ""
echo "=== Installation complete ==="
echo "OurWay Agent v${VERSION} is installed and running."
