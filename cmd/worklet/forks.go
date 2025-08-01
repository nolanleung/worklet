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
	Short: "List all forks with their DNS names",
	Long:  `List all registered forks and their services with accessible DNS names.`,
	RunE:  runForks,
}

func runForks(cmd *cobra.Command, args []string) error {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get list of forks
	forks, err := client.ListForks(ctx)
	if err != nil {
		return fmt.Errorf("failed to list forks: %w", err)
	}

	if len(forks) == 0 {
		fmt.Println("No forks registered")
		return nil
	}

	// Display forks with their DNS names
	for i, fork := range forks {
		if i > 0 {
			fmt.Println()
		}

		fmt.Printf("Fork: %s\n", fork.ForkID)
		if fork.ProjectName != "" && fork.ProjectName != fork.ForkID {
			fmt.Printf("Project: %s\n", fork.ProjectName)
		}

		if len(fork.Services) == 0 {
			fmt.Println("  No services")
		} else {
			fmt.Println("Services:")
			for _, svc := range fork.Services {
				// Generate DNS name
				subdomain := svc.Subdomain
				if subdomain == "" {
					subdomain = svc.Name
				}
				dnsName := fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", subdomain, fork.ProjectName, fork.ForkID)
				fmt.Printf("  - %-15s → %s\n", svc.Name, dnsName)
			}
		}
	}

	return nil
}
