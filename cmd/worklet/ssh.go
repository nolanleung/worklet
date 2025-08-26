package worklet

import (
	"fmt"

	"github.com/nolanleung/worklet/internal/docker"
	"github.com/spf13/cobra"
)

var sshCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Manage SSH credentials for worklet containers",
	Long:  `Commands for managing SSH credentials that can be used inside worklet containers.`,
}

var sshSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up SSH credentials for use in containers",
	Long: `Copy your SSH keys and config from ~/.ssh into a Docker volume
for use in worklet containers. This allows git and SSH operations
to work inside containers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return docker.SetupSSHCredentials()
	},
}

var sshStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check SSH credentials and test GitHub connectivity",
	Long:  `Check if SSH credentials are configured and test connectivity to GitHub.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// First check if credentials exist
		configured, err := docker.CheckSSHCredentials()
		if err != nil {
			return fmt.Errorf("failed to check SSH credentials: %w", err)
		}
		
		if !configured {
			fmt.Println("✗ SSH credentials are not configured")
			fmt.Println("\nRun 'worklet ssh setup' to configure SSH credentials")
			return nil
		}
		
		fmt.Println("✓ SSH credentials volume is configured")
		
		// Test GitHub connectivity
		fmt.Print("Testing GitHub SSH connectivity... ")
		
		connected, message, err := docker.TestSSHGitHub()
		if err != nil {
			fmt.Printf("\n⚠️  Error testing connection: %v\n", err)
		} else if connected {
			fmt.Printf("✓\n")
			if message != "" {
				fmt.Printf("  Authenticated as: %s\n", message)
			}
			fmt.Println("\n✅ SSH is properly configured for GitHub access")
			fmt.Println("\nTo use SSH in your worklet containers, add this to your .worklet.jsonc:")
			fmt.Println(`  "credentials": {
    "ssh": true
  }`)
		} else {
			fmt.Printf("✗\n")
			if message != "" {
				fmt.Printf("  Error: %s\n", message)
			}
			fmt.Println("\n⚠️  SSH credentials exist but cannot connect to GitHub")
			fmt.Println("  Please check:")
			fmt.Println("  1. Your SSH key is added to your GitHub account")
			fmt.Println("  2. Your SSH key has the correct permissions")
			fmt.Println("  3. You have network connectivity to GitHub")
			fmt.Println("\nYou may need to run 'worklet ssh setup' again with a working SSH key")
		}
		
		return nil
	},
}

var sshClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove SSH credentials from Docker volume",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Confirm before clearing
		fmt.Print("Are you sure you want to clear SSH credentials? (y/N): ")
		var response string
		fmt.Scanln(&response)
		
		if response != "y" && response != "Y" {
			fmt.Println("Cancelled")
			return nil
		}

		return docker.ClearSSHCredentials()
	},
}

func init() {
	sshCmd.AddCommand(sshSetupCmd)
	sshCmd.AddCommand(sshStatusCmd)
	sshCmd.AddCommand(sshClearCmd)
}