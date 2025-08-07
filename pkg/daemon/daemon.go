package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/internal/nginx"
)

// Daemon represents the worklet daemon server
type Daemon struct {
	socketPath   string
	listener     net.Listener
	forks        map[string]*ForkInfo
	forksMu      sync.RWMutex
	nextForkID   int
	ctx          context.Context
	cancel       context.CancelFunc
	stateFile    string
	nginxManager *docker.NginxManager
}

// NewDaemon creates a new daemon instance
func NewDaemon(socketPath string) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Determine state file path
	homeDir, _ := os.UserHomeDir()
	stateFile := filepath.Join(homeDir, ".worklet", "daemon.state")
	
	// Create nginx manager
	nginxConfigPath := filepath.Join(homeDir, ".worklet", "nginx")
	nginxManager, err := docker.NewNginxManager(nginxConfigPath)
	if err != nil {
		log.Printf("Failed to create nginx manager: %v", err)
	}
	
	return &Daemon{
		socketPath:   socketPath,
		forks:        make(map[string]*ForkInfo),
		nextForkID:   1,
		ctx:          ctx,
		cancel:       cancel,
		stateFile:    stateFile,
		nginxManager: nginxManager,
	}
}


// Start starts the daemon server
func (d *Daemon) Start() error {
	// Ensure socket directory exists
	socketDir := filepath.Dir(d.socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}
	
	// Remove existing socket file if it exists
	os.Remove(d.socketPath)
	
	// Create Unix socket listener
	listener, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create Unix socket: %w", err)
	}
	d.listener = listener
	
	// Set socket permissions (owner read/write only)
	if err := os.Chmod(d.socketPath, 0600); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}
	
	// Load state (only nextForkID now)
	if err := d.loadState(); err != nil {
		log.Printf("Failed to load state: %v", err)
	}
	
	// Discover any running worklet containers
	if err := d.discoverContainers(); err != nil {
		log.Printf("Failed to discover containers: %v", err)
	}
	
	// Start accepting connections
	go d.acceptConnections()
	
	// Start cleanup routine
	go d.cleanupRoutine()
	
	// Start Docker event listener for real-time container monitoring
	go d.startEventListener()
	
	// Start nginx proxy container
	if d.nginxManager != nil {
		// Generate fresh nginx config from validated state
		d.updateNginxConfig()
		
		// Now start nginx with the fresh config
		if err := d.nginxManager.Start(d.ctx); err != nil {
			log.Printf("Failed to start nginx proxy: %v", err)
		} else {
			log.Printf("Started nginx proxy container")
		}
	}
	
	log.Printf("Daemon started on %s", d.socketPath)
	return nil
}

// Stop stops the daemon server
func (d *Daemon) Stop() error {
	d.cancel()
	
	if d.listener != nil {
		d.listener.Close()
	}
	
	// Save state before stopping
	if err := d.saveState(); err != nil {
		log.Printf("Failed to save state: %v", err)
	}
	
	// Stop nginx proxy container
	if d.nginxManager != nil {
		if err := d.nginxManager.Stop(context.Background()); err != nil {
			log.Printf("Failed to stop nginx proxy: %v", err)
		}
	}
	
	// Remove socket file
	os.Remove(d.socketPath)
	
	return nil
}

// acceptConnections handles incoming client connections
func (d *Daemon) acceptConnections() {
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.ctx.Done():
				return
			default:
				log.Printf("Failed to accept connection: %v", err)
				continue
			}
		}
		
		go d.handleConnection(conn)
	}
}

// handleConnection handles a single client connection
func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()
	
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	
	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("Failed to decode message: %v", err)
			}
			return
		}
		
		response := d.handleMessage(&msg)
		if err := encoder.Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
			return
		}
	}
}

