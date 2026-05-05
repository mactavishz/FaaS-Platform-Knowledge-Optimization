#!/bin/bash
# Deploy faasd to a remote Ubuntu server via SSH.
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
Usage: $(basename "$0") --host <host> [OPTIONS]

Deploy faasd to a remote Ubuntu server.

Required:
  --host <host>             SSH target - a bare alias (e.g., hetzner), user@host,
                            or user@ip. Bare aliases are resolved via ~/.ssh/config.
  --env-file <path>         Local path to .env file to upload (required unless --delete)

Optional:
  --delete                  Remove faasd from the target host and delete the remote checkout
  --port <number>           SSH port (default: 22, or as set in ~/.ssh/config)
  --branch <name>           Git branch to deploy (default: $DEFAULT_BRANCH)
  --github-token <token>    GitHub PAT for HTTPS clone
                            (fallback: GITHUB_TOKEN environment variable)
  --ssh-key <path>          Path to SSH private key (uses ssh-agent/default if omitted)
  --deploy-dir <path>       Remote deployment directory (default: $DEFAULT_DEPLOY_DIR)
  --skip-deps               Skip Go, containerd, and CNI installation on the target
  --help                    Show this help message

Examples:
  # Fresh deploy using an SSH config alias
  $(basename "$0") --host hetzner --env-file .env

  # Fresh deploy with explicit user@ip and custom port
  $(basename "$0") --host ubuntu@192.168.1.100 --port 2222 --env-file .env

  # Deploy a specific branch with explicit token and SSH key
  $(basename "$0") --host ubuntu@myserver.com \
    --env-file .env \
    --branch feature/my-changes \
    --github-token ghp_xxxx \
    --ssh-key ~/.ssh/id_ed25519

  # Re-deploy (update) skipping dependency installation
  $(basename "$0") --host hetzner --env-file .env --skip-deps

  # Delete faasd and remove the remote checkout
  $(basename "$0") --host hetzner --delete
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
SSH_PORT=""
DEPLOY_DIR="$DEFAULT_DEPLOY_DIR"
SKIP_DEPS=false
DELETE=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)          HOST="$2"; shift 2 ;;
        --env-file)      ENV_FILE="$2"; shift 2 ;;
        --delete)        DELETE=true; shift ;;
        --port)          SSH_PORT="$2"; shift 2 ;;
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

if [[ "$DELETE" != "true" ]]; then
    if [[ -z "$ENV_FILE" ]]; then
        echo "ERROR: --env-file is required unless --delete is used"
        errors=$((errors + 1))
    elif [[ ! -f "$ENV_FILE" ]]; then
        echo "ERROR: env file not found: $ENV_FILE"
        errors=$((errors + 1))
    fi
fi

if [[ "$DELETE" != "true" && -z "$GITHUB_TOKEN" ]]; then
    echo "ERROR: GitHub token is required. Use --github-token or set GITHUB_TOKEN env var."
    errors=$((errors + 1))
fi

if [[ -n "$SSH_KEY" && ! -f "$SSH_KEY" ]]; then
    echo "ERROR: SSH key not found: $SSH_KEY"
    errors=$((errors + 1))
fi

if [[ -n "$SSH_PORT" && ! "$SSH_PORT" =~ ^[0-9]+$ ]]; then
    echo "ERROR: --port must be a number: $SSH_PORT"
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
# ssh uses -p, scp uses -P for port - build separate option strings.
SSH_OPTS="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 -o BatchMode=yes"
SCP_OPTS="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 -o BatchMode=yes"

if [[ -n "$SSH_PORT" ]]; then
    SSH_OPTS="$SSH_OPTS -p $SSH_PORT"
    SCP_OPTS="$SCP_OPTS -P $SSH_PORT"
fi

if [[ -n "$SSH_KEY" ]]; then
    SSH_OPTS="$SSH_OPTS -i $SSH_KEY"
    SCP_OPTS="$SCP_OPTS -i $SSH_KEY"
fi

ssh_cmd() {
    # shellcheck disable=SC2086
    ssh $SSH_OPTS "$HOST" "$@"
}

scp_cmd() {
    # shellcheck disable=SC2086
    scp $SCP_OPTS "$@"
}

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

remove_remote_deploy_dir() {
    log_step "Removing remote checkout at $DEPLOY_DIR"
    ssh_cmd "if [ -e '$DEPLOY_DIR' ]; then rm -rf '$DEPLOY_DIR' || sudo rm -rf '$DEPLOY_DIR'; fi"
}

extract_env_value() {
    local key="$1"
    local fallback="$2"
    local value

    value=$(grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | cut -d= -f2- | tr -d '[:space:]' || true)

    if [[ -n "$value" ]]; then
        echo "$value"
    else
        echo "$fallback"
    fi
}

delete_faasd_fallback() {
    log_step "Running fallback faasd cleanup"
    ssh_cmd "sudo systemctl disable --now faasd 2>/dev/null || true; sudo systemctl disable --now faasd-provider 2>/dev/null || true; sudo systemctl disable --now faasd-gateway 2>/dev/null || true; sudo rm -f /etc/default/faasd; sudo rm -f /usr/local/bin/faasd /usr/local/bin/faasd-gateway; sudo rm -rf /var/lib/faasd; sudo rm -f /usr/lib/systemd/system/faasd-provider.service /usr/lib/systemd/system/faasd.service /usr/lib/systemd/system/faasd-gateway.service; sudo rm -f /lib/systemd/system/faasd-provider.service /lib/systemd/system/faasd.service /lib/systemd/system/faasd-gateway.service; sudo systemctl daemon-reload"
}

