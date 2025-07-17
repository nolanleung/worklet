package worklet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	Short: "Enable Claude in worklet configuration",
	Long: `Enables Claude in your worklet configuration by adding the necessary credential settings.
This allows worklet containers to use Claude credentials managed by 'worklet credentials claude'.`,
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

	// Ensure run section exists
	runSection, ok := configMap["run"].(map[string]interface{})
	if !ok {
		runSection = make(map[string]interface{})
		configMap["run"] = runSection
	}

	// Get or create credentials section
	credentials, ok := runSection["credentials"].(map[string]interface{})
	if !ok {
		credentials = make(map[string]interface{})
	}

	// Check if Claude is already enabled
	if claude, ok := credentials["claude"].(bool); ok && claude && !linkForce {
		fmt.Println("Claude is already enabled in configuration.")
		fmt.Println("Use --force to re-enable.")
		return nil
	}

	// Enable Claude
	credentials["claude"] = true
	runSection["credentials"] = credentials

	// Marshal back to JSON with indentation
	updatedJSON, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %w", err)
	}

	// Write back to file
	if err := os.WriteFile(configPath, updatedJSON, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Println("Successfully enabled Claude in worklet configuration.")
	fmt.Println("\nNext steps:")
	fmt.Println("1. If you haven't already, set up Claude credentials:")
	fmt.Println("   $ worklet credentials claude setup")
	fmt.Println("")
	fmt.Println("2. Use Claude in your worklet containers:")
	fmt.Println("   $ worklet switch my-fork")
	fmt.Println("   /workspace # claude --help")

	return nil
}