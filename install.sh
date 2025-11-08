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
    echo "   Please create config.json before installing as service"
    echo "   For Serbia/Belgrade, use:"
    echo "     cp config.serbia.json config.json"
fi

echo ""
echo "════════════════════════════════════════════"
echo "  Installation Complete!"
echo "════════════════════════════════════════════"
echo ""

# Ask if user wants to install systemd service
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo "📦 Install as systemd service?"
    read -p "Install to /opt/restartko and create systemd service? (y/N) " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo ""
        echo "🚀 Installing systemd service..."
        
        # Check if config.json exists
        if [ ! -f config.json ]; then
            echo "❌ config.json not found. Please create it first."
            exit 1
        fi
        
        # Create service user
        SERVICE_USER="restartko"
        echo "👤 Creating service user..."
        if ! id "$SERVICE_USER" &>/dev/null; then
            sudo useradd -r -s /bin/false -d /opt/restartko -c "RestartKO Network Monitor" "$SERVICE_USER"
            echo "✅ Service user created: $SERVICE_USER"
        else
            echo "ℹ️  Service user already exists: $SERVICE_USER"
        fi
        
        # Create installation directory
        echo "📁 Creating /opt/restartko..."
        sudo mkdir -p /opt/restartko
        
        # Copy binary and config
        echo "📋 Copying files..."
        sudo cp restartko /opt/restartko/
        sudo cp config.json /opt/restartko/
        sudo chmod +x /opt/restartko/restartko
        
        # Set proper ownership
        echo "🔐 Setting file ownership..."
        sudo chown -R "$SERVICE_USER:$SERVICE_USER" /opt/restartko
        
        # Set ownership for state directory
        sudo chown -R "$SERVICE_USER:$SERVICE_USER" /var/log/restartko
        
        # Set capabilities for the installed binary (if using raw sockets)
        if command -v setcap &> /dev/null; then
            sudo setcap cap_net_raw+ep /opt/restartko/restartko 2>/dev/null || true
            echo "✅ Capabilities set for raw socket mode"
        fi
        
        # Check if raw sockets are enabled for NoNewPrivileges setting
        NO_NEW_PRIVILEGES="true"
        if grep -q '"use_raw_sockets"[[:space:]]*:[[:space:]]*true' config.json 2>/dev/null; then
            NO_NEW_PRIVILEGES="false"
            echo "ℹ️  Raw sockets enabled - adjusting security settings"
        fi
        
        # Create systemd service file
        echo "⚙️  Creating systemd service..."
        sudo tee /etc/systemd/system/restartko.service > /dev/null <<EOF
[Unit]
Description=RestartKO Network Monitor
Documentation=https://github.com/yourusername/restartko
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=/opt/restartko

# Wait 30 seconds for network to be fully ready after boot
ExecStartPre=/bin/sleep 30
ExecStart=/opt/restartko/restartko -config /opt/restartko/config.json

# Auto-restart if service crashes
Restart=always
RestartSec=10

# Security settings
NoNewPrivileges=$NO_NEW_PRIVILEGES
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/restartko

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=restartko

# Resource limits
LimitNOFILE=65536
MemoryMax=256M

[Install]
WantedBy=multi-user.target
EOF
        
        echo "✅ Systemd service created"
        
        # Reload systemd and enable service
        sudo systemctl daemon-reload
        sudo systemctl enable restartko
        
        echo ""
        echo "✅ Systemd service installed!"
        echo ""
        echo "Service Details:"
        echo "  • Service Name: restartko"
        echo "  • Install Directory: /opt/restartko"
        echo "  • Service User: $SERVICE_USER"
        echo "  • Config File: /opt/restartko/config.json"
        echo "  • State File: /var/log/restartko/state.json"
        echo ""
        echo "To start the service:"
        echo "  sudo systemctl start restartko"
        echo ""
        echo "To check status:"
        echo "  sudo systemctl status restartko"
        echo ""
        echo "To view logs:"
        echo "  sudo journalctl -u restartko -f"
        echo ""
        echo "To edit config:"
        echo "  sudo nano /opt/restartko/config.json"
        echo "  sudo systemctl restart restartko"
        echo ""
    else
        echo ""
        echo "Skipping systemd installation."
        echo ""
        echo "Manual steps:"
        echo "  1. Configure config.json with your settings"
        echo "  2. Test ping: ./restartko -test-ping 8.8.8.8"
        echo "  3. Test router: ./restartko -test-router"
        echo "  4. Run service: ./restartko"
        echo ""
        echo "State file location: /var/log/restartko/state.json"
        echo "  ✓ Compatible with log2ram (minimal SD card writes)"
        echo ""
    fi
else
    echo "Manual steps:"
    echo "  1. Configure config.json with your settings"
    echo "  2. Test ping: ./restartko -test-ping 8.8.8.8"
    echo "  3. Test router: ./restartko -test-router"
    echo "  4. Run service: ./restartko"
    echo ""
fi
