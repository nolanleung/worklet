package worklet

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var (
	switchPrintPath bool
)

var switchCmd = &cobra.Command{
	Use:   "switch [fork-id|name|index]",
	Short: "Switch to a fork and run it in Docker",
	Long: `Switch to a fork directory by ID, name, or index number and run it in a Docker container.
	
Examples:
  worklet switch                    # Interactive selection
  worklet switch fork-abc123        # By full fork ID
  worklet switch abc123             # By partial fork ID
  worklet switch 1                  # By index from list
  worklet switch my-project         # By fork name
  worklet switch --print-path 1     # Just print the path (for scripting)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var selectedFork *fork.ForkInfo
		var err error

		if len(args) == 0 {
			// Interactive mode
			selectedFork, err = selectForkInteractive()
			if err != nil {
				return err
			}
		} else {
			// Direct selection mode
			selectedFork, err = fork.FindFork(args[0])
			if err != nil {
				return err
			}
		}

		// If --print-path flag is set, just print the path
		if switchPrintPath {
			fmt.Println(selectedFork.Path)
			return nil
		}

		// Otherwise, run the fork in Docker
		fmt.Fprintf(os.Stderr, "Switching to fork: %s (%s)\n", selectedFork.Name, selectedFork.SessionID)
		
		// Run the Docker container in the fork directory
		return RunInDirectory(selectedFork.Path)
	},
}

func init() {
	switchCmd.Flags().BoolVar(&switchPrintPath, "print-path", false, "Print the fork path instead of running in Docker")
}

func selectForkInteractive() (*fork.ForkInfo, error) {
	forks, err := fork.ListForks()
	if err != nil {
		return nil, fmt.Errorf("failed to list forks: %w", err)
	}

	if len(forks) == 0 {
		return nil, fmt.Errorf("no forks found")
	}

	// If only one fork exists, select it automatically
	if len(forks) == 1 {
		return &forks[0], nil
	}

	// Display forks
	fmt.Fprintln(os.Stderr, "Select a fork to switch to:")
	fmt.Fprintln(os.Stderr)
	
	for i, f := range forks {
		age := formatAge(f.CreatedAt)
		size := formatSize(f.Size)
		fmt.Fprintf(os.Stderr, "[%d] %-20s (%s) - %s - %s\n", 
			i+1, truncate(f.Name, 20), f.SessionID, age, size)
		if f.Description != "" {
			fmt.Fprintf(os.Stderr, "    %s\n", f.Description)
		}
	}
	
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, "Enter number, fork ID, or name: ")

	// Read user input
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}
	
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("no selection made")
	}

	// Try to parse as number first
	if num, err := strconv.Atoi(input); err == nil {
		if num >= 1 && num <= len(forks) {
			return &forks[num-1], nil
		}
		return nil, fmt.Errorf("invalid selection: %d", num)
	}

	// Otherwise use FindFork for flexible matching
	return fork.FindFork(input)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}