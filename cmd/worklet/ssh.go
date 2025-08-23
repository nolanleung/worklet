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
	Short: "Check if SSH credentials are configured",
	RunE: func(cmd *cobra.Command, args []string) error {
		configured, err := docker.CheckSSHCredentials()
		if err != nil {
			return fmt.Errorf("failed to check SSH credentials: %w", err)
		}

		if configured {
			fmt.Println("✓ SSH credentials are configured")
			fmt.Println("\nTo use SSH in your worklet containers, add this to your .worklet.jsonc:")
			fmt.Println(`  "credentials": {
    "ssh": true
  }`)
		} else {
			fmt.Println("✗ SSH credentials are not configured")
			fmt.Println("\nRun 'worklet ssh setup' to configure SSH credentials")
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