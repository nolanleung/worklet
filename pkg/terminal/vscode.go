package terminal

import (
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"
	
	"github.com/nolanleung/worklet/internal/docker"
)

// LaunchVSCode opens VSCode with the container attached using Dev Containers extension
func LaunchVSCode(containerID string) error {
	// Get project name from container
	projectName := docker.GetProjectNameFromContainer(containerID)
	
	// Ensure devcontainer config exists for extension support
	if err := docker.EnsureDevContainerConfig(containerID, projectName); err != nil {
		// Log warning but continue - VSCode will still work without it
		fmt.Printf("Note: Could not set up VSCode extensions: %v\n", err)
	}
	
	// Convert container ID to hex format for VSCode URI
	// VSCode expects the container ID in hex format
	containerHex := hex.EncodeToString([]byte(containerID))
	
	// Build the VSCode remote URI for attached container
	vscodeURI := fmt.Sprintf("vscode-remote://attached-container+%s/workspace", containerHex)
	
	// Determine the command based on the platform
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin", "linux":
		// On macOS and Linux, use 'code' command
		cmd = exec.Command("code", "--folder-uri", vscodeURI)
	case "windows":
		// On Windows, use 'code.cmd'
		cmd = exec.Command("code.cmd", "--folder-uri", vscodeURI)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	
	// Start VSCode in the background
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch VSCode: %w", err)
	}
	
	return nil
}

// GetVSCodeCommand returns the command to open VSCode with the container
func GetVSCodeCommand(containerID string) string {
	containerHex := hex.EncodeToString([]byte(containerID))
	return fmt.Sprintf("code --folder-uri vscode-remote://attached-container+%s/workspace", containerHex)
}