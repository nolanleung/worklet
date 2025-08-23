package worklet

import (
	"context"
	"fmt"
	"time"

	"github.com/nolanleung/worklet/internal/version"
	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display version information for the worklet CLI and daemon.`,
	RunE:  runVersion,
}

var daemonVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show daemon version",
	Long:  `Display version information for the running worklet daemon.`,
	RunE:  runDaemonVersion,
}

func init() {
	// Add daemon version subcommand to daemon command
	daemonCmd.AddCommand(daemonVersionCmd)
}

func runVersion(cmd *cobra.Command, args []string) error {
	// Show CLI version
	versionInfo := version.GetInfo()
	fmt.Printf("Worklet CLI\n")
	fmt.Printf("  Version: %s\n", versionInfo.Version)
	if versionInfo.BuildTime != "unknown" {
		fmt.Printf("  Build Time: %s\n", versionInfo.BuildTime)
	}
	if versionInfo.GitCommit != "unknown" {
		fmt.Printf("  Git Commit: %s\n", versionInfo.GitCommit)
	}
	
	// Try to get daemon version if running
	socketPath := daemon.GetDefaultSocketPath()
	if daemon.IsDaemonRunning(socketPath) {
		client := daemon.NewClient(socketPath)
		if err := client.Connect(); err == nil {
			defer client.Close()
			
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			if daemonVersion, err := client.GetVersion(ctx); err == nil {
				fmt.Printf("\nWorklet Daemon\n")
				fmt.Printf("  Version: %s\n", daemonVersion.Version)
				if daemonVersion.StartTime != "" {
					if startTime, err := time.Parse(time.RFC3339, daemonVersion.StartTime); err == nil {
						uptime := time.Since(startTime)
						fmt.Printf("  Uptime: %s\n", formatDuration(uptime))
					}
				}
			}
		}
	} else {
		fmt.Printf("\nWorklet Daemon: not running\n")
	}
	
	return nil
}

func runDaemonVersion(cmd *cobra.Command, args []string) error {
	socketPath := daemon.GetDefaultSocketPath()
	
	if !daemon.IsDaemonRunning(socketPath) {
		return fmt.Errorf("daemon is not running")
	}
	
	client := daemon.NewClient(socketPath)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	versionInfo, err := client.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get daemon version: %w", err)
	}
	
	fmt.Printf("Daemon Version: %s\n", versionInfo.Version)
	if versionInfo.BuildTime != "" && versionInfo.BuildTime != "unknown" {
		fmt.Printf("Build Time: %s\n", versionInfo.BuildTime)
	}
	if versionInfo.GitCommit != "" && versionInfo.GitCommit != "unknown" {
		fmt.Printf("Git Commit: %s\n", versionInfo.GitCommit)
	}
	if versionInfo.StartTime != "" {
		if startTime, err := time.Parse(time.RFC3339, versionInfo.StartTime); err == nil {
			uptime := time.Since(startTime)
			fmt.Printf("Started: %s\n", startTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("Uptime: %s\n", formatDuration(uptime))
		}
	}
	
	return nil
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}