#!/bin/bash
# shellcheck disable=SC2154
# disable SC2154 because variables are passed in from Terraform template rendering and not defined in this script

set -euo pipefail

LOG_FILE="/var/log/vm-provision.log"
exec > >(tee -a "$LOG_FILE") 2>&1

export DEBIAN_FRONTEND=noninteractive
export GO_VERSION="${go_version}"
export NERDCTL_VERSION="${nerdctl_version}"
export GATEWAY_PORT="${gateway_port}"

PROJECT_ROOT="${deploy_dir}"
DEPLOY_USER="${ssh_user}"
DEPLOY_HOME="/home/$DEPLOY_USER"
DEPLOY_GOPATH="$DEPLOY_HOME/go"
DEPLOY_GOMODCACHE="$DEPLOY_GOPATH/pkg/mod"
DEPLOY_GOCACHE="$DEPLOY_HOME/.cache/go-build"
GITHUB_TOKEN="$(printf '%s' "${github_token_b64}" | base64 -d)"
REPO_URL="$(printf '%s' "${repo_url_b64}" | base64 -d)"
REPO_REF="$(printf '%s' "${repo_ref_b64}" | base64 -d)"
ENV_CONTENT_B64="${env_content_b64}"

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64) echo "amd64" ;;
    aarch64) echo "arm64" ;;
    *)
      echo "Unsupported architecture: $arch" >&2
      return 1
      ;;
  esac
}

run_as_deploy_user() {
  sudo -H -u "$DEPLOY_USER" env \
    HOME="$DEPLOY_HOME" \
    GOPATH="$DEPLOY_GOPATH" \
    GOMODCACHE="$DEPLOY_GOMODCACHE" \
    GOCACHE="$DEPLOY_GOCACHE" \
    PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH" \
    "$@"
}

run_as_deploy_user_shell() {
  run_as_deploy_user bash -lc "$1"
}

ensure_deploy_user_environment() {
  echo "==> Preparing $DEPLOY_USER build environment..."
  if ! id "$DEPLOY_USER" >/dev/null 2>&1; then
    echo "ERROR: deployment user '$DEPLOY_USER' does not exist. Check GCE SSH metadata user creation before startup scripts run." >&2
    exit 1
  fi

  install -d -m 0755 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$PROJECT_ROOT"
  install -d -m 0755 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$DEPLOY_HOME"
  install -d -m 0755 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$DEPLOY_GOPATH" "$DEPLOY_GOMODCACHE" "$DEPLOY_GOCACHE"
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$PROJECT_ROOT" "$DEPLOY_GOPATH" "$DEPLOY_HOME/.cache"
}

install_common_dependencies() {
  export ARCH
  ARCH="$(detect_arch)"

  echo "==> Installing base packages..."
  apt-get update
  apt-get install -y ca-certificates gnupg git make sudo runc bridge-utils debian-keyring debian-archive-keyring apt-transport-https curl zip

  echo "==> Installing Go $GO_VERSION..."
  rm -rf /usr/local/go
  curl -sSL "https://go.dev/dl/go$GO_VERSION.linux-$ARCH.tar.gz" -o /tmp/go.tar.gz
  tar -xzf /tmp/go.tar.gz -C /usr/local
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  go version

  echo "==> Installing Go helper tools..."
  rm -rf /tmp/faas-tools-bin
  install -d -m 0755 /tmp/faas-tools-bin
  chown "$DEPLOY_USER:$DEPLOY_USER" /tmp/faas-tools-bin
  run_as_deploy_user env GOBIN=/tmp/faas-tools-bin go install github.com/jesseduffield/lazydocker@latest
  install -m 0755 /tmp/faas-tools-bin/lazydocker /usr/local/bin/lazydocker
  curl -sSL "https://github.com/containerd/nerdctl/releases/download/v$NERDCTL_VERSION/nerdctl-$NERDCTL_VERSION-linux-$ARCH.tar.gz" -o /tmp/nerdctl.tar.gz
  tar -C /usr/local/bin -xzf /tmp/nerdctl.tar.gz
}

install_docker() {
  echo "==> Removing conflicting container runtimes if present..."
  # shellcheck disable=SC2046
  apt-get remove -y $(dpkg --get-selections docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc 2>/dev/null | cut -f1) 2>/dev/null || true

  echo "==> Installing Docker..."
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  # shellcheck source=/dev/null
  . /etc/os-release
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" > /etc/apt/sources.list.d/docker.list
  apt-get update
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

  if id "$DEPLOY_USER" >/dev/null 2>&1; then
    usermod -aG docker "$DEPLOY_USER"
  fi
}

