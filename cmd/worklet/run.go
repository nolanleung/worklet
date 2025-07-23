package worklet

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/nolanleung/worklet/pkg/terminal"
	"github.com/spf13/cobra"
)

var (
	mountMode          bool
	tempMode           bool
	withTerminal       bool
	noTerminal         bool
	openTerminal       bool
	runTerminalPort    int
)

var runCmd = &cobra.Command{
	Use:   "run [command]",
	Short: "Run the repository in a Docker container with Docker-in-Docker support",
	Long: `Runs the repository in a Docker container with Docker-in-Docker capabilities based on .worklet.jsonc configuration.

By default, worklet run creates a persistent isolated environment. Use --mount to run directly in the current directory, or --temp to create a temporary environment that auto-cleans up.

Examples:
  worklet run                    # Run in persistent isolated environment
  worklet run --mount            # Run with current directory mounted
  worklet run --temp             # Run in temporary environment
  worklet run echo "hello"       # Run echo command
  worklet run python app.py      # Run Python script
  worklet run npm test           # Run npm test`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Handle conflicting flags
		if withTerminal && noTerminal {
			withTerminal = false
		}

		// If mount mode, run directly in current directory
		if mountMode {
			return RunInDirectory(cwd, args...)
		}

		// TODO: Implement isolated environment creation
		// For now, just run in current directory
		fmt.Println("Note: Isolated environment creation not yet implemented, running in current directory")
		return RunInDirectory(cwd, args...)
	},
}

func init() {
	runCmd.Flags().BoolVar(&mountMode, "mount", false, "Mount current directory instead of creating isolated environment")
	runCmd.Flags().BoolVar(&tempMode, "temp", false, "Create temporary environment that auto-cleans up")
	runCmd.Flags().BoolVarP(&withTerminal, "with-terminal", "t", true, "Start terminal server for web-based container access")
	runCmd.Flags().BoolVar(&noTerminal, "no-terminal", false, "Disable terminal server")
	runCmd.Flags().BoolVar(&openTerminal, "open-terminal", false, "Open terminal in browser automatically")
	runCmd.Flags().IntVar(&runTerminalPort, "terminal-port", 8181, "Port for terminal server (default: 8181)")
}

// RunInDirectory runs worklet in the specified directory
func RunInDirectory(dir string, cmdArgs ...string) error {
	// Load config
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("failed to load .worklet.jsonc: %w", err)
	}

	// Generate session ID
	sessionID := generateSessionID()

	// Handle terminal server if enabled
	shouldStartTerminal := withTerminal && !noTerminal
	if shouldStartTerminal {
		if err := startOrConnectTerminalServer(sessionID); err != nil {
			// Don't fail the run command if terminal server fails
			log.Printf("Warning: Failed to start terminal server: %v", err)
		}
	}

	// Register with daemon if it's running
	registerWithDaemon(sessionID, dir, cfg)
	
	// Ensure we unregister on exit
	defer func() {
		unregisterFromDaemon(sessionID)
	}()

	// Run in Docker
	if err := docker.RunContainer(dir, cfg, sessionID, mountMode, cmdArgs...); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	return nil
}

func generateSessionID() string {
	// Simple ID generation based on PID
	return fmt.Sprintf("%d", os.Getpid())
}

