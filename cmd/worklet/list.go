package worklet

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var (
	listJSON    bool
	listVerbose bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all repository forks",
	Long:  `Lists all forks created by worklet, showing their details and sizes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		forks, err := fork.ListForks()
		if err != nil {
			return fmt.Errorf("failed to list forks: %w", err)
		}

		if len(forks) == 0 {
			fmt.Println("No forks found.")
			return nil
		}

		if listJSON {
			return printForksJSON(forks)
		}

		return printForksTable(forks)
	},
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&listVerbose, "verbose", false, "Show detailed information")
}

func printForksJSON(forks []fork.ForkInfo) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(forks)
}

func printForksTable(forks []fork.ForkInfo) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	// Print header
	fmt.Fprintln(w, "FORK ID\tNAME\tCREATED\tSIZE\tSOURCE")
	fmt.Fprintln(w, "-------\t----\t-------\t----\t------")

	var totalSize int64
	for _, f := range forks {
		age := formatAge(f.CreatedAt)
		size := formatSize(f.Size)
		
		if listVerbose {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", f.SessionID, f.Name, age, size, f.SourcePath)
			if f.Description != "" {
				fmt.Fprintf(w, "\t%s\n", f.Description)
			}
		} else {
			// Truncate long paths
			sourcePath := f.SourcePath
			if len(sourcePath) > 40 {
				sourcePath = "..." + sourcePath[len(sourcePath)-37:]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", f.SessionID, f.Name, age, size, sourcePath)
		}
		
		totalSize += f.Size
	}

	w.Flush()
	
	// Print summary
	fmt.Printf("\nTotal: %d forks (%s)\n", len(forks), formatSize(totalSize))
	
	return nil
}

func formatAge(t time.Time) string {
	age := time.Since(t)
	
	if age < time.Minute {
		return "just now"
	} else if age < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(age.Minutes()))
	} else if age < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(age.Hours()))
	} else if age < 7*24*time.Hour {
		return fmt.Sprintf("%d days ago", int(age.Hours()/24))
	} else if age < 30*24*time.Hour {
		return fmt.Sprintf("%d weeks ago", int(age.Hours()/(24*7)))
	} else {
		return fmt.Sprintf("%d months ago", int(age.Hours()/(24*30)))
	}
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}