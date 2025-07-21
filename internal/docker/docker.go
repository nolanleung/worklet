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

func RunContainer(workDir string, cfg *config.WorkletConfig, forkID string, mountMode bool, cmdArgs ...string) error {
	var imageName string
	var err error

	// In copy mode, build a temporary image with the workspace files
	if !mountMode {
		imageName, err = buildCopyImage(workDir, cfg, forkID)
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
		imageName = cfg.Run.Image
		if imageName == "" {
			imageName = "docker:dind"
		}
	}

	// Build docker run command
	args := []string{"run", "--rm", "-it"}

	// Add container name if we have services configured
	if len(cfg.Services) > 0 {
		// For now, use the first service name as the container name
		// In the future, we might want to run multiple containers
		containerName := fmt.Sprintf("worklet-%s-%s", forkID, cfg.Services[0].Name)
		args = append(args, "--name", containerName)

		// Add to worklet network
		args = append(args, "--network", "worklet-network")

		// Expose ports
		for _, service := range cfg.Services {
			args = append(args, "-p", fmt.Sprintf("%d:%d", service.Port, service.Port))
		}
	}

	// Add worklet fork labels for terminal discovery
	args = append(args, "--label", "worklet.fork=true")
	args = append(args, "--label", fmt.Sprintf("worklet.fork.id=%s", forkID))

	// In mount mode, add volume mount
	if mountMode {
		absWorkDir, err := filepath.Abs(workDir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		args = append(args, "-v", fmt.Sprintf("%s:/workspace", absWorkDir))
	}

	// Always set working directory
	args = append(args, "-w", "/workspace")

	// Determine isolation mode (default to "full" if not specified)
	isolation := cfg.Run.Isolation
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
		if cfg.Run.Privileged {
			args = append(args, "--privileged")
		}

	default:
		return fmt.Errorf("invalid isolation mode: %s (must be 'full' or 'shared')", isolation)
	}

	// Add environment variables
	for key, value := range cfg.Run.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Build init script
	var initScripts []string

	// Add user-provided init script
	if len(cfg.Run.InitScript) > 0 {
		initScripts = append(initScripts, cfg.Run.InitScript...)
	}

	// Add credential init script if needed
	if cfg.Run.Credentials != nil && cfg.Run.Credentials.Claude {
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
	for _, volume := range cfg.Run.Volumes {
		args = append(args, "-v", volume)
	}

	// Add credential volumes if configured
	if cfg.Run.Credentials != nil && cfg.Run.Credentials.Claude {
		credentialMounts := GetCredentialVolumeMounts(true)
		args = append(args, credentialMounts...)
	}

	// Add image (use temporary image in copy mode, configured image in mount mode)
	args = append(args, imageName)

	// Add command
	if len(cmdArgs) > 0 {
		// Use provided command arguments
		args = append(args, cmdArgs...)
	} else if len(cfg.Run.Command) > 0 {
		// Fall back to config command
		args = append(args, cfg.Run.Command...)
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
func buildCopyImage(workDir string, cfg *config.WorkletConfig, forkID string) (string, error) {
	// Generate unique image name
	imageName := fmt.Sprintf("worklet-temp-%s", forkID)

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

	// Copy files to build context, respecting .dockerignore and fork exclude patterns
	if err := copyWorkspace(workDir, workspaceDir, cfg.Fork.Exclude); err != nil {
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

	// Add patterns from config (fork.exclude)
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
