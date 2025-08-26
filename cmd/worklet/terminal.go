package worklet

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/nolanleung/worklet/pkg/terminal"
	"github.com/spf13/cobra"
)

var terminalCmd = &cobra.Command{
	Use:   "terminal",
	Short: "Manage the web-based terminal server for container access",
	Long:  `Manage the web-based terminal server that provides access to container shells through a browser interface.`,
}

var (
	terminalPort       int
	openBrowser        bool
	terminalCORSOrigin string
	proxyEnabled       bool
)

var terminalStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the terminal server",
	Long:  `Start a web-based terminal server that provides access to container shells through a browser interface.`,
	RunE:  runTerminal,
}

var terminalStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the terminal server",
	Long:  `Stop the running terminal server.`,
	RunE:  stopTerminal,
}

func init() {
	// Add subcommands
	terminalCmd.AddCommand(terminalStartCmd)
	terminalCmd.AddCommand(terminalStopCmd)
	
	// For backward compatibility, also allow running terminal directly
	terminalCmd.RunE = runTerminal
	
	// Add flags to both terminal and terminal start commands
	for _, cmd := range []*cobra.Command{terminalCmd, terminalStartCmd} {
		cmd.Flags().IntVarP(&terminalPort, "port", "p", 8181, "Port to run the terminal server on")
		cmd.Flags().BoolVarP(&openBrowser, "open", "o", true, "Open browser automatically")
		cmd.Flags().StringVar(&terminalCORSOrigin, "cors-origin", "*", "CORS allowed origin (use '*' to allow all origins)")
		cmd.Flags().BoolVar(&proxyEnabled, "proxy", false, "Enable reverse proxy for *.local.worklet.sh domains")
	}
	
	rootCmd.AddCommand(terminalCmd)
}

func runTerminal(cmd *cobra.Command, args []string) error {
	// Clean any stale lock files first
	if err := terminal.CleanStaleLockFile(); err != nil {
		return fmt.Errorf("failed to clean stale lock file: %w", err)
	}

	// Check if terminal server is already running
	lockInfo, running, err := terminal.IsTerminalRunning()
	if err != nil {
		return fmt.Errorf("failed to check terminal status: %w", err)
	}

	if running && lockInfo != nil {
		return fmt.Errorf("terminal server is already running on port %d (PID: %d)", lockInfo.Port, lockInfo.PID)
	}

	// Create lock file before starting server
	if err := terminal.CreateLockFile(terminalPort); err != nil {
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	// Set up signal handler to clean up lock file
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		terminal.RemoveLockFile()
		os.Exit(0)
	}()

	// Ensure we remove lock file on exit
	defer terminal.RemoveLockFile()

	server := terminal.NewServer(terminalPort)

	// Configure CORS
	server.SetCORSOrigin(terminalCORSOrigin)

	url := fmt.Sprintf("http://localhost:%d", terminalPort)
	fmt.Printf("Starting terminal server on %s\n", url)
	fmt.Printf("CORS origin: %s\n", terminalCORSOrigin)
	fmt.Println("\n💡 Tip: Press 's' in the terminal to open the session in VSCode")

	// Open browser if requested
	if openBrowser {
		go func() {
			// Small delay to ensure server is started
			time.Sleep(500 * time.Millisecond)
			openURL(url)
		}()
	}

	// Start server (blocks)
	return server.Start()
}

func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}

func stopTerminal(cmd *cobra.Command, args []string) error {
	// Check if terminal server is running
	lockInfo, running, err := terminal.IsTerminalRunning()
	if err != nil {
		return fmt.Errorf("failed to check terminal status: %w", err)
	}

	if !running || lockInfo == nil {
		fmt.Println("Terminal server is not running")
		return nil
	}

	// Try to stop the process
	process, err := os.FindProcess(lockInfo.PID)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := process.Signal(os.Interrupt); err != nil {
		// If SIGTERM fails, try SIGKILL
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to stop terminal server: %w", err)
		}
	}

	// Remove lock file
	if err := terminal.RemoveLockFile(); err != nil {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}

	fmt.Printf("Terminal server stopped (was running on port %d)\n", lockInfo.Port)
	return nil
}