func registerWithDaemon(sessionID, workDir string, cfg *config.WorkletConfig) {
	socketPath := daemon.GetDefaultSocketPath()
	
	// Check if daemon is running
	if !daemon.IsDaemonRunning(socketPath) {
		// Try to auto-start daemon
		if err := autoStartDaemon(); err != nil {
			log.Printf("Failed to auto-start daemon: %v", err)
			return
		}
	}
	
	// Connect to daemon
	client := daemon.NewClient(socketPath)
	if err := client.Connect(); err != nil {
		log.Printf("Failed to connect to daemon: %v", err)
		return
	}
	defer client.Close()
	
	// Prepare service info
	var services []daemon.ServiceInfo
	for _, svc := range cfg.Services {
		services = append(services, daemon.ServiceInfo{
			Name:      svc.Name,
			Port:      svc.Port,
			Subdomain: svc.Subdomain,
		})
	}
	
	// Register session (still using RegisterFork for now - will be updated when we refactor daemon)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Get project name from config
	projectName := cfg.Name
	if projectName == "" {
		projectName = "worklet"
	}
	
	req := daemon.RegisterForkRequest{
		ForkID:      sessionID,
		ProjectName: projectName,
		WorkDir:     workDir,
		Services:    services,
	}
	
	if err := client.RegisterFork(ctx, req); err != nil {
		log.Printf("Failed to register session with daemon: %v", err)
		return
	}
	
	// If we have services, display the URLs
	if len(services) > 0 {
		// Get project name from config
		projectName := cfg.Name
		if projectName == "" {
			projectName = "worklet"
		}
		
		fmt.Println("\nServices available at (via proxy on port 80):")
		for _, svc := range services {
			subdomain := svc.Subdomain
			if subdomain == "" {
				subdomain = svc.Name
			}
			fmt.Printf("  - http://%s.%s-%s.%s\n", subdomain, projectName, sessionID, config.WorkletDomain)
		}
		fmt.Println()
		fmt.Println("Note: Services are only accessible through the nginx proxy, not directly on host ports.")
	}
}

func unregisterFromDaemon(sessionID string) {
	socketPath := daemon.GetDefaultSocketPath()
	
	// Check if daemon is running
	if !daemon.IsDaemonRunning(socketPath) {
		return
	}
	
	// Connect to daemon
	client := daemon.NewClient(socketPath)
	if err := client.Connect(); err != nil {
		return
	}
	defer client.Close()
	
	// Unregister session (still using UnregisterFork for now - will be updated when we refactor daemon)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.UnregisterFork(ctx, sessionID); err != nil {
		log.Printf("Failed to unregister session from daemon: %v", err)
	}
}

func autoStartDaemon() error {
	// Get executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	
	// Start daemon
	cmd := exec.Command(exePath, "daemon", "start")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	
	// Wait for daemon to be ready
	socketPath := daemon.GetDefaultSocketPath()
	for i := 0; i < 10; i++ {
		if daemon.IsDaemonRunning(socketPath) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	
	return fmt.Errorf("daemon failed to start")
}

func startOrConnectTerminalServer(sessionID string) error {
	// Clean any stale lock files first
	if err := terminal.CleanStaleLockFile(); err != nil {
		return fmt.Errorf("failed to clean stale lock file: %w", err)
	}

	// Check if terminal server is already running
	lockInfo, running, err := terminal.IsTerminalRunning()
	if err != nil {
		return fmt.Errorf("failed to check terminal status: %w", err)
	}

	var port int
	if running && lockInfo != nil {
		// Terminal server is already running
		port = lockInfo.Port
		fmt.Printf("Terminal already running at: http://localhost:%d\n", port)
		fmt.Printf("Connect to session: %s\n", sessionID)
	} else {
		// Start new terminal server
		port = runTerminalPort
		if err := startTerminalServer(port); err != nil {
			return fmt.Errorf("failed to start terminal server: %w", err)
		}
		fmt.Printf("Starting terminal server at: http://localhost:%d\n", port)
		fmt.Printf("Connect to session: %s\n", sessionID)
	}

	// Open browser if requested
	if openTerminal {
		url := fmt.Sprintf("http://localhost:%d", port)
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowserURL(url)
		}()
	}

	return nil
}

func startTerminalServer(port int) error {
	// Check if port is available
	if !isPortAvailable(port) {
		return fmt.Errorf("port %d is already in use", port)
	}

	// Get executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start terminal server in background
	cmd := exec.Command(exePath, "terminal", "-p", fmt.Sprintf("%d", port), "--cors-origin", "*")
	
	// Set up to run in background
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start terminal server: %w", err)
	}

	// Create lock file with the process PID
	if err := terminal.CreateLockFile(port); err != nil {
		// Try to kill the process if lock file creation fails
		cmd.Process.Kill()
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	// Wait a bit to ensure server is ready
	time.Sleep(500 * time.Millisecond)

	return nil
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func openBrowserURL(url string) error {
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