// handleMessage processes a message and returns a response
func (d *Daemon) handleMessage(msg *Message) *Message {
	switch msg.Type {
	case MsgRegisterFork:
		return d.handleRegisterFork(msg)
	case MsgUnregisterFork:
		return d.handleUnregisterFork(msg)
	case MsgListForks:
		return d.handleListForks(msg)
	case MsgGetForkInfo:
		return d.handleGetForkInfo(msg)
	case MsgRefreshFork:
		return d.handleRefreshFork(msg)
	case MsgRefreshAll:
		return d.handleRefreshAll(msg)
	case MsgRequestForkID:
		return d.handleRequestForkID(msg)
	case MsgHealthCheck:
		return &Message{
			Type: MsgSuccess,
			ID:   msg.ID,
		}
	case MsgTriggerDiscovery:
		return d.handleTriggerDiscovery(msg)
	default:
		return &Message{
			Type: MsgError,
			ID:   msg.ID,
			Payload: mustMarshal(ErrorResponse{
				Error: fmt.Sprintf("unknown message type: %s", msg.Type),
			}),
		}
	}
}

func (d *Daemon) handleRegisterFork(msg *Message) *Message {
	var req RegisterForkRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return errorResponse(msg.ID, "invalid request payload")
	}
	
	d.forksMu.Lock()
	d.forks[req.ForkID] = &ForkInfo{
		ForkID:       req.ForkID,
		ProjectName:  req.ProjectName,
		ContainerID:  req.ContainerID,
		WorkDir:      req.WorkDir,
		Services:     req.Services,
		Metadata:     req.Metadata,
		RegisteredAt: time.Now(),
		LastSeenAt:   time.Now(),
	}
	d.forksMu.Unlock()
	
	// Update nginx configuration and ensure it's connected to the fork's network
	d.updateNginxConfig()
	
	// Connect nginx to the session's network
	if d.nginxManager != nil {
		networkName := fmt.Sprintf("worklet-%s", req.ForkID)
		if err := d.nginxManager.ConnectToNetwork(context.Background(), networkName); err != nil {
			log.Printf("Warning: failed to connect nginx to network %s: %v", networkName, err)
		}
	}
	
	return &Message{
		Type: MsgSuccess,
		ID:   msg.ID,
		Payload: mustMarshal(SuccessResponse{
			Message: fmt.Sprintf("Fork %s registered", req.ForkID),
		}),
	}
}

func (d *Daemon) handleUnregisterFork(msg *Message) *Message {
	var req UnregisterForkRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return errorResponse(msg.ID, "invalid request payload")
	}
	
	d.forksMu.Lock()
	delete(d.forks, req.ForkID)
	d.forksMu.Unlock()
	
	// Update nginx configuration
	d.updateNginxConfig()
	
	return &Message{
		Type: MsgSuccess,
		ID:   msg.ID,
		Payload: mustMarshal(SuccessResponse{
			Message: fmt.Sprintf("Fork %s unregistered", req.ForkID),
		}),
	}
}

func (d *Daemon) handleListForks(msg *Message) *Message {
	// First discover any containers that might have been started while daemon was down
	// or containers that failed to register initially
	if err := d.discoverContainers(); err != nil {
		log.Printf("Warning: failed to discover containers before listing: %v", err)
		// Continue anyway - we'll still return what we have
	}
	
	// Then validate and cleanup stale forks
	// This ensures the list is always fresh and accurate
	if err := d.validateAndCleanupForks(); err != nil {
		log.Printf("Warning: failed to validate forks before listing: %v", err)
		// Continue anyway with potentially stale data
	}
	
	d.forksMu.RLock()
	forks := make([]ForkInfo, 0, len(d.forks))
	for _, fork := range d.forks {
		forks = append(forks, *fork)
	}
	d.forksMu.RUnlock()
	
	return &Message{
		Type: MsgForkList,
		ID:   msg.ID,
		Payload: mustMarshal(ListForksResponse{
			Forks: forks,
		}),
	}
}