delete_faasd() {
    log_phase "Phase 1: Removing faasd deployment"

    if ssh_cmd "test -d '$DEPLOY_DIR/faasd' && command -v make >/dev/null 2>&1"; then
        log_step "Repository checkout found - running faasd teardown"
        if ! ssh_cmd "bash -lc 'make -C \"$DEPLOY_DIR/faasd\" down'"; then
            log_step "Repo-based teardown failed, falling back to direct cleanup"
            delete_faasd_fallback
        fi
    else
        log_step "Repository checkout not found - using fallback cleanup"
        delete_faasd_fallback
    fi

    remove_remote_deploy_dir

    echo ""
    echo "=================================================="
    echo "  faasd removed successfully from $HOST"
    echo "=================================================="
    echo "  Services stopped and uninstalled"
    echo "  Deployment directory removed: $DEPLOY_DIR"
    echo "=================================================="
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

if [[ "$DELETE" == "true" ]]; then
    delete_faasd
    exit 0
fi

# ---------------------------------------------------------------------------
# Phase 1: Install prerequisites
# ---------------------------------------------------------------------------
if [[ "$SKIP_DEPS" == "true" ]]; then
    log_phase "Phase 1: Skipping prerequisite installation (--skip-deps)"
else
    log_phase "Phase 1: Installing prerequisites on $HOST"

    log_step "Uploading install scripts..."
    scp_cmd \
        "$SCRIPT_DIR/install-common-dependencies.sh" \
        "$SCRIPT_DIR/install-faasd-dependencies.sh" \
        "$HOST:/tmp/"

    log_step "Installing common dependencies (Go, git, make, curl, zip)..."
    ssh_cmd "sudo DEPLOY_USER=$REMOTE_USER bash /tmp/install-common-dependencies.sh"

    log_step "Installing faasd dependencies (containerd, CNI)..."
    ssh_cmd "sudo DEPLOY_USER=$REMOTE_USER bash /tmp/install-faasd-dependencies.sh"

    log_step "Verifying Go installation..."
    ssh_cmd "bash -lc 'go version'"

    log_step "Verifying containerd installation..."
    ssh_cmd "containerd --version"

    log_step "Cleaning up remote temp scripts..."
    ssh_cmd "rm -f /tmp/install-common-dependencies.sh /tmp/install-faasd-dependencies.sh"
fi

# ---------------------------------------------------------------------------
# Phase 2: Clone or update repository
# ---------------------------------------------------------------------------
log_phase "Phase 2: Cloning/updating repository on $HOST"

# Inject GitHub token for all HTTPS github.com URLs (covers submodules too).
# This is set temporarily and removed immediately after clone/pull.
REPO_URL_WITH_TOKEN="https://${GITHUB_TOKEN}@github.com/mactavishz/FaaS-Platform-Knowledge-Optimization.git"

if ssh_cmd "test -d '$DEPLOY_DIR/.git'" 2>/dev/null; then
    log_step "Repository exists at $DEPLOY_DIR - updating..."

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

log_step "Uploading $ENV_FILE -> $DEPLOY_DIR/.env"
scp_cmd "$ENV_FILE" "$HOST:$DEPLOY_DIR/.env"
log_step "Environment file uploaded"

# ---------------------------------------------------------------------------
# Phase 4: Build and deploy
# ---------------------------------------------------------------------------
log_phase "Phase 4: Building and deploying faasd"

log_step "Uploading build script..."
scp_cmd "$SCRIPT_DIR/build-faasd.sh" "$HOST:$DEPLOY_DIR/scripts/build-faasd.sh"

log_step "Running build-faasd.sh on remote (this may take several minutes)..."
ssh_cmd "bash -lc 'PROJECT_ROOT=\"$DEPLOY_DIR\" bash \"$DEPLOY_DIR/scripts/build-faasd.sh\"'"

# ---------------------------------------------------------------------------
# Phase 5: Verification
# ---------------------------------------------------------------------------
log_phase "Phase 5: Verifying deployment"

all_active=true
for svc in faasd faasd-provider faasd-gateway; do
    if ssh_cmd "systemctl is-active --quiet $svc"; then
        log_step "$svc: active"
    else
        log_step "$svc: FAILED"
        all_active=false
    fi
done

if ! ssh_cmd "curl -fsS -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/system/functions | grep -Eq '^(200|401|403)$'"; then
    echo ""
    echo "ERROR: faasd gateway did not respond as expected on http://127.0.0.1:8080/system/functions"
    echo "  Check logs on the remote machine:"
    echo "    ssh $HOST 'journalctl -u faasd-gateway --no-pager -n 50'"
    exit 1
fi
log_step "Gateway endpoint reachable"

if [[ "$all_active" == "false" ]]; then
    echo ""
    echo "ERROR: One or more faasd services failed to start."
    echo "  Check logs on the remote machine:"
    echo "    ssh $HOST 'journalctl -u faasd --no-pager -n 50'"
    echo "    ssh $HOST 'journalctl -u faasd-provider --no-pager -n 50'"
    echo "    ssh $HOST 'journalctl -u faasd-gateway --no-pager -n 50'"
    exit 1
fi

GATEWAY_PORT=$(extract_env_value GATEWAY_PORT 8080)
REMOTE_HOST="${HOST##*@}"

echo ""
echo "=================================================="
echo "  faasd deployed successfully to $HOST"
echo "=================================================="
echo "  Gateway:   http://$REMOTE_HOST:$GATEWAY_PORT"
echo ""
echo "  Get admin password:"
echo "    ssh $HOST 'sudo cat /var/lib/faasd/secrets/basic-auth-password'"
echo ""
echo "  View logs:"
echo "    ssh $HOST 'journalctl -u faasd -f'"
echo "    ssh $HOST 'journalctl -u faasd-provider -f'"
echo "    ssh $HOST 'journalctl -u faasd-gateway -f'"
echo "=================================================="
