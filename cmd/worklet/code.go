package worklet

import (
	"fmt"

	"github.com/nolanleung/worklet/pkg/terminal"
	"github.com/spf13/cobra"
)

var codeCmd = &cobra.Command{
	Use:   "code [session-id]",
	Short: "Open a worklet session in VSCode",
	Long: `Opens a worklet session in VSCode using the Dev Containers extension.
	
If no session ID is provided, it will attempt to open the most recent session.
VSCode must be installed with the Dev Containers extension enabled.

Example:
  worklet code abc123  # Opens session abc123 in VSCode
  worklet code         # Opens the most recent session`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var sessionID string
		
		if len(args) > 0 {
			sessionID = args[0]
		} else {
			// Get the most recent session
			sessions, err := terminal.ListSessions()
			if err != nil {
				return fmt.Errorf("failed to list sessions: %w", err)
			}
			
			if len(sessions) == 0 {
				return fmt.Errorf("no active sessions found. Run 'worklet run' to start a session")
			}
			
			// Use the first session (most recent)
			sessionID = sessions[0].ID
			fmt.Printf("Opening most recent session: %s\n", sessionID)
		}
		
		// Get container ID for the session
		containerID, err := terminal.GetContainerID(sessionID)
		if err != nil {
			return fmt.Errorf("failed to get container for session %s: %w", sessionID, err)
		}
		
		// Launch VSCode with extension support
		if err := terminal.LaunchVSCode(containerID); err != nil {
			return fmt.Errorf("failed to launch VSCode: %w", err)
		}
		
		fmt.Println("✓ VSCode launched with your extensions")
		fmt.Println("\nNote: Extensions will auto-install on first connection.")
		return nil
	},
}


func init() {
	// This will be added to root command in root.go
}