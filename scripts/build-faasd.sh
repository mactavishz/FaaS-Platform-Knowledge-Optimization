#!/bin/bash

export PROJECT_ROOT=/vagrant

# Build faasd
cd $PROJECT_ROOT/faasd
make local

# Install faasd binary first (required for faasd install command)
echo "Installing new faasd binary..."
sudo rm -f /usr/local/bin/faasd
sudo cp bin/faasd /usr/local/bin/faasd
sudo chmod +x /usr/local/bin/faasd

# Verify the binary was installed
echo "Verifying binary..."
ls -lh /usr/local/bin/faasd
/usr/local/bin/faasd version

# Check if services are already installed
if sudo systemctl list-unit-files | grep -q "faasd.service"; then
    # Services exist - stop them before updating
    echo "Stopping faasd services..."
    sudo systemctl stop faasd || true
    sudo systemctl stop faasd-provider || true

    # Wait for services to fully stop
    echo "Waiting for services to stop..."
    sleep 3

    # Force kill any remaining faasd processes
    echo "Ensuring all faasd processes are stopped..."
    sudo pkill -9 faasd || true
    sleep 1

    # Flush old logs
    echo "Flushing old logs..."
    sudo journalctl --rotate --vacuum-time=1s -u faasd || true
    sudo journalctl --rotate --vacuum-time=1s -u faasd-provider || true
    
    # Restart the services
    echo "Restarting faasd services..."
    sudo systemctl start faasd-provider
    sudo systemctl start faasd
else
    # First-time setup - run faasd install
    echo "First-time setup: Installing faasd services..."
    sudo /usr/local/bin/faasd install
fi

echo "Configuring faasd local registry pull settings..."
sudo REGISTRY_HOSTNAME="${REGISTRY_HOSTNAME:-registry.local}" \
    REGISTRY_PORT="${REGISTRY_PORT:-5050}" \
    REGISTRY_HOST_GATEWAY_IP="${REGISTRY_HOST_GATEWAY_IP:-}" \
    FAASD_PLAIN_HTTP_REGISTRIES="${FAASD_PLAIN_HTTP_REGISTRIES:-}" \
    bash "$PROJECT_ROOT/scripts/configure-faasd-registry.sh"

# Wait for services to start
echo "Waiting for services to start..."
sleep 5

# Check the logs
echo "=== faasd-provider logs ==="
sudo journalctl -u faasd-provider -n 10 --no-pager

echo "=== faasd logs ==="
sudo journalctl -u faasd -n 10 --no-pager