func (d *Daemon) handleGetForkInfo(msg *Message) *Message {
	var req GetForkInfoRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return errorResponse(msg.ID, "invalid request payload")
	}
	
	d.forksMu.RLock()
	fork, exists := d.forks[req.ForkID]
	d.forksMu.RUnlock()
	
	if !exists {
		return errorResponse(msg.ID, fmt.Sprintf("fork %s not found", req.ForkID))
	}
	
	return &Message{
		Type: MsgForkInfo,
		ID:   msg.ID,
		Payload: mustMarshal(fork),
	}
}

func (d *Daemon) handleRefreshFork(msg *Message) *Message {
	var req RefreshForkRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return errorResponse(msg.ID, "invalid request payload")
	}
	
	// Refresh the specific fork
	refreshed, err := d.refreshFork(req.ForkID)
	if err != nil {
		return errorResponse(msg.ID, err.Error())
	}
	
	if refreshed {
		// Update nginx configuration if fork was refreshed
		d.updateNginxConfig()
	}
	
	return &Message{
		Type: MsgSuccess,
		ID:   msg.ID,
		Payload: mustMarshal(SuccessResponse{
			Message: fmt.Sprintf("Fork %s refreshed", req.ForkID),
		}),
	}
}

func (d *Daemon) handleRefreshAll(msg *Message) *Message {
	// First discover any running containers not in our state
	if err := d.discoverContainers(); err != nil {
		log.Printf("Failed to discover containers during refresh: %v", err)
	}
	
	// Then refresh all forks
	count, err := d.refreshAllForks()
	if err != nil {
		return errorResponse(msg.ID, err.Error())
	}
	
	if count > 0 {
		// Update nginx configuration if any forks were refreshed
		d.updateNginxConfig()
	}
	
	return &Message{
		Type: MsgSuccess,
		ID:   msg.ID,
		Payload: mustMarshal(SuccessResponse{
			Message: fmt.Sprintf("Refreshed %d fork(s)", count),
		}),
	}
}

func (d *Daemon) handleTriggerDiscovery(msg *Message) *Message {
	// Trigger container discovery immediately
	if err := d.discoverContainers(); err != nil {
		return &Message{
			Type: MsgError,
			ID:   msg.ID,
			Payload: mustMarshal(ErrorResponse{
				Error: fmt.Sprintf("failed to discover containers: %v", err),
			}),
		}
	}
	
	return &Message{
		Type: MsgSuccess,
		ID:   msg.ID,
		Payload: mustMarshal(SuccessResponse{
			Message: "Container discovery triggered",
		}),
	}
}

func (d *Daemon) handleRequestForkID(msg *Message) *Message {
	d.forksMu.Lock()
	forkID := fmt.Sprintf("%d", d.nextForkID)
	d.nextForkID++
	d.forksMu.Unlock()
	
	// Save state with updated counter
	go d.saveState()
	
	return &Message{
		Type: MsgForkID,
		ID:   msg.ID,
		Payload: mustMarshal(RequestForkIDResponse{
			ForkID: forkID,
		}),
	}
}

// cleanupRoutine periodically cleans up stale fork registrations
func (d *Daemon) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Minute) // Reduced from 5 minutes for faster cleanup
	defer ticker.Stop()
	
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.cleanupStaleForks()
		}
	}
}

func (d *Daemon) cleanupStaleForks() {
	// Validate and cleanup stale forks periodically
	if err := d.validateAndCleanupForks(); err != nil {
		log.Printf("Failed to cleanup stale forks: %v", err)
	}
}

