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
# Install caddy
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg
chmod o+r /etc/apt/sources.list.d/caddy-stable.list
apt-get update
apt-get install -y caddy


# Install golang
echo "Installing golang ..."
rm -rf /usr/local/go
curl -sSL https://go.dev/dl/go$GO_VERSION.linux-$ARCH.tar.gz -o /tmp/go.tar.gz \
  && tar -xzf /tmp/go.tar.gz -C /usr/local

# Add Go to PATH system-wide using /etc/profile.d/
sh -c 'cat > /etc/profile.d/go.sh <<EOF
export PATH=\$PATH:/usr/local/go/bin
EOF'

# Also add to vagrant user's bashrc for interactive shells
echo 'export PATH=$PATH:/usr/local/go/bin' >> /home/vagrant/.bashrc