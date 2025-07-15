package docker

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/pkg/proxy"
)

//go:embed dind-entrypoint.sh
var dindEntrypointScript string

func RunContainer(workDir string, cfg *config.WorkletConfig, forkID string) error {
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

	// Add working directory mount
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	args = append(args, "-v", fmt.Sprintf("%s:/workspace", absWorkDir))
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

	// Add init script if provided
	if len(cfg.Run.InitScript) > 0 {
		// Join commands with && to ensure each succeeds
		initScript := strings.Join(cfg.Run.InitScript, " && ")
		args = append(args, "-e", fmt.Sprintf("WORKLET_INIT_SCRIPT=%s", initScript))
	}

	// Add additional volumes
	for _, volume := range cfg.Run.Volumes {
		args = append(args, "-v", volume)
	}

	// Add image
	image := cfg.Run.Image
	if image == "" {
		image = "docker:dind"
	}
	args = append(args, image)

	// Add command
	if len(cfg.Run.Command) > 0 {
		args = append(args, cfg.Run.Command...)
	} else {
		args = append(args, "sh")
	}

	// Execute docker command
	cmd := exec.Command("docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Register with proxy if services are configured
	var forkMapping *proxy.ForkMapping
	if len(cfg.Services) > 0 {
		// Initialize proxy if not already done
		if err := proxy.InitGlobalProxy(); err != nil {
			fmt.Printf("Warning: Failed to initialize proxy: %v\n", err)
		} else {
			// Convert config services to proxy services
			var proxyServices []proxy.ServicePort
			for _, service := range cfg.Services {
				proxyServices = append(proxyServices, proxy.ServicePort{
					ServiceName:   service.Name,
					ContainerPort: service.Port,
					Subdomain:     service.Subdomain,
				})
			}

			// Register with proxy
			mapping, err := proxy.RegisterForkWithProxy(forkID, proxyServices)
			if err != nil {
				fmt.Printf("Warning: Failed to register with proxy: %v\n", err)
				fmt.Printf("Run 'worklet proxy start' to start the proxy server\n")
			} else {
				forkMapping = mapping
				fmt.Printf("\nProxy URLs:\n")
				for _, service := range cfg.Services {
					url, _ := forkMapping.GetServiceURL(service.Name)
					fmt.Printf("  %s: %s\n", service.Name, url)
				}
				fmt.Printf("\n")
			}
		}
	}

	fmt.Printf("Running: docker %s\n", strings.Join(args, " "))

	// Run the container
	err = cmd.Run()

	// Unregister from proxy when container exits
	if forkMapping != nil {
		if unregErr := proxy.UnregisterForkFromProxy(forkID); unregErr != nil {
			fmt.Printf("Warning: Failed to unregister from proxy: %v\n", unregErr)
		}
	}

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
