package worklet

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/pkg/daemon"
	"github.com/spf13/cobra"
)

var (
	mountMode bool
	tempMode  bool
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
