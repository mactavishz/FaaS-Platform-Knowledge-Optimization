#!/bin/bash

set -e

export PROJECT_ROOT=/vagrant

echo "==> Stopping existing tinyFaaS services..."
sudo systemctl stop tf-manager 2>/dev/null || echo "tf-manager not running"
sudo systemctl stop tf-rproxy 2>/dev/null || echo "tf-rproxy not running"
sudo systemctl stop tf-gateway 2>/dev/null || echo "tf-gateway not running"

# Navigate to tinyFaaS directory
cd $PROJECT_ROOT/tinyFaaS

echo "==> Building and installing tinyFaaS..."
sudo make install

echo "==> Configuring autoscaler environment..."
# Set default values
TF_AUTOSCALER_ENABLED=${TF_AUTOSCALER_ENABLED:-true}
TF_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION=${TF_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION:-5m}
TF_GATEWAY_PORT=${TF_GATEWAY_PORT:-80}

# Load local config if exists (git-ignored)
if [ -f "$PROJECT_ROOT/.tinyfaas.env" ]; then
    echo "==> Loading local configuration from .tinyfaas.env"
    source "$PROJECT_ROOT/.tinyfaas.env"
fi

echo "==> Autoscaler settings: ENABLED=$TF_AUTOSCALER_ENABLED, IDLE_DURATION=$TF_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION"
echo "==> Gateway port: $TF_GATEWAY_PORT"

# Create environment files for autoscaler configuration
sudo tee /etc/default/tinyfaas > /dev/null <<EOF
# Autoscaler configuration
TF_AUTOSCALER_ENABLED=$TF_AUTOSCALER_ENABLED
TF_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION=$TF_DEFAULT_SCALE_TO_ZERO_IDLE_DURATION

# Gateway configuration
TF_GATEWAY_PORT=$TF_GATEWAY_PORT
EOF

echo "==> Reloading systemd daemon..."
sudo systemctl daemon-reload

echo "==> Starting tinyFaaS services..."
sudo systemctl start tf-gateway
sudo systemctl start tf-rproxy
sudo systemctl start tf-manager

# Wait a moment and check if services are running
sleep 3
if systemctl is-active --quiet tf-gateway && systemctl is-active --quiet tf-rproxy && systemctl is-active --quiet tf-manager; then
    echo "==> tinyFaaS is running successfully!"
    echo "==> Service status:"
    sudo systemctl status tf-gateway --no-pager -l | head -10
    sudo systemctl status tf-rproxy --no-pager -l | head -10
    sudo systemctl status tf-manager --no-pager -l | head -10
    echo "==> View logs with:"
    echo "    journalctl -u tf-gateway -f"
    echo "    journalctl -u tf-manager -f"
    echo "    journalctl -u tf-rproxy -f"
else
    echo "==> ERROR: tinyFaaS failed to start."
    if ! systemctl is-active --quiet tf-gateway; then
        echo "==> tf-gateway failed:"
        sudo systemctl status tf-gateway --no-pager -l
    fi
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
