package proxy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	nginxContainerName = "worklet-nginx-proxy"
	nginxImage         = "nginx:alpine"
	workletNetwork     = "worklet-network"
)

// Server manages the nginx proxy server
type Server struct {
	manager      *Manager
	nginxConfig  *NginxConfig
	configDir    string
	isRunning    bool
	persistence  *PersistenceStore
}

// NewServer creates a new proxy server
func NewServer(configDir string) (*Server, error) {
	Debug("Creating new proxy server with config dir: %s", configDir)
	configPath := filepath.Join(configDir, "nginx.conf")

	nginxConfig, err := NewNginxConfig(configPath)
	if err != nil {
		DebugError("nginx config creation", err)
		return nil, err
	}
	Debug("Nginx config initialized at: %s", configPath)

	persistence := NewPersistenceStore(configDir)
	Debug("Persistence store initialized")
	
	server := &Server{
		manager:     NewManager(),
		nginxConfig: nginxConfig,
		configDir:   configDir,
		isRunning:   false,
		persistence: persistence,
	}
	
	// Load persisted mappings
	Debug("Loading persisted mappings")
	if err := server.loadPersistedMappings(); err != nil {
		// Log error but don't fail - we can continue without persisted mappings
		DebugError("loading persisted mappings", err)
		fmt.Printf("Warning: Failed to load persisted mappings: %v\n", err)
	}
	
	return server, nil
}

// Start starts the nginx proxy server
func (s *Server) Start(ctx context.Context) error {
	start := time.Now()
	defer DebugDuration("Server.Start", start)
	
	Debug("Starting proxy server")
	if s.isRunning {
		Debug("Proxy server is already running")
		return fmt.Errorf("proxy server is already running")
	}

	// Ensure config directory exists
	Debug("Creating config directory: %s", s.configDir)
	if err := os.MkdirAll(s.configDir, 0755); err != nil {
		DebugError("create config directory", err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create Docker network if it doesn't exist
	Debug("Creating Docker network: %s", workletNetwork)
	if err := s.createNetwork(); err != nil {
		DebugError("create network", err)
		return fmt.Errorf("failed to create network: %w", err)
	}

	// Generate initial nginx config
	mappings := s.manager.GetAllMappings()
	Debug("Generating nginx config with %d mappings", len(mappings))
	if err := s.nginxConfig.GenerateConfig(mappings); err != nil {
		DebugError("generate nginx config", err)
		return fmt.Errorf("failed to generate nginx config: %w", err)
	}

	// Validate the config file exists and is readable
	configPath := s.nginxConfig.GetConfigPath()
	Debug("Validating nginx config at: %s", configPath)
	if _, err := os.Stat(configPath); err != nil {
		DebugError("stat nginx config", err)
		return fmt.Errorf("nginx config file not accessible at %s: %w", configPath, err)
	}

	// Stop any existing container
	Debug("Stopping any existing nginx container")
	s.stopContainer()

	// Start nginx container
	dockerArgs := []string{
		"run",
		"--name", nginxContainerName,
		"--network", workletNetwork,
		"-p", "80:80",
		"-v", fmt.Sprintf("%s:/etc/nginx/conf.d/default.conf:ro", s.nginxConfig.GetConfigPath()),
		"--rm",
		"-d",
		nginxImage,
	}
	Debug("Running docker command: docker %s", strings.Join(dockerArgs, " "))
	
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		Debug("Docker run failed with output: %s", string(output))
		DebugError("docker run", err)
		return fmt.Errorf("failed to start nginx container: %w\nOutput: %s", err, string(output))
	}
	Debug("Docker container started with ID: %s", strings.TrimSpace(string(output)))

	// Wait a moment for container to fully start
	Debug("Waiting for container to fully start")
	time.Sleep(500 * time.Millisecond)

	// Verify container is actually running
	Debug("Verifying container is running")
	if !s.IsRunning() {
		// Try to get logs to understand why it failed
		Debug("Container is not running, fetching logs")
		logs, logsErr := s.GetLogs(50)
		if logsErr != nil {
			DebugError("get logs", logsErr)
		}
		return fmt.Errorf("nginx container started but is not running. Logs:\n%s", logs)
	}

	s.isRunning = true
	return nil
}

// Stop stops the nginx proxy server
func (s *Server) Stop() error {
	if !s.isRunning {
		return nil
	}

	if err := s.stopContainer(); err != nil {
		return fmt.Errorf("failed to stop nginx container: %w", err)
	}

	s.isRunning = false
	return nil
}

// Reload reloads the nginx configuration
func (s *Server) Reload() error {
	if !s.isRunning {
		return fmt.Errorf("proxy server is not running")
	}

	// Generate new config
	if err := s.nginxConfig.GenerateConfig(s.manager.GetAllMappings()); err != nil {
		return fmt.Errorf("failed to generate nginx config: %w", err)
	}

	// Reload nginx
	cmd := exec.Command("docker", "exec", nginxContainerName, "nginx", "-s", "reload")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	return nil
}

// RegisterFork registers a fork with the proxy and reloads nginx
func (s *Server) RegisterFork(forkID string, services []ServicePort) (*ForkMapping, error) {
	mapping, err := s.manager.RegisterFork(forkID, services)
	if err != nil {
		return nil, err
	}

	// Save to persistence
	if err := s.savePersistedMappings(); err != nil {
		fmt.Printf("Warning: Failed to persist mappings: %v\n", err)
	}

	// Reload nginx if running
	if s.isRunning {
		if err := s.Reload(); err != nil {
			// Rollback on failure
			s.manager.UnregisterFork(forkID)
			return nil, fmt.Errorf("failed to reload nginx: %w", err)
		}
	}

	return mapping, nil
}

// UnregisterFork removes a fork from the proxy and reloads nginx
func (s *Server) UnregisterFork(forkID string) error {
	if err := s.manager.UnregisterFork(forkID); err != nil {
		return err
	}

	// Save to persistence
	if err := s.savePersistedMappings(); err != nil {
		fmt.Printf("Warning: Failed to persist mappings: %v\n", err)
	}

	// Reload nginx if running
	if s.isRunning {
		if err := s.Reload(); err != nil {
			return fmt.Errorf("failed to reload nginx: %w", err)
		}
	}

	return nil
}

// GetManager returns the proxy manager
func (s *Server) GetManager() *Manager {
	return s.manager
}

// IsRunning returns whether the proxy server is running by checking the actual container state
func (s *Server) IsRunning() bool {
	// Check actual container state, not just the flag
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", nginxContainerName)
	output, err := cmd.Output()
	if err != nil {
		// Container doesn't exist or error checking
		s.isRunning = false
		return false
	}

	// Parse the output (will be "true" or "false")
	running := strings.TrimSpace(string(output)) == "true"
	s.isRunning = running
	return running
}

// createNetwork creates the Docker network if it doesn't exist
func (s *Server) createNetwork() error {
	start := time.Now()
	defer DebugDuration("createNetwork", start)
	
	// Check if network exists
	Debug("Checking if network %s exists", workletNetwork)
	cmd := exec.Command("docker", "network", "ls", "--filter", fmt.Sprintf("name=%s", workletNetwork), "-q")
	output, err := cmd.CombinedOutput()
	if err != nil {
		Debug("Failed to list networks: %s", string(output))
		DebugError("docker network ls", err)
		return fmt.Errorf("failed to list networks: %w\nOutput: %s", err, string(output))
	}

	// If network doesn't exist, create it
	if len(output) == 0 {
		Debug("Network %s does not exist, creating it", workletNetwork)
		fmt.Printf("Creating Docker network: %s\n", workletNetwork)
		cmd = exec.Command("docker", "network", "create", workletNetwork)
		output, err := cmd.CombinedOutput()
		if err != nil {
			Debug("Failed to create network: %s", string(output))
			DebugError("docker network create", err)
			return fmt.Errorf("failed to create network: %w\nOutput: %s", err, string(output))
		}
		Debug("Network created successfully")
		fmt.Printf("Network created successfully\n")
	} else {
		Debug("Network %s already exists", workletNetwork)
	}

	return nil
}

// GetLogs retrieves the logs from the nginx container
func (s *Server) GetLogs(tail int) (string, error) {
	args := []string{"logs"}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	args = append(args, nginxContainerName)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// GetContainerInfo returns detailed information about the proxy container
func (s *Server) GetContainerInfo() (string, error) {
	cmd := exec.Command("docker", "inspect", nginxContainerName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	return string(output), nil
}

// CheckHealth checks if the proxy is healthy by hitting the health endpoint
func (s *Server) CheckHealth() error {
	if !s.IsRunning() {
		return fmt.Errorf("proxy container is not running")
	}

	// Try to hit the health endpoint
	cmd := exec.Command("curl", "-f", "-s", "http://localhost/health")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("health check failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// stopContainer stops the nginx container
func (s *Server) stopContainer() error {
	// First try graceful stop
	cmd := exec.Command("docker", "stop", nginxContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Log the error but continue with force remove
		fmt.Printf("Warning: Failed to stop container gracefully: %s\n", string(output))
	}

	// Give it time to stop
	time.Sleep(1 * time.Second)

	// Force remove if still exists
	cmd = exec.Command("docker", "rm", "-f", nginxContainerName)
	output, err = cmd.CombinedOutput()
	if err != nil {
		// Only return error if it's not "container not found"
		if !strings.Contains(string(output), "No such container") {
			return fmt.Errorf("failed to remove container: %w\nOutput: %s", err, string(output))
		}
	}

	return nil
}

// loadPersistedMappings loads mappings from disk into the manager
func (s *Server) loadPersistedMappings() error {
	persisted, err := s.persistence.Load()
	if err != nil {
		return err
	}
	
	// Convert and add each mapping to the manager
	for forkID, pm := range persisted {
		// Only load the mapping data, don't mark as active yet
		fm := ConvertFromPersistent(pm)
		s.manager.mu.Lock()
		s.manager.mappings[forkID] = fm
		s.manager.mu.Unlock()
	}
	
	return nil
}

// savePersistedMappings saves current mappings to disk
func (s *Server) savePersistedMappings() error {
	s.manager.mu.RLock()
	defer s.manager.mu.RUnlock()
	
	persisted := make(map[string]*PersistentMapping)
	for forkID, fm := range s.manager.mappings {
		// Check if container is actually running to determine active status
		isActive := s.isContainerRunning(fm.ServicePorts)
		persisted[forkID] = ConvertToPersistent(fm, isActive)
	}
	
	return s.persistence.Save(persisted)
}

// isContainerRunning checks if any container for the services is running
func (s *Server) isContainerRunning(services map[string]ServicePort) bool {
	for _, svc := range services {
		cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", svc.ContainerName)
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "true" {
			return true
		}
	}
	return false
}
