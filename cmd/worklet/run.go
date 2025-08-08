package worklet

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/google/uuid"
	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/internal/projects"
	"github.com/nolanleung/worklet/pkg/daemon"
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
	linkClaude      bool
)

var runCmd = &cobra.Command{
	Use:   "run [git-url|path] [command]",
	Short: "Run a repository or git URL in a detached Docker container with Docker-in-Docker support",
	Long: `Runs a repository in a detached Docker container with Docker-in-Docker capabilities based on .worklet.jsonc configuration.

All worklet sessions run in the background (detached mode). You can access running sessions through the terminal server or by using docker exec directly.

By default, worklet run creates a persistent isolated environment. Use --mount to run directly in the current directory, or --temp to create a temporary environment that auto-cleans up.

Examples:
  worklet run                                       # Run in persistent isolated environment
  worklet run --mount                               # Run with current directory mounted
  worklet run --temp                                # Run in temporary environment
  worklet run echo "hello"                          # Run echo command
  worklet run python app.py                         # Run Python script
  worklet run npm test                              # Run npm test
  worklet run https://github.com/user/repo          # Clone and run a git repository
  worklet run github.com/user/repo                  # Clone and run (shortened format)
  worklet run git@github.com:user/repo.git          # Clone and run (SSH format)
  worklet run github.com/user/repo#branch           # Clone specific branch
  worklet run github.com/user/repo@abc123def        # Clone specific commit`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Handle conflicting flags
		if withTerminal && noTerminal {
			withTerminal = false
		}

		var workDir string
		var cmdArgs []string
		var isClonedRepo bool
		var shouldCleanup bool

		// Check if first argument is a git URL
		if len(args) > 0 && isGitURL(args[0]) {
			// Parse the URL and any reference
			parsed := parseGitURLWithRef(args[0])

			// Extract repository name for temp directory
			repoName := extractRepoNameFromURL(parsed.URL)

			// Create temporary directory
			tempDir, err := createTempDirectory(repoName)
			if err != nil {
				return fmt.Errorf("failed to create temporary directory: %w", err)
			}

			// Clone the repository with optional reference
			if err := cloneRepository(parsed.URL, tempDir, parsed.Ref); err != nil {
				// Clean up on failure
				cleanupTempDirectory(tempDir)
				return fmt.Errorf("failed to clone repository: %w", err)
			}

			workDir = tempDir
			cmdArgs = args[1:] // Remove the URL from command args
			isClonedRepo = true
			shouldCleanup = tempMode || !mountMode // Clean up unless explicitly mounting

			// Config detection will happen automatically in RunInDirectory
		} else {
			// Use current directory
			var err error
			workDir, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
			cmdArgs = args
		}

		// Set up cleanup for cloned repositories
		if isClonedRepo && shouldCleanup {
			defer func() {
				if err := cleanupTempDirectory(workDir); err != nil {
					log.Printf("Warning: Failed to clean up temporary directory: %v", err)
				}
			}()
		}

		// If mount mode is explicitly set for a cloned repo, inform the user
		if mountMode && isClonedRepo {
			fmt.Printf("Repository cloned to: %s\n", workDir)
			fmt.Println("Note: Using --mount with a git URL will preserve the cloned directory")
		}

		// Run in the determined directory with cloned repo flag
		return runInDirectoryWithClonedFlag(workDir, isClonedRepo && linkClaude, cmdArgs...)
	},
}

func init() {
	runCmd.Flags().BoolVar(&mountMode, "mount", false, "Mount current directory instead of creating isolated environment")
	runCmd.Flags().BoolVar(&tempMode, "temp", false, "Create temporary environment that auto-cleans up")
	runCmd.Flags().BoolVarP(&withTerminal, "with-terminal", "t", true, "Start terminal server for web-based container access")
	runCmd.Flags().BoolVar(&noTerminal, "no-terminal", false, "Disable terminal server")
	runCmd.Flags().BoolVar(&openTerminal, "open-terminal", false, "Open terminal in browser automatically")
	runCmd.Flags().IntVar(&runTerminalPort, "terminal-port", 8181, "Port for terminal server (default: 8181)")
	runCmd.Flags().BoolVar(&linkClaude, "link-claude", true, "Automatically link Claude credentials for cloned repositories")
}

