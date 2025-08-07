package worklet

import (
	"context"
	"fmt"
	"time"

	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/spf13/cobra"
)

var forksCmd = &cobra.Command{
	Use:   "forks",
	Short: "List all active worklet sessions with their DNS names",
	Long:  `List all active worklet sessions and their services with accessible DNS names.`,
	RunE:  runForks,
}

func runForks(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect to daemon to get fork information
	socketPath := daemon.GetDefaultSocketPath()
	client := daemon.NewClient(socketPath)
	
	// Check if daemon is running
	if !daemon.IsDaemonRunning(socketPath) {
		return fmt.Errorf("daemon is not running. Start it with: worklet daemon start")
	}
	
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	// Get list of forks from daemon
	forks, err := client.ListForks(ctx)
	if err != nil {
		return fmt.Errorf("failed to list forks: %w", err)
	}

	if len(forks) == 0 {
		fmt.Println("No active sessions found")
		return nil
	}

	// Display forks with their DNS names
	for i, fork := range forks {
		if i > 0 {
			fmt.Println()
		}

		fmt.Printf("Session: %s\n", fork.ForkID)
		if fork.ProjectName != "" && fork.ProjectName != fork.ForkID {
			fmt.Printf("Project: %s\n", fork.ProjectName)
		}
		if fork.ContainerID != "" {
			fmt.Printf("Container: %s\n", fork.ContainerID[:12])
		}
		fmt.Printf("Status: running\n")

		if len(fork.Services) == 0 {
			fmt.Println("Services: none")
		} else {
			fmt.Println("Services:")
			for _, svc := range fork.Services {
				// Generate URL for the service
				subdomain := svc.Subdomain
				if subdomain == "" {
					subdomain = svc.Name
				}
				url := fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", 
					subdomain, fork.ProjectName, fork.ForkID)
				fmt.Printf("  - %-15s → %s (port %d)\n", svc.Name, url, svc.Port)
			}
		}
	}

	return nil
}
