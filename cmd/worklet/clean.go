package worklet

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var (
	cleanForce     bool
	cleanDryRun    bool
	cleanOlderThan int
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove all forks",
	Long:  `Remove all forks or forks older than a specified number of days.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get all forks
		allForks, err := fork.ListForks()
		if err != nil {
			return fmt.Errorf("failed to list forks: %w", err)
		}

		if len(allForks) == 0 {
			fmt.Println("No forks to clean.")
			return nil
		}

		// Filter by age if specified
		var forksToClean []fork.ForkInfo
		cutoffTime := time.Now().AddDate(0, 0, -cleanOlderThan)
		
		for _, f := range allForks {
			if cleanOlderThan > 0 && f.CreatedAt.After(cutoffTime) {
				continue
			}
			forksToClean = append(forksToClean, f)
		}

		if len(forksToClean) == 0 {
			fmt.Printf("No forks older than %d days found.\n", cleanOlderThan)
			return nil
		}

		// Show what will be removed
		if cleanOlderThan > 0 {
			fmt.Printf("The following forks older than %d days will be removed:\n", cleanOlderThan)
		} else {
			fmt.Println("The following forks will be removed:")
		}
		fmt.Println()

		var totalSize int64
		for _, f := range forksToClean {
			age := formatAge(f.CreatedAt)
			fmt.Printf("  • %s (%s) - %s - %s\n", f.SessionID, f.Name, formatSize(f.Size), age)
			totalSize += f.Size
		}

		fmt.Printf("\nTotal: %d forks\n", len(forksToClean))
		fmt.Printf("Total space to be freed: %s\n", formatSize(totalSize))

		// If dry run, stop here
		if cleanDryRun {
			fmt.Println("\n(Dry run - no changes made)")
			return nil
		}

		// Confirm unless --force is used
		if !cleanForce {
			fmt.Print("\nAre you sure you want to remove all these forks? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}

			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				fmt.Println("Clean cancelled.")
				return nil
			}
		}

		// Remove all forks
		fmt.Println("\nRemoving forks...")
		progressBar := createSimpleProgressBar(len(forksToClean))
		
		var errors []error
		removed := 0
		for i, f := range forksToClean {
			if err := fork.RemoveFork(f.SessionID); err != nil {
				errors = append(errors, fmt.Errorf("failed to remove %s: %w", f.SessionID, err))
			} else {
				removed++
			}
			progressBar(i + 1)
		}
		fmt.Println() // New line after progress

		// Report results
		if removed > 0 {
			fmt.Printf("\nSuccessfully removed %d fork(s).\n", removed)
			fmt.Printf("Freed %s of disk space.\n", formatSize(totalSize))
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
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Skip confirmation prompt")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview what would be deleted without making changes")
	cleanCmd.Flags().IntVar(&cleanOlderThan, "older-than", 0, "Only remove forks older than specified days")
}

func createSimpleProgressBar(total int) func(current int) {
	return func(current int) {
		percentage := float64(current) / float64(total) * 100
		fmt.Printf("\r[%-50s] %.0f%%", strings.Repeat("=", int(percentage/2)), percentage)
	}
}