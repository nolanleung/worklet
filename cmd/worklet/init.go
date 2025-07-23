package worklet

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	initForce   bool
	initMinimal bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new .worklet.jsonc configuration file",
	Long:  `Creates a .worklet.jsonc configuration file in the current directory with sensible defaults.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := ".worklet.jsonc"

		// Check if config already exists
		if _, err := os.Stat(configPath); err == nil && !initForce {
			fmt.Print(".worklet.jsonc already exists. Overwrite? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}

			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				fmt.Println("Init cancelled.")
				return nil
			}
		}

		// Get project name from current directory
		projectName := getProjectName()

		// Generate config
		config := generateConfig(projectName, initMinimal)

		// Write config file
		if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}

		fmt.Printf("✓ Created .worklet.jsonc\n")
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "Overwrite existing config without confirmation")
	initCmd.Flags().BoolVarP(&initMinimal, "minimal", "m", false, "Create minimal config without comments")
}

func getProjectName() string {
	// Try to get from current directory name
	cwd, err := os.Getwd()
	if err == nil {
		return filepath.Base(cwd)
	}
	return "my-project"
}

func generateConfig(projectName string, minimal bool) string {
	if minimal {
		return fmt.Sprintf(`{
  "fork": {
    "name": "%s",
    "exclude": ["*.log", ".DS_Store", "*.tmp", "*.swp"]
  },
  "run": {
    "image": "worklet/base:latest",
    "isolation": "full",
    "initScript": ["echo 'Container started'"]
  }
}`, projectName)
	}

	return fmt.Sprintf(`{
  // Worklet configuration file
  "fork": {
    // Name for this fork (used in listings)
    "name": "%s",
    
    // Description of what this fork is for
    "description": "Development fork",
    
    // Include .git directory for full Git workflow (default: true)
    "includeGit": true,
    
    // Patterns to exclude from fork
    "exclude": [
      "*.log",      // Log files
      ".DS_Store",  // macOS metadata
      "*.tmp",      // Temporary files
      "*.swp"       // Vim swap files
    ]
  },
  "run": {
    // Docker image to use (defaults to worklet/base:latest if not specified)
    "image": "worklet/base:latest",
    
    // Isolation mode: "full" (default) or "shared"
    // - "full": Runs a separate Docker daemon inside the container (true isolation)
    // - "shared": Mounts host Docker socket (containers run on host)
    "isolation": "full",
    
    // Command to run in the container
    "command": ["sh"],
    
    // Scripts to run on container start (before command)
    // Example: ["apt-get update", "apt-get install -y git", "npm install"]
    "initScript": [
      "echo 'Worklet container started'"
    ],
    
    // Environment variables
    "environment": {
      "DOCKER_TLS_CERTDIR": "",
      "DOCKER_DRIVER": "overlay2"
    },
    
    // Additional volume mounts
    // Example: ["/host/path:/container/path", "volume-name:/data"]
    "volumes": [],
    
    // Run in privileged mode (required for "full" isolation mode)
    "privileged": true
  }
}`, projectName)
}
