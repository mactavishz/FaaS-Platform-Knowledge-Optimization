#!/bin/bash

set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
    echo "This script must run as root (use sudo)."
    exit 1
fi

REGISTRY_HOSTNAME="${REGISTRY_HOSTNAME:-registry.local}"
REGISTRY_PORT="${REGISTRY_PORT:-5050}"
REGISTRY_HOST_GATEWAY_IP="${REGISTRY_HOST_GATEWAY_IP:-}"

if [[ -z "${REGISTRY_HOST_GATEWAY_IP}" ]]; then
    REGISTRY_HOST_GATEWAY_IP="$(ip route | awk '/^default / { print $3; exit }')"
fi

if [[ -z "${REGISTRY_HOST_GATEWAY_IP}" ]]; then
    echo "Failed to detect host gateway IP. Set REGISTRY_HOST_GATEWAY_IP explicitly."
    exit 1
fi

REGISTRY_ADDRESS="${REGISTRY_HOSTNAME}:${REGISTRY_PORT}"
if [[ -n "${FAASD_PLAIN_HTTP_REGISTRIES:-}" ]]; then
    PLAIN_HTTP_REGISTRIES="${FAASD_PLAIN_HTTP_REGISTRIES}"
else
    PLAIN_HTTP_REGISTRIES="${REGISTRY_HOSTNAME},${REGISTRY_ADDRESS}"
fi

echo "Configuring local registry alias ${REGISTRY_HOSTNAME} -> ${REGISTRY_HOST_GATEWAY_IP}"

TMP_HOSTS="$(mktemp)"
grep -Ev "[[:space:]]${REGISTRY_HOSTNAME}([[:space:]]|$)" /etc/hosts > "${TMP_HOSTS}" || true
printf "%s %s\n" "${REGISTRY_HOST_GATEWAY_IP}" "${REGISTRY_HOSTNAME}" >> "${TMP_HOSTS}"
cat "${TMP_HOSTS}" > /etc/hosts
rm -f "${TMP_HOSTS}"

DROPIN_DIR="/etc/systemd/system/faasd-provider.service.d"
DROPIN_FILE="${DROPIN_DIR}/10-local-registry.conf"

mkdir -p "${DROPIN_DIR}"
cat > "${DROPIN_FILE}" <<EOF
[Service]
Environment="FAASD_PLAIN_HTTP_REGISTRIES=${PLAIN_HTTP_REGISTRIES}"
EOF

echo "Wrote ${DROPIN_FILE}"

echo "Reloading systemd and restarting faasd services"
systemctl daemon-reload

if systemctl list-unit-files | grep -q '^faasd-provider.service'; then
    systemctl restart faasd-provider
fi

if systemctl list-unit-files | grep -q '^faasd.service'; then
    systemctl restart faasd
fi

echo "Local registry pull configuration is active for: ${PLAIN_HTTP_REGISTRIES}"
