package worklet

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nolanleung/worklet/internal/projects"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var rootCmd = &cobra.Command{
	Use:   "worklet",
	Short: "A CLI tool for running projects in Docker containers",
	Long:  `Worklet helps you run projects in Docker containers with Docker-in-Docker support.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check if we're in an interactive terminal
		if !isInteractiveTerminal() {
			// Non-interactive mode: show project list and instructions
			return showNonInteractiveProjectList()
		}

		return RunCLI()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(terminalCmd)
	rootCmd.AddCommand(refreshCmd)
	rootCmd.AddCommand(forksCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(sshCmd)
	rootCmd.AddCommand(codeCmd)
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(versionCmd)
}

// isInteractiveTerminal checks if we're running in an interactive terminal
func isInteractiveTerminal() bool {
	// Check if stdin is a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}

	// Check if we can open /dev/tty (required for bubbletea)
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	tty.Close()

	return true
}

// showNonInteractiveProjectList displays projects in non-interactive mode
func showNonInteractiveProjectList() error {
	manager, err := projects.NewManager()
	if err != nil {
		return fmt.Errorf("failed to initialize project manager: %w", err)
	}

	projectList := manager.List()
	if len(projectList) == 0 {
		fmt.Println("No projects found.")
		fmt.Println("\nTo add a project, run 'worklet run' in a directory with .worklet.jsonc")
		return nil
	}

	fmt.Println("Worklet Projects:")
	fmt.Println()

	for i, p := range projectList {
		name := p.Name
		if name == "" {
			name = filepath.Base(p.Path)
		}
		fmt.Printf("%d. %s\n   %s\n", i+1, name, p.Path)
		if i >= 9 {
			// Limit to 10 projects in non-interactive mode
			remaining := len(projectList) - 10
			if remaining > 0 {
				fmt.Printf("\n... and %d more projects\n", remaining)
			}
			break
		}
	}

	fmt.Println("\nTo run a project:")
	fmt.Println("  cd <project-path> && worklet run")
	fmt.Println("\nTo see all projects:")
	fmt.Println("  worklet projects list")

	return nil
}
