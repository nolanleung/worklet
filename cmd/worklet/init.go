package worklet

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nolanleung/worklet/internal/config"
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
	// Try to detect project type and generate appropriate config
	dir, err := os.Getwd()
	if err != nil {
		// Fall back to default if we can't get current directory
		return generateDefaultConfig(projectName)
	}

	projectType, err := config.DetectProjectType(dir)
	if err != nil || projectType == config.ProjectTypeUnknown {
		// Fall back to default for unknown project types
		return generateDefaultConfig(projectName)
	}

	// Generate config using existing detection logic
	detectedConfig, err := config.GenerateDefaultConfig(dir, projectType, false)
	if err != nil {
		// Fall back to default if detection fails
		return generateDefaultConfig(projectName)
	}

	// Convert the config to JSONC format with comments
	return formatConfigAsJSONC(detectedConfig)
}

func generateDefaultConfig(projectName string) string {
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

func formatConfigAsJSONC(cfg *config.WorkletConfig) string {
	// Build initScript array as string
	initScriptLines := []string{}
	for _, cmd := range cfg.Run.InitScript {
		initScriptLines = append(initScriptLines, fmt.Sprintf(`      "%s"`, escapeQuotes(cmd)))
	}
	initScriptStr := "[]"
	if len(initScriptLines) > 0 {
		initScriptStr = fmt.Sprintf("[\n%s\n    ]", strings.Join(initScriptLines, ",\n"))
	}

	// Build command array as string
	commandParts := []string{}
	for _, part := range cfg.Run.Command {
		commandParts = append(commandParts, fmt.Sprintf(`"%s"`, escapeQuotes(part)))
	}
	commandStr := fmt.Sprintf("[%s]", strings.Join(commandParts, ", "))

	// Build environment object as string
	envLines := []string{}
	for key, value := range cfg.Run.Environment {
		envLines = append(envLines, fmt.Sprintf(`      "%s": "%s"`, key, escapeQuotes(value)))
	}
	envStr := "{}"
	if len(envLines) > 0 {
		envStr = fmt.Sprintf("{\n%s\n    }", strings.Join(envLines, ",\n"))
	}

	// Build volumes array as string
	volumeLines := []string{}
	for _, vol := range cfg.Run.Volumes {
		volumeLines = append(volumeLines, fmt.Sprintf(`      "%s"`, escapeQuotes(vol)))
	}
	volumeStr := "[]"
	if len(volumeLines) > 0 {
		volumeStr = fmt.Sprintf("[\n%s\n    ]", strings.Join(volumeLines, ",\n"))
	}

	// Format the final JSONC
	result := fmt.Sprintf(`{
  // Worklet configuration file
  "name": "%s",
  "run": {
    // Container image
    "image": "%s",
    
    // Command to run in the container
    "command": %s,
    
    // Scripts to run on container start (before command)
    "initScript": %s,
    
    // Environment variables
    "environment": %s,
    
    // Additional volume mounts
    // Example: ["/host/path:/container/path", "volume-name:/data"]
    "volumes": %s`, 
		escapeQuotes(cfg.Name),
		escapeQuotes(cfg.Run.Image),
		commandStr,
		initScriptStr,
		envStr,
		volumeStr)

	// Add optional fields
	if cfg.Run.Privileged {
		result += `,
    
    // Docker-in-Docker support
    "privileged": true`
	}

	if cfg.Run.Isolation != "" {
		result += fmt.Sprintf(`,
    
    // Container isolation mode
    "isolation": "%s"`, cfg.Run.Isolation)
	}

	result += `
  }
}`

	return result
}

func escapeQuotes(s string) string {
	// Escape quotes and backslashes for JSON strings
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
