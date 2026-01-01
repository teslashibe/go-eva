.PHONY: build build-arm64 build-remote test deploy clean setup-pi

# Default robot IP (can override with: make deploy ROBOT_IP=192.168.68.XX)
ROBOT_IP ?= 192.168.68.77
ROBOT_USER ?= pollen
ROBOT_PASS ?= root
VERSION ?= 1.0.0

# Build for current platform (Mac - for testing with mock)
build:
	go build -o go-eva ./cmd/go-eva

# Cross-compile for Raspberry Pi 4 (ARM64) with CGO for libusb
# Requires: brew install FiloSottile/musl-cross/musl-cross
build-arm64:
	CGO_ENABLED=1 \
	CC=aarch64-linux-gnu-gcc \
	GOOS=linux \
	GOARCH=arm64 \
	go build -o go-eva-arm64 ./cmd/go-eva
	@echo "Built go-eva-arm64 for Raspberry Pi (with CGO/libusb)"

# Build directly on the Pi (most reliable for CGO)
build-remote:
	@echo "Building on $(ROBOT_IP)..."
	rsync -avz --exclude '.git' --exclude 'go-eva-arm64' --exclude 'go-eva' \
		. $(ROBOT_USER)@$(ROBOT_IP):/tmp/go-eva-build/
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "\
		cd /tmp/go-eva-build && \
		go build -o go-eva ./cmd/go-eva && \
		echo '$(ROBOT_PASS)' | sudo -S mv go-eva /usr/local/bin/go-eva && \
		sudo chmod +x /usr/local/bin/go-eva"
	@echo "✅ Built and installed go-eva on $(ROBOT_IP)"

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -cover ./internal/...

# Setup Pi with required dependencies (run once)
setup-pi:
	@echo "Setting up $(ROBOT_IP) for go-eva..."
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "\
		echo '$(ROBOT_PASS)' | sudo -S apt-get update && \
		sudo apt-get install -y libusb-1.0-0-dev golang && \
		echo 'SUBSYSTEM==\"usb\", ATTR{idVendor}==\"38fb\", MODE=\"0666\"' | \
			sudo tee /etc/udev/rules.d/99-xvf3800.rules && \
		sudo udevadm control --reload-rules && \
		sudo mkdir -p /etc/go-eva"
	sshpass -p "$(ROBOT_PASS)" scp configs/config.yaml $(ROBOT_USER)@$(ROBOT_IP):/tmp/config.yaml
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "\
		echo '$(ROBOT_PASS)' | sudo -S cp /tmp/config.yaml /etc/go-eva/config.yaml"
	@echo "✅ Pi setup complete"

# Deploy to robot (uses remote build)
deploy: build-remote
	@echo "Deploying service config..."
	sshpass -p "$(ROBOT_PASS)" scp scripts/go-eva.service $(ROBOT_USER)@$(ROBOT_IP):/tmp/go-eva.service
	sshpass -p "$(ROBOT_PASS)" scp configs/config.yaml $(ROBOT_USER)@$(ROBOT_IP):/tmp/config.yaml
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "\
		echo '$(ROBOT_PASS)' | sudo -S cp /tmp/go-eva.service /etc/systemd/system/ && \
		sudo cp /tmp/config.yaml /etc/go-eva/config.yaml && \
		sudo systemctl daemon-reload && \
		sudo systemctl restart go-eva"
	@echo "✅ Deployed and restarted go-eva"

# Install service (first time only)
install: setup-pi build-remote
	@echo "Installing go-eva service..."
	sshpass -p "$(ROBOT_PASS)" scp scripts/go-eva.service $(ROBOT_USER)@$(ROBOT_IP):/tmp/go-eva.service
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "\
		echo '$(ROBOT_PASS)' | sudo -S cp /tmp/go-eva.service /etc/systemd/system/ && \
		sudo systemctl daemon-reload && \
		sudo systemctl enable go-eva && \
		sudo systemctl start go-eva"
	@echo "✅ Installed and started go-eva service"

# Check service status
status:
	@sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "sudo systemctl status go-eva --no-pager"

# View logs
logs:
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "sudo journalctl -u go-eva -f"

# View recent logs
logs-recent:
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "sudo journalctl -u go-eva -n 50 --no-pager"

# Test API endpoints
test-api:
	@echo "=== Health ===" && curl -s http://$(ROBOT_IP):9000/health | python3 -m json.tool
	@echo "\n=== DOA ===" && curl -s http://$(ROBOT_IP):9000/api/audio/doa | python3 -m json.tool
	@echo "\n=== Stats ===" && curl -s http://$(ROBOT_IP):9000/api/stats | python3 -m json.tool

# Clean build artifacts
clean:
	rm -f go-eva go-eva-arm64
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "rm -rf /tmp/go-eva-build" 2>/dev/null || true

# Show help
help:
	@echo "go-eva Makefile"
	@echo ""
	@echo "Usage: make [target] [ROBOT_IP=xxx]"
	@echo ""
	@echo "Targets:"
	@echo "  build        Build for local platform (mock mode)"
	@echo "  build-remote Build on the Pi (recommended for production)"
	@echo "  test         Run tests"
	@echo "  setup-pi     Install dependencies on Pi (run once)"
	@echo "  install      Full install: setup + build + service"
	@echo "  deploy       Build and restart service"
	@echo "  status       Check service status"
	@echo "  logs         Follow service logs"
	@echo "  test-api     Test HTTP endpoints"
	@echo "  clean        Remove build artifacts"
	@echo ""
	@echo "Environment:"
	@echo "  ROBOT_IP     Robot IP address (default: 192.168.68.77)"
	@echo "  ROBOT_USER   SSH user (default: pollen)"
	@echo "  ROBOT_PASS   SSH password (default: root)"