// validateAndCleanupForks checks if containers still exist for registered forks
func (d *Daemon) validateAndCleanupForks() error {
	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	// List all containers (including stopped ones)
	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Create a map of existing container names for quick lookup
	containerNames := make(map[string]bool)
	for _, c := range containers {
		for _, name := range c.Names {
			// Container names start with /, so trim it
			containerNames[strings.TrimPrefix(name, "/")] = true
		}
	}

	// Check each fork
	d.forksMu.Lock()
	defer d.forksMu.Unlock()

	var forksToRemove []string
	for forkID, fork := range d.forks {
		// Construct expected container name
		containerName := fork.ProjectName + "-" + forkID
		if fork.ProjectName == "" {
			containerName = "worklet-" + forkID
		}

		// Check if container exists
		if !containerNames[containerName] {
			log.Printf("Container %s not found, removing fork %s", containerName, forkID)
			forksToRemove = append(forksToRemove, forkID)
		}
	}

	// Remove stale forks
	for _, forkID := range forksToRemove {
		delete(d.forks, forkID)
	}

	if len(forksToRemove) > 0 {
		// Update nginx configuration
		d.updateNginxConfig()
		
		log.Printf("Cleaned up %d stale fork(s)", len(forksToRemove))
	}

	return nil
}

// discoverContainers finds running worklet containers by labels and registers them
func (d *Daemon) discoverContainers() error {
	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()
	
	// List containers with worklet.session=true label
	filters := filters.NewArgs()
	filters.Add("label", "worklet.session=true")
	
	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	
	d.forksMu.Lock()
	
	discoveredCount := 0
	for _, container := range containers {
		// Skip if container is not running
		if container.State != "running" {
			continue
		}
		
		// Extract fork information from labels
		forkID := container.Labels["worklet.session.id"]
		projectName := container.Labels["worklet.project.name"]
		workDir := container.Labels["worklet.workdir"]
		
		if forkID == "" {
			continue
		}
		
		// Check if fork is already registered
		if _, exists := d.forks[forkID]; exists {
			continue
		}
		
		// Load services from .worklet.jsonc if workdir is available
		var services []ServiceInfo
		
		if workDir != "" {
			// Try to load config from workdir
			configPath := filepath.Join(workDir, ".worklet.jsonc")
			if configData, err := os.ReadFile(configPath); err == nil {
				// Parse the config to get services
				var cfg struct {
					Services []struct {
						Name      string `json:"name"`
						Port      int    `json:"port"`
						Subdomain string `json:"subdomain"`
					} `json:"services"`
				}
				
				if err := json.Unmarshal(configData, &cfg); err == nil {
					// Use services from config file
					for _, svc := range cfg.Services {
						services = append(services, ServiceInfo{
							Name:      svc.Name,
							Port:      svc.Port,
							Subdomain: svc.Subdomain,
						})
					}
				} else {
					log.Printf("Failed to parse config for fork %s: %v", forkID, err)
				}
			} else {
				log.Printf("Failed to read config for fork %s: %v", forkID, err)
			}
		}
		
		// If we couldn't load from config, fall back to labels (for backward compatibility)
		if len(services) == 0 {
			serviceMap := make(map[string]*ServiceInfo)
			
			for label, value := range container.Labels {
				if strings.HasPrefix(label, "worklet.service.") {
					parts := strings.Split(label, ".")
					if len(parts) == 4 {
						serviceName := parts[2]
						field := parts[3]
						
						if _, ok := serviceMap[serviceName]; !ok {
							serviceMap[serviceName] = &ServiceInfo{Name: serviceName}
						}
						
						switch field {
						case "port":
							if port, err := strconv.Atoi(value); err == nil {
								serviceMap[serviceName].Port = port
							}
						case "subdomain":
							serviceMap[serviceName].Subdomain = value
						}
					}
				}
			}
			
			// Convert service map to slice
			for _, svc := range serviceMap {
				services = append(services, *svc)
			}
		}
		
		// If still no services defined, add a default service
		// This ensures containers without explicit services still get nginx routing
		if len(services) == 0 {
			services = append(services, ServiceInfo{
				Name:      "app",
				Port:      3000,
				Subdomain: "app",
			})
			log.Printf("No services defined for fork %s, using default service (app:3000)", forkID)
		}
		
		// Ensure forks map is initialized (defensive check)
		if d.forks == nil {
			d.forks = make(map[string]*ForkInfo)
		}
		
		// Register the discovered fork
		d.forks[forkID] = &ForkInfo{
			ForkID:       forkID,
			ProjectName:  projectName,
			ContainerID:  container.ID,
			WorkDir:      workDir,
			Services:     services,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		
		discoveredCount++
		log.Printf("Discovered and registered fork %s from container %s", forkID, container.Names[0])
	}
	
	// Release the lock before calling other methods
	d.forksMu.Unlock()
	
	if discoveredCount > 0 {
		// Update nginx configuration (now safe to call)
		d.updateNginxConfig()
		
		// Ensure nginx is connected to all discovered session networks
		if d.nginxManager != nil {
			if err := d.nginxManager.EnsureConnectedToAllNetworks(context.Background()); err != nil {
				log.Printf("Warning: failed to connect nginx to all networks: %v", err)
			}
		}
		
		log.Printf("Discovered and registered %d fork(s)", discoveredCount)
	}
	
	return nil
}

// DaemonState represents the persistent state of the daemon
type DaemonState struct {
	NextForkID int `json:"next_fork_id"`
}

// State persistence methods
func (d *Daemon) saveState() error {
	d.forksMu.RLock()
	nextForkID := d.nextForkID
	d.forksMu.RUnlock()
	
	state := DaemonState{
		NextForkID: nextForkID,
	}
	
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	
	// Ensure directory exists
	stateDir := filepath.Dir(d.stateFile)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	
	return os.WriteFile(d.stateFile, data, 0600)
}

func (d *Daemon) loadState() error {
	data, err := os.ReadFile(d.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet
		}
		return err
	}
	
	d.forksMu.Lock()
	defer d.forksMu.Unlock()
	
	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		// Try to handle old format gracefully
		var oldState struct {
			Forks      map[string]*ForkInfo `json:"forks"`
			NextForkID int                  `json:"next_fork_id"`
		}
		if err := json.Unmarshal(data, &oldState); err != nil {
			return err
		}
		// Only use nextForkID from old state
		d.nextForkID = oldState.NextForkID
	} else {
		d.nextForkID = state.NextForkID
	}
	
	if d.nextForkID < 1 {
		d.nextForkID = 1
	}
	
	return nil
}

