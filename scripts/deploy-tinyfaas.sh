#!/bin/bash
# Deploy tinyFaaS to a remote Ubuntu server via SSH.
# Run this script from the repo root on your local machine.

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_HTTPS="https://github.com/mactavishz/FaaS-Platform-Knowledge-Optimization.git"

DEFAULT_BRANCH="main"
DEFAULT_DEPLOY_DIR="/opt/faas-platform"

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
    cat <<EOF
Usage: $(basename "$0") --host <user@host> --env-file <path> [OPTIONS]

Deploy tinyFaaS to a remote Ubuntu server.

Required:
  --host <user@host>        SSH target (e.g., ubuntu@192.168.1.100)
  --env-file <path>         Local path to .tinyfaas.env file to upload

Optional:
  --branch <name>           Git branch to deploy (default: $DEFAULT_BRANCH)
  --github-token <token>    GitHub PAT for HTTPS clone
                            (fallback: GITHUB_TOKEN environment variable)
  --ssh-key <path>          Path to SSH private key (uses ssh-agent/default if omitted)
  --deploy-dir <path>       Remote deployment directory (default: $DEFAULT_DEPLOY_DIR)
  --skip-deps               Skip Go and Docker installation on the target
  --help                    Show this help message

Examples:
  # Fresh deploy with all defaults
  $(basename "$0") --host ubuntu@192.168.1.100 --env-file .tinyfaas.env

  # Deploy a specific branch with explicit token and SSH key
  $(basename "$0") --host ubuntu@myserver.com \\
    --env-file .tinyfaas.env \\
    --branch feature/my-changes \\
    --github-token ghp_xxxx \\
    --ssh-key ~/.ssh/id_ed25519

  # Re-deploy (update) skipping dependency installation
  $(basename "$0") --host ubuntu@myserver.com --env-file .tinyfaas.env --skip-deps
EOF
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
HOST=""
ENV_FILE=""
BRANCH="$DEFAULT_BRANCH"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
SSH_KEY=""
DEPLOY_DIR="$DEFAULT_DEPLOY_DIR"
SKIP_DEPS=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)          HOST="$2"; shift 2 ;;
        --env-file)      ENV_FILE="$2"; shift 2 ;;
        --branch)        BRANCH="$2"; shift 2 ;;
        --github-token)  GITHUB_TOKEN="$2"; shift 2 ;;
        --ssh-key)       SSH_KEY="$2"; shift 2 ;;
        --deploy-dir)    DEPLOY_DIR="$2"; shift 2 ;;
        --skip-deps)     SKIP_DEPS=true; shift ;;
        --help)          usage; exit 0 ;;
        *)               echo "Unknown option: $1"; echo; usage; exit 1 ;;
    esac
done

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------
errors=0

if [[ -z "$HOST" ]]; then
    echo "ERROR: --host is required"
    errors=$((errors + 1))
fi

if [[ -z "$ENV_FILE" ]]; then
    echo "ERROR: --env-file is required"
    errors=$((errors + 1))
elif [[ ! -f "$ENV_FILE" ]]; then
    echo "ERROR: env file not found: $ENV_FILE"
    errors=$((errors + 1))
fi

if [[ -z "$GITHUB_TOKEN" ]]; then
    echo "ERROR: GitHub token is required. Use --github-token or set GITHUB_TOKEN env var."
    errors=$((errors + 1))
fi

if [[ -n "$SSH_KEY" && ! -f "$SSH_KEY" ]]; then
    echo "ERROR: SSH key not found: $SSH_KEY"
    errors=$((errors + 1))
fi

if [[ $errors -gt 0 ]]; then
    echo
    usage
    exit 1
fi

# ---------------------------------------------------------------------------
# SSH/SCP helpers
# ---------------------------------------------------------------------------
SSH_OPTS="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 -o BatchMode=yes"
if [[ -n "$SSH_KEY" ]]; then
    SSH_OPTS="$SSH_OPTS -i $SSH_KEY"
fi

