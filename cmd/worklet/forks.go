package worklet

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/spf13/cobra"
)

var (
	forksDebug bool
)

var forksCmd = &cobra.Command{
	Use:   "forks",
	Short: "List all active worklet sessions with their DNS names",
	Long:  `List all active worklet sessions and their services with accessible DNS names.`,
	RunE:  runForks,
}

func init() {
	forksCmd.Flags().BoolVar(&forksDebug, "debug", false, "Enable debug logging")
}

func runForks(cmd *cobra.Command, args []string) error {
	startTime := time.Now()
	
	if forksDebug {
		log.SetPrefix("[DEBUG] ")
		log.Printf("Starting forks command at %v", startTime)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect to daemon to get fork information
	socketPath := daemon.GetDefaultSocketPath()
	if forksDebug {
		log.Printf("Using socket path: %s", socketPath)
	}
	
	client := daemon.NewClient(socketPath)
	
	if forksDebug {
		log.Printf("Connecting to daemon...")
	}
	
	connectStart := time.Now()
	if err := client.Connect(); err != nil {
		if forksDebug {
			log.Printf("Failed to connect after %v: %v", time.Since(connectStart), err)
		}
		// If we can't connect, assume daemon is not running
		return fmt.Errorf("daemon is not running. Start it with: worklet daemon start")
	}
	defer client.Close()
	
	if forksDebug {
		log.Printf("Connected successfully (took %v)", time.Since(connectStart))
		log.Printf("Requesting fork list from daemon...")
	}

	// Get list of forks from daemon
	listStart := time.Now()
	forks, err := client.ListForks(ctx)
	if err != nil {
		if forksDebug {
			log.Printf("Failed to list forks after %v: %v", time.Since(listStart), err)
			log.Printf("Total time before failure: %v", time.Since(startTime))
		}
		return fmt.Errorf("failed to list forks: %w", err)
	}
	
	if forksDebug {
		log.Printf("Received %d forks from daemon (took %v)", len(forks), time.Since(listStart))
		log.Printf("Total command execution time: %v", time.Since(startTime))
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