// refreshFork refreshes information for a specific fork
func (d *Daemon) refreshFork(forkID string) (bool, error) {
	d.forksMu.Lock()
	defer d.forksMu.Unlock()
	
	fork, exists := d.forks[forkID]
	if !exists {
		return false, fmt.Errorf("fork %s not found", forkID)
	}
	
	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return false, fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()
	
	// Construct container name
	containerName := fork.ProjectName + "-" + forkID
	if fork.ProjectName == "" {
		containerName = "worklet-" + forkID
	}
	
	// Inspect container to get current information
	containerInfo, err := cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		// Container might not exist anymore
		delete(d.forks, forkID)
		return true, nil
	}
	
	// Update last seen time
	fork.LastSeenAt = time.Now()
	
	// Update container ID if changed
	fork.ContainerID = containerInfo.ID
	
	// Note: We do NOT auto-discover services from container ports
	// Services should only come from .worklet.jsonc via RegisterFork or discoverContainers
	// This prevents Docker daemon ports (2375/2376) from being exposed through nginx
	
	// Save updated fork info
	d.forks[forkID] = fork
	
	return true, nil
}

// refreshAllForks refreshes information for all registered forks
func (d *Daemon) refreshAllForks() (int, error) {
	// Get list of fork IDs to refresh
	d.forksMu.RLock()
	forkIDs := make([]string, 0, len(d.forks))
	for forkID := range d.forks {
		forkIDs = append(forkIDs, forkID)
	}
	d.forksMu.RUnlock()
	
	refreshedCount := 0
	var lastErr error
	
	// Refresh each fork
	for _, forkID := range forkIDs {
		refreshed, err := d.refreshFork(forkID)
		if err != nil {
			log.Printf("Failed to refresh fork %s: %v", forkID, err)
			lastErr = err
			continue
		}
		if refreshed {
			refreshedCount++
		}
	}
	
	if lastErr != nil && refreshedCount == 0 {
		return refreshedCount, lastErr
	}
	
	return refreshedCount, nil
}

// updateNginxConfig updates the nginx configuration with all registered forks
func (d *Daemon) updateNginxConfig() {
	if d.nginxManager == nil {
		return
	}
	
	d.forksMu.RLock()
	defer d.forksMu.RUnlock()
	
	var services []nginx.ForkService
	
	for _, fork := range d.forks {
		// If fork has no services configured, skip it
		if len(fork.Services) == 0 {
			continue
		}
		
		// Add each service from the fork
		for _, svc := range fork.Services {
			services = append(services, nginx.AddService(
				fork.ForkID,
				fork.ProjectName,
				svc.Name,
				svc.Port,
				svc.Subdomain,
			))
		}
	}
	
	// Generate nginx config
	nginxConfig, err := nginx.GenerateConfig(services)
	if err != nil {
		log.Printf("Failed to generate nginx config: %v", err)
		return
	}
	
	// Update nginx configuration
	if err := d.nginxManager.UpdateConfig(context.Background(), nginxConfig); err != nil {
		log.Printf("Failed to update nginx config: %v", err)
		return
	}
	
	log.Printf("Updated nginx configuration with %d services", len(services))
}

// startEventListener listens for Docker container events and updates fork state in real-time
func (d *Daemon) startEventListener() {
	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Printf("Failed to create Docker client for event listener: %v", err)
		return
	}
	defer cli.Close()
	
	// Set up filters for worklet containers
	eventFilters := filters.NewArgs()
	eventFilters.Add("type", string(events.ContainerEventType))
	eventFilters.Add("label", "worklet.session=true")
	
	// Subscribe to events
	eventsChan, errChan := cli.Events(d.ctx, events.ListOptions{
		Filters: eventFilters,
	})
	
	log.Printf("Started Docker event listener for worklet containers")
	
	for {
		select {
		case event := <-eventsChan:
			// Handle container lifecycle events
			switch event.Action {
			case "die", "stop", "kill", "remove":
				// Extract session ID from event attributes
				sessionID := event.Actor.Attributes["worklet.session.id"]
				if sessionID != "" {
					d.handleContainerRemoved(sessionID)
				}
			case "start":
				// When a container starts, re-discover to pick it up
				if err := d.discoverContainers(); err != nil {
					log.Printf("Failed to discover containers after start event: %v", err)
				}
			}
		case err := <-errChan:
			if err != nil {
				log.Printf("Docker event stream error: %v", err)
				// Try to reconnect after a delay
				time.Sleep(5 * time.Second)
				go d.startEventListener() // Restart the listener
				return
			}
		case <-d.ctx.Done():
			log.Printf("Stopping Docker event listener")
			return
		}
	}
}

// handleContainerRemoved removes a fork when its container is removed
func (d *Daemon) handleContainerRemoved(sessionID string) {
	d.forksMu.Lock()
	defer d.forksMu.Unlock()
	
	if fork, exists := d.forks[sessionID]; exists {
		log.Printf("Container for session %s was removed, cleaning up fork registration", sessionID)
		delete(d.forks, sessionID)
		
		// Update nginx configuration
		d.updateNginxConfig()
		
		// Log the removal
		if fork.ProjectName != "" {
			log.Printf("Removed fork %s (project: %s) due to container removal", sessionID, fork.ProjectName)
		} else {
			log.Printf("Removed fork %s due to container removal", sessionID)
		}
	}
}

// Helper functions
func errorResponse(id, errMsg string) *Message {
	return &Message{
		Type: MsgError,
		ID:   id,
		Payload: mustMarshal(ErrorResponse{
			Error: errMsg,
		}),
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}