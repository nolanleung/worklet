package worklet

import (
	"fmt"
	"os"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var forkCmd = &cobra.Command{
	Use:   "fork",
	Short: "Fork the current repository to ~/.worklet/forks/",
	Long:  `Creates a copy of the current repository in ~/.worklet/forks/ with a unique temporary directory and session lock file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Load config
		cfg, err := config.LoadConfig(cwd)
		if err != nil {
			return fmt.Errorf("failed to load .worklet.jsonc: %w", err)
		}

		// Create fork
		forkPath, err := fork.CreateFork(cwd, cfg)
		if err != nil {
			return fmt.Errorf("failed to create fork: %w", err)
		}

		fmt.Printf("Fork created at: %s\n", forkPath)
		return nil
	},
}