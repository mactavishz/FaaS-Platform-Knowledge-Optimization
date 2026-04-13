#!/bin/bash

set -euo pipefail

export PROJECT_ROOT=/vagrant

wait_for_gateway() {
    local gateway_url="http://127.0.0.1:8080/system/functions"
    local max_attempts=24
    local attempt=1

    echo "Waiting for faasd gateway to accept connections..."
    while [[ "$attempt" -le "$max_attempts" ]]; do
        local status_code
        status_code="$(curl -s -o /dev/null -w '%{http_code}' "$gateway_url" || true)"

        if [[ "$status_code" == "200" || "$status_code" == "401" || "$status_code" == "403" ]]; then
            echo "faasd gateway is reachable (HTTP $status_code)."
            return 0
        fi

        echo "Gateway not ready yet (attempt $attempt/$max_attempts, status: ${status_code:-none})"
        sleep 2
        attempt=$((attempt + 1))
    done

    echo "Timed out waiting for faasd gateway at $gateway_url"
    return 1
}

echo "==> Shutting down the old services..."
make -C $PROJECT_ROOT/faasd down

# Build and install gateway binary first
echo "==> Building and installing faasd-gateway..."
make -C $PROJECT_ROOT/faas-gateway install

# Build and install faasd
echo "==> Building and installing faasd..."
make -C $PROJECT_ROOT/faasd install

# wait for the services to start
if sudo systemctl is-active --quiet faasd && sudo systemctl is-active --quiet faasd-provider && sudo systemctl is-active --quiet faasd-gateway; then
    echo "==> faasd is running successfully!"
    echo "==> Service status:"
    sudo systemctl status faasd --no-pager -l | head -10
    sudo systemctl status faasd-provider --no-pager -l | head -10
    sudo systemctl status faasd-gateway --no-pager -l | head -10
    echo "==> View logs with:"
    echo "    journalctl -u faasd -f"
    echo "    journalctl -u faasd-provider -f"
    echo "    journalctl -u faasd-gateway -f"
else
    echo "==> ERROR: faasd failed to start."
    if ! systemctl is-active --quiet faasd; then
        echo "==> faasd failed:"
        sudo systemctl status faasd --no-pager -l
    fi
    if ! systemctl is-active --quiet faasd-provider; then
        echo "==> faasd-provider failed:"
        sudo systemctl status faasd-provider --no-pager -l
    fi
    if ! systemctl is-active --quiet faasd-gateway; then
        echo "==> faasd-gateway failed:"
        sudo systemctl status faasd-gateway --no-pager -l
    fi
    exit 1
fi

# Wait for services to start
wait_for_gateway
