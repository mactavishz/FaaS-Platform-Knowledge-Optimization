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

validate_registry_pull_config() {
    local registry_hostname="${REGISTRY_HOSTNAME:-registry.local}"
    local provider_env
    local dropin_file="/etc/systemd/system/faasd-provider.service.d/10-local-registry.conf"
    local provider_started_at
    local dropin_updated_at

    provider_env="$(sudo systemctl show faasd-provider --property=Environment --value || true)"
    if [[ "${provider_env}" != *"FAASD_PLAIN_HTTP_REGISTRIES="* ]]; then
        echo "ERROR: faasd-provider is missing FAASD_PLAIN_HTTP_REGISTRIES."
        echo "faasd will attempt HTTPS pulls and fail against the local HTTP registry."
        echo "Current Environment from systemd: ${provider_env}"
        return 1
    fi

    if ! grep -Eq "[[:space:]]${registry_hostname}([[:space:]]|$)" /etc/hosts; then
        echo "ERROR: /etc/hosts in faasd VM is missing ${registry_hostname}."
        echo "faasd cannot resolve local registry alias without this entry."
        return 1
    fi

    if [[ ! -f "${dropin_file}" ]]; then
        echo "ERROR: expected drop-in file not found: ${dropin_file}"
        return 1
    fi

    provider_started_at="$(sudo systemctl show faasd-provider --property=ActiveEnterTimestamp --value || true)"
    dropin_updated_at="$(stat -c '%y' "${dropin_file}" || true)"

    if [[ -z "${provider_started_at}" || -z "${dropin_updated_at}" ]]; then
        echo "ERROR: unable to read faasd-provider start time or registry drop-in timestamp."
        return 1
    fi

    if ! sudo sh -c "tr '\0' '\n' </proc/\$(systemctl show faasd-provider --property=MainPID --value)/environ | grep -q '^FAASD_PLAIN_HTTP_REGISTRIES='"; then
        echo "ERROR: running faasd-provider process does not include FAASD_PLAIN_HTTP_REGISTRIES in its environment."
        echo "Restart faasd-provider so new drop-in variables are applied."
        return 1
    fi

    echo "faasd-provider started at: ${provider_started_at}"
    echo "registry drop-in updated at: ${dropin_updated_at}"
    echo "Verified faasd registry pull configuration for ${registry_hostname}."
}

cd $PROJECT_ROOT/faasd

echo "==> Shutting down the old services..."
make down

echo "==> Building and installing faasd..."
make install

# wait for the services to start
if sudo systemctl is-active --quiet faasd && systemctl is-active --quiet faasd-provider; then
    echo "==> faasd is running successfully!"
    echo "==> Service status:"
    sudo systemctl status faasd --no-pager -l | head -10
    sudo systemctl status faasd-provider --no-pager -l | head -10
    echo "==> View logs with:"
    echo "    journalctl -u faasd -f"
    echo "    journalctl -u faasd-provider -f"
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
    exit 1
fi

echo "Configuring faasd local registry pull settings..."
sudo REGISTRY_HOSTNAME="${REGISTRY_HOSTNAME:-registry.local}" \
    REGISTRY_PORT="${REGISTRY_PORT:-5050}" \
    REGISTRY_HOST_GATEWAY_IP="${REGISTRY_HOST_GATEWAY_IP:-}" \
    RESTART_FAASD_SERVICES=0 \
    FAASD_PLAIN_HTTP_REGISTRIES="${FAASD_PLAIN_HTTP_REGISTRIES:-}" \
    bash "$PROJECT_ROOT/scripts/configure-faasd-registry.sh"

echo "Reloading systemd and restarting faasd services with final config..."
sudo systemctl daemon-reload
sudo systemctl restart faasd-provider
sudo systemctl restart faasd

echo "Validating faasd local registry pull settings..."
validate_registry_pull_config

# Wait for services to start
wait_for_gateway