// RunInDirectory runs worklet in the specified directory (always detached)
func RunInDirectory(dir string, cmdArgs ...string) error {
	return runInDirectoryWithCloned(dir, false, cmdArgs...)
}

// runInDirectoryWithClonedFlag runs worklet with cloned repo flag (always detached)
func runInDirectoryWithClonedFlag(dir string, isClonedRepo bool, cmdArgs ...string) error {
	return runInDirectoryWithCloned(dir, isClonedRepo, cmdArgs...)
}

// AttachToContainer executes an interactive shell in an existing container for a session
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

	// Execute an interactive shell using docker exec
	cmd := exec.Command("docker", "exec", "-it", containerID, "/bin/sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Attaching to container %s...\n", containerName)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute shell in container: %w", err)
	}

	return nil
}

// runInDirectoryWithCloned runs worklet with cloned repo flag (always detached)
func runInDirectoryWithCloned(dir string, isClonedRepo bool, cmdArgs ...string) error {
	// Load config or detect project type
	cfg, err := config.LoadConfigOrDetect(dir, isClonedRepo)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Track project in history
	if manager, err := projects.NewManager(); err == nil {
		projectName := cfg.Name
		if projectName == "" {
			projectName = filepath.Base(dir)
		}
		manager.AddOrUpdate(dir, projectName)
	}

	// Ensure daemon is running for nginx proxy support
	if err := ensureDaemonRunning(); err != nil {
		log.Printf("Warning: Failed to start daemon: %v", err)
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
	composePath := getComposePath(dir, cfg)
	if composePath != "" {
		projectName := cfg.Name
		if projectName == "" {
			projectName = "worklet"
		}

		if err := docker.StartComposeServices(dir, composePath, sessionID, projectName, isolation); err != nil {
			log.Printf("Warning: Failed to start compose services: %v", err)
		} else {
			if isolation == "full" {
				fmt.Printf("Docker-compose services will be started inside the container from: %s\n", composePath)
			} else {
				fmt.Printf("Started docker-compose services from: %s\n", composePath)
			}
		}
	}

	// Session discovery is now handled via Docker labels
	// Sessions run detached, so no cleanup on exit needed

	// Run in Docker (always detached)
	opts := docker.RunOptions{
		WorkDir:     dir,
		Config:      cfg,
		SessionID:   sessionID,
		MountMode:   mountMode,
		ComposePath: composePath,
		CmdArgs:     cmdArgs,
	}

	containerID, err := docker.RunContainer(opts)
	if err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	// Update project manager with container ID
	if manager, err := projects.NewManager(); err == nil {
		manager.UpdateForkStatus(dir, sessionID, true)
	}

	// Trigger daemon discovery for immediate nginx update
	triggerDaemonDiscovery()

	fmt.Printf("Container started in background with ID: %s\n", containerID[:12])
	fmt.Printf("Session ID: %s\n", sessionID)
	
	// Get project name for URL generation
	projectName := cfg.Name
	if projectName == "" {
		projectName = "worklet"
	}
	
	// Display service URLs if services are defined
	if len(cfg.Services) > 0 {
		fmt.Println("Access your app at:")
		for _, svc := range cfg.Services {
			subdomain := svc.Subdomain
			if subdomain == "" {
				subdomain = svc.Name
			}
			url := fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", subdomain, projectName, sessionID)
			fmt.Printf("  - %s: %s (port %d)\n", svc.Name, url, svc.Port)
		}
	} else if shouldStartTerminal {
		// If no services defined but terminal is enabled, show terminal URL
		fmt.Printf("Access terminal at: http://localhost:%d\n", runTerminalPort)
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

// gitURLRef represents a git URL with an optional branch or commit reference
type gitURLRef struct {
	URL string
	Ref string // branch name or commit hash
}

// parseGitURLWithRef parses a git URL and extracts any branch or commit reference
func parseGitURLWithRef(urlStr string) *gitURLRef {
	result := &gitURLRef{}

	// Check for branch reference (# separator)
	if idx := strings.LastIndex(urlStr, "#"); idx != -1 {
		result.URL = urlStr[:idx]
		result.Ref = urlStr[idx+1:]
		return result
	}

	// Check for commit reference (@ separator)
	if idx := strings.LastIndex(urlStr, "@"); idx != -1 {
		// Make sure it's not part of git@ SSH URL
		if !strings.HasPrefix(urlStr, "git@") || strings.Count(urlStr[:idx], "@") > 0 {
			result.URL = urlStr[:idx]
			result.Ref = urlStr[idx+1:]
			return result
		}
	}

	// No reference specified
	result.URL = urlStr
	return result
}

// isCommitHash checks if a string looks like a git commit hash
func isCommitHash(ref string) bool {
	// Git commit hashes are 40 characters hex, but we also accept short hashes (min 7 chars)
	if len(ref) < 7 || len(ref) > 40 {
		return false
	}
	// Check if it's all hex characters
	if matched, _ := regexp.MatchString(`^[a-fA-F0-9]+$`, ref); matched {
		return true
	}
	return false
}

// isGitURL checks if the given string is a git URL
func isGitURL(arg string) bool {
	// First parse out any reference
	parsed := parseGitURLWithRef(arg)
	urlToCheck := parsed.URL

	// Check for common git URL patterns
	gitURLPatterns := []string{
		`^https?://`,                             // HTTP(S) URLs
		`^git@`,                                  // SSH URLs like git@github.com:user/repo.git
		`^ssh://`,                                // SSH URLs
		`^git://`,                                // Git protocol URLs
		`^(github\.com|gitlab\.com|bitbucket\.)`, // Common git hosting services without protocol
	}

	for _, pattern := range gitURLPatterns {
		if matched, _ := regexp.MatchString(pattern, urlToCheck); matched {
			return true
		}
	}

	// Also check if it looks like a github/gitlab shorthand (e.g., "user/repo")
	if matched, _ := regexp.MatchString(`^[\w-]+/[\w.-]+$`, urlToCheck); matched {
		return true
	}

	return false
}

// normalizeGitURL converts various git URL formats to a standard format
func normalizeGitURL(urlStr string) string {
	// Handle shortened formats like "github.com/user/repo"
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") &&
		!strings.HasPrefix(urlStr, "git@") && !strings.HasPrefix(urlStr, "ssh://") &&
		!strings.HasPrefix(urlStr, "git://") {
		// Check if it's a github/gitlab/bitbucket shorthand
		if strings.HasPrefix(urlStr, "github.com/") ||
			strings.HasPrefix(urlStr, "gitlab.com/") ||
			strings.HasPrefix(urlStr, "bitbucket.org/") {
			urlStr = "https://" + urlStr
		} else if matched, _ := regexp.MatchString(`^[\w-]+/[\w.-]+$`, urlStr); matched {
			// Assume GitHub for "user/repo" format
			urlStr = "https://github.com/" + urlStr
		}
	}

	// Ensure .git suffix for consistency
	if !strings.HasSuffix(urlStr, ".git") &&
		(strings.Contains(urlStr, "github.com") ||
			strings.Contains(urlStr, "gitlab.com") ||
			strings.Contains(urlStr, "bitbucket.org")) {
		urlStr += ".git"
	}

	return urlStr
}

// cloneRepository clones a git repository to a target directory with optional branch/commit
func cloneRepository(gitURL, targetDir, ref string) error {
	normalizedURL := normalizeGitURL(gitURL)

	if ref != "" {
		fmt.Printf("Cloning repository from %s (ref: %s)...\n", normalizedURL, ref)
	} else {
		fmt.Printf("Cloning repository from %s...\n", normalizedURL)
	}

	// Configure clone options
	cloneOpts := &git.CloneOptions{
		URL:      normalizedURL,
		Progress: os.Stdout,
	}

	// Handle branch vs commit reference
	if ref != "" {
		if isCommitHash(ref) {
			// For commits, we need the full history
			// Don't set Depth, clone all history
			fmt.Println("Cloning full history to checkout specific commit...")
		} else {
			// For branches, set the reference name
			cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
			cloneOpts.SingleBranch = true
			fmt.Printf("Cloning branch: %s\n", ref)
		}
	} else {
		// Default shallow clone for faster cloning when no ref specified
		cloneOpts.Depth = 1
	}

	// Try to determine and set up authentication
	auth, err := getGitAuth(normalizedURL)
	if err == nil && auth != nil {
		cloneOpts.Auth = auth
	}

	// Perform the clone
	repo, err := git.PlainClone(targetDir, false, cloneOpts)
	if err != nil {
		if err == transport.ErrAuthenticationRequired {
			return fmt.Errorf("authentication required to clone repository. Please ensure you have proper credentials configured")
		}
		if err == plumbing.ErrReferenceNotFound {
			return fmt.Errorf("branch '%s' not found in repository", ref)
		}
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	// If a commit hash was specified, checkout that commit
	if ref != "" && isCommitHash(ref) {
		fmt.Printf("Checking out commit: %s\n", ref)

		worktree, err := repo.Worktree()
		if err != nil {
			return fmt.Errorf("failed to get worktree: %w", err)
		}

		// Try to resolve the commit hash (supports short hashes)
		hash, err := repo.ResolveRevision(plumbing.Revision(ref))
		if err != nil {
			return fmt.Errorf("commit '%s' not found in repository: %w", ref, err)
		}

		// Checkout the specific commit
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: *hash,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout commit %s: %w", ref, err)
		}

		fmt.Printf("Checked out commit: %s\n", hash.String()[:7])
	}

	fmt.Println("Repository cloned successfully")
	return nil
}

// getGitAuth attempts to get authentication for git operations
func getGitAuth(gitURL string) (transport.AuthMethod, error) {
	// Parse the URL to determine the protocol
	u, err := url.Parse(gitURL)
	if err != nil {
		// Try alternative parsing for SSH URLs
		if strings.HasPrefix(gitURL, "git@") {
			// SSH authentication
			return getSshAuth()
		}
		return nil, err
	}

	switch u.Scheme {
	case "https", "http":
		// Try to get credentials from environment or git credential helper
		return getHttpAuth()
	case "ssh", "git":
		return getSshAuth()
	default:
		if strings.HasPrefix(gitURL, "git@") {
			return getSshAuth()
		}
	}

	return nil, nil
}

// getHttpAuth gets HTTP authentication from environment variables
func getHttpAuth() (transport.AuthMethod, error) {
	// Check for GitHub token
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &http.BasicAuth{
			Username: "oauth2",
			Password: token,
		}, nil
	}

	// Check for GitLab token
	if token := os.Getenv("GITLAB_TOKEN"); token != "" {
		return &http.BasicAuth{
			Username: "oauth2",
			Password: token,
		}, nil
	}

	// Check for generic git credentials
	if username := os.Getenv("GIT_USERNAME"); username != "" {
		if password := os.Getenv("GIT_PASSWORD"); password != "" {
			return &http.BasicAuth{
				Username: username,
				Password: password,
			}, nil
		}
	}

	return nil, nil
}

// getSshAuth gets SSH authentication
func getSshAuth() (transport.AuthMethod, error) {
	// Try to use SSH agent first
	auth, err := ssh.NewSSHAgentAuth("git")
	if err == nil {
		return auth, nil
	}

	// Fall back to default SSH key
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Try common SSH key locations
	keyPaths := []string{
		filepath.Join(homeDir, ".ssh", "id_rsa"),
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa"),
	}

	for _, keyPath := range keyPaths {
		if _, err := os.Stat(keyPath); err == nil {
			auth, err := ssh.NewPublicKeysFromFile("git", keyPath, "")
			if err == nil {
				return auth, nil
			}
		}
	}

	return nil, fmt.Errorf("no SSH key found")
}

// createTempDirectory creates a temporary directory for cloned repositories
func createTempDirectory(repoName string) (string, error) {
	// Create a base temporary directory for worklet
	baseDir := filepath.Join(os.TempDir(), "worklet-repos")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create base temp directory: %w", err)
	}

	// Extract repo name from URL if needed
	if repoName == "" {
		repoName = "repo"
	} else {
		// Clean the repo name to be filesystem-safe
		repoName = filepath.Base(repoName)
		repoName = strings.TrimSuffix(repoName, ".git")
	}

	// Create a unique directory name with timestamp
	timestamp := time.Now().Format("20060102-150405")
	dirName := fmt.Sprintf("%s-%s-%s", repoName, timestamp, uuid.New().String()[:8])
	tempDir := filepath.Join(baseDir, dirName)

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	return tempDir, nil
}

// cleanupTempDirectory removes a temporary directory
func cleanupTempDirectory(dir string) error {
	// Only clean up if it's in the worklet temp directory
	baseDir := filepath.Join(os.TempDir(), "worklet-repos")
	if !strings.HasPrefix(dir, baseDir) {
		return fmt.Errorf("refusing to clean non-temporary directory: %s", dir)
	}

	fmt.Printf("Cleaning up temporary directory: %s\n", dir)
	return os.RemoveAll(dir)
}

// ensureDaemonRunning ensures the daemon is running, starting it if necessary
func ensureDaemonRunning() error {
	socketPath := daemon.GetDefaultSocketPath()

	// Check if daemon is already running
	if daemon.IsDaemonRunning(socketPath) {
		return nil
	}

	// Start daemon in background
	log.Println("Starting worklet daemon for nginx proxy support...")
	if err := StartDaemonBackground(socketPath); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	log.Println("Worklet daemon started successfully")
	return nil
}

// triggerDaemonDiscovery tells the daemon to discover containers immediately
func triggerDaemonDiscovery() {
	socketPath := daemon.GetDefaultSocketPath()

	// Check if daemon is running
	if !daemon.IsDaemonRunning(socketPath) {
		return // Daemon not running, skip
	}

	// Create client and trigger discovery
	client := daemon.NewClient(socketPath)
	if err := client.Connect(); err != nil {
		log.Printf("Warning: Failed to connect to daemon for discovery trigger: %v", err)
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.TriggerDiscovery(ctx); err != nil {
		log.Printf("Warning: Failed to trigger daemon discovery: %v", err)
	}
}

// extractRepoNameFromURL extracts repository name from git URL
func extractRepoNameFromURL(gitURL string) string {
	// Normalize the URL first
	normalizedURL := normalizeGitURL(gitURL)

	// Try to parse as URL
	u, err := url.Parse(normalizedURL)
	if err == nil && u.Path != "" {
		// Extract the repository name from the path
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 {
			repoName := parts[len(parts)-1]
			return strings.TrimSuffix(repoName, ".git")
		}
	}

	// Handle SSH format (git@github.com:user/repo.git)
	if strings.HasPrefix(normalizedURL, "git@") {
		parts := strings.Split(normalizedURL, ":")
		if len(parts) == 2 {
			pathParts := strings.Split(parts[1], "/")
			if len(pathParts) > 0 {
				repoName := pathParts[len(pathParts)-1]
				return strings.TrimSuffix(repoName, ".git")
			}
		}
	}

	// Fallback to generic name
	return "repository"
}
