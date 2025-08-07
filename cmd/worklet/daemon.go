package worklet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the worklet daemon",
	Long:  `Start, stop, and manage the worklet daemon that handles fork registrations and proxy routing.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the worklet daemon",
	Long:  `Start the worklet daemon in the background. The daemon manages fork registrations and enables automatic proxy routing.`,
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the worklet daemon",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check daemon status",
	RunE:  runDaemonStatus,
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View daemon logs",
	RunE:  runDaemonLogs,
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the worklet daemon",
	Long:  `Stop the worklet daemon if running, then start it again. This is useful for applying configuration changes or recovering from issues.`,
	RunE:  runDaemonRestart,
}

var (
	daemonForeground bool
)

func init() {
	daemonStartCmd.Flags().BoolVar(&daemonForeground, "foreground", false, "Run daemon in foreground")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	socketPath := daemon.GetDefaultSocketPath()

	// Check if daemon is already running
	if daemon.IsDaemonRunning(socketPath) {
		return fmt.Errorf("daemon is already running")
	}

	if daemonForeground {
		// Run in foreground
		return runDaemonForeground(socketPath)
	}

	// Start daemon in background
	return StartDaemonBackground(socketPath)
}

func runDaemonForeground(socketPath string) error {
	d := daemon.NewDaemon(socketPath)

	if err := d.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	fmt.Println("Daemon started in foreground mode")
	fmt.Println("Press Ctrl+C to stop")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down daemon...")
	return d.Stop()
}

// StartDaemonBackground starts the daemon process in the background
func StartDaemonBackground(socketPath string) error {
	// Get executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Prepare log file
	homeDir, _ := os.UserHomeDir()
	logDir := filepath.Join(homeDir, ".worklet", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile := filepath.Join(logDir, "daemon.log")

	// Start daemon process
	cmd := exec.Command(exePath, "daemon", "start", "--foreground")

	// Redirect output to log file
	outFile, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer outFile.Close()

	cmd.Stdout = outFile
	cmd.Stderr = outFile

	// Start process in background
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	// Save PID
	pidFile := filepath.Join(homeDir, ".worklet", "daemon.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		// Try to kill the process if we can't save the PID
		cmd.Process.Kill()
		return fmt.Errorf("failed to save daemon PID: %w", err)
	}

	// Wait a moment to ensure daemon starts successfully
	time.Sleep(2 * time.Second)

	// Check if daemon is running
	if !daemon.IsDaemonRunning(socketPath) {
		return fmt.Errorf("daemon failed to start (check logs at %s)", logFile)
	}

	fmt.Printf("Daemon started successfully (PID: %d)\n", cmd.Process.Pid)
	fmt.Printf("Logs: %s\n", logFile)
	fmt.Printf("Nginx proxy will be available on port 80\n")

	return nil
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	pidFile := filepath.Join(homeDir, ".worklet", "daemon.pid")

	// Read PID
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("daemon is not running (PID file not found)")
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	// Find process
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find daemon process: %w", err)
	}

	// Send termination signal
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		os.Remove(pidFile)
		return fmt.Errorf("daemon is not running")
	}

	// Wait for process to exit
	done := make(chan bool)
	go func() {
		process.Wait()
		done <- true
	}()

	select {
	case <-done:
		fmt.Println("Daemon stopped successfully")
	case <-time.After(5 * time.Second):
		// Force kill if not stopped
		process.Kill()
		fmt.Println("Daemon force stopped")
	}

	// Clean up PID file
	os.Remove(pidFile)

	return nil
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	socketPath := daemon.GetDefaultSocketPath()

	if !daemon.IsDaemonRunning(socketPath) {
		fmt.Println("Daemon is not running")
		return nil
	}

	fmt.Println("Daemon is running")

	// Connect to daemon and get fork list
	client := daemon.NewClient(socketPath)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	forks, err := client.ListForks(ctx)
	if err != nil {
		return fmt.Errorf("failed to list forks: %w", err)
	}

	fmt.Printf("\nRegistered forks: %d\n", len(forks))
	if len(forks) > 0 {
		fmt.Println("\nFork ID          Container ID     Services")
		fmt.Println("---------------- ---------------- --------")
		for _, fork := range forks {
			services := ""
			for i, svc := range fork.Services {
				if i > 0 {
					services += ", "
				}
				services += fmt.Sprintf("%s:%d", svc.Name, svc.Port)
			}
			fmt.Printf("%-16s %-16s %s\n", fork.ForkID, fork.ContainerID[:12], services)
		}
	}

	return nil
}

func runDaemonLogs(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	logFile := filepath.Join(homeDir, ".worklet", "logs", "daemon.log")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", logFile)
	}

	// Use tail to show logs
	tailCmd := exec.Command("tail", "-f", "-n", "100", logFile)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr

	return tailCmd.Run()
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	socketPath := daemon.GetDefaultSocketPath()
	
	// Stop daemon if running
	if daemon.IsDaemonRunning(socketPath) {
		fmt.Println("Stopping existing daemon...")
		if err := runDaemonStop(cmd, args); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}
		
		// Wait a moment for cleanup
		time.Sleep(1 * time.Second)
	}
	
	// Start daemon
	fmt.Println("Starting daemon...")
	return runDaemonStart(cmd, args)
}
