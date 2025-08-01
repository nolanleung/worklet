package worklet

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/spf13/cobra"
)

var (
	refreshAll bool
)

var refreshCmd = &cobra.Command{
	Use:   "refresh [fork-id]",
	Short: "Refresh fork information in the daemon",
	Long: `Refresh fork information in the worklet daemon to update service discovery and nginx routing.

If no fork ID is provided and you're running inside a worklet session, the current session will be refreshed.
Use --all to refresh all registered forks.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefresh,
}

func init() {
	refreshCmd.Flags().BoolVar(&refreshAll, "all", false, "Refresh all registered forks")
}

func runRefresh(cmd *cobra.Command, args []string) error {
	socketPath := daemon.GetDefaultSocketPath()

	// Check if daemon is running
	if !daemon.IsDaemonRunning(socketPath) {
		return fmt.Errorf("daemon is not running")
	}

	// Connect to daemon
	client := daemon.NewClient(socketPath)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if refreshAll {
		// Refresh all forks
		fmt.Println("Refreshing all forks...")
		if err := client.RefreshAll(ctx); err != nil {
			return fmt.Errorf("failed to refresh all forks: %w", err)
		}
		fmt.Println("All forks refreshed successfully")
		return nil
	}

	// Determine fork ID to refresh
	var forkID string
	if len(args) > 0 {
		forkID = args[0]
	} else {
		// Try to detect current session ID from environment
		sessionID := os.Getenv("WORKLET_SESSION_ID")
		if sessionID == "" {
			return fmt.Errorf("no fork ID specified and WORKLET_SESSION_ID environment variable not set\n\nUsage:\n  worklet refresh <fork-id>  # Refresh specific fork\n  worklet refresh --all      # Refresh all forks\n\nUse 'worklet forks' to list available fork IDs")
		}
		forkID = sessionID
		fmt.Printf("No fork ID specified, using current session: %s\n", forkID)
	}

	// Refresh specific fork
	fmt.Printf("Refreshing fork %s...\n", forkID)
	if err := client.RefreshFork(ctx, forkID); err != nil {
		return fmt.Errorf("failed to refresh fork: %w", err)
	}
	fmt.Printf("Fork %s refreshed successfully\n", forkID)

	// Show updated fork info
	forkInfo, err := client.GetForkInfo(ctx, forkID)
	if err == nil && forkInfo != nil {
		fmt.Printf("\nUpdated fork information:\n")
		fmt.Printf("  Project: %s\n", forkInfo.ProjectName)
		fmt.Printf("  Container: %s\n", forkInfo.ContainerID[:12])
		if len(forkInfo.Services) > 0 {
			fmt.Printf("  Services:\n")
			for _, svc := range forkInfo.Services {
				fmt.Printf("    - %s (port %d)\n", svc.Name, svc.Port)
			}
		}
	}

	return nil
}