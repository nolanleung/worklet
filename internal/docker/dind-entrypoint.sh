#!/bin/sh
set -e

# Trap and ignore signals to prevent accidental container termination
# Users must use Ctrl+P, Ctrl+Q to detach from the session
trap '' INT TERM HUP

# Start Docker daemon in the background if we're in full isolation mode
if [ "$WORKLET_ISOLATION" = "full" ]; then
    echo "Starting Docker daemon in full isolation mode..."
    
    # Ensure required directories exist
    mkdir -p /var/run
    mkdir -p /var/log
    
    # Function to start Docker daemon with a specific storage driver
    start_dockerd() {
        local driver=$1
        echo "Attempting to start Docker daemon..."
        
        # Start Docker daemon with explicit configuration
        nohup dockerd \
            --log-level=error \
            --host=unix:///var/run/docker.sock \
            > /var/log/docker.log 2> /var/log/docker-errors.log &
        
        return $?
    }
    
    # Try to start with overlay2 first (preferred)
    start_dockerd "overlay2"
    DOCKERD_PID=$!
    
    # Wait for Docker to be ready
    echo "Waiting for Docker daemon to start..."
    timeout=30
    while [ $timeout -gt 0 ]; do
        if docker version >/dev/null 2>&1; then
            echo "✓ Docker daemon is ready"
            # Show which storage driver is being used
            docker info 2>/dev/null | grep "Storage Driver" || true
            break
        fi
        
        # Show progress
        if [ $((timeout % 5)) -eq 0 ]; then
            echo "  Waiting... ($timeout seconds remaining)"
        fi
        
        timeout=$((timeout - 1))
        sleep 1
    done
    
    if [ $timeout -eq 0 ]; then
        echo "" >&2
        echo "ERROR: Docker daemon failed to start after 30 seconds" >&2
        echo "" >&2
        
        # If still not working, show diagnostic information
        if ! docker version >/dev/null 2>&1; then
            echo "" >&2
            echo "=== Docker Daemon Error Log (last 20 lines) ===" >&2
            tail -20 /var/log/docker-errors.log 2>/dev/null || echo "(No error log available)" >&2
            echo "" >&2
            echo "=== Docker Daemon Output Log (last 20 lines) ===" >&2
            tail -20 /var/log/docker.log 2>/dev/null || echo "(No output log available)" >&2
            echo "" >&2
            echo "=== System Information ===" >&2
            echo "Kernel: $(uname -r)" >&2
            echo "Architecture: $(uname -m)" >&2
            echo "" >&2
            echo "=== Available Storage Drivers ===" >&2
            # Check for overlay module
            if [ -e /proc/filesystems ]; then
                grep overlay /proc/filesystems 2>/dev/null || echo "overlay: not available" >&2
            fi
            echo "" >&2
            echo "Please check:" >&2
            echo "1. The container is running with --privileged flag" >&2
            echo "2. The host kernel supports the required storage drivers" >&2
            echo "3. There is sufficient disk space available" >&2
            echo "" >&2
            exit 1
        fi
    fi
    
    # Start docker-compose services if configured
    if [ -n "$WORKLET_COMPOSE_FILE" ] && [ -f "$WORKLET_COMPOSE_FILE" ]; then
        echo "Starting docker-compose services..."
        
        # Check if docker compose plugin is available
        if ! docker compose version >/dev/null 2>&1; then
            echo "Installing docker compose plugin..."
            
            # Create plugin directory
            mkdir -p /usr/local/lib/docker/cli-plugins
            
            # Download compose plugin (v2.39.1 - stable version)
            COMPOSE_VERSION="v2.39.1"
            ARCH=$(uname -m)
            case $ARCH in
                x86_64) ARCH="x86_64" ;;
                aarch64) ARCH="aarch64" ;;
                armv7l) ARCH="armv7" ;;
                *) echo "Unsupported architecture: $ARCH" >&2; ;;
            esac
            
            if [ -n "$ARCH" ]; then
                wget -q -O /usr/local/lib/docker/cli-plugins/docker-compose \
                    "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${ARCH}"
                chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
                
                # Verify installation
                if docker compose version >/dev/null 2>&1; then
                    echo "Docker compose plugin installed successfully"
                else
                    echo "Warning: Failed to install docker compose plugin" >&2
                fi
            fi
        fi
        
        # Generate compose project name
        COMPOSE_PROJECT_NAME="${WORKLET_PROJECT_NAME}-${WORKLET_SESSION_ID}"
        
        # Start services using docker compose plugin
        if docker compose version >/dev/null 2>&1; then
            echo "Starting services with docker compose..."
            docker compose -f "$WORKLET_COMPOSE_FILE" -p "$COMPOSE_PROJECT_NAME" up -d
            if [ $? -eq 0 ]; then
                echo "Docker-compose services started successfully"
            else
                echo "Warning: Failed to start docker-compose services" >&2
            fi
        else
            echo "Error: Docker compose plugin not available" >&2
        fi
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