ssh_cmd() {
    # shellcheck disable=SC2086
    ssh $SSH_OPTS "$HOST" "$@"
}

scp_cmd() {
    # shellcheck disable=SC2086
    scp $SSH_OPTS "$@"
}

# ---------------------------------------------------------------------------
# Cleanup trap
# ---------------------------------------------------------------------------
TMP_DIR=""
cleanup() {
    [[ -n "$TMP_DIR" ]] && rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log_phase() {
    echo ""
    echo "==> $*"
    echo "    $(printf '=%.0s' {1..60})"
}

log_step() {
    echo "    --> $*"
}

# ---------------------------------------------------------------------------
# Phase 0: Connectivity check
# ---------------------------------------------------------------------------
log_phase "Phase 0: Validating SSH connectivity to $HOST"

if ssh_cmd "echo ok" > /dev/null 2>&1; then
    log_step "SSH connection successful"
else
    echo "ERROR: Cannot connect to $HOST via SSH."
    echo "  Make sure SSH keys are configured and the host is reachable."
    exit 1
fi

REMOTE_USER=$(ssh_cmd "whoami")
log_step "Remote user: $REMOTE_USER"

# ---------------------------------------------------------------------------
# Phase 1: Install prerequisites
# ---------------------------------------------------------------------------
if [[ "$SKIP_DEPS" == "true" ]]; then
    log_phase "Phase 1: Skipping prerequisite installation (--skip-deps)"
else
    log_phase "Phase 1: Installing prerequisites on $HOST"

    TMP_DIR=$(mktemp -d)

    log_step "Uploading install scripts..."
    scp_cmd \
        "$SCRIPT_DIR/install-common-dependencies.sh" \
        "$SCRIPT_DIR/install-tinyfaas-dependencies.sh" \
        "$HOST:/tmp/"

    log_step "Installing common dependencies (Go, git, make, curl, zip)..."
    ssh_cmd "sudo DEPLOY_USER=$REMOTE_USER bash /tmp/install-common-dependencies.sh"

    log_step "Installing tinyFaaS dependencies (Docker CE)..."
    ssh_cmd "sudo DEPLOY_USER=$REMOTE_USER bash /tmp/install-tinyfaas-dependencies.sh"

    log_step "Verifying Go installation..."
    ssh_cmd "bash -lc 'go version'"

    log_step "Verifying Docker installation..."
    ssh_cmd "docker --version"

    log_step "Cleaning up remote temp scripts..."
    ssh_cmd "rm -f /tmp/install-common-dependencies.sh /tmp/install-tinyfaas-dependencies.sh"
fi

# ---------------------------------------------------------------------------
# Phase 2: Clone or update repository
# ---------------------------------------------------------------------------
log_phase "Phase 2: Cloning/updating repository on $HOST"

# Inject GitHub token for all HTTPS github.com URLs (covers submodules too).
# This is set temporarily and removed immediately after clone/pull.
REPO_URL_WITH_TOKEN="https://${GITHUB_TOKEN}@github.com/mactavishz/FaaS-Platform-Knowledge-Optimization.git"

if ssh_cmd "test -d '$DEPLOY_DIR/.git'" 2>/dev/null; then
    log_step "Repository exists at $DEPLOY_DIR — updating..."

    ssh_cmd "git config --global url.\"https://${GITHUB_TOKEN}@github.com/\".insteadOf \"https://github.com/\""
    ssh_cmd "cd '$DEPLOY_DIR' && git fetch origin"
    ssh_cmd "cd '$DEPLOY_DIR' && git checkout '$BRANCH'"
    ssh_cmd "cd '$DEPLOY_DIR' && git pull origin '$BRANCH'"
    ssh_cmd "cd '$DEPLOY_DIR' && git submodule update --init --recursive"
    ssh_cmd "git config --global --unset \"url.https://${GITHUB_TOKEN}@github.com/.insteadOf\"" || true
else
    log_step "Fresh clone of $REPO_HTTPS into $DEPLOY_DIR..."

    PARENT_DIR=$(dirname "$DEPLOY_DIR")
    ssh_cmd "sudo mkdir -p '$PARENT_DIR'"
    ssh_cmd "sudo chown '$REMOTE_USER':'$REMOTE_USER' '$PARENT_DIR'"

    ssh_cmd "git config --global url.\"https://${GITHUB_TOKEN}@github.com/\".insteadOf \"https://github.com/\""
    ssh_cmd "git clone --recurse-submodules --branch '$BRANCH' '$REPO_URL_WITH_TOKEN' '$DEPLOY_DIR'"
    ssh_cmd "git config --global --unset \"url.https://${GITHUB_TOKEN}@github.com/.insteadOf\"" || true
fi

# Strip token from remote URL so it's never persisted on the machine.
ssh_cmd "cd '$DEPLOY_DIR' && git remote set-url origin '$REPO_HTTPS'"
log_step "Repository ready at $DEPLOY_DIR"

# ---------------------------------------------------------------------------
# Phase 3: Upload environment file
# ---------------------------------------------------------------------------
log_phase "Phase 3: Uploading environment configuration"

log_step "Uploading $ENV_FILE -> $DEPLOY_DIR/.tinyfaas.env"
scp_cmd "$ENV_FILE" "$HOST:$DEPLOY_DIR/.tinyfaas.env"
log_step "Environment file uploaded"

# ---------------------------------------------------------------------------
# Phase 4: Build and deploy
# ---------------------------------------------------------------------------
log_phase "Phase 4: Building and deploying tinyFaaS"

log_step "Uploading build script..."
scp_cmd "$SCRIPT_DIR/build-tinyfaas.sh" "$HOST:$DEPLOY_DIR/scripts/build-tinyfaas.sh"

log_step "Running build-tinyfaas.sh on remote (this may take several minutes)..."
ssh_cmd "bash -lc 'PROJECT_ROOT=\"$DEPLOY_DIR\" bash \"$DEPLOY_DIR/scripts/build-tinyfaas.sh\"'"

# ---------------------------------------------------------------------------
# Phase 5: Verification
# ---------------------------------------------------------------------------
log_phase "Phase 5: Verifying deployment"

all_active=true
for svc in tf-gateway tf-rproxy tf-manager; do
    if ssh_cmd "systemctl is-active --quiet $svc"; then
        log_step "$svc: active"
    else
        log_step "$svc: FAILED"
        all_active=false
    fi
done

if [[ "$all_active" == "false" ]]; then
    echo ""
    echo "ERROR: One or more tinyFaaS services failed to start."
    echo "  Check logs on the remote machine:"
    echo "    ssh $HOST 'journalctl -u tf-manager --no-pager -n 50'"
    exit 1
fi

# Extract port values from the uploaded env file for the summary.
_port() {
    grep -E "^${1}=" "$ENV_FILE" 2>/dev/null | cut -d= -f2 | tr -d '[:space:]' || echo "$2"
}
TF_GATEWAY_PORT=$(_port TF_GATEWAY_PORT 80)
TF_MANAGER_PORT=$(_port TF_MANAGER_PORT 8080)
TF_RPROXY_PORT=$(_port TF_RPROXY_PORT 8000)

REMOTE_HOST="${HOST##*@}"

echo ""
echo "=================================================="
echo "  tinyFaaS deployed successfully to $HOST"
echo "=================================================="
echo "  Gateway:  http://$REMOTE_HOST:$TF_GATEWAY_PORT"
echo "  Manager:  http://$REMOTE_HOST:$TF_MANAGER_PORT  (internal)"
echo "  RProxy:   http://$REMOTE_HOST:$TF_RPROXY_PORT   (internal)"
echo ""
echo "  View logs:"
echo "    ssh $HOST 'journalctl -u tf-gateway -f'"
echo "    ssh $HOST 'journalctl -u tf-manager -f'"
echo "    ssh $HOST 'journalctl -u tf-rproxy -f'"
echo "=================================================="