clone_repo() {
  echo "==> Cloning benchmark repository..."
  rm -rf "$PROJECT_ROOT"
  install -d -m 0755 "$PROJECT_ROOT"
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$PROJECT_ROOT"

  local git_config="/tmp/faas-benchmark-gitconfig"
  install -m 0600 -o "$DEPLOY_USER" -g "$DEPLOY_USER" /dev/null "$git_config"
  run_as_deploy_user env GIT_CONFIG_GLOBAL="$git_config" git config --global url."https://x-access-token:$GITHUB_TOKEN@github.com/".insteadOf "https://github.com/"
  run_as_deploy_user env GIT_CONFIG_GLOBAL="$git_config" git clone --recurse-submodules "$REPO_URL" "$PROJECT_ROOT"
  run_as_deploy_user env GIT_CONFIG_GLOBAL="$git_config" git -C "$PROJECT_ROOT" fetch --tags origin
  run_as_deploy_user env GIT_CONFIG_GLOBAL="$git_config" git -C "$PROJECT_ROOT" checkout "$REPO_REF"
  run_as_deploy_user env GIT_CONFIG_GLOBAL="$git_config" git -C "$PROJECT_ROOT" submodule update --init --recursive
  rm -f "$git_config"
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$PROJECT_ROOT"
}

write_environment() {
  echo "==> Writing tinyFaaS environment..."
  install -d -m 0755 /etc/default
  : > /etc/default/tinyfaas

  if [ -n "$ENV_CONTENT_B64" ]; then
    printf '%s' "$ENV_CONTENT_B64" | base64 -d > /etc/default/tinyfaas
  fi

  chmod 0644 /etc/default/tinyfaas
}

wait_for_gateway() {
  local gateway_url="http://127.0.0.1:$GATEWAY_PORT/system/list"
  local max_attempts=120
  local attempt=1

  echo "==> Waiting for tinyFaaS gateway at $gateway_url..."
  while [ "$attempt" -le "$max_attempts" ]; do
    local status_code
    status_code="$(curl -s -o /dev/null -w '%%{http_code}' "$gateway_url" || true)"

    if [ "$status_code" = "200" ]; then
      echo "==> tinyFaaS gateway is reachable."
      return 0
    fi

    echo "Gateway not ready yet (attempt $attempt/$max_attempts, status: $status_code)"
    sleep 5
    attempt=$((attempt + 1))
  done

  echo "Timed out waiting for tinyFaaS gateway."
  return 1
}

show_failure_logs() {
  echo "==> tinyFaaS service status:"
  systemctl status tf-gateway --no-pager -l || true
  systemctl status tf-server --no-pager -l || true
  echo "==> Recent tinyFaaS logs:"
  journalctl -u tf-gateway -n 100 --no-pager || true
  journalctl -u tf-server -n 100 --no-pager || true
}

install_ops_agent() {
  echo "==> Installing Google Cloud Ops Agent..."
  curl -sSO https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh
  sudo bash add-google-cloud-ops-agent-repo.sh --also-install
}

main() {
  install_ops_agent
  echo "==> Starting tinyFaaS benchmark VM provisioning..."
  ensure_deploy_user_environment
  install_common_dependencies
  install_docker
  clone_repo
  write_environment

  echo "==> Verifying $DEPLOY_USER passwordless sudo..."
  run_as_deploy_user sudo -n true

  echo "==> Building and installing tinyFaaS..."
  run_as_deploy_user_shell "cd '$PROJECT_ROOT/tinyFaaS' && sudo env HOME='$DEPLOY_HOME' GOPATH='$DEPLOY_GOPATH' GOMODCACHE='$DEPLOY_GOMODCACHE' GOCACHE='$DEPLOY_GOCACHE' PATH='/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH' make install"
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$PROJECT_ROOT" "$DEPLOY_GOPATH" "$DEPLOY_HOME/.cache"

  echo "==> Starting tinyFaaS services..."
  systemctl daemon-reload
  systemctl enable --now tf-server
  systemctl enable --now tf-gateway

  if ! systemctl is-active --quiet tf-gateway || ! systemctl is-active --quiet tf-server; then
    show_failure_logs
    exit 1
  fi

  wait_for_gateway || {
    show_failure_logs
    exit 1
  }

  echo "==> tinyFaaS provisioning completed successfully."
}

main "$@"
