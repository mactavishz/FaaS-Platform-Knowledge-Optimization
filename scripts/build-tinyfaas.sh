#!/bin/bash

set -e

export PROJECT_ROOT=/vagrant
TINYFAAS_PID_FILE=/tmp/tinyfaas.pid
CADDY_PID_FILE=/tmp/caddy.pid

echo "==> Stopping existing tinyFaaS processes..."
# Check if PID file exists and send SIGTERM for graceful shutdown
if [ -f "$TINYFAAS_PID_FILE" ]; then
    OLD_PID=$(cat "$TINYFAAS_PID_FILE")
    if ps -p "$OLD_PID" > /dev/null 2>&1; then
        echo "Found running tinyFaaS process (PID: $OLD_PID), sending graceful shutdown signal..."
        # Send SIGTERM for graceful shutdown - tinyFaaS now handles this properly
        kill -TERM "$OLD_PID" 2>/dev/null || true
        
        # Wait for graceful shutdown (max 35 seconds: 30s for services + 5s buffer)
        for i in {1..35}; do
            if ! ps -p "$OLD_PID" > /dev/null 2>&1; then
                echo "tinyFaaS stopped gracefully"
                break
            fi
            sleep 1
        done
        
        # Force kill only if still running after graceful shutdown timeout
        if ps -p "$OLD_PID" > /dev/null 2>&1; then
            echo "Graceful shutdown timed out, force killing..."
            kill -9 "$OLD_PID" 2>/dev/null || true
        fi
    fi
    rm -f "$TINYFAAS_PID_FILE"
fi

echo "==> Stopping existing caddy server..."
sudo caddy stop --config $PROJECT_ROOT/tinyFaaS/Caddyfile || echo "Caddy server not running."

# Navigate to tinyFaaS directory
cd $PROJECT_ROOT/tinyFaaS

echo "==> Building tinyFaaS..."
make build

# Get the binary name from Makefile
BINARY_NAME=$(make bin-name)
echo "==> Binary name: $BINARY_NAME"

# Start caddy server
sudo caddy start --config $PROJECT_ROOT/tinyFaaS/Caddyfile --pidfile $CADDY_PID_FILE

echo "==> Starting tinyFaaS in background..."
# Start tinyFaaS in background with nohup and redirect output to log file
# No need for setsid anymore - tinyFaaS handles rproxy shutdown internally
nohup ./$BINARY_NAME > /tmp/tinyfaas.log 2>&1 &
TINYFAAS_PID=$!

# Save PID to file
echo "$TINYFAAS_PID" > "$TINYFAAS_PID_FILE"
echo "==> tinyFaaS started with PID: $TINYFAAS_PID (saved to $TINYFAAS_PID_FILE)"

# Wait a moment and check if the process is still running
sleep 3
if ps -p $TINYFAAS_PID > /dev/null; then
    echo "==> tinyFaaS is running successfully!"
    echo "==> Logs are available at: /tmp/tinyfaas.log"
    echo "==> HTTP endpoint: http://localhost:8000"
    echo "==> Management API: http://localhost:8080"
else
    echo "==> ERROR: tinyFaaS failed to start. Check /tmp/tinyfaas.log for details."
    tail -n 20 /tmp/tinyfaas.log
    exit 1
fi
