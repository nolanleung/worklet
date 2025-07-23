package worklet

import (
	"fmt"

	"github.com/nolanleung/worklet/internal/docker"
	"github.com/spf13/cobra"
)

var credentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Manage credentials for external services",
	Long:  `Manage credentials for external services like Claude that can be used inside worklet containers.`,
}

var credentialsClaudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Manage Claude credentials",
	Long:  `Manage Claude credentials that will be available in all worklet containers.`,
}

var credentialsClaudeSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up Claude credentials interactively",
	Long: `Interactively set up Claude credentials by running the Claude login process.
This will store credentials in a Docker volume that's automatically mounted in worklet containers.`,
	RunE: runCredentialsClaudeSetup,
}

var credentialsClaudeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Claude credential status",
	Long:  `Check if Claude credentials are configured.`,
	RunE: runCredentialsClaudeStatus,
}

var credentialsClaudeClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear Claude credentials",
	Long:  `Remove stored Claude credentials.`,
	RunE: runCredentialsClaudeClear,
}

func init() {
	// Add credentials command to root
	rootCmd.AddCommand(credentialsCmd)
	
	// Add claude subcommand
	credentialsCmd.AddCommand(credentialsClaudeCmd)
	
	// Add claude subcommands
	credentialsClaudeCmd.AddCommand(credentialsClaudeSetupCmd)
	credentialsClaudeCmd.AddCommand(credentialsClaudeStatusCmd)
	credentialsClaudeCmd.AddCommand(credentialsClaudeClearCmd)
}

func runCredentialsClaudeSetup(cmd *cobra.Command, args []string) error {
	// Check current status first
	configured, err := docker.CheckClaudeCredentials()
	if err != nil {
		return fmt.Errorf("failed to check credential status: %w", err)
	}
	
	if configured {
		fmt.Println("Claude credentials are already configured.")
		fmt.Println("Run 'worklet credentials claude clear' to remove them first if you want to reconfigure.")
		return nil
	}
	
	// Setup credentials
	if err := docker.SetupClaudeCredentials(); err != nil {
		return fmt.Errorf("failed to setup Claude credentials: %w", err)
	}
	
	fmt.Println("\nClaude credentials have been successfully configured!")
	fmt.Println("They will be automatically available in worklet containers that have Claude enabled.")
	fmt.Println("\nTo enable Claude in a worklet project, add to your .worklet.jsonc:")
	fmt.Println(`  "run": {
    "credentials": {
      "claude": true
    }
  }`)
	
	return nil
}

func runCredentialsClaudeStatus(cmd *cobra.Command, args []string) error {
	configured, err := docker.CheckClaudeCredentials()
	if err != nil {
		return fmt.Errorf("failed to check credential status: %w", err)
	}
	
	if configured {
		fmt.Println("✓ Claude credentials are configured")
		fmt.Printf("  Volume: %s\n", docker.ClaudeCredentialsVolume)
		fmt.Println("  Status: Ready to use in worklet containers")
	} else {
		fmt.Println("✗ Claude credentials are not configured")
		fmt.Println("  Run 'worklet credentials claude setup' to configure")
	}
	
	return nil
}

func runCredentialsClaudeClear(cmd *cobra.Command, args []string) error {
	// Check if credentials exist
	configured, err := docker.CheckClaudeCredentials()
	if err != nil {
		return fmt.Errorf("failed to check credential status: %w", err)
	}
	
	if !configured {
		fmt.Println("No Claude credentials to clear.")
		return nil
	}
	
	// Confirm with user
	fmt.Println("This will remove all stored Claude credentials.")
	fmt.Print("Are you sure? (y/N): ")
	
	var response string
	fmt.Scanln(&response)
	
	if response != "y" && response != "Y" {
		fmt.Println("Cancelled.")
		return nil
	}
	
	// Clear credentials
	if err := docker.ClearClaudeCredentials(); err != nil {
		return fmt.Errorf("failed to clear credentials: %w", err)
	}
	
	fmt.Println("Claude credentials have been cleared.")
	return nil
}