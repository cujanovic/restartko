#!/bin/bash

# RestartKO Installation Script
# This script installs RestartKO as a systemd service

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
SERVICE_NAME="restartko"
SERVICE_USER="restartko"
INSTALL_DIR="/opt/restartko"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# Check if this is an update or fresh install
UPDATE_MODE=false
if [ -d "$INSTALL_DIR" ] && [ -f "$SERVICE_FILE" ]; then
    UPDATE_MODE=true
    echo -e "${YELLOW}🔄 Existing installation detected - Running in UPDATE mode${NC}"
else
    echo -e "${GREEN}🚀 Installing RestartKO Service${NC}"
fi
echo "================================================"

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}❌ This script must be run as root${NC}"
   echo "Please run: sudo $0"
   exit 1
fi

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${YELLOW}⚠️  Go is not installed. Please install Go 1.21 or later.${NC}"
    echo "   Visit: https://golang.org/doc/install"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo -e "${GREEN}✓ Go version: $GO_VERSION${NC}"

# Check Go version (require 1.21+)
REQUIRED_VERSION="1.21"
if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
    echo -e "${RED}❌ Go version 1.21 or later is required. Current version: $GO_VERSION${NC}"
    exit 1
fi

# Create service user (skip if updating)
if [ "$UPDATE_MODE" = false ]; then
    echo -e "${GREEN}👤 Creating service user...${NC}"
    if ! id "$SERVICE_USER" &>/dev/null; then
        useradd -r -s /bin/false -d "$INSTALL_DIR" -c "RestartKO Network Monitor" "$SERVICE_USER"
        echo -e "${GREEN}✅ Service user created: $SERVICE_USER${NC}"
    else
        echo -e "${YELLOW}⚠️  Service user already exists: $SERVICE_USER${NC}"
    fi
else
    echo -e "${GREEN}👤 Service user already exists: $SERVICE_USER${NC}"
fi

# Stop service if updating
if [ "$UPDATE_MODE" = true ]; then
    echo -e "${YELLOW}⏹️  Stopping service for update...${NC}"
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    echo -e "${GREEN}✅ Service stopped${NC}"
fi

# Save existing config and state to temporary location if updating
TEMP_CONFIG=""
TEMP_STATE=""
if [ "$UPDATE_MODE" = true ] && [ -f "$INSTALL_DIR/config.json" ]; then
    echo -e "${YELLOW}💾 Preserving existing configuration...${NC}"
    TEMP_CONFIG=$(mktemp)
    cp "$INSTALL_DIR/config.json" "$TEMP_CONFIG"
    echo -e "${GREEN}✅ Configuration preserved${NC}"
fi

if [ "$UPDATE_MODE" = true ] && [ -f "/var/log/restartko/state.json" ]; then
    echo -e "${YELLOW}💾 Preserving state file (history)...${NC}"
    TEMP_STATE=$(mktemp)
    cp "/var/log/restartko/state.json" "$TEMP_STATE"
    echo -e "${GREEN}✅ State file preserved${NC}"
fi

# Create installation directory
echo -e "${GREEN}📁 Creating installation directory...${NC}"
mkdir -p "$INSTALL_DIR"

# Create state directory
echo -e "${GREEN}📁 Creating state directory...${NC}"
mkdir -p /var/log/restartko

# Copy files to installation directory
echo -e "${GREEN}📋 Copying service files...${NC}"
cp *.go "$INSTALL_DIR/" 2>/dev/null || true
cp go.mod "$INSTALL_DIR/"
cp go.sum "$INSTALL_DIR/" 2>/dev/null || true

# Copy config file (if not updating)
if [ "$UPDATE_MODE" = false ]; then
    if [ -f "config.serbia.json" ]; then
        echo -e "${GREEN}📋 Using config.serbia.json as default config${NC}"
        cp config.serbia.json "$INSTALL_DIR/config.json"
    elif [ -f "config.json" ]; then
        cp config.json "$INSTALL_DIR/"
    else
        echo -e "${RED}❌ No config file found (config.json or config.serbia.json)${NC}"
        exit 1
    fi
fi

# Restore existing config if this was an update
if [ "$UPDATE_MODE" = true ] && [ -n "$TEMP_CONFIG" ] && [ -f "$TEMP_CONFIG" ]; then
    cp "$TEMP_CONFIG" "$INSTALL_DIR/config.json"
    rm -f "$TEMP_CONFIG"
    echo -e "${GREEN}✅ Existing configuration restored${NC}"
fi

# Restore existing state file if this was an update
if [ "$UPDATE_MODE" = true ] && [ -n "$TEMP_STATE" ] && [ -f "$TEMP_STATE" ]; then
    cp "$TEMP_STATE" "/var/log/restartko/state.json"
    rm -f "$TEMP_STATE"
    echo -e "${GREEN}✅ State file (history) restored${NC}"
fi

# Create one-time backup of original config for fresh installs only
if [ "$UPDATE_MODE" = false ] && [ ! -f "$INSTALL_DIR/config.json.original" ]; then
    cp "$INSTALL_DIR/config.json" "$INSTALL_DIR/config.json.original"
    echo -e "${GREEN}✅ Original configuration backed up to config.json.original${NC}"
fi

# Set proper ownership
chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
chown -R "$SERVICE_USER:$SERVICE_USER" /var/log/restartko

# Build the service binary
echo -e "${GREEN}🔨 Building service binary...${NC}"
cd "$INSTALL_DIR"

# Install dependencies
echo "Installing Go dependencies..."
su -s /bin/bash -c "cd $INSTALL_DIR && go mod download" "$SERVICE_USER"
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to download Go dependencies${NC}"
    exit 1
fi

# Build the binary with optimizations
echo "Building restartko binary (optimized)..."
su -s /bin/bash -c "cd $INSTALL_DIR && go build -ldflags='-s -w' -trimpath -o restartko" "$SERVICE_USER"
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to build binary${NC}"
    exit 1
fi

# Make binary executable
chmod +x restartko
echo -e "${GREEN}✅ Binary built successfully${NC}"

# Check if raw sockets are enabled in config and grant capability if needed
echo -e "${GREEN}🔍 Checking configuration...${NC}"
if [ -f "$INSTALL_DIR/config.json" ]; then
    USE_RAW=$(grep -o '"use_raw_sockets"[[:space:]]*:[[:space:]]*true' "$INSTALL_DIR/config.json" || true)
    if [ -n "$USE_RAW" ]; then
        echo -e "${GREEN}🔐 Raw sockets enabled in config, granting CAP_NET_RAW capability...${NC}"
        setcap cap_net_raw+ep restartko
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✅ CAP_NET_RAW capability granted${NC}"
            if getcap restartko | grep -q cap_net_raw; then
                echo -e "${GREEN}✅ Capability verified${NC}"
            fi
        else
            echo -e "${YELLOW}⚠️  Failed to set capability. Install 'libcap' package.${NC}"
            echo -e "${YELLOW}   Service will fall back to unprivileged mode.${NC}"
        fi
    else
        echo -e "${GREEN}ℹ️  Raw sockets disabled in config (using unprivileged mode)${NC}"
        echo -e "${GREEN}   To enable: Set use_raw_sockets: true in config.json${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Config file not found, skipping capability grant${NC}"
fi

# Create network wait script
echo -e "${GREEN}📡 Creating network wait script...${NC}"
cat > "$INSTALL_DIR/wait-for-network.sh" << 'WAITSCRIPT'
#!/bin/bash
# Wait for network to be ready
MAX_WAIT=60
WAIT_INTERVAL=2
elapsed=0

echo "Waiting for network to be ready..."

# Wait for default route to be available
while [ $elapsed -lt $MAX_WAIT ]; do
    if ip route | grep -q default; then
        echo "✅ Network is ready (default route found)"
        exit 0
    fi
    echo "⏳ Waiting for network... ($elapsed/$MAX_WAIT seconds)"
    sleep $WAIT_INTERVAL
    elapsed=$((elapsed + WAIT_INTERVAL))
done

echo "⚠️  Timeout waiting for network, proceeding anyway (service will auto-restart if needed)"
exit 0
WAITSCRIPT

chmod +x "$INSTALL_DIR/wait-for-network.sh"
chown "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR/wait-for-network.sh"

# Check if raw sockets are enabled for NoNewPrivileges setting
NO_NEW_PRIVILEGES="true"
if [ -f "$INSTALL_DIR/config.json" ]; then
    USE_RAW_SOCKETS=$(grep -o '"use_raw_sockets"[[:space:]]*:[[:space:]]*true' "$INSTALL_DIR/config.json" || true)
    if [ -n "$USE_RAW_SOCKETS" ]; then
        NO_NEW_PRIVILEGES="false"
        echo -e "${GREEN}ℹ️  Raw sockets enabled - setting NoNewPrivileges=false for capabilities${NC}"
    fi
fi

# Create systemd service file
echo -e "${GREEN}⚙️  Creating systemd service...${NC}"
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=RestartKO Network Monitor
Documentation=https://github.com/cujanovic/restartko
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR

# Wait for network to be ready before starting
ExecStartPre=$INSTALL_DIR/wait-for-network.sh
ExecStart=$INSTALL_DIR/restartko

# Auto-restart if service crashes or network wasn't ready
Restart=always
RestartSec=15

StandardOutput=journal
StandardError=journal
SyslogIdentifier=restartko

# Security settings
NoNewPrivileges=$NO_NEW_PRIVILEGES
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/restartko

# Resource limits
LimitNOFILE=65536
MemoryMax=256M

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and enable service
echo -e "${GREEN}🔄 Reloading systemd and enabling service...${NC}"
systemctl daemon-reload
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to reload systemd${NC}"
    exit 1
fi

systemctl enable "$SERVICE_NAME"
if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to enable service${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Service installed and enabled${NC}"

echo ""
echo -e "${YELLOW}DEBUG: UPDATE_MODE=${UPDATE_MODE}${NC}"
if [ "$UPDATE_MODE" = true ]; then
    echo -e "${GREEN}🎉 Update completed successfully!${NC}"
    echo "================================================"
    echo -e "${GREEN}What was updated:${NC}"
    echo "  • Service binary rebuilt with latest code"
    echo "  • Dependencies updated (go.mod, go.sum)"
    echo "  • Configuration preserved (your settings kept)"
    echo "  • State/history preserved"
    echo ""
    echo -e "${YELLOW}🔄 Restarting service...${NC}"
    systemctl start "$SERVICE_NAME"
    RESTART_STATUS=$?
    
    if [ $RESTART_STATUS -ne 0 ]; then
        echo -e "${RED}❌ Failed to start service (exit code: $RESTART_STATUS)${NC}"
        echo -e "${YELLOW}Checking status...${NC}"
        systemctl status "$SERVICE_NAME" --no-pager || true
        echo ""
        echo -e "${RED}Check logs with:${NC}"
        echo "  sudo journalctl -u $SERVICE_NAME -n 50"
        exit 1
    fi
    
    echo -e "${GREEN}⏳ Waiting for service to start...${NC}"
    sleep 3
    
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "${GREEN}✅ Service restarted successfully!${NC}"
        echo ""
        echo -e "${GREEN}📊 Current status:${NC}"
        systemctl status "$SERVICE_NAME" --no-pager -l | head -20 || true
    else
        echo -e "${RED}❌ Service is not active. Check logs:${NC}"
        echo "  sudo journalctl -u $SERVICE_NAME -n 50"
        exit 1
    fi
else
    echo -e "${GREEN}🎉 Installation completed successfully!${NC}"
    echo "================================================"
    echo -e "${GREEN}Service Details:${NC}"
    echo "  • Service Name: $SERVICE_NAME"
    echo "  • Install Directory: $INSTALL_DIR"
    echo "  • Service User: $SERVICE_USER"
    echo "  • Config File: $INSTALL_DIR/config.json"
    echo "  • State File: /var/log/restartko/state.json"
    echo ""
    echo -e "${YELLOW}⚠️  Next Steps:${NC}"
    echo "  1. Update router password in: $INSTALL_DIR/config.json"
    echo "  2. Start the service with: sudo systemctl start $SERVICE_NAME"
fi

echo ""
echo -e "${GREEN}Management Commands:${NC}"
echo "  • Start service:    sudo systemctl start $SERVICE_NAME"
echo "  • Stop service:     sudo systemctl stop $SERVICE_NAME"
echo "  • Restart service:  sudo systemctl restart $SERVICE_NAME"
echo "  • Check status:     sudo systemctl status $SERVICE_NAME"
echo "  • View logs:        sudo journalctl -u $SERVICE_NAME -f"
echo ""
echo -e "${GREEN}📝 Configuration:${NC}"
echo "  • Edit config:      sudo nano $INSTALL_DIR/config.json"
echo "  • Original backup:  $INSTALL_DIR/config.json.original"
echo ""
if [ "$UPDATE_MODE" = true ]; then
    echo -e "${GREEN}🔄 To update again in the future:${NC}"
else
    echo -e "${GREEN}🔄 To update in the future:${NC}"
fi
echo "  • Pull latest code from git"
echo "  • Run: sudo ./install.sh"
echo ""
echo -e "${GREEN}🚀 Service is ready!${NC}"
