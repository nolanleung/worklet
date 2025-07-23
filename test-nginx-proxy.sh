#!/bin/bash

echo "Testing worklet nginx proxy implementation..."

# Start daemon
echo "1. Starting daemon..."
./worklet daemon start

sleep 3

# Check if nginx container is running
echo "2. Checking nginx container..."
docker ps | grep worklet-nginx-proxy

# Create a test worklet config
echo "3. Creating test project..."
mkdir -p /tmp/test-worklet
cat > /tmp/test-worklet/.worklet.jsonc <<EOF
{
  "name": "test-project",
  "run": {
    "image": "node:18",
    "command": ["sh", "-c", "npx -y http-server -p 3000"]
  },
  "services": [
    {
      "name": "web",
      "port": 3000,
      "subdomain": "app"
    }
  ]
}
EOF

# Run worklet in the test directory
echo "4. Running worklet..."
cd /tmp/test-worklet
./worklet run --detach

# Wait for container to start
sleep 5

# Check nginx config
echo "5. Checking nginx configuration..."
docker exec worklet-nginx-proxy cat /etc/nginx/nginx.conf

# Test the proxy (this will fail without proper DNS setup)
echo "6. Testing proxy endpoint..."
curl -v http://localhost:80 -H "Host: app.test-project.local.worklet.sh"

# Cleanup
echo "7. Cleaning up..."
cd -
./worklet daemon stop

echo "Test complete!"