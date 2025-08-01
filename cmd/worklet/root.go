package worklet

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "worklet",
	Short: "A CLI tool for running projects in Docker containers",
	Long:  `Worklet helps you run projects in Docker containers with Docker-in-Docker support.`,
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
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(terminalCmd)
	rootCmd.AddCommand(refreshCmd)
	rootCmd.AddCommand(forksCmd)
}