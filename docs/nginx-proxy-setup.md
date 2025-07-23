# Worklet Nginx Proxy Setup

The worklet nginx proxy provides easy access to services running inside worklet containers via friendly URLs.

## How It Works

When you start the worklet daemon, it automatically:
1. Starts an nginx container (`worklet-nginx-proxy`) on port 8080
2. Joins the nginx container to the `worklet-network` Docker network
3. Updates nginx configuration whenever containers are registered/unregistered

## URL Format

Services are accessible via URLs in the format:
```
http://[service]-[fork-id].local.worklet.sh:8080
```

For example:
- `http://app-myproject.local.worklet.sh:8080` - Frontend service
- `http://api-myproject.local.worklet.sh:8080` - API service

## DNS Setup

Since `*.local.worklet.sh` is not a real domain, you need to configure your local DNS to resolve these addresses to localhost. Here are several options:

### Option 1: /etc/hosts (Simple but Manual)

Add entries to `/etc/hosts` for each service:
```bash
127.0.0.1 app-myproject.local.worklet.sh
127.0.0.1 api-myproject.local.worklet.sh
```

### Option 2: dnsmasq (Recommended for macOS)

1. Install dnsmasq:
```bash
brew install dnsmasq
```

2. Configure dnsmasq to resolve *.local.worklet.sh to localhost:
```bash
echo "address=/.local.worklet.sh/127.0.0.1" >> $(brew --prefix)/etc/dnsmasq.conf
```

3. Start dnsmasq:
```bash
sudo brew services start dnsmasq
```

4. Add dnsmasq as a DNS resolver:
```bash
sudo mkdir -p /etc/resolver
echo "nameserver 127.0.0.1" | sudo tee /etc/resolver/local.worklet.sh
```

### Option 3: Use a Chrome Extension

Install a hosts file editor extension that can redirect *.local.worklet.sh to localhost.

## Configuration Example

In your `.worklet.jsonc` file, define services:

```jsonc
{
  "name": "myproject",
  "run": {
    "image": "node:18",
    "command": ["npm", "start"]
  },
  "services": [
    {
      "name": "frontend",
      "port": 3000,
      "subdomain": "app"  // Accessible at app-myproject.local.worklet.sh:8080
    },
    {
      "name": "backend",
      "port": 5000,
      "subdomain": "api"  // Accessible at api-myproject.local.worklet.sh:8080
    }
  ]
}
```

## Troubleshooting

1. **Check if nginx container is running:**
   ```bash
   docker ps | grep worklet-nginx-proxy
   ```

2. **View nginx logs:**
   ```bash
   docker logs worklet-nginx-proxy
   ```

3. **Check nginx configuration:**
   ```bash
   docker exec worklet-nginx-proxy cat /etc/nginx/nginx.conf
   ```

4. **Test without DNS (using Host header):**
   ```bash
   curl -H "Host: app-myproject.local.worklet.sh" http://localhost:8080
   ```

## Security Note

The nginx proxy is configured to listen on all interfaces (0.0.0.0:8080). For local development only, consider binding to localhost only if security is a concern.