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
	echo "==> Writing configuration from $ENV_FILE to /etc/default/tinyfaas"
    sudo cat $ENV_FILE | sudo tee /etc/default/tinyfaas
else
	echo "==> No env file found at $ENV_FILE"
fi

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
