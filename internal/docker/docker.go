package docker

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/internal/env"
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

// RunContainer runs a container in detached mode and returns the container ID
func RunContainer(opts RunOptions) (string, error) {
	var imageName string
	var err error

	// Ensure session-specific Docker network exists before running container
	if err := EnsureSessionNetworkExists(opts.SessionID); err != nil {
		return "", fmt.Errorf("failed to ensure session Docker network exists: %w", err)
	}

	// In copy mode, build a temporary image with the workspace files
	if !opts.MountMode {
		imageName, err = buildCopyImage(opts.WorkDir, opts.Config, opts.SessionID)
		if err != nil {
			return "", fmt.Errorf("failed to build copy image: %w", err)
		}
		// Note: We don't clean up the image here since container will be running
	} else {
		// In mount mode, use the configured image
		imageName = opts.Config.Run.Image
		if imageName == "" {
			imageName = "worklet/base:latest"
		}

		// Process environment templates for mount mode (write to host directory)
		if err := processEnvironmentTemplates(opts.WorkDir, opts.WorkDir, opts); err != nil {
			// Log warning but don't fail the container start
			fmt.Printf("Warning: Failed to process environment templates: %v\n", err)
		}
	}

	// Build docker run command for detached mode
	args := []string{"run", "-d"}

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
			return "", fmt.Errorf("failed to get absolute path: %w", err)
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

		// Set Docker environment variables for DinD
		args = append(args, "-e", "DOCKER_TLS_CERTDIR=")
		args = append(args, "-e", "DOCKER_DRIVER=overlay2")

		// Create volume for Docker data
		args = append(args, "-v", fmt.Sprintf("worklet-%s:/var/lib/docker", opts.SessionID))

		// In mount mode, we need to mount the entrypoint script since it's not in the base image
		// In copy mode, the entrypoint script is already included in the built image
		if opts.MountMode {
			// Mount the entrypoint script
			scriptPath, err := getEntrypointScriptPath()
			if err != nil {
				return "", fmt.Errorf("failed to get entrypoint script path: %w", err)
			}
			// Ensure cleanup of temp script file
			defer os.Remove(scriptPath)

			args = append(args, "-v", fmt.Sprintf("%s:/entrypoint.sh:ro", scriptPath))
		}

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
		return "", fmt.Errorf("invalid isolation mode: %s (must be 'full' or 'shared')", isolation)
	}

	// Add environment variables
	for key, value := range opts.Config.Run.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Add service environment variables from templating
	serviceEnvVars := getServiceEnvironmentVariables(opts.Config, opts.SessionID)
	for key, value := range serviceEnvVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// Disable Corepack prompts for Node.js projects
	args = append(args, "-e", "COREPACK_ENABLE_DOWNLOAD_PROMPT=0")

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

	// Add credential init scripts if needed
	if opts.Config.Run.Credentials != nil {
		// Add Claude credential init script
		if opts.Config.Run.Credentials.Claude {
			if credInitScript := GetCredentialInitScript(true); credInitScript != "" {
				// Prepend credential setup to ensure it runs first
				initScripts = append([]string{credInitScript}, initScripts...)
			}
		}
		
		// Add SSH credential init script
		if opts.Config.Run.Credentials.SSH {
			if sshInitScript := GetSSHInitScript(true); sshInitScript != "" {
				// Prepend SSH setup to ensure it runs early
				initScripts = append([]string{sshInitScript}, initScripts...)
			}
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

	// Add pnpm store volume if this is a pnpm project
	if _, err := os.Stat(filepath.Join(opts.WorkDir, "pnpm-lock.yaml")); err == nil {
		pnpmStoreVolume := fmt.Sprintf("worklet-pnpm-store-%s", projectName)
		if err := ensureDockerVolumeExists(pnpmStoreVolume); err != nil {
			return "", fmt.Errorf("failed to create pnpm store volume: %w", err)
		}
		args = append(args, "-v", fmt.Sprintf("%s:/pnpm/store", pnpmStoreVolume))
	}

	// Add credential volumes if configured
	if opts.Config.Run.Credentials != nil {
		// Mount Claude credentials
		if opts.Config.Run.Credentials.Claude {
			credentialMounts := GetCredentialVolumeMounts(true)
			args = append(args, credentialMounts...)
		}
		
		// Mount SSH credentials
		if opts.Config.Run.Credentials.SSH {
			sshMounts := GetSSHVolumeMounts(true)
			args = append(args, sshMounts...)
		}
	}

	// Add image (use temporary image in copy mode, configured image in mount mode)
	args = append(args, imageName)

	// For detached mode, use a long-running command if no command specified
	if len(opts.CmdArgs) > 0 {
		args = append(args, opts.CmdArgs...)
	} else if len(opts.Config.Run.Command) > 0 {
		args = append(args, opts.Config.Run.Command...)
	} else {
		// Default to sleep for detached containers
		args = append(args, "sleep", "infinity")
	}

	// Execute docker command and capture output to get container ID
	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("docker command failed: %w\nStderr: %s", err, exitErr.Stderr)
		}
		return "", fmt.Errorf("docker command failed: %w", err)
	}

	// Extract container ID from output
	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return "", fmt.Errorf("failed to get container ID from docker run output")
	}

	// Set up devcontainer configuration for VSCode support
	projectName = opts.Config.Name
	if projectName == "" {
		projectName = "worklet"
	}
	
	// Generate and write devcontainer.json (non-blocking, best effort)
	go func() {
		// Small delay to ensure container is fully started
		time.Sleep(1 * time.Second)
		if err := EnsureDevContainerConfig(containerID, projectName); err != nil {
			// Log warning but don't fail - VSCode will still work without it
			fmt.Printf("Note: Could not set up VSCode extensions auto-sync: %v\n", err)
		}
	}()

	return containerID, nil
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
	imageName := fmt.Sprintf("worklet-temp-%s-%s", strings.ToLower(projectName), sessionID)

	// Get base image
	baseImage := cfg.Run.Image
	if baseImage == "" {
		baseImage = "worklet/base:latest"
	}

	// Create temporary directory for build context
	buildDir, err := os.MkdirTemp("", "worklet-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	// Write the entrypoint script to the build context
	entrypointPath := filepath.Join(buildDir, "entrypoint.sh")
	if err := os.WriteFile(entrypointPath, []byte(dindEntrypointScript), 0755); err != nil {
		return "", fmt.Errorf("failed to write entrypoint script: %w", err)
	}

	// Create Dockerfile
	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	dockerfileContent := fmt.Sprintf(`FROM %s
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
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

	// Process environment templates for copy mode (write to build context, not host)
	opts := RunOptions{
		WorkDir:   workDir,
		Config:    cfg,
		SessionID: sessionID,
	}
	if err := processEnvironmentTemplates(workDir, workspaceDir, opts); err != nil {
		// Log warning but don't fail the build
		fmt.Printf("Warning: Failed to process environment templates: %v\n", err)
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

		// Check if path should be excluded BEFORE following symlinks
		if matcher.Match(pathComponents, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if this is a symlink
		linkInfo, err := os.Lstat(path)
		if err != nil {
			return err
		}

		// Construct destination path
		dstPath := filepath.Join(dst, relPath)

		// Handle symlinks specially
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			// Read the symlink target
			target, err := os.Readlink(path)
			if err != nil {
				// If we can't read the symlink, skip it
				fmt.Printf("Warning: Skipping unreadable symlink: %s\n", relPath)
				return nil
			}

			// Resolve the absolute path of the target
			absoluteTarget := target
			if !filepath.IsAbs(target) {
				absoluteTarget = filepath.Join(filepath.Dir(path), target)
			}

			// Check if the target is within the source directory
			absTarget, err := filepath.Abs(absoluteTarget)
			if err != nil {
				// Skip symlinks we can't resolve
				fmt.Printf("Warning: Skipping unresolvable symlink: %s\n", relPath)
				return nil
			}

			absSrc, err := filepath.Abs(src)
			if err != nil {
				return err
			}

			// If the symlink points outside the workspace, skip it
			if !strings.HasPrefix(absTarget, absSrc) {
				fmt.Printf("Info: Skipping symlink pointing outside workspace: %s -> %s\n", relPath, target)
				return nil
			}

			// For symlinks pointing inside the workspace, copy as regular files/directories
			// This handles the case where symlinks point to already copied content
			targetInfo, err := os.Stat(path)
			if err != nil {
				// If we can't stat the target, skip the symlink
				fmt.Printf("Warning: Skipping broken symlink: %s\n", relPath)
				return nil
			}

			if targetInfo.IsDir() {
				// For directory symlinks, create the directory but don't recurse
				// The actual content will be copied when we walk to the real directory
				return os.MkdirAll(dstPath, targetInfo.Mode())
			}

			// For file symlinks, copy the file content
			return copyFile(path, dstPath)
		}

		// Handle regular files and directories
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		return copyFile(path, dstPath)
	})
}

// ensureDockerVolumeExists creates a Docker volume if it doesn't exist
func ensureDockerVolumeExists(volumeName string) error {
	// Check if volume already exists
	cmd := exec.Command("docker", "volume", "inspect", volumeName)
	if err := cmd.Run(); err == nil {
		// Volume already exists
		return nil
	}

	// Create the volume
	cmd = exec.Command("docker", "volume", "create", volumeName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create Docker volume %s: %w", volumeName, err)
	}

	return nil
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

// processEnvironmentTemplates processes .env.example files with templating
// srcDir is where to read .env.example files from
// targetDir is where to write processed .env files to
func processEnvironmentTemplates(srcDir, targetDir string, opts RunOptions) error {
	// Only process templates if we have services defined
	if len(opts.Config.Services) == 0 {
		return nil
	}

	// Get project name
	projectName := opts.Config.Name
	if projectName == "" {
		projectName = "worklet"
	}

	// Process env files with templating
	return config.ProcessEnvFilesWithTemplating(
		srcDir,
		targetDir,
		opts.SessionID,
		projectName,
		opts.Config.Services,
	)
}

// getServiceEnvironmentVariables generates environment variables for services
func getServiceEnvironmentVariables(cfg *config.WorkletConfig, sessionID string) map[string]string {
	// Convert ServiceConfig to env.ServiceInfo
	var serviceInfos []env.ServiceInfo
	for _, svc := range cfg.Services {
		serviceInfos = append(serviceInfos, env.ServiceInfo{
			Name:      svc.Name,
			Port:      svc.Port,
			Subdomain: svc.Subdomain,
		})
	}

	// Get project name
	projectName := cfg.Name
	if projectName == "" {
		projectName = "worklet"
	}

	// Create template context
	ctx := env.TemplateContext{
		SessionID:   sessionID,
		ProjectName: projectName,
		Services:    serviceInfos,
	}

	// Get service environment variables
	return env.GetServiceEnvironmentVariables(ctx)
}
