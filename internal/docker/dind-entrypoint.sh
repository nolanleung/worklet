#!/bin/sh
set -e

# Trap and ignore signals to prevent accidental container termination
# Users must use Ctrl+P, Ctrl+Q to detach from the session
trap '' INT TERM HUP

# Start Docker daemon in the background if we're in full isolation mode
if [ "$WORKLET_ISOLATION" = "full" ]; then
    echo "Starting Docker daemon in full isolation mode..."
    
    # Ensure /var/run exists
    mkdir -p /var/run
    
    # Start dockerd in a new session to isolate from terminal signals
    if command -v setsid >/dev/null 2>&1; then
        # Use setsid to create a new session
        setsid dockerd \
            --host=unix:///var/run/docker.sock \
            --storage-driver="${DOCKER_DRIVER:-overlay2}" \
            ${DOCKER_OPTS} >/dev/null 2>&1 &
    else
        # Fallback to nohup if setsid is not available
        nohup dockerd \
            --host=unix:///var/run/docker.sock \
            --storage-driver="${DOCKER_DRIVER:-overlay2}" \
            ${DOCKER_OPTS} >/dev/null 2>&1 &
    fi
    
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

# Run init script if provided
if [ -n "$WORKLET_INIT_SCRIPT" ]; then
    echo "Running initialization script..."
    # Use eval to properly handle the script as multiple commands
    eval "$WORKLET_INIT_SCRIPT"
    if [ $? -ne 0 ]; then
        echo "Init script failed" >&2
        exit 1
    fi
fi

# Inform user about detach sequence
echo ""
echo "=== Worklet Session Started ==="
echo "Use Ctrl+P, Ctrl+Q to detach from this session"
echo "==============================="
echo ""

# Execute the provided command or shell
if [ $# -eq 0 ]; then
    exec sh
elif [ $# -eq 1 ]; then
    # Single argument - check if it contains spaces (likely a multi-word command)
    case "$1" in
        *" "*)
            # Contains spaces, use shell to parse it
            exec sh -c "$1"
            ;;
        *)
            # Single command, execute directly
            exec "$1"
            ;;
    esac
else
    # Multiple arguments, execute directly
    exec "$@"
fi