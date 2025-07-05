package worklet

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var removeForce bool

var removeCmd = &cobra.Command{
	Use:   "remove [fork-ids...]",
	Short: "Remove specific forks",
	Long:  `Remove one or more forks by their session IDs.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get all forks to validate IDs and show details
		allForks, err := fork.ListForks()
		if err != nil {
			return fmt.Errorf("failed to list forks: %w", err)
		}

		// Create a map for quick lookup
		forksMap := make(map[string]fork.ForkInfo)
		for _, f := range allForks {
			forksMap[f.SessionID] = f
		}

		// Validate all fork IDs exist
		var forksToRemove []fork.ForkInfo
		for _, id := range args {
			if f, exists := forksMap[id]; exists {
				forksToRemove = append(forksToRemove, f)
			} else {
				return fmt.Errorf("fork %s not found", id)
			}
		}

		// Show what will be removed
		fmt.Println("The following forks will be removed:")
		fmt.Println()
		var totalSize int64
		for _, f := range forksToRemove {
			fmt.Printf("  • %s (%s) - %s\n", f.SessionID, f.Name, formatSize(f.Size))
			totalSize += f.Size
		}
		fmt.Printf("\nTotal space to be freed: %s\n", formatSize(totalSize))

		// Confirm unless --force is used
		if !removeForce {
			fmt.Print("\nAre you sure you want to remove these forks? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}

			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				fmt.Println("Removal cancelled.")
				return nil
			}
		}

		// Remove the forks
		var errors []error
		removed := 0
		for _, f := range forksToRemove {
			if err := fork.RemoveFork(f.SessionID); err != nil {
				errors = append(errors, fmt.Errorf("failed to remove %s: %w", f.SessionID, err))
			} else {
				removed++
			}
		}

		// Report results
		if removed > 0 {
			fmt.Printf("\nSuccessfully removed %d fork(s).\n", removed)
		}

		if len(errors) > 0 {
			fmt.Println("\nErrors occurred:")
			for _, err := range errors {
				fmt.Printf("  • %v\n", err)
			}
			return fmt.Errorf("failed to remove some forks")
		}

		return nil
	},
}

func init() {
	removeCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "Skip confirmation prompt")
}