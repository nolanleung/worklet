package worklet

import (
	"context"
	"fmt"
	"time"

	"github.com/nolanleung/worklet/internal/docker"
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

	// Get list of sessions from Docker API
	sessions, err := docker.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions found")
		return nil
	}

	// Display sessions with their DNS names
	for i, session := range sessions {
		if i > 0 {
			fmt.Println()
		}

		fmt.Printf("Session: %s\n", session.SessionID)
		if session.ProjectName != "" && session.ProjectName != session.SessionID {
			fmt.Printf("Project: %s\n", session.ProjectName)
		}
		fmt.Printf("Status: %s\n", session.Status)
		fmt.Printf("Container: %s\n", session.ContainerID[:12])

		if len(session.Services) == 0 {
			fmt.Println("  No services")
		} else {
			fmt.Println("Services:")
			for _, svc := range session.Services {
				// Generate DNS name
				dnsName := docker.GetSessionDNSName(session, svc)
				fmt.Printf("  - %-15s → %s\n", svc.Name, dnsName)
			}
		}
	}

	return nil
}
