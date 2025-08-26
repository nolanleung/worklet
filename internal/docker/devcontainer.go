package docker

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// DevContainerConfig represents the devcontainer.json structure
type DevContainerConfig struct {
	Name            string                 `json:"name"`
	WorkspaceFolder string                 `json:"workspaceFolder"`
	RemoteUser      string                 `json:"remoteUser,omitempty"`
	Customizations  DevContainerCustom     `json:"customizations"`
}

// DevContainerCustom represents VSCode customizations
type DevContainerCustom struct {
	VSCode DevContainerVSCode `json:"vscode"`
}

// DevContainerVSCode represents VSCode-specific settings
type DevContainerVSCode struct {
	Extensions []string               `json:"extensions"`
	Settings   map[string]interface{} `json:"settings"`
}

// GetUserVSCodeExtensions returns list of user's installed VSCode extensions
func GetUserVSCodeExtensions() ([]string, error) {
	// Check if code command is available
	if _, err := exec.LookPath("code"); err != nil {
		// Return empty list if VSCode CLI not available
		return []string{}, nil
	}

	cmd := exec.Command("code", "--list-extensions")
	output, err := cmd.Output()
	if err != nil {
		// Return empty list on error rather than failing
		return []string{}, nil
	}

	extensions := strings.Split(strings.TrimSpace(string(output)), "\n")
	
	// Filter out empty strings
	var filtered []string
	for _, ext := range extensions {
		ext = strings.TrimSpace(ext)
		if ext != "" {
			filtered = append(filtered, ext)
		}
	}
	
	return filtered, nil
}

// GenerateDevContainerConfig creates devcontainer.json content
func GenerateDevContainerConfig(projectName string) (string, error) {
	if projectName == "" {
		projectName = "Worklet Session"
	}

	// Get user's VSCode extensions
	extensions, err := GetUserVSCodeExtensions()
	if err != nil {
		// Use minimal set if can't get user extensions
		extensions = []string{}
	}

	// Always include the Dev Containers extension
	hasDevContainer := false
	for _, ext := range extensions {
		if ext == "ms-vscode-remote.remote-containers" {
			hasDevContainer = true
			break
		}
	}
	if !hasDevContainer {
		extensions = append(extensions, "ms-vscode-remote.remote-containers")
	}

	config := DevContainerConfig{
		Name:            projectName,
		WorkspaceFolder: "/workspace",
		RemoteUser:      "root",
		Customizations: DevContainerCustom{
			VSCode: DevContainerVSCode{
				Extensions: extensions,
				Settings: map[string]interface{}{
					"terminal.integrated.defaultProfile.linux": "bash",
					"terminal.integrated.shell.linux":          "/bin/bash",
				},
			},
		},
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal devcontainer config: %w", err)
	}

	return string(jsonBytes), nil
}

// EnsureDevContainerConfig ensures container has devcontainer.json
func EnsureDevContainerConfig(containerID string, projectName string) error {
	if containerID == "" {
		return fmt.Errorf("container ID is required")
	}

	// Generate config
	config, err := GenerateDevContainerConfig(projectName)
	if err != nil {
		return fmt.Errorf("failed to generate devcontainer config: %w", err)
	}

	// Create .devcontainer directory in container
	mkdirCmd := exec.Command("docker", "exec", containerID, "mkdir", "-p", "/workspace/.devcontainer")
	if err := mkdirCmd.Run(); err != nil {
		// Don't fail if directory creation fails (might already exist)
		// Just log it for debugging
		fmt.Printf("Warning: Could not create .devcontainer directory: %v\n", err)
	}

	// Write config to container using echo to avoid issues with stdin
	escapedConfig := strings.ReplaceAll(config, `"`, `\"`)
	escapedConfig = strings.ReplaceAll(escapedConfig, `$`, `\$`)
	escapedConfig = strings.ReplaceAll(escapedConfig, "`", "\\`")
	
	writeCmd := exec.Command("docker", "exec", containerID, "sh", "-c",
		fmt.Sprintf(`echo '%s' > /workspace/.devcontainer/devcontainer.json`, escapedConfig))
	
	if err := writeCmd.Run(); err != nil {
		return fmt.Errorf("failed to write devcontainer config: %w", err)
	}

	return nil
}

// GetProjectNameFromContainer retrieves the project name from container labels
func GetProjectNameFromContainer(containerID string) string {
	cmd := exec.Command("docker", "inspect", 
		"--format", "{{index .Config.Labels \"worklet.project.name\"}}", 
		containerID)
	
	output, err := cmd.Output()
	if err != nil {
		return "Worklet Session"
	}
	
	projectName := strings.TrimSpace(string(output))
	if projectName == "" || projectName == "<no value>" {
		return "Worklet Session"
	}
	
	return projectName
}