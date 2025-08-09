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
	initForce bool
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
		config := generateConfig(projectName)

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
}

func getProjectName() string {
	// Try to get from current directory name
	cwd, err := os.Getwd()
	if err == nil {
		return filepath.Base(cwd)
	}
	return "my-project"
}

func generateConfig(projectName string) string {

	return fmt.Sprintf(`{
  // Worklet configuration file
  "name": "%s",
  "run": {
    // Command to run in the container
    "command": ["sh"],
    
    // Scripts to run on container start (before command)
    // Example: ["apt-get update", "apt-get install -y git", "npm install"]
    "initScript": [
      "echo 'Worklet container started'"
    ],
    // Environment variables
    "environment": {
    },
    
    // Additional volume mounts
    // Example: ["/host/path:/container/path", "volume-name:/data"]
    "volumes": [],
  }
}`, projectName)
}
