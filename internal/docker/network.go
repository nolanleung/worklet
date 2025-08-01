package docker

import (
	"fmt"
	"os/exec"
	"strings"
)

const WorkletNetworkName = "worklet-network"

// EnsureNetworkExists creates the worklet Docker network if it doesn't exist
// Deprecated: Use EnsureSessionNetworkExists for session-specific networks
func EnsureNetworkExists() error {
	// Check if network exists
	exists, err := NetworkExists(WorkletNetworkName)
	if err != nil {
		return fmt.Errorf("failed to check network existence: %w", err)
	}

	if exists {
		return nil
	}

	// Create the network
	return CreateNetwork(WorkletNetworkName)
}

// EnsureSessionNetworkExists creates a session-specific Docker network if it doesn't exist
func EnsureSessionNetworkExists(sessionID string) error {
	networkName := fmt.Sprintf("worklet-%s", sessionID)

	// Check if network exists
	exists, err := NetworkExists(networkName)
	if err != nil {
		return fmt.Errorf("failed to check network existence: %w", err)
	}

	if exists {
		return nil
	}

	// Create the network
	return CreateNetwork(networkName)
}

// RemoveSessionNetwork removes a session-specific Docker network
func RemoveSessionNetwork(sessionID string) error {
	networkName := fmt.Sprintf("worklet-%s", sessionID)
	return RemoveNetwork(networkName)
}

// GetSessionNetworkName returns the network name for a session
func GetSessionNetworkName(sessionID string) string {
	return fmt.Sprintf("worklet-%s", sessionID)
}

// NetworkExists checks if a Docker network exists
func NetworkExists(networkName string) (bool, error) {
	cmd := exec.Command("docker", "network", "ls", "--format", "{{.Name}}")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to list networks: %w", err)
	}

	networks := strings.Split(string(output), "\n")
	for _, network := range networks {
		if strings.TrimSpace(network) == networkName {
			return true, nil
		}
	}

	return false, nil
}

// CreateNetwork creates a Docker network
func CreateNetwork(networkName string) error {
	cmd := exec.Command("docker", "network", "create", networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create network: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// RemoveNetwork removes a Docker network
func RemoveNetwork(networkName string) error {
	cmd := exec.Command("docker", "network", "rm", networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the error is because the network doesn't exist
		if strings.Contains(string(output), "not found") {
			return nil
		}
		return fmt.Errorf("failed to remove network: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ListNetworkContainers lists containers connected to a network
func ListNetworkContainers(networkName string) ([]string, error) {
	// Use docker network inspect to get connected containers
	cmd := exec.Command("docker", "network", "inspect", networkName, "--format", "{{range .Containers}}{{.Name}} {{end}}")
	output, err := cmd.Output()
	if err != nil {
		// Network might not exist
		return nil, nil
	}

	containerNames := strings.Fields(string(output))
	return containerNames, nil
}
