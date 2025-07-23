package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// ClaudeCredentialsVolume is the name of the Docker volume for Claude credentials
	ClaudeCredentialsVolume = "worklet-claude-credentials"
)

// VolumeExists checks if a Docker volume exists
func VolumeExists(volumeName string) (bool, error) {
	cmd := exec.Command("docker", "volume", "inspect", volumeName)
	err := cmd.Run()
	if err != nil {
		// Check if it's just a "not found" error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect volume: %w", err)
	}
	return true, nil
}

// CreateVolume creates a Docker volume if it doesn't exist
func CreateVolume(volumeName string) error {
	exists, err := VolumeExists(volumeName)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	cmd := exec.Command("docker", "volume", "create", volumeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create volume: %w, output: %s", err, output)
	}

	return nil
}

// RemoveVolume removes a Docker volume
func RemoveVolume(volumeName string) error {
	cmd := exec.Command("docker", "volume", "rm", volumeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove volume: %w, output: %s", err, output)
	}
	return nil
}

// SetupClaudeCredentials runs an interactive container to set up Claude credentials
func SetupClaudeCredentials() error {
	// Ensure volume exists
	if err := CreateVolume(ClaudeCredentialsVolume); err != nil {
		return fmt.Errorf("failed to create credentials volume: %w", err)
	}

	fmt.Println("Setting up Claude credentials...")
	fmt.Println("This will run Claude's login process in a container.")
	fmt.Println()

	// Run container with Claude CLI to perform login
	// Mount the volume at /claude-config to store all Claude files
	args := []string{
		"run", "--rm", "-it",
		"-v", fmt.Sprintf("%s:/claude-config", ClaudeCredentialsVolume),
		"--entrypoint", "sh",
		"worklet/base:latest",
		"-c",
		`# Create directories and symlinks for Claude config
		mkdir -p /claude-config/.claude
		ln -sf /claude-config/.claude /root/.claude
		ln -sf /claude-config/.claude.json /root/.claude.json
		ln -sf /claude-config/.claude.json.backup /root/.claude.json.backup
		
		# Run Claude login
		claude "/login"
		
		# Copy config files to volume after login
		cp /root/.claude.json /claude-config/.claude.json 2>/dev/null || true
		cp /root/.claude.json.backup /claude-config/.claude.json.backup 2>/dev/null || true`,
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run Claude login: %w", err)
	}

	fmt.Println("\nClaude credentials have been configured and stored in Docker volume.")
	return nil
}

// CheckClaudeCredentials checks if Claude credentials are configured
func CheckClaudeCredentials() (bool, error) {
	// Check if volume exists
	exists, err := VolumeExists(ClaudeCredentialsVolume)
	if err != nil {
		return false, err
	}

	if !exists {
		return false, nil
	}

	// Run a container to check if credentials and config files exist
	args := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:/claude-config:ro", ClaudeCredentialsVolume),
		"--entrypoint", "sh",
		"alpine",
		"-c",
		"test -f /claude-config/.claude/credentials.json && test -f /claude-config/.claude.json && echo 'configured' || echo 'not configured'",
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check credentials: %w", err)
	}

	return strings.TrimSpace(string(output)) == "configured", nil
}

// ClearClaudeCredentials removes Claude credentials
func ClearClaudeCredentials() error {
	exists, err := VolumeExists(ClaudeCredentialsVolume)
	if err != nil {
		return err
	}

	if !exists {
		fmt.Println("No Claude credentials to clear.")
		return nil
	}

	return RemoveVolume(ClaudeCredentialsVolume)
}

// GetCredentialVolumeMounts returns volume mount arguments for credential volumes
func GetCredentialVolumeMounts(mountClaude bool) []string {
	var mounts []string

	if mountClaude {
		// Check if volume exists
		if exists, _ := VolumeExists(ClaudeCredentialsVolume); exists {
			// Mount the volume at a temporary location
			mounts = append(mounts, "-v", fmt.Sprintf("%s:/claude-config", ClaudeCredentialsVolume))
		}
	}

	return mounts
}

// GetCredentialInitScript returns initialization commands for setting up credentials
func GetCredentialInitScript(mountClaude bool) string {
	if !mountClaude {
		return ""
	}

	// Check if volume exists
	if exists, _ := VolumeExists(ClaudeCredentialsVolume); !exists {
		return ""
	}

	// Return script to set up Claude config symlinks
	return `# Set up Claude configuration
if [ -d /claude-config ]; then
	mkdir -p /root
	ln -sf /claude-config/.claude /root/.claude
	ln -sf /claude-config/.claude.json /root/.claude.json
	ln -sf /claude-config/.claude.json.backup /root/.claude.json.backup
fi`
}
