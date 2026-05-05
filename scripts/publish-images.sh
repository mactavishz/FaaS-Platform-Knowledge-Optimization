#!/bin/bash

set -euo pipefail

WORKFLOWS=(linear3 iot tree webshop)

# check if GITHUB_TOKEN is set
if [ -z "${GITHUB_TOKEN:-}" ]; then
    echo "Error: GITHUB_TOKEN environment variable is not set. Please set it to a personal access token with permissions to publish to GitHub Container Registry."
    exit 1
fi

# publish images to github container registry
for workflow in "${WORKFLOWS[@]}"; do
    REGISTRY_PREFIX="ghcr.io/mactavishz/faasd-" faas-cli publish --platforms linux/amd64,linux/arm64 -f ./tests/workflows/faasd/${workflow}/stack.yaml
done 

# check if docker is logged in to docker hub
if ! docker login; then
    echo "Error: Not logged in to Docker Hub."
    exit 1
fi

# publish images to docker hub
for workflow in "${WORKFLOWS[@]}"; do
    REGISTRY_PREFIX="macsalvation/faasd-" faas-cli publish --platforms linux/amd64,linux/arm64 -f ./tests/workflows/faasd/${workflow}/stack.yaml
done 