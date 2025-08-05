package worklet

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/internal/projects"
	"github.com/nolanleung/worklet/pkg/terminal"
	"github.com/spf13/cobra"
)

var (
	mountMode       bool
	tempMode        bool
	withTerminal    bool
	noTerminal      bool
	openTerminal    bool
	runTerminalPort int
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
	return runInDirectoryWithMode(dir, false, cmdArgs...)
}

// RunInBackground runs worklet in the specified directory in detached mode
func RunInBackground(dir string, cmdArgs ...string) error {
	return runInDirectoryWithMode(dir, true, cmdArgs...)
}

// AttachToContainer attaches to an existing container for a session
func AttachToContainer(sessionID string) error {
	// Try to find the container by session ID label
	checkCmd := exec.Command("docker", "ps", "-q", "-f", fmt.Sprintf("label=worklet.session.id=%s", sessionID))
	output, err := checkCmd.Output()
	if err != nil || len(output) == 0 {
		return fmt.Errorf("no running container found for session %s", sessionID)
	}
	
	containerID := strings.TrimSpace(string(output))
	
	// Get container name for display
	nameCmd := exec.Command("docker", "inspect", "-f", "{{.Name}}", containerID)
	nameOutput, _ := nameCmd.Output()
	containerName := strings.TrimPrefix(strings.TrimSpace(string(nameOutput)), "/")
	
	// Try to attach using docker attach command
	cmd := exec.Command("docker", "attach", containerID)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	fmt.Printf("Attaching to container %s...\n", containerName)
	fmt.Println("Tip: Use Ctrl+P, Ctrl+Q to detach without stopping the container")
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to attach to container: %w", err)
	}
	
	return nil
}

// runInDirectoryWithMode runs worklet with specified attach mode
func runInDirectoryWithMode(dir string, detached bool, cmdArgs ...string) error {
	// Load config
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("failed to load .worklet.jsonc: %w", err)
	}

	// Track project in history
	if manager, err := projects.NewManager(); err == nil {
		projectName := cfg.Name
		if projectName == "" {
			projectName = filepath.Base(dir)
		}
		manager.AddOrUpdate(dir, projectName)
	}

	// Get session ID from daemon or generate fallback
	sessionID := getSessionID()

	// Handle terminal server if enabled
	shouldStartTerminal := withTerminal && !noTerminal
	if shouldStartTerminal {
		if err := startOrConnectTerminalServer(sessionID); err != nil {
			// Don't fail the run command if terminal server fails
			log.Printf("Warning: Failed to start terminal server: %v", err)
		}
	}

	// Get isolation mode
	isolation := cfg.Run.Isolation
	if isolation == "" {
		isolation = "full"
	}

	// Start docker-compose services if configured
	composeStarted := false
	composePath := getComposePath(dir, cfg)
	if composePath != "" {
		projectName := cfg.Name
		if projectName == "" {
			projectName = "worklet"
		}

		if err := docker.StartComposeServices(dir, composePath, sessionID, projectName, isolation); err != nil {
			log.Printf("Warning: Failed to start compose services: %v", err)
		} else {
			composeStarted = true
			if isolation == "full" {
				fmt.Printf("Docker-compose services will be started inside the container from: %s\n", composePath)
			} else {
				fmt.Printf("Started docker-compose services from: %s\n", composePath)
			}
		}
	}

	// Session discovery is now handled via Docker labels
	// No need for separate daemon registration

	// For detached mode, we don't want to cleanup on exit
	if !detached {
		// Ensure cleanup on exit
		defer func() {
			// Session cleanup is handled via Docker container stop/remove

			// Stop compose services if we started them
			if composeStarted && composePath != "" {
				projectName := cfg.Name
				if projectName == "" {
					projectName = "worklet"
				}

				if err := docker.StopComposeServices(dir, composePath, sessionID, projectName, isolation); err != nil {
					log.Printf("Warning: Failed to stop compose services: %v", err)
				}
			}

			// Clean up session network
			if err := docker.RemoveSessionNetwork(sessionID); err != nil {
				log.Printf("Warning: Failed to remove session network: %v", err)
			}
		}()
	}

	// Run in Docker
	opts := docker.RunOptions{
		WorkDir:     dir,
		Config:      cfg,
		SessionID:   sessionID,
		MountMode:   mountMode,
		ComposePath: composePath,
		CmdArgs:     cmdArgs,
		Detached:    detached,
	}
	
	if detached {
		containerID, err := docker.RunContainerDetached(opts)
		if err != nil {
			return fmt.Errorf("failed to run container: %w", err)
		}
		
		// Update project manager with container ID
		if manager, err := projects.NewManager(); err == nil {
			manager.UpdateForkStatus(dir, sessionID, true)
		}
		
		fmt.Printf("Container started in background with ID: %s\n", containerID[:12])
		fmt.Printf("Session ID: %s\n", sessionID)
		if shouldStartTerminal {
			fmt.Printf("Access terminal at: http://localhost:%d\n", runTerminalPort)
		}
		return nil
	}
	
	if err := docker.RunContainer(opts); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	return nil
}

func getSessionID() string {
	// Generate a new session ID using UUID
	// Format: first 8 characters of a UUID for readability
	id := uuid.New().String()
	return id[:8]
}

func getComposePath(workDir string, cfg *config.WorkletConfig) string {
	return docker.GetComposePath(workDir, cfg.Run.ComposePath)
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
