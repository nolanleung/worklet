package worklet

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var (
	mountMode bool
	forkMode  bool
	noFork    bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the repository in an isolated fork with Docker-in-Docker support",
	Long:  `Creates an isolated copy (fork) of the repository and runs it in a Docker container with Docker-in-Docker capabilities based on .worklet.jsonc configuration.

By default, worklet run creates a fork to ensure your source files are not modified. Use --no-fork to run directly in the source directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Handle conflicting flags
		if noFork {
			forkMode = false
		}

		// If fork mode is enabled, create a fork first
		if forkMode {
			// Load config to get fork settings
			cfg, err := config.LoadConfig(cwd)
			if err != nil {
				return fmt.Errorf("failed to load .worklet.jsonc: %w", err)
			}

			// Create a fork with auto-generated name
			fmt.Println("Creating isolated fork for this run...")
			forkPath, err := fork.CreateFork(cwd, cfg)
			if err != nil {
				return fmt.Errorf("failed to create fork: %w", err)
			}

			// Extract fork ID from path (e.g., "fork-123" from the directory name)
			forkID := filepath.Base(forkPath)
			fmt.Printf("Running in fork: %s\n", forkID)
			
			// Run in the fork directory
			return RunInDirectoryWithForkID(forkPath, forkID)
		}

		// Otherwise run in the current directory
		return RunInDirectory(cwd)
	},
}

func init() {
	runCmd.Flags().BoolVar(&mountMode, "mount", true, "Mount workspace directory instead of copying files (allows real-time sync)")
	runCmd.Flags().BoolVar(&forkMode, "fork", true, "Create an isolated fork before running (default: true)")
	runCmd.Flags().BoolVar(&noFork, "no-fork", false, "Run directly in source directory without creating a fork")
}

// RunInDirectory runs worklet in the specified directory
func RunInDirectory(dir string) error {
	return RunInDirectoryWithForkID(dir, "")
}

// RunInDirectoryWithForkID runs worklet in the specified directory with an optional fork ID
func RunInDirectoryWithForkID(dir string, forkID string) error {
	// Load config
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("failed to load .worklet.jsonc: %w", err)
	}

	// Generate a simple fork ID if not provided
	if forkID == "" {
		// For direct run command, use a simple ID
		forkID = "run-" + generateSimpleID()
	}

	// Run in Docker
	if err := docker.RunContainer(dir, cfg, forkID, mountMode); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	return nil
}

func generateSimpleID() string {
	// Simple ID generation for run command
	return fmt.Sprintf("%d", os.Getpid())
}