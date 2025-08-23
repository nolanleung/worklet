package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// SSHCredentialsVolume is the name of the Docker volume for SSH credentials
	SSHCredentialsVolume = "worklet-ssh-credentials"
)

// SetupSSHCredentials runs an interactive container to set up SSH credentials
func SetupSSHCredentials() error {
	// Ensure volume exists
	if err := CreateVolume(SSHCredentialsVolume); err != nil {
		return fmt.Errorf("failed to create SSH credentials volume: %w", err)
	}

	fmt.Println("Setting up SSH credentials...")
	fmt.Println("This will copy your SSH keys and config into a Docker volume for use in worklet containers.")
	fmt.Println()

	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	sshDir := filepath.Join(homeDir, ".ssh")

	// Check if .ssh directory exists
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		return fmt.Errorf("SSH directory %s does not exist", sshDir)
	}

	// List SSH files to copy
	fmt.Println("Copying SSH files from", sshDir)
	
	// Create a temporary container to copy files into the volume
	// We'll mount both the host SSH directory and the credentials volume
	args := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:/host-ssh:ro", sshDir),
		"-v", fmt.Sprintf("%s:/ssh-config", SSHCredentialsVolume),
		"--entrypoint", "sh",
		"alpine",
		"-c",
		`# Copy SSH files to volume
		cp -r /host-ssh/* /ssh-config/ 2>/dev/null || true
		
		# Set proper permissions
		chmod 700 /ssh-config
		chmod 600 /ssh-config/id_* 2>/dev/null || true
		chmod 600 /ssh-config/config 2>/dev/null || true
		chmod 644 /ssh-config/*.pub 2>/dev/null || true
		chmod 644 /ssh-config/known_hosts* 2>/dev/null || true
		
		# List what was copied
		echo "Copied SSH files:"
		ls -la /ssh-config/`,
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy SSH files: %w", err)
	}

	fmt.Println("\nSSH credentials have been configured and stored in Docker volume.")
	fmt.Println("To use SSH in your worklet containers, add this to your .worklet.jsonc:")
	fmt.Println(`  "credentials": {
    "ssh": true
  }`)
	return nil
}

// CheckSSHCredentials checks if SSH credentials are configured
func CheckSSHCredentials() (bool, error) {
	// Check if volume exists
	exists, err := VolumeExists(SSHCredentialsVolume)
	if err != nil {
		return false, err
	}

	if !exists {
		return false, nil
	}

	// Run a container to check if SSH keys exist
	args := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:/ssh-config:ro", SSHCredentialsVolume),
		"--entrypoint", "sh",
		"alpine",
		"-c",
		"ls /ssh-config/id_* 2>/dev/null | grep -q . && echo 'configured' || echo 'not configured'",
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check SSH credentials: %w", err)
	}

	return strings.TrimSpace(string(output)) == "configured", nil
}

// ClearSSHCredentials removes SSH credentials
func ClearSSHCredentials() error {
	exists, err := VolumeExists(SSHCredentialsVolume)
	if err != nil {
		return err
	}

	if !exists {
		fmt.Println("No SSH credentials to clear.")
		return nil
	}

	if err := RemoveVolume(SSHCredentialsVolume); err != nil {
		return fmt.Errorf("failed to remove SSH credentials volume: %w", err)
	}

	fmt.Println("SSH credentials cleared.")
	return nil
}

// GetSSHVolumeMounts returns volume mount arguments for SSH credentials
func GetSSHVolumeMounts(mountSSH bool) []string {
	var mounts []string

	if mountSSH {
		// Check if volume exists
		if exists, _ := VolumeExists(SSHCredentialsVolume); exists {
			// Mount the volume at a temporary location
			mounts = append(mounts, "-v", fmt.Sprintf("%s:/ssh-config:ro", SSHCredentialsVolume))
		}
	}

	return mounts
}

// GetSSHInitScript returns initialization commands for setting up SSH
func GetSSHInitScript(mountSSH bool) string {
	if !mountSSH {
		return ""
	}

	// Check if volume exists
	if exists, _ := VolumeExists(SSHCredentialsVolume); !exists {
		return ""
	}

	// Return script to set up SSH configuration
	return `# Set up SSH configuration
if [ -d /ssh-config ]; then
	mkdir -p /root/.ssh
	chmod 700 /root/.ssh
	
	# Copy SSH files from volume
	cp -r /ssh-config/* /root/.ssh/ 2>/dev/null || true
	
	# Set proper permissions
	chmod 600 /root/.ssh/id_* 2>/dev/null || true
	chmod 600 /root/.ssh/config 2>/dev/null || true
	chmod 644 /root/.ssh/*.pub 2>/dev/null || true
	chmod 644 /root/.ssh/known_hosts* 2>/dev/null || true
	
	# Start ssh-agent if not running
	if [ -z "$SSH_AUTH_SOCK" ]; then
		eval "$(ssh-agent -s)" > /dev/null 2>&1
		# Add all private keys
		for key in /root/.ssh/id_*; do
			if [ -f "$key" ] && [ "${key%.pub}" = "$key" ]; then
				ssh-add "$key" 2>/dev/null || true
			fi
		done
	fi
	
	# Configure git to use SSH
	git config --global url."git@github.com:".insteadOf "https://github.com/" 2>/dev/null || true
	git config --global url."git@gitlab.com:".insteadOf "https://gitlab.com/" 2>/dev/null || true
	git config --global url."git@bitbucket.org:".insteadOf "https://bitbucket.org/" 2>/dev/null || true
fi`
}