package docker

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/nolanleung/worklet/internal/config"
)

//go:embed dind-entrypoint.sh
var dindEntrypointScript string

// RunOptions contains all options for running a container
type RunOptions struct {
	WorkDir     string
	Config      *config.WorkletConfig
	SessionID   string
	MountMode   bool
	ComposePath string // Resolved compose path
	CmdArgs     []string
}

func RunContainer(opts RunOptions) error {
	var imageName string
	var err error

	// Ensure session-specific Docker network exists before running container
	if err := EnsureSessionNetworkExists(opts.SessionID); err != nil {
		return fmt.Errorf("failed to ensure session Docker network exists: %w", err)
	}

	// In copy mode, build a temporary image with the workspace files
	if !opts.MountMode {
		imageName, err = buildCopyImage(opts.WorkDir, opts.Config, opts.SessionID)
		if err != nil {
			return fmt.Errorf("failed to build copy image: %w", err)
		}
		// Ensure cleanup of temporary image
		defer func() {
			if removeErr := removeImage(imageName); removeErr != nil {
				fmt.Printf("Warning: Failed to remove temporary image %s: %v\n", imageName, removeErr)
			}
		}()
	} else {
		// In mount mode, use the configured image
		imageName = opts.Config.Run.Image
		if imageName == "" {
			imageName = "docker:dind"
		}
	}

	// Build docker run command
	args := []string{"run", "--rm", "-it"}

	// Add container name using project name and session ID
	projectName := opts.Config.Name
	if projectName == "" {
		projectName = "worklet"
	}
	containerName := fmt.Sprintf("%s-%s", projectName, opts.SessionID)
	args = append(args, "--name", containerName)

	// Add to session-specific worklet network for container-to-container communication
	networkName := GetSessionNetworkName(opts.SessionID)
	args = append(args, "--network", networkName)

	// Add worklet labels for terminal discovery
	args = append(args, "--label", "worklet.session=true")
	args = append(args, "--label", fmt.Sprintf("worklet.session.id=%s", opts.SessionID))
	args = append(args, "--label", fmt.Sprintf("worklet.project.name=%s", projectName))
	args = append(args, "--label", fmt.Sprintf("worklet.workdir=%s", opts.WorkDir))

	// Add service labels for discovery
	for _, svc := range opts.Config.Services {
		args = append(args, "--label", fmt.Sprintf("worklet.service.%s.port=%d", svc.Name, svc.Port))
		args = append(args, "--label", fmt.Sprintf("worklet.service.%s.subdomain=%s", svc.Name, svc.Subdomain))
	}

	// In mount mode, add volume mount
	if opts.MountMode {
		absWorkDir, err := filepath.Abs(opts.WorkDir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		args = append(args, "-v", fmt.Sprintf("%s:/workspace", absWorkDir))
	}

	// Always set working directory
	args = append(args, "-w", "/workspace")

	// Determine isolation mode (default to "full" if not specified)
	isolation := opts.Config.Run.Isolation
	if isolation == "" {
		isolation = "full"
	}

	// Configure based on isolation mode
	switch isolation {
	case "full":
		// Full isolation with Docker-in-Docker
		// Always need privileged for DinD
		args = append(args, "--privileged")

		// Set isolation mode environment variable
		args = append(args, "-e", "WORKLET_ISOLATION=full")

		// Mount the entrypoint script
		scriptPath, err := getEntrypointScriptPath()
		if err != nil {
			return fmt.Errorf("failed to get entrypoint script path: %w", err)
		}
		// Ensure cleanup of temp script file
		defer os.Remove(scriptPath)

		args = append(args, "-v", fmt.Sprintf("%s:/entrypoint.sh:ro", scriptPath))

		// Set entrypoint
		args = append(args, "--entrypoint", "/entrypoint.sh")

	case "shared":
		// Shared Docker daemon via socket mount
		args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock")

		// Add privileged flag if specified
		if opts.Config.Run.Privileged {
			args = append(args, "--privileged")
		}

	default:
		return fmt.Errorf("invalid isolation mode: %s (must be 'full' or 'shared')", isolation)
	}

	// Add environment variables
	for key, value := range opts.Config.Run.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Add session ID environment variable
	args = append(args, "-e", fmt.Sprintf("WORKLET_SESSION_ID=%s", opts.SessionID))
	
	// Add project name environment variable
	args = append(args, "-e", fmt.Sprintf("WORKLET_PROJECT_NAME=%s", projectName))

	// Mount compose file if configured and in full isolation mode
	if opts.ComposePath != "" && isolation == "full" {
		// Check if compose file exists
		if _, err := os.Stat(opts.ComposePath); err == nil {
			// Mount the compose file into the container
			args = append(args, "-v", fmt.Sprintf("%s:/workspace/docker-compose.yml:ro", opts.ComposePath))
			args = append(args, "-e", "WORKLET_COMPOSE_FILE=/workspace/docker-compose.yml")
		} else {
			fmt.Printf("Warning: Compose file not found: %s\n", opts.ComposePath)
		}
	}

	// Build init script
	var initScripts []string

	// Add user-provided init script
	if len(opts.Config.Run.InitScript) > 0 {
		initScripts = append(initScripts, opts.Config.Run.InitScript...)
	}

	// Add credential init script if needed
	if opts.Config.Run.Credentials != nil && opts.Config.Run.Credentials.Claude {
		if credInitScript := GetCredentialInitScript(true); credInitScript != "" {
			// Prepend credential setup to ensure it runs first
			initScripts = append([]string{credInitScript}, initScripts...)
		}
	}

	// Set combined init script if we have any
	if len(initScripts) > 0 {
		initScript := strings.Join(initScripts, " && ")
		args = append(args, "-e", fmt.Sprintf("WORKLET_INIT_SCRIPT=%s", initScript))
	}

	// Add additional volumes
	for _, volume := range opts.Config.Run.Volumes {
		args = append(args, "-v", volume)
	}

	// Add credential volumes if configured
	if opts.Config.Run.Credentials != nil && opts.Config.Run.Credentials.Claude {
		credentialMounts := GetCredentialVolumeMounts(true)
		args = append(args, credentialMounts...)
	}

	// Add image (use temporary image in copy mode, configured image in mount mode)
	args = append(args, imageName)

	// Add command
	if len(opts.CmdArgs) > 0 {
		// Use provided command arguments
		args = append(args, opts.CmdArgs...)
	} else if len(opts.Config.Run.Command) > 0 {
		// Fall back to config command
		args = append(args, opts.Config.Run.Command...)
	} else {
		// Default to shell
		args = append(args, "sh")
	}

	// Execute docker command
	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Running: docker %s\n", strings.Join(args, " "))

	// Run the container
	err = cmd.Run()

	if err != nil {
		return fmt.Errorf("docker command failed: %w", err)
	}

	return nil
}

// getEntrypointScriptPath extracts the embedded entrypoint script to a temp file
func getEntrypointScriptPath() (string, error) {
	// Create a temporary file for the script
	tmpFile, err := os.CreateTemp("", "worklet-dind-entrypoint-*.sh")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	// Write the embedded script content
	if _, err := tmpFile.WriteString(dindEntrypointScript); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write script: %w", err)
	}

	// Close the file
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// Make it executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to make script executable: %w", err)
	}

	return tmpFile.Name(), nil
}

