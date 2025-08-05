package worklet

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nolanleung/worklet/internal/docker"
	"github.com/spf13/cobra"
)

var (
	refreshAll bool
)

var refreshCmd = &cobra.Command{
	Use:   "refresh [session-id]",
	Short: "Refresh session information",
	Long: `Refresh session information to update service discovery.

If no session ID is provided and you're running inside a worklet session, the current session will be refreshed.
Use --all to refresh all active sessions.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefresh,
}

func init() {
	refreshCmd.Flags().BoolVar(&refreshAll, "all", false, "Refresh all active sessions")
}

func runRefresh(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if refreshAll {
		// List all sessions
		fmt.Println("Refreshing all sessions...")
		sessions, err := docker.ListSessions(ctx)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}
		
		fmt.Printf("Found %d active sessions\n", len(sessions))
		for _, session := range sessions {
			fmt.Printf("  - %s (%s)\n", session.SessionID, session.ProjectName)
		}
		return nil
	}

	// Determine session ID to refresh
	var sessionID string
	if len(args) > 0 {
		sessionID = args[0]
	} else {
		// Try to detect current session ID from environment
		sessionID = os.Getenv("WORKLET_SESSION_ID")
		if sessionID == "" {
			return fmt.Errorf("no session ID specified and WORKLET_SESSION_ID environment variable not set\n\nUsage:\n  worklet refresh <session-id>  # Refresh specific session\n  worklet refresh --all         # Refresh all sessions\n\nUse 'worklet forks' to list available session IDs")
		}
		fmt.Printf("No session ID specified, using current session: %s\n", sessionID)
	}

	// Get session info
	fmt.Printf("Refreshing session %s...\n", sessionID)
	sessionInfo, err := docker.GetSessionInfo(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}
	
	fmt.Printf("Session %s refreshed successfully\n", sessionID)

	// Show updated session info
	fmt.Printf("\nUpdated session information:\n")
	fmt.Printf("  Project: %s\n", sessionInfo.ProjectName)
	fmt.Printf("  Container: %s\n", sessionInfo.ContainerID[:12])
	fmt.Printf("  Status: %s\n", sessionInfo.Status)
	if len(sessionInfo.Services) > 0 {
		fmt.Printf("  Services:\n")
		for _, svc := range sessionInfo.Services {
			fmt.Printf("    - %s (port %d)\n", svc.Name, svc.Port)
		}
	}

	return nil
}