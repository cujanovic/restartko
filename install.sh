#!/bin/bash

# RestartKO Installation Script

set -e

echo "════════════════════════════════════════════"
echo "  RestartKO Installation"
echo "════════════════════════════════════════════"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or later."
    echo "   Visit: https://golang.org/doc/install"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo "✓ Go version: $GO_VERSION"

# Check Go version (require 1.21+)
REQUIRED_VERSION="1.21"
if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
    echo "❌ Go version 1.21 or later is required. Current version: $GO_VERSION"
    exit 1
fi

echo ""
echo "📦 Downloading dependencies..."
go mod download

echo ""
echo "🔨 Building RestartKO..."
go build -o restartko -ldflags="-s -w"

if [ ! -f restartko ]; then
    echo "❌ Build failed"
    exit 1
fi

echo "✅ Build successful"

# Create state directory in /var/log (works with log2ram)
echo ""
echo "📁 Creating state directory..."
sudo mkdir -p /var/log/restartko
sudo chown $USER:$USER /var/log/restartko
echo "✅ State directory created: /var/log/restartko"

# Set capabilities for ICMP ping (Linux only) - only needed if use_raw_sockets is true
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo ""
    echo "🔐 Setting capabilities for ICMP ping (raw sockets)..."
    echo "   Note: Only needed if 'use_raw_sockets' is set to true in config"
    
    if command -v setcap &> /dev/null; then
        sudo setcap cap_net_raw+ep ./restartko || {
            echo "⚠️  Failed to set capabilities."
            echo "   If using 'use_raw_sockets: true', run as root or fix capabilities"
            echo "   If using 'use_raw_sockets: false' (default), no special permissions needed"
        }
        echo "✅ Capabilities set for raw socket mode"
    else
        echo "⚠️  setcap not found."
        echo "   For raw sockets mode, you may need to run as root"
        echo "   Or use 'use_raw_sockets: false' in config (default)"
    fi
fi

# Check if config.json exists
if [ ! -f config.json ]; then
    echo ""
    echo "⚠️  config.json not found"
    echo "   Please create config.json before running the service"
fi

echo ""
echo "════════════════════════════════════════════"
echo "  Installation Complete!"
echo "════════════════════════════════════════════"
echo ""
echo "Next steps:"
echo "  1. Configure config.json with your settings"
echo "  2. Test ping: ./restartko -test-ping 8.8.8.8"
echo "  3. Test router: ./restartko -test-router"
echo "  4. Run service: ./restartko"
echo ""
echo "State file location: /var/log/restartko/state.json"
echo "  ✓ Compatible with log2ram (minimal SD card writes)"
echo ""
echo "For systemd service installation:"
echo "  sudo cp restartko /opt/restartko/"
echo "  sudo cp config.json /opt/restartko/"
echo "  Create systemd service (see README.md)"
echo ""

