#!/bin/bash

set -e

export PROJECT_ROOT=/vagrant
CADDY_PID_FILE=/tmp/caddy.pid

echo "==> Stopping existing tinyFaaS services..."
sudo systemctl stop tf-manager 2>/dev/null || echo "tf-manager not running"
sudo systemctl stop tf-rproxy 2>/dev/null || echo "tf-rproxy not running"

echo "==> Stopping existing caddy server..."
sudo caddy stop --config $PROJECT_ROOT/tinyFaaS/Caddyfile || echo "Caddy server not running."

# Navigate to tinyFaaS directory
cd $PROJECT_ROOT/tinyFaaS

echo "==> Building and installing tinyFaaS..."
sudo make install

echo "==> Reloading systemd daemon..."
sudo systemctl daemon-reload

# Start caddy server
sudo caddy start --config $PROJECT_ROOT/tinyFaaS/Caddyfile --pidfile $CADDY_PID_FILE

echo "==> Starting tinyFaaS services..."
sudo systemctl start tf-rproxy
sudo systemctl start tf-manager

# Wait a moment and check if services are running
sleep 3
if systemctl is-active --quiet tf-rproxy && systemctl is-active --quiet tf-manager; then
    echo "==> tinyFaaS is running successfully!"
    echo "==> Service status:"
    sudo systemctl status tf-rproxy --no-pager -l | head -10
    sudo systemctl status tf-manager --no-pager -l | head -10
    echo "==> View logs with:"
    echo "    journalctl -u tf-manager -f"
    echo "    journalctl -u tf-rproxy -f"
else
    echo "==> ERROR: tinyFaaS failed to start."
    if ! systemctl is-active --quiet tf-rproxy; then
        echo "==> tf-rproxy failed:"
        sudo systemctl status tf-rproxy --no-pager -l
    fi
    if ! systemctl is-active --quiet tf-manager; then
        echo "==> tf-manager failed:"
        sudo systemctl status tf-manager --no-pager -l
    fi
    exit 1
fi
