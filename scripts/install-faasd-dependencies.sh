#!/bin/bash
# This script should be run as root

# environment variables
export CONTAINERD_VERSION=2.2.0
export CNI_PLUGIN_VERSION=1.8.0

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

# Install CNI plugins
echo "Installing CNI plugins ..."
rm -rf /opt/cni
mkdir -p /opt/cni/bin
curl -sSL https://github.com/containernetworking/plugins/releases/download/v${CNI_PLUGIN_VERSION}/cni-plugins-linux-${ARCH}-v${CNI_PLUGIN_VERSION}.tgz | tar -xz -C /opt/cni/bin

# Add CNI to PATH system-wide
sh -c 'cat > /etc/profile.d/cni.sh <<EOF
export PATH=\$PATH:/opt/cni/bin
EOF'

# Also add to vagrant user's bashrc for interactive shells
echo 'export PATH=$PATH:/opt/cni/bin' >> /home/vagrant/.bashrc

# Make a config folder for CNI definitions
mkdir -p /etc/cni/net.d

# Make an initial loopback configuration
sh -c 'cat >/etc/cni/net.d/99-loopback.conf <<-EOF
{
    "cniVersion": "1.1.0",
    "type": "loopback"
}
EOF'

# Install containerd
echo "Installing containerd ..."
curl -sSL https://github.com/containerd/containerd/releases/download/v$CONTAINERD_VERSION/containerd-$CONTAINERD_VERSION-linux-$ARCH.tar.gz -o /tmp/containerd.tar.gz \
  && tar -xvf /tmp/containerd.tar.gz -C /usr/local/bin/ --strip-components=1

containerd -version

# Create containerd systemd service
echo "Configuring containerd service ..."
curl -sLS https://raw.githubusercontent.com/containerd/containerd/v$CONTAINERD_VERSION/containerd.service > /tmp/containerd.service

# Extend the timeouts for low-performance VMs
echo "[Manager]" | tee -a /tmp/containerd.service
echo "DefaultTimeoutStartSec=3m" | tee -a /tmp/containerd.service

cp /tmp/containerd.service /lib/systemd/system/
systemctl enable containerd

systemctl daemon-reload
systemctl restart containerd

# enable ipv4 forwarding 
/sbin/sysctl -w net.ipv4.conf.all.forwarding=1
echo "net.ipv4.conf.all.forwarding=1" | tee -a /etc/sysctl.conf

echo "Dependencies installed and configured."