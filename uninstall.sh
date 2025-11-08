#!/bin/bash

# RestartKO Uninstallation Script

set -e

echo "════════════════════════════════════════════"
echo "  RestartKO Uninstallation"
echo "════════════════════════════════════════════"
echo ""

# Check if running as systemd service
if systemctl is-active --quiet restartko 2>/dev/null; then
    echo "🛑 Stopping systemd service..."
    sudo systemctl stop restartko
    echo "✅ Service stopped"
fi

if systemctl is-enabled --quiet restartko 2>/dev/null; then
    echo "🔓 Disabling systemd service..."
    sudo systemctl disable restartko
    echo "✅ Service disabled"
fi

# Remove systemd service file
if [ -f /etc/systemd/system/restartko.service ]; then
    echo "🗑️  Removing systemd service file..."
    sudo rm /etc/systemd/system/restartko.service
    sudo systemctl daemon-reload
    echo "✅ Service file removed"
fi

# Remove installation directory
if [ -d /opt/restartko ]; then
    echo "🗑️  Removing installation directory..."
    read -p "Remove /opt/restartko? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo rm -rf /opt/restartko
        echo "✅ Installation directory removed"
    fi
fi

# Remove state directory
if [ -d /var/log/restartko ]; then
    echo "🗑️  Removing state directory..."
    read -p "Remove /var/log/restartko? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo rm -rf /var/log/restartko
        echo "✅ State directory removed"
    fi
fi

# Remove service user
SERVICE_USER="restartko"
if id "$SERVICE_USER" &>/dev/null; then
    echo "👤 Removing service user..."
    read -p "Remove user $SERVICE_USER? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo userdel "$SERVICE_USER" 2>/dev/null
        echo "✅ Service user removed"
    fi
fi

echo ""
echo "════════════════════════════════════════════"
echo "  Uninstallation Complete!"
echo "════════════════════════════════════════════"
echo ""
echo "Note: Local files in current directory were not removed."
echo "To remove them manually: rm -rf restartko"
echo ""

