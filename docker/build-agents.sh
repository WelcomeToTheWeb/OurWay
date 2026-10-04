#!/bin/bash
set -e

VERSION=${VERSION:-1.0.0}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}
BUILD_TIME=${BUILD_TIME:-$(date -u '+%Y-%m-%d %H:%M:%S UTC')}

echo "Building OurWay agents (v${VERSION})..."
mkdir -p dist/agents

# Build Linux amd64
echo "Building Linux amd64 agent..."
GOOS=linux GOARCH=amd64 go build -C agent \
    -ldflags "-s -w -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-agent-linux-amd64 .

# Build Linux arm64
echo "Building Linux arm64 agent..."
GOOS=linux GOARCH=arm64 go build -C agent \
    -ldflags "-s -w -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-agent-linux-arm64 .

# Build macOS amd64
echo "Building macOS amd64 agent..."
GOOS=darwin GOARCH=amd64 go build -C agent \
    -ldflags "-s -w -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-agent-darwin-amd64 .

# Build macOS arm64
echo "Building macOS arm64 agent..."
GOOS=darwin GOARCH=arm64 go build -C agent \
    -ldflags "-s -w -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-agent-darwin-arm64 .

# Build Windows amd64
echo "Building Windows amd64 agent..."
GOOS=windows GOARCH=amd64 go build -C agent \
    -ldflags "-s -w -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-agent-windows-amd64.exe .

# Build Windows arm64
echo "Building Windows arm64 agent..."
GOOS=windows GOARCH=arm64 go build -C agent \
    -ldflags "-s -w -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-agent-windows-arm64.exe .

# Build the Windows remote-control exe and the native viewer. The server
# serves both from /api/v2/installers (the agent downloads the remote exe
# per session; the console links the viewer).
echo "Building Windows remote-control exe..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -C agent \
    -ldflags "-s -w -H windowsgui -X ourway/agent/config.Version=${VERSION}" \
    -o ../dist/agents/ourway-remote-windows-amd64.exe ./cmd/ourway-remote

echo "Building Windows viewer..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -C cmd/ourway-viewer \
    -ldflags "-s -w -H windowsgui" \
    -o ../../dist/agents/ourway-viewer-windows-amd64.exe .

# Build installer CLI for all platforms.
# For each platform, the matching agent binary is copied into
# cmd/ourway-installer/assets/agent and embedded into the installer
# (//go:embed) so the installer is fully self-contained.
# NOTE: assets/agent is overwritten on every iteration and is NOT restored
# afterwards — the agent for the LAST built platform (windows/arm64) is
# what remains in the working tree. That's fine: the checked-in placeholder
# is only a git-tracking stub, and every built installer carries the
# correct binary for its own platform.
echo ""
echo "Building installer CLI..."
for os in linux darwin windows; do
    for arch in amd64 arm64; do
        suffix=""
        if [ "$os" = "windows" ]; then
            suffix=".exe"
        fi
        echo "  ${os}/${arch}..."
        cp "dist/agents/ourway-agent-${os}-${arch}${suffix}" cmd/ourway-installer/assets/agent
        GOOS=$os GOARCH=$arch go build -C cmd/ourway-installer \
            -ldflags "-s -w -X main.version=${VERSION} -X 'main.buildTime=${BUILD_TIME}' -X main.gitCommit=${GIT_COMMIT}" \
            -o ../../dist/agents/ourway-installer-${os}-${arch}${suffix} .
    done
done

# Copy install scripts to dist
echo ""
echo "Copying install scripts..."
cp install.sh dist/agents/
cp install.ps1 dist/agents/

echo ""
echo "Built agents and installers:"
ls -la dist/agents/
