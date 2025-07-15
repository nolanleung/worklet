package proxy

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const nginxConfigTemplate = `# Auto-generated nginx configuration for worklet
# DO NOT EDIT - This file is managed by worklet

upstream docker {
    server unix:/var/run/docker.sock;
}

# Health check endpoint
server {
    listen 80;
    server_name localhost;
    
    location /health {
        access_log off;
        return 200 "healthy\n";
        add_header Content-Type text/plain;
    }
}

{{range $forkID, $mapping := .}}
{{range $serviceName, $service := $mapping.ServicePorts}}
server {
    listen 80;
    server_name {{$service.Subdomain}}.{{$mapping.RandomHost}}.fork.local.worklet.sh;

    location / {
        proxy_pass http://{{$service.ContainerName}}:{{$service.ContainerPort}};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        
        # Timeouts
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
}

{{end}}
{{end}}

# Default server to handle unmatched requests
server {
    listen 80 default_server;
    server_name _;
    return 404;
}
`

// NginxConfig manages nginx configuration generation
type NginxConfig struct {
	configPath string
	tmpl       *template.Template
}

// NewNginxConfig creates a new nginx configuration manager
func NewNginxConfig(configPath string) (*NginxConfig, error) {
	tmpl, err := template.New("nginx").Parse(nginxConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nginx template: %w", err)
	}

	return &NginxConfig{
		configPath: configPath,
		tmpl:       tmpl,
	}, nil
}

// GenerateConfig generates the nginx configuration file
func (n *NginxConfig) GenerateConfig(mappings map[string]*ForkMapping) error {
	Debug("Generating nginx config at %s with %d mappings", n.configPath, len(mappings))
	
	// Ensure directory exists
	dir := filepath.Dir(n.configPath)
	Debug("Ensuring nginx config directory exists: %s", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		DebugError("create nginx config directory", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Generate configuration
	var buf bytes.Buffer
	Debug("Executing nginx template")
	if err := n.tmpl.Execute(&buf, mappings); err != nil {
		DebugError("execute nginx template", err)
		return fmt.Errorf("failed to execute template: %w", err)
	}

	// Log generated config in debug mode
	if IsDebugEnabled() {
		Debug("Generated nginx config (%d bytes):\n%s", buf.Len(), buf.String())
	}

	// Write to file
	Debug("Writing nginx config to file")
	if err := os.WriteFile(n.configPath, buf.Bytes(), 0644); err != nil {
		DebugError("write nginx config", err)
		return fmt.Errorf("failed to write config file: %w", err)
	}

	Debug("Nginx config generated successfully")
	return nil
}

// GetConfigPath returns the configuration file path
func (n *NginxConfig) GetConfigPath() string {
	return n.configPath
}

// ValidateConfig validates the nginx configuration by running nginx -t
func (n *NginxConfig) ValidateConfig() error {
	// Create a temporary container to validate the config
	cmd := exec.Command("docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/etc/nginx/conf.d/default.conf:ro", n.configPath),
		nginxImage,
		"nginx", "-t",
	)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx config validation failed: %w\nOutput: %s", err, string(output))
	}
	
	return nil
}