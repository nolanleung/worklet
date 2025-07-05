#!/bin/sh
set -e

# Start Docker daemon in the background if we're in full isolation mode
if [ "$WORKLET_ISOLATION" = "full" ]; then
    echo "Starting Docker daemon in full isolation mode..."
    
    # Ensure /var/run exists
    mkdir -p /var/run
    
    # Start dockerd in the background
    dockerd \
        --host=unix:///var/run/docker.sock \
        --storage-driver="${DOCKER_DRIVER:-overlay2}" \
        ${DOCKER_OPTS} &
    
    # Wait for Docker to be ready
    echo "Waiting for Docker daemon to start..."
    timeout=30
    while [ $timeout -gt 0 ]; do
        if docker version >/dev/null 2>&1; then
            echo "Docker daemon is ready"
            break
        fi
        timeout=$((timeout - 1))
        sleep 1
    done
    
    if [ $timeout -eq 0 ]; then
        echo "Docker daemon failed to start" >&2
        exit 1
    fi
fi

# Execute the provided command or shell
if [ $# -eq 0 ]; then
    exec sh
else
    exec "$@"
fi