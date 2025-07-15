package worklet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tidwall/jsonc"
)

var (
	linkForce bool
)

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Link external tools to worklet configuration",
	Long:  `Link external tools and services to your worklet configuration by adding necessary volume mounts and settings.`,
}

var linkClaudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Link Claude Code authentication to worklet",
	Long: `Links Claude Code authentication by adding a volume mount for ~/.claude to your worklet configuration.
This allows you to use Claude Code inside worklet containers.`,
	RunE: runLinkClaude,
}

func init() {
	linkCmd.AddCommand(linkClaudeCmd)
	linkClaudeCmd.Flags().BoolVar(&linkForce, "force", false, "Force overwrite existing Claude volume mounts")
}

func runLinkClaude(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath := filepath.Join(cwd, ".worklet.jsonc")

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf(".worklet.jsonc not found. Run 'worklet init' first")
	}

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse JSONC to JSON
	jsonData := jsonc.ToJSON(data)

	// Parse into a map to preserve structure and order
	var configMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &configMap); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Define the Claude volume mount
	claudeVolume := fmt.Sprintf("%s/.claude:/root/.claude:ro", homeDir)

	// Ensure run section exists
	runSection, ok := configMap["run"].(map[string]interface{})
	if !ok {
		runSection = make(map[string]interface{})
		configMap["run"] = runSection
	}

	// Get or create volumes array
	volumes, ok := runSection["volumes"].([]interface{})
	if !ok {
		volumes = []interface{}{}
	}

	// Check if Claude volume already exists
	claudeVolumeExists := false
	for _, v := range volumes {
		if volStr, ok := v.(string); ok {
			// Check if this is a Claude volume mount
			if strings.Contains(volStr, "/.claude:") {
				if linkForce {
					// Remove existing Claude volume mount
					continue
				} else {
					claudeVolumeExists = true
					break
				}
			}
		}
	}

	if claudeVolumeExists {
		fmt.Println("Claude volume mount already exists in configuration.")
		fmt.Println("Use --force to overwrite existing Claude volume mounts.")
		return nil
	}

	// Add Claude volume
	volumes = append(volumes, claudeVolume)
	runSection["volumes"] = volumes

	// Marshal back to JSON with indentation
	updatedJSON, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %w", err)
	}

	// Write back to file
	if err := os.WriteFile(configPath, updatedJSON, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Println("Successfully linked Claude authentication to worklet configuration.")
	fmt.Printf("Added volume mount: %s\n", claudeVolume)
	fmt.Println("\nYou can now use Claude Code inside your worklet containers!")
	fmt.Println("Example:")
	fmt.Println("  $ worklet switch my-fork")
	fmt.Println("  /workspace # claude --help")

	return nil
}