#!/bin/bash

set -e

export PROJECT_ROOT="${PROJECT_ROOT:-/vagrant}"

# Navigate to tinyFaaS directory
cd $PROJECT_ROOT/tinyFaaS

echo "==> Shutting down the old services..."
make down

echo "==> Building and installing tinyFaaS..."
make install

echo "==> Configuring tinyFaaS environment..."

# Load configuration file if present.
# By default this uses .env, but tests can override via ENV_FILE.
ENV_FILE=${ENV_FILE:-"$PROJECT_ROOT/.env"}
if [ -f "$ENV_FILE" ]; then
	echo "==> Loading configuration from $ENV_FILE"
	source "$ENV_FILE"
else
	echo "==> No env file found at $ENV_FILE, using defaults/env vars"
fi

# Set default values after loading env file so partially specified profiles still work.
: "${AUTOSCALER_ENABLED:=true}"
: "${CALLGRAPH_ENABLED:=true}"
: "${CALLGRAPH_METHOD:=SMA}"
: "${CALLGRAPH_SMA_WINDOW_SIZE:=10}"
: "${CALLGRAPH_EMA_ALPHA:=0.3}"
: "${DEFAULT_SCALE_TO_ZERO_IDLE_DURATION:=5m}"
: "${GATEWAY_IP:=0.0.0.0}"
: "${GATEWAY_PORT:=80}"
: "${RPROXY_PORT:=8000}"
: "${MANAGER_PORT:=8080}"
: "${ENV:=development}"

echo "==> Autoscaler settings: ENABLED=$AUTOSCALER_ENABLED, IDLE_DURATION=$DEFAULT_SCALE_TO_ZERO_IDLE_DURATION"
echo "==> Callgraph settings: ENABLED=$CALLGRAPH_ENABLED, METHOD=$CALLGRAPH_METHOD, SMA_WINDOW_SIZE=$CALLGRAPH_SMA_WINDOW_SIZE, EMA_ALPHA=$CALLGRAPH_EMA_ALPHA"
echo "==> Gateway IP: $GATEWAY_IP"
echo "==> Gateway port: $GATEWAY_PORT"
echo "==> Manager port: $MANAGER_PORT"
echo "==> RProxy port: $RPROXY_PORT"
echo "==> Environment: $ENV"

# Create environment files for autoscaler configuration
sudo tee /etc/default/tinyfaas > /dev/null <<EOF
# Autoscaler configuration
AUTOSCALER_ENABLED=$AUTOSCALER_ENABLED
DEFAULT_SCALE_TO_ZERO_IDLE_DURATION=$DEFAULT_SCALE_TO_ZERO_IDLE_DURATION

# Callgraph configuration
CALLGRAPH_ENABLED=$CALLGRAPH_ENABLED
CALLGRAPH_METHOD=$CALLGRAPH_METHOD
CALLGRAPH_SMA_WINDOW_SIZE=$CALLGRAPH_SMA_WINDOW_SIZE
CALLGRAPH_EMA_ALPHA=$CALLGRAPH_EMA_ALPHA

# Gateway configuration
GATEWAY_IP=$GATEWAY_IP
GATEWAY_PORT=$GATEWAY_PORT

# RProxy configuration
RPROXY_PORT=$RPROXY_PORT

# Manager configuration
MANAGER_PORT=$MANAGER_PORT

# Environment configuration
ENV=$ENV
EOF

echo "==> Reloading systemd daemon..."
sudo systemctl daemon-reload

echo "==> Starting tinyFaaS services..."
sudo systemctl enable --now tf-gateway
sudo systemctl enable --now tf-rproxy
sudo systemctl enable --now tf-manager

# Wait a moment and check if services are running
sleep 5
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
