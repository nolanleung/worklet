package worklet

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/nolanleung/worklet/internal/projects"
	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage worklet projects",
	Long:  `Manage worklet project history and settings.`,
}

var projectsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	Long:  `List all projects in the worklet history.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := projects.NewManager()
		if err != nil {
			return fmt.Errorf("failed to initialize project manager: %w", err)
		}

		projectList := manager.List()
		if len(projectList) == 0 {
			fmt.Println("No projects found.")
			return nil
		}

		// Create a tabwriter for aligned output
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH\tLAST ACCESSED\tRUNS")
		fmt.Fprintln(w, "----\t----\t-------------\t----")

		for _, p := range projectList {
			name := p.Name
			if name == "" {
				name = filepath.Base(p.Path)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", 
				name, 
				p.Path, 
				formatTime(p.LastAccessed),
				p.RunCount)
		}

		w.Flush()
		return nil
	},
}

var projectsRemoveCmd = &cobra.Command{
	Use:   "remove <path>",
	Short: "Remove a project from history",
	Long:  `Remove a project from the worklet history.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := projects.NewManager()
		if err != nil {
			return fmt.Errorf("failed to initialize project manager: %w", err)
		}

		path := args[0]
		if err := manager.Remove(path); err != nil {
			return fmt.Errorf("failed to remove project: %w", err)
		}

		fmt.Printf("Project removed: %s\n", path)
		return nil
	},
}

var projectsClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all project history",
	Long:  `Clear all projects from the worklet history.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Ask for confirmation
		fmt.Print("Are you sure you want to clear all project history? (y/N): ")
		var response string
		fmt.Scanln(&response)
		
		if response != "y" && response != "Y" {
			fmt.Println("Operation cancelled.")
			return nil
		}

		manager, err := projects.NewManager()
		if err != nil {
			return fmt.Errorf("failed to initialize project manager: %w", err)
		}

		if err := manager.Clear(); err != nil {
			return fmt.Errorf("failed to clear projects: %w", err)
		}

		fmt.Println("All project history cleared.")
		return nil
	},
}

var projectsInfoCmd = &cobra.Command{
	Use:   "info [path]",
	Short: "Show detailed information about a project",
	Long:  `Show detailed information about a specific project. If no path is provided, uses the current directory.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		manager, err := projects.NewManager()
		if err != nil {
			return fmt.Errorf("failed to initialize project manager: %w", err)
		}

		project, err := manager.GetProject(path)
		if err != nil {
			return fmt.Errorf("project not found in history: %w", err)
		}

		fmt.Printf("Project Information:\n")
		fmt.Printf("  Name:          %s\n", project.Name)
		fmt.Printf("  Path:          %s\n", project.Path)
		fmt.Printf("  Last Accessed: %s\n", project.LastAccessed.Format(time.RFC3339))
		fmt.Printf("  Run Count:     %d\n", project.RunCount)
		
		if project.ForkID != "" {
			fmt.Printf("  Fork ID:       %s\n", project.ForkID)
			if project.IsRunning {
				fmt.Printf("  Status:        Running\n")
			} else {
				fmt.Printf("  Status:        Stopped\n")
			}
		}

		return nil
	},
}

var projectsCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove projects with non-existent directories",
	Long:  `Clean up the project history by removing entries for directories that no longer exist.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		manager, err := projects.NewManager()
		if err != nil {
			return fmt.Errorf("failed to initialize project manager: %w", err)
		}

		// Get current list
		before := len(manager.List())

		if err := manager.CleanStale(); err != nil {
			return fmt.Errorf("failed to clean stale projects: %w", err)
		}

		// Get updated list
		after := len(manager.List())
		removed := before - after

		if removed > 0 {
			fmt.Printf("Removed %d stale project(s).\n", removed)
		} else {
			fmt.Println("No stale projects found.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(projectsCmd)
	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsRemoveCmd)
	projectsCmd.AddCommand(projectsClearCmd)
	projectsCmd.AddCommand(projectsInfoCmd)
	projectsCmd.AddCommand(projectsCleanCmd)
}

func formatTime(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		return fmt.Sprintf("%d min ago", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(duration.Hours()))
	} else {
		return fmt.Sprintf("%d days ago", int(duration.Hours()/24))
	}
}