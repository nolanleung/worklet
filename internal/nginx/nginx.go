package nginx

import (
	"bytes"
	"fmt"
	"text/template"
	
	"github.com/nolanleung/worklet/internal/config"
)

// ForkService represents a service within a fork
type ForkService struct {
	ForkID      string
	ProjectName string
	Service     string
	Port        int
	Subdomain   string
}

// Config holds the nginx configuration data
type Config struct {
	Services      []ForkService
	WorkletDomain string
}

// nginxTemplate is the base nginx configuration template
const nginxTemplate = `
events {
    worker_connections 1024;
}

http {
    # Docker DNS resolver - use Docker's embedded DNS server
    resolver 127.0.0.11 valid=30s ipv6=off;
    resolver_timeout 5s;

    # Basic settings
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    # Logging
    access_log /var/log/nginx/access.log;
    error_log /var/log/nginx/error.log;

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_types text/plain text/css text/xml text/javascript application/json application/javascript application/xml+rss application/rss+xml application/atom+xml image/svg+xml;

    # Proxy settings
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    
    # WebSocket support
    map $http_upgrade $connection_upgrade {
        default upgrade;
        '' close;
    }

    {{range .Services}}
    # Service: {{.Service}} for fork {{.ForkID}}
    server {
        listen 80;
        server_name {{if .Subdomain}}{{.Subdomain}}.{{.ProjectName}}-{{.ForkID}}{{else}}{{.ProjectName}}-{{.ForkID}}{{end}}.{{$.WorkletDomain}};

        location / {
            # Use variable to force runtime DNS resolution
            set $upstream {{.ProjectName}}-{{.ForkID}}:{{.Port}};
            proxy_pass http://$upstream;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $connection_upgrade;
            proxy_read_timeout 86400;
            
            # Disable buffering for streaming responses
            proxy_buffering off;
            proxy_cache off;
        }
    }
    {{end}}

    # Default server to handle unmatched requests
    server {
        listen 80 default_server;
        server_name _;
        return 404;
    }
}
`

// GenerateConfig generates an nginx configuration from the provided services
func GenerateConfig(services []ForkService) (string, error) {
	tmpl, err := template.New("nginx").Parse(nginxTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse nginx template: %w", err)
	}

	cfg := Config{
		Services:      services,
		WorkletDomain: config.WorkletDomain,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("failed to execute nginx template: %w", err)
	}

	return buf.String(), nil
}

// AddService creates a ForkService entry
func AddService(forkID, projectName, serviceName string, port int, subdomain string) ForkService {
	return ForkService{
		ForkID:      forkID,
		ProjectName: projectName,
		Service:     serviceName,
		Port:        port,
		Subdomain:   subdomain,
	}
}