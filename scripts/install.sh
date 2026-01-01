#!/bin/bash
# Install go-eva on Reachy Mini

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Default values
ROBOT_IP="${ROBOT_IP:-192.168.68.77}"
ROBOT_USER="${ROBOT_USER:-pollen}"
ROBOT_PASS="${ROBOT_PASS:-root}"

echo "🤖 Installing go-eva on $ROBOT_USER@$ROBOT_IP"

# Build for ARM64
echo "📦 Building for ARM64..."
cd "$PROJECT_DIR"
GOOS=linux GOARCH=arm64 go build -o go-eva-arm64 ./cmd/go-eva

# Copy binary
echo "📤 Copying binary..."
sshpass -p "$ROBOT_PASS" scp go-eva-arm64 "$ROBOT_USER@$ROBOT_IP:/home/pollen/go-eva"

# Copy service file
echo "📤 Copying service file..."
sshpass -p "$ROBOT_PASS" scp scripts/go-eva.service "$ROBOT_USER@$ROBOT_IP:/home/pollen/"

# Install and start service
echo "🔧 Installing systemd service..."
sshpass -p "$ROBOT_PASS" ssh "$ROBOT_USER@$ROBOT_IP" << 'EOF'
echo 'root' | sudo -S bash -c '
    cp /home/pollen/go-eva.service /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable go-eva
    systemctl restart go-eva
    sleep 2
    systemctl status go-eva --no-pager
'
EOF

echo ""
echo "✅ go-eva installed successfully!"
echo ""
echo "Test with:"
echo "  curl http://$ROBOT_IP:9000/health"
echo "  curl http://$ROBOT_IP:9000/api/audio/doa"

