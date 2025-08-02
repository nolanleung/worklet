#!/bin/bash
# Install worklet binary to /usr/local/bin

echo "Installing worklet to /usr/local/bin..."
echo "You may be prompted for your password."

# Build the binary if needed
if [ ! -f worklet ]; then
    echo "Building worklet..."
    go build -o worklet cmd/worklet/*.go
fi

# Install with sudo
sudo cp worklet /usr/local/bin/worklet
sudo chmod +x /usr/local/bin/worklet

echo "Installation complete!"
echo "Starting daemon..."
worklet daemon start

echo "You can now use 'worklet' from anywhere."