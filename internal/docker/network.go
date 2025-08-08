package docker

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
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
	
	fmt.Printf("Ensuring Docker network '%s' exists...\n", networkName)

	// Check if network exists
	exists, err := NetworkExists(networkName)
	if err != nil {
		return fmt.Errorf("failed to check network existence: %w", err)
	}

	if exists {
		fmt.Printf("Network '%s' already exists\n", networkName)
		return nil
	}

	// Create the network with retry logic
	fmt.Printf("Creating Docker network '%s'...\n", networkName)
	
	var lastErr error
	for i := 0; i < 3; i++ {
		if err := CreateNetwork(networkName); err != nil {
			lastErr = err
			fmt.Printf("Attempt %d: Failed to create network: %v\n", i+1, err)
			// Small delay before retry
			time.Sleep(500 * time.Millisecond)
			continue
		}
		
		// Verify the network was actually created
		exists, err = NetworkExists(networkName)
		if err != nil {
			lastErr = fmt.Errorf("failed to verify network creation: %w", err)
			continue
		}
		
		if exists {
			fmt.Printf("Successfully created network '%s'\n", networkName)
			return nil
		}
		
		lastErr = fmt.Errorf("network was not found after creation")
	}
	
	return fmt.Errorf("failed to create network after 3 attempts: %w", lastErr)
}

// RemoveSessionNetwork removes a session-specific Docker network
func RemoveSessionNetwork(sessionID string) error {
	networkName := fmt.Sprintf("worklet-%s", sessionID)
	return RemoveNetwork(networkName)
}

// RemoveSessionNetworkSafe removes a session-specific Docker network only if no containers are connected
func RemoveSessionNetworkSafe(sessionID string) error {
	networkName := fmt.Sprintf("worklet-%s", sessionID)
	
	// Check if network exists
	exists, err := NetworkExists(networkName)
	if err != nil {
		return fmt.Errorf("failed to check network existence: %w", err)
	}
	
	if !exists {
		// Network doesn't exist, nothing to do
		return nil
	}
	
	// Check for connected containers
	containers, err := ListNetworkContainers(networkName)
	if err != nil {
		// If we can't list containers, don't remove the network to be safe
		return fmt.Errorf("failed to list network containers: %w", err)
	}
	
	if len(containers) > 0 {
		// Network still has connected containers, don't remove
		return nil
	}
	
	// Safe to remove the network
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

// CleanupOrphanedNetworks removes all worklet networks that have no connected containers
func CleanupOrphanedNetworks() (int, error) {
	// List all networks
	cmd := exec.Command("docker", "network", "ls", "--format", "{{.Name}}")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to list networks: %w", err)
	}
	
	networks := strings.Split(string(output), "\n")
	removedCount := 0
	
	for _, network := range networks {
		network = strings.TrimSpace(network)
		// Only process worklet session networks (worklet-* pattern)
		if !strings.HasPrefix(network, "worklet-") || network == "worklet-network" {
			continue
		}
		
		// Check for connected containers
		containers, err := ListNetworkContainers(network)
		if err != nil {
			// Skip if we can't check
			continue
		}
		
		if len(containers) == 0 {
			// No containers connected, safe to remove
			if err := RemoveNetwork(network); err == nil {
				removedCount++
			}
		}
	}
	
	return removedCount, nil
}
