package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComposeService represents a service from docker-compose.yml
type ComposeService struct {
	Name      string
	Image     string
	Ports     []string
	Networks  []string
	Container string // Container name when running
}

// ComposeFile represents the structure of a docker-compose.yml file
type ComposeFile struct {
	Version  string                          `yaml:"version"`
	Services map[string]ComposeServiceConfig `yaml:"services"`
	Networks map[string]interface{}          `yaml:"networks,omitempty"`
}

// ComposeServiceConfig represents a service configuration in docker-compose.yml
type ComposeServiceConfig struct {
	Image       string                 `yaml:"image"`
	Ports       []string               `yaml:"ports"`
	Environment interface{}            `yaml:"environment,omitempty"`
	Volumes     []string               `yaml:"volumes,omitempty"`
	Networks    interface{}            `yaml:"networks,omitempty"`
	Labels      map[string]string      `yaml:"labels,omitempty"`
	Other       map[string]interface{} `yaml:",inline"`
}

// StartComposeServices starts docker-compose services for a worklet session
func StartComposeServices(workDir, composePath, sessionID, projectName string, isolation string) error {
	if !fileExists(composePath) {
		return fmt.Errorf("docker-compose file not found: %s", composePath)
	}

	// In full isolation mode, compose will be started inside the container by the entrypoint script
	if isolation == "full" {
		fmt.Printf("Docker-compose will be started inside the container (full isolation mode)\n")
		return nil
	}

	// For shared isolation, run compose on the host as before
	// Ensure session network exists
	networkName := fmt.Sprintf("worklet-%s", sessionID)
	if err := EnsureSessionNetworkExists(sessionID); err != nil {
		return fmt.Errorf("failed to create session network: %w", err)
	}

	// Generate project name for docker-compose
	composeProjectName := fmt.Sprintf("%s-%s", projectName, sessionID)

	// Build docker-compose command
	args := []string{
		"compose",
		"-f", composePath,
		"-p", composeProjectName,
		"up", "-d",
	}

	// Set environment variables for docker-compose
	env := os.Environ()
	env = append(env, fmt.Sprintf("WORKLET_SESSION_ID=%s", sessionID))
	env = append(env, fmt.Sprintf("WORKLET_PROJECT_NAME=%s", projectName))
	env = append(env, fmt.Sprintf("WORKLET_NETWORK=%s", networkName))

	// Execute docker-compose up
	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Starting docker-compose services with project name: %s\n", composeProjectName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker-compose services: %w", err)
	}

	// Connect containers to worklet session network
	if err := connectComposeContainersToNetwork(workDir, composePath, composeProjectName, networkName); err != nil {
		return fmt.Errorf("failed to connect containers to session network: %w", err)
	}

	return nil
}

// StopComposeServices stops docker-compose services for a worklet session
func StopComposeServices(workDir, composePath, sessionID, projectName string, isolation string) error {
	if !fileExists(composePath) {
		return nil // Nothing to stop if compose file doesn't exist
	}

	// In full isolation mode, compose services run inside the container and will be stopped when container exits
	if isolation == "full" {
		// Services will be cleaned up when the container stops
		return nil
	}

	// For shared isolation, stop compose on the host
	// Generate project name for docker-compose
	composeProjectName := fmt.Sprintf("%s-%s", projectName, sessionID)

	// Build docker-compose command
	args := []string{
		"compose",
		"-f", composePath,
		"-p", composeProjectName,
		"down",
	}

	// Execute docker-compose down
	cmd := exec.Command("docker", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Stopping docker-compose services for project: %s\n", composeProjectName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop docker-compose services: %w", err)
	}

	return nil
}

// ParseComposeServices parses a docker-compose.yml file and extracts service information
func ParseComposeServices(composePath string) ([]ComposeService, error) {
	if !fileExists(composePath) {
		return nil, fmt.Errorf("docker-compose file not found: %s", composePath)
	}

	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var composeFile ComposeFile
	if err := yaml.Unmarshal(data, &composeFile); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	var services []ComposeService
	for serviceName, serviceConfig := range composeFile.Services {
		service := ComposeService{
			Name:  serviceName,
			Image: serviceConfig.Image,
			Ports: serviceConfig.Ports,
		}

		// Extract network information if present
		if serviceConfig.Networks != nil {
			switch networks := serviceConfig.Networks.(type) {
			case []interface{}:
				for _, network := range networks {
					if networkStr, ok := network.(string); ok {
						service.Networks = append(service.Networks, networkStr)
					}
				}
			case map[string]interface{}:
				for networkName := range networks {
					service.Networks = append(service.Networks, networkName)
				}
			}
		}

		services = append(services, service)
	}

	return services, nil
}

// GetComposeServicesForDaemon extracts service information for daemon registration
func GetComposeServicesForDaemon(composePath, sessionID, projectName string) ([]ServiceInfo, error) {
	services, err := ParseComposeServices(composePath)
	if err != nil {
		return nil, err
	}

	var serviceInfos []ServiceInfo
	for _, service := range services {
		// Extract port information
		var port int
		if len(service.Ports) > 0 {
			// Parse port mapping (e.g., "8080:80" or "80")
			portStr := service.Ports[0]
			if strings.Contains(portStr, ":") {
				parts := strings.Split(portStr, ":")
				if len(parts) >= 2 {
					portStr = parts[len(parts)-1] // Use container port
				}
			}

			if parsedPort, parseErr := strconv.Atoi(portStr); parseErr == nil {
				port = parsedPort
			}
		}

		if port > 0 {
			serviceInfos = append(serviceInfos, ServiceInfo{
				Name:      service.Name,
				Port:      port,
				Subdomain: service.Name, // Use service name as subdomain
			})
		}
	}

	return serviceInfos, nil
}

// connectComposeContainersToNetwork connects all compose containers to the worklet session network
func connectComposeContainersToNetwork(workDir, composePath, projectName, networkName string) error {
	// Get list of containers for this compose project
	cmd := exec.Command("docker", "compose", "-f", composePath, "-p", projectName, "ps", "-q")
	cmd.Dir = workDir
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list compose containers: %w", err)
	}

	containerIDs := strings.Fields(strings.TrimSpace(string(output)))

	// Connect each container to the session network
	for _, containerID := range containerIDs {
		if containerID == "" {
			continue
		}

		connectCmd := exec.Command("docker", "network", "connect", networkName, containerID)
		if err := connectCmd.Run(); err != nil {
			// Log warning but don't fail - container might already be connected
			fmt.Printf("Warning: Failed to connect container %s to network %s: %v\n", containerID, networkName, err)
		}
	}

	return nil
}

// ServiceInfo represents service information for daemon registration
type ServiceInfo struct {
	Name      string
	Port      int
	Subdomain string
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetComposePath determines the path to the docker-compose file
func GetComposePath(workDir string, composePath string) string {
	// If explicitly configured, use that path
	if composePath != "" {
		if filepath.IsAbs(composePath) {
			return composePath
		}
		return filepath.Join(workDir, composePath)
	}

	// Check for default compose files
	defaultPaths := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	for _, defaultPath := range defaultPaths {
		fullPath := filepath.Join(workDir, defaultPath)
		if fileExists(fullPath) {
			return fullPath
		}
	}

	return ""
}
