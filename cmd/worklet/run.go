package worklet

import (
	"fmt"
	"os"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the repository in a Docker container with Docker-in-Docker support",
	Long:  `Executes the repository in a Docker container with Docker-in-Docker capabilities based on .worklet.jsonc configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		return RunInDirectory(cwd)
	},
}

// RunInDirectory runs worklet in the specified directory
func RunInDirectory(dir string) error {
	// Load config
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("failed to load .worklet.jsonc: %w", err)
	}

	// Run in Docker
	if err := docker.RunContainer(dir, cfg); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	return nil
}