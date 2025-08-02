package docker

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	nginxContainerName = "worklet-nginx-proxy"
	nginxImage         = "nginx:alpine"
	nginxConfigDir     = "/etc/nginx"
	nginxConfigFile    = "nginx.conf"
)

// NginxManager handles nginx proxy container operations
type NginxManager struct {
	client     *client.Client
	configPath string // Host path where nginx config is stored
}

// NewNginxManager creates a new nginx manager
func NewNginxManager(configPath string) (*NginxManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Ensure config directory exists
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return &NginxManager{
		client:     cli,
		configPath: configPath,
	}, nil
}

// Start starts the nginx proxy container
func (nm *NginxManager) Start(ctx context.Context) error {
	// Check if container already exists
	exists, _, err := nm.containerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	// Always remove existing container to ensure fresh configuration
	if exists {
		if err := nm.Remove(ctx); err != nil {
			return fmt.Errorf("failed to remove existing container: %w", err)
		}
	}

	// Pull nginx image
	if err := pullImage(ctx, nm.client, nginxImage); err != nil {
		return fmt.Errorf("failed to pull nginx image: %w", err)
	}

	// Create and start new container
	containerConfig := &container.Config{
		Image: nginxImage,
		Labels: map[string]string{
			"worklet.nginx": "true",
		},
	}

	hostConfig := &container.HostConfig{
		// Use default bridge network mode to allow port binding
		// The container will be connected to WorkletNetworkName after creation
		PortBindings: nat.PortMap{
			"80/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "80"},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: nm.configPath,
				Target: nginxConfigDir,
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	resp, err := nm.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, nginxContainerName)
	if err != nil {
		return fmt.Errorf("failed to create nginx container: %w", err)
	}

	if err := nm.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start nginx container: %w", err)
	}

	// Ensure the main worklet network exists and connect to it
	if err := EnsureNetworkExists(); err != nil {
		log.Printf("Warning: failed to ensure main worklet network exists: %v", err)
	}
	
	// Connect to the main worklet network first
	if err := nm.ConnectToNetwork(ctx, WorkletNetworkName); err != nil {
		log.Printf("Warning: failed to connect to main worklet network: %v", err)
	}

	// Connect to all existing session networks
	if err := nm.EnsureConnectedToAllNetworks(ctx); err != nil {
		log.Printf("Warning: failed to connect to all networks: %v", err)
	}

	return nil
}

// Stop stops the nginx proxy container
func (nm *NginxManager) Stop(ctx context.Context) error {
	exists, running, err := nm.containerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	if !exists || !running {
		return nil // Not running
	}

	return nm.client.ContainerStop(ctx, nginxContainerName, container.StopOptions{})
}

// Remove removes the nginx proxy container
func (nm *NginxManager) Remove(ctx context.Context) error {
	exists, _, err := nm.containerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	if !exists {
		return nil
	}

	// Stop first if running
	_ = nm.Stop(ctx)

	return nm.client.ContainerRemove(ctx, nginxContainerName, container.RemoveOptions{
		Force: true,
	})
}

// ConnectToNetwork connects the nginx container to a specific network
func (nm *NginxManager) ConnectToNetwork(ctx context.Context, networkName string) error {
	exists, _, err := nm.containerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	if !exists {
		return fmt.Errorf("nginx container does not exist")
	}

	// Check if already connected
	inspect, err := nm.client.ContainerInspect(ctx, nginxContainerName)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	// Check if already connected to this network
	if _, connected := inspect.NetworkSettings.Networks[networkName]; connected {
		return nil // Already connected
	}

	// Connect to the network
	if err := nm.client.NetworkConnect(ctx, networkName, nginxContainerName, nil); err != nil {
		// Ignore error if network doesn't exist or already connected
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("failed to connect to network %s: %w", networkName, err)
		}
	}

	log.Printf("Connected nginx to network: %s", networkName)
	return nil
}

// EnsureConnectedToAllNetworks ensures nginx is connected to all worklet session networks
func (nm *NginxManager) EnsureConnectedToAllNetworks(ctx context.Context) error {
	// List all networks
	networks, err := nm.client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Connect to all worklet networks including the main worklet-network
	for _, net := range networks {
		if strings.HasPrefix(net.Name, "worklet-") {
			if err := nm.ConnectToNetwork(ctx, net.Name); err != nil {
				log.Printf("Warning: failed to connect to network %s: %v", net.Name, err)
			}
		}
	}

	return nil
}

// Reload reloads the nginx configuration
func (nm *NginxManager) Reload(ctx context.Context) error {
	exists, running, err := nm.containerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	if !exists || !running {
		return fmt.Errorf("nginx container is not running")
	}

	// Ensure connected to all networks before reloading
	if err := nm.EnsureConnectedToAllNetworks(ctx); err != nil {
		log.Printf("Warning: failed to ensure network connections: %v", err)
	}

	// Execute nginx reload command
	exec, err := nm.client.ContainerExecCreate(ctx, nginxContainerName, container.ExecOptions{
		Cmd:          []string{"nginx", "-s", "reload"},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to exec to capture output
	attach, err := nm.client.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attach.Close()

	// Start the exec
	if err := nm.client.ContainerExecStart(ctx, exec.ID, container.ExecStartOptions{}); err != nil {
		return fmt.Errorf("failed to start nginx reload: %w", err)
	}

	// Read output
	output, err := io.ReadAll(attach.Reader)
	if err != nil {
		return fmt.Errorf("failed to read reload output: %w", err)
	}

	// Inspect exec to check exit code
	inspectResp, err := nm.client.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	// Check if reload was successful
	if inspectResp.ExitCode != 0 {
		return fmt.Errorf("nginx reload failed with exit code %d: %s", inspectResp.ExitCode, string(output))
	}

	// Log successful reload
	if len(output) > 0 {
		log.Printf("nginx reload output: %s", string(output))
	} else {
		log.Printf("nginx configuration reloaded successfully")
	}

	return nil
}

// UpdateConfig writes a new nginx configuration and reloads
func (nm *NginxManager) UpdateConfig(ctx context.Context, config string) error {
	configFile := filepath.Join(nm.configPath, nginxConfigFile)

	// Write config to file
	if err := os.WriteFile(configFile, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	log.Printf("Updated nginx config file: %s", configFile)

	// Reload nginx if running
	exists, running, err := nm.containerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	if exists && running {
		// Add a small delay to allow containers to fully start
		// This helps avoid DNS resolution issues when nginx reloads
		time.Sleep(3 * time.Second)
		
		log.Printf("Reloading nginx configuration...")
		
		// Try to reload with retries
		var lastErr error
		for i := 0; i < 3; i++ {
			if err := nm.Reload(ctx); err != nil {
				lastErr = err
				log.Printf("nginx reload attempt %d failed: %v", i+1, err)
				if i < 2 {
					time.Sleep(2 * time.Second)
				}
			} else {
				// Success
				return nil
			}
		}
		
		return fmt.Errorf("failed to reload nginx after 3 attempts: %w", lastErr)
	}

	log.Printf("nginx container not running, config updated but not reloaded")
	return nil
}

// containerStatus checks if the nginx container exists and is running
func (nm *NginxManager) containerStatus(ctx context.Context) (exists bool, running bool, err error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", nginxContainerName)

	containers, err := nm.client.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     true,
	})
	if err != nil {
		return false, false, err
	}

	if len(containers) == 0 {
		return false, false, nil
	}

	for _, c := range containers {
		for _, name := range c.Names {
			if strings.TrimPrefix(name, "/") == nginxContainerName {
				return true, c.State == "running", nil
			}
		}
	}

	return false, false, nil
}

// GetConfigPath returns the nginx config file path
func (nm *NginxManager) GetConfigPath() string {
	return filepath.Join(nm.configPath, nginxConfigFile)
}

// pullImage pulls a Docker image if it doesn't exist locally
func pullImage(ctx context.Context, cli *client.Client, imageName string) error {
	// Check if image exists locally
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return err
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == imageName {
				return nil // Image already exists
			}
		}
	}

	// Pull the image
	out, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer out.Close()

	// Consume the output to ensure the pull completes
	_, err = io.Copy(io.Discard, out)
	return err
}