// buildCopyImage builds a temporary Docker image with the workspace files copied in
func buildCopyImage(workDir string, cfg *config.WorkletConfig, sessionID string) (string, error) {
	// Generate unique image name
	projectName := cfg.Name
	if projectName == "" {
		projectName = "worklet"
	}
	imageName := fmt.Sprintf("worklet-temp-%s-%s", projectName, sessionID)

	// Get base image
	baseImage := cfg.Run.Image
	if baseImage == "" {
		baseImage = "docker:dind"
	}

	// Create temporary directory for build context
	buildDir, err := os.MkdirTemp("", "worklet-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	// Create Dockerfile
	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	dockerfileContent := fmt.Sprintf(`FROM %s
COPY workspace /workspace
WORKDIR /workspace
`, baseImage)

	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Create workspace directory in build context
	workspaceDir := filepath.Join(buildDir, "workspace")
	if err := os.Mkdir(workspaceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Copy files to build context, respecting .dockerignore patterns
	if err := copyWorkspace(workDir, workspaceDir, []string{}); err != nil {
		return "", fmt.Errorf("failed to copy workspace: %w", err)
	}

	// Build the image
	cmd := exec.Command("docker", "build", "-t", imageName, buildDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Building temporary image with copied files...\n")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build image: %w", err)
	}

	return imageName, nil
}

// removeImage removes a Docker image
func removeImage(imageName string) error {
	cmd := exec.Command("docker", "rmi", imageName)
	return cmd.Run()
}

// copyWorkspace copies files from source to destination, respecting exclude patterns
func copyWorkspace(src, dst string, excludePatterns []string) error {
	fmt.Printf("Copying workspace files from %s to %s...\n", src, dst)
	// Create gitignore patterns from config excludes
	var patterns []gitignore.Pattern

	// Always exclude .dockerignore itself
	patterns = append(patterns, gitignore.ParsePattern(".dockerignore", nil))

	// Add patterns from excludePatterns parameter
	for _, pattern := range excludePatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" && !strings.HasPrefix(pattern, "#") {
			patterns = append(patterns, gitignore.ParsePattern(pattern, nil))
		}
	}

	// Read and parse .dockerignore file if it exists
	dockerignorePath := filepath.Join(src, ".dockerignore")
	if data, err := os.ReadFile(dockerignorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}

	// Create matcher with all patterns
	matcher := gitignore.NewMatcher(patterns)

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Convert path to components for matcher
		pathComponents := strings.Split(relPath, string(filepath.Separator))

		// Check if path should be excluded
		if matcher.Match(pathComponents, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Construct destination path
		dstPath := filepath.Join(dst, relPath)

		// Create directory or copy file
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		return copyFile(path, dstPath)
	})
}

// copyFile copies a single file
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Copy file permissions
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, sourceInfo.Mode())
}
