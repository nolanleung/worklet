package worklet

import (
	"context"
	"fmt"

	"github.com/nolanleung/worklet/internal/docker"
	"github.com/spf13/cobra"
)

var (
	cleanupForce bool
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up orphaned Docker resources",
	Long: `Removes orphaned worklet containers, networks, volumes, and images.

By default, this command preserves pnpm store volumes for faster subsequent runs.
Use --force to remove ALL orphaned resources including pnpm volumes.

Examples:
  worklet cleanup        # Clean up orphaned resources (keeps pnpm volumes)
  worklet cleanup --force # Clean up ALL orphaned resources`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Scanning for orphaned Docker resources...")
		
		opts := docker.CleanupOptions{
			Force: cleanupForce,
		}
		
		if cleanupForce {
			fmt.Println("Force mode enabled - will remove pnpm volumes")
		} else {
			fmt.Println("Preserving pnpm volumes (use --force to remove)")
		}
		
		return docker.CleanupAllOrphaned(context.Background(), opts)
	},
}

func init() {
	cleanupCmd.Flags().BoolVarP(&cleanupForce, "force", "f", false, "Remove all orphaned resources including pnpm volumes")
}