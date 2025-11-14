#!/bin/bash

export PROJECT_ROOT=/vagrant

# Build faasd
cd $PROJECT_ROOT/faasd
make local

# Stop the existing faasd services
echo "Stopping faasd services..."
sudo systemctl stop faasd
sudo systemctl stop faasd-provider

# Wait for services to fully stop
echo "Waiting for services to stop..."
sleep 3

# Force kill any remaining faasd processes
echo "Ensuring all faasd processes are stopped..."
sudo pkill -9 faasd || true
sleep 1

# Flush old logs
echo "Flushing old logs..."
sudo journalctl --rotate --vacuum-time=1s -u faasd
sudo journalctl --rotate --vacuum-time=1s -u faasd-provider

# Remove the old binary and copy the new one
echo "Installing new faasd binary..."
sudo rm -f /usr/local/bin/faasd
sudo cp bin/faasd /usr/local/bin/faasd
sudo chmod +x /usr/local/bin/faasd

# Verify the binary was copied
echo "Verifying binary..."
ls -lh /usr/local/bin/faasd
/usr/local/bin/faasd version

# Restart the services
echo "Restarting faasd services..."
sudo systemctl start faasd-provider
sudo systemctl start faasd

# Wait for services to start
echo "Waiting for services to start..."
sleep 5

# Check the logs
echo "=== faasd-provider logs ==="
sudo journalctl -u faasd-provider -n 10 --no-pager

echo "=== faasd logs ==="
sudo journalctl -u faasd -n 10 --no-pager