#!/bin/bash

set -e

export PROJECT_ROOT=/vagrant
TINYFAAS_PID_FILE=/tmp/tinyfaas.pid

echo "==> Stopping existing tinyFaaS processes..."
# Check if PID file exists and try to kill the process
if [ -f "$TINYFAAS_PID_FILE" ]; then
    OLD_PID=$(cat "$TINYFAAS_PID_FILE")
    if ps -p "$OLD_PID" > /dev/null 2>&1; then
        echo "Found running tinyFaaS process (PID: $OLD_PID), stopping it and its children..."
        # Kill the process group to ensure child processes (rproxy) are also killed
        kill -TERM -$OLD_PID 2>/dev/null || kill "$OLD_PID" || true
        sleep 3
        # Force kill if still running
        if ps -p "$OLD_PID" > /dev/null 2>&1; then
            echo "Force killing process group..."
            kill -9 -$OLD_PID 2>/dev/null || kill -9 "$OLD_PID" || true
            sleep 1
        fi
    fi
    rm -f "$TINYFAAS_PID_FILE"
fi

# Double-check: kill any remaining processes using the ports
for PORT in 8000 8080 8081 5683 9000; do
    PID=$(lsof -ti:$PORT 2>/dev/null || true)
    if [ -n "$PID" ]; then
        echo "Killing process using port $PORT (PID: $PID)..."
        kill -9 $PID 2>/dev/null || true
    fi
done

# Navigate to tinyFaaS directory
cd $PROJECT_ROOT/tinyFaaS

echo "==> Building tinyFaaS..."
make build

# Get the binary name from Makefile
BINARY_NAME=$(make bin-name)
echo "==> Binary name: $BINARY_NAME"

echo "==> Starting tinyFaaS in background..."
# Start tinyFaaS in background with nohup and redirect output to log file
# Use setsid to create a new session so all child processes are in the same process group
nohup setsid ./$BINARY_NAME > /tmp/tinyfaas.log 2>&1 &
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
