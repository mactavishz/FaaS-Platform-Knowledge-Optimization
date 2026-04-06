#!/bin/bash

set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
    echo "This script must run as root (use sudo)."
    exit 1
fi

REGISTRY_HOSTNAME="${1:-registry.local}"
REGISTRY_HOST_IP="${2:-127.0.0.1}"

echo "Configuring host alias ${REGISTRY_HOSTNAME} -> ${REGISTRY_HOST_IP}"

TMP_HOSTS="$(mktemp)"
grep -Ev "[[:space:]]${REGISTRY_HOSTNAME}([[:space:]]|$)" /etc/hosts > "${TMP_HOSTS}" || true
printf "%s %s\n" "${REGISTRY_HOST_IP}" "${REGISTRY_HOSTNAME}" >> "${TMP_HOSTS}"
cat "${TMP_HOSTS}" > /etc/hosts
rm -f "${TMP_HOSTS}"

echo "Host alias configured."
