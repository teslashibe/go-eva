.PHONY: build build-arm64 test deploy clean

# Default robot IP (can override with: make deploy ROBOT_IP=192.168.68.XX)
ROBOT_IP ?= 192.168.68.77
ROBOT_USER ?= pollen
ROBOT_PASS ?= root

# Build for current platform
build:
	go build -o go-eva ./cmd/go-eva

# Cross-compile for Raspberry Pi 4 (ARM64)
build-arm64:
	GOOS=linux GOARCH=arm64 go build -o go-eva-arm64 ./cmd/go-eva
	@echo "Built go-eva-arm64 for Raspberry Pi"

# Run tests
test:
	go test -v ./...

# Deploy to robot
deploy: build-arm64
	@echo "Deploying to $(ROBOT_USER)@$(ROBOT_IP)..."
	sshpass -p "$(ROBOT_PASS)" scp go-eva-arm64 $(ROBOT_USER)@$(ROBOT_IP):/home/pollen/go-eva
	sshpass -p "$(ROBOT_PASS)" scp scripts/go-eva.service $(ROBOT_USER)@$(ROBOT_IP):/home/pollen/
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "echo '$(ROBOT_PASS)' | sudo -S cp /home/pollen/go-eva.service /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl restart go-eva"
	@echo "✅ Deployed and restarted go-eva on robot"

# Install service (first time only)
install: build-arm64
	@echo "Installing go-eva service on $(ROBOT_USER)@$(ROBOT_IP)..."
	sshpass -p "$(ROBOT_PASS)" scp go-eva-arm64 $(ROBOT_USER)@$(ROBOT_IP):/home/pollen/go-eva
	sshpass -p "$(ROBOT_PASS)" scp scripts/go-eva.service $(ROBOT_USER)@$(ROBOT_IP):/home/pollen/
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "echo '$(ROBOT_PASS)' | sudo -S cp /home/pollen/go-eva.service /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl enable go-eva && sudo systemctl start go-eva"
	@echo "✅ Installed and started go-eva service"

# Check service status
status:
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "sudo systemctl status go-eva"

# View logs
logs:
	sshpass -p "$(ROBOT_PASS)" ssh $(ROBOT_USER)@$(ROBOT_IP) "sudo journalctl -u go-eva -f"

# Clean build artifacts
clean:
	rm -f go-eva go-eva-arm64

