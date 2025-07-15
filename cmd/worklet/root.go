package worklet

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "worklet",
	Short: "A CLI tool for managing repository forks and Docker containers",
	Long:  `Worklet helps you fork repositories and run them in Docker containers with Docker-in-Docker support.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(forkCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(switchCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(proxyCmd)
}