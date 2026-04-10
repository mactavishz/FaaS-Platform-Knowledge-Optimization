#!/bin/bash
# This script should be run as root

# environment variables
export GO_VERSION=1.25.4
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
  ARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
  ARCH="arm64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi
export ARCH

# Install
apt-get update
apt-get install -y ca-certificates gnupg git make runc bridge-utils debian-keyring debian-archive-keyring apt-transport-https curl zip

# Install golang
echo "Installing golang ..."
rm -rf /usr/local/go
curl -sSL https://go.dev/dl/go$GO_VERSION.linux-$ARCH.tar.gz -o /tmp/go.tar.gz \
  && tar -xzf /tmp/go.tar.gz -C /usr/local

# Expose Go binaries from the extracted tree without breaking GOROOT discovery.
ln -sf /usr/local/go/bin/go /usr/local/bin/go
ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

# Test golang installation
go version

# Install go tools
echo "Installing golang tools ..."
GOBIN=/usr/local/bin go install github.com/jesseduffield/lazydocker@latest
NERDCTL_VERSION=2.2.2
curl -sSL https://github.com/containerd/nerdctl/releases/download/v$NERDCTL_VERSION/nerdctl-$NERDCTL_VERSION-linux-$ARCH.tar.gz \
  -o /tmp/nerdctl.tar.gz \
  && tar -C /usr/local/bin -xzf /tmp/nerdctl.tar.gz