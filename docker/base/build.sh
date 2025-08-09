#!/bin/bash
set -e

# Script to build the worklet Docker image with Claude Code

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default image name
IMAGE_NAME="${IMAGE_NAME:-worklet/base:latest}"

echo -e "${YELLOW}Building Worklet Docker image with Claude Code...${NC}"
echo "Image name: $IMAGE_NAME"

# Get the directory of this script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Build the image
docker build -t "$IMAGE_NAME" "$SCRIPT_DIR"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}Successfully built image: $IMAGE_NAME${NC}"
    echo ""
    echo "To use this image, update your .worklet.jsonc:"
    echo '  "run": {'
    echo "    \"image\": \"$IMAGE_NAME\","
    echo '    ...'
    echo '  }'
else
    echo -e "${RED}Failed to build image${NC}"
    exit 1
fi

# Clean up dangling images
echo -e "${YELLOW}Cleaning up dangling images...${NC}"
docker image prune -f