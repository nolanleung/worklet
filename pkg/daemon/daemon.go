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
	"syscall"
	"time"
	
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/internal/nginx"
	"github.com/nolanleung/worklet/internal/version"
)

var debugMode = os.Getenv("WORKLET_DEBUG") == "true"

func debugLog(format string, args ...interface{}) {
	if debugMode {
		log.Printf("[DEBUG] " + format, args...)
	}
}

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
	pidFile      string
	nginxManager *docker.NginxManager
	startTime    time.Time
	
	// Cache for container information
	forksCache      []ForkInfo
	forksCacheMu    sync.RWMutex
	forksCacheTime  time.Time
	forksCacheTTL   time.Duration
}

// NewDaemon creates a new daemon instance
func NewDaemon(socketPath string) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Determine state file path
	homeDir, _ := os.UserHomeDir()
	stateFile := filepath.Join(homeDir, ".worklet", "daemon.state")
	pidFile := filepath.Join(homeDir, ".worklet", "daemon.pid")
	
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
		pidFile:      pidFile,
		nginxManager: nginxManager,
		startTime:    time.Now(),
		forksCacheTTL: 5 * time.Second, // Cache TTL of 5 seconds
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
	
	// Clean up any orphaned networks from previous runs
	if removedCount, err := docker.CleanupOrphanedNetworks(); err != nil {
		log.Printf("Failed to cleanup orphaned networks at startup: %v", err)
	} else if removedCount > 0 {
		log.Printf("Cleaned up %d orphaned network(s) at startup", removedCount)
	}
	
	// Start accepting connections
	go d.acceptConnections()
	
	// Start Docker event listener for real-time container monitoring
	go d.startEventListener()
	
	// Start PID file checker to ensure only one daemon runs
	go d.startPIDChecker()
	
	// Start background container discovery for periodic updates
	go d.startPeriodicDiscovery()
	
	// Start nginx proxy container
	if d.nginxManager != nil {
		// Generate fresh nginx config from validated state
		d.updateNginxConfig()
		
		// Now start nginx with the fresh config
		if err := d.nginxManager.Start(d.ctx); err != nil {
			log.Printf("Failed to start nginx proxy: %v", err)
		} else {
			log.Printf("Started nginx proxy container")
			
			// Start nginx health check goroutine
			go d.startNginxHealthCheck()
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
	
	// Clean up PID file
	d.removePIDFromFile()
	
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
	
	debugLog("New client connection from %v", conn.RemoteAddr())
	
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	
	for {
		var msg Message
		decodeStart := time.Now()
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				log.Printf("Failed to decode message: %v", err)
			}
			debugLog("Connection closed from %v", conn.RemoteAddr())
			return
		}
		debugLog("Received message: Type=%s, ID=%s (decode took %v)", msg.Type, msg.ID, time.Since(decodeStart))
		
		handleStart := time.Now()
		response := d.handleMessage(&msg)
		debugLog("Handled message: Type=%s, ID=%s, ResponseType=%s (took %v)", msg.Type, msg.ID, response.Type, time.Since(handleStart))
		
		encodeStart := time.Now()
		if err := encoder.Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
			return
		}
		debugLog("Sent response for message ID=%s (encode took %v)", msg.ID, time.Since(encodeStart))
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
	case MsgGetVersion:
		return d.handleGetVersion(msg)
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
	
	// Invalidate cache since we modified forks
	d.invalidateCache()
	
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
	
	// Invalidate cache since we modified forks
	d.invalidateCache()
	
	// Update nginx configuration
	d.updateNginxConfig()
	
	// Clean up the session network if no containers are using it
	if err := docker.RemoveSessionNetworkSafe(req.ForkID); err != nil {
		log.Printf("Warning: failed to remove network for session %s: %v", req.ForkID, err)
	}
	
	return &Message{
		Type: MsgSuccess,
		ID:   msg.ID,
		Payload: mustMarshal(SuccessResponse{
			Message: fmt.Sprintf("Fork %s unregistered", req.ForkID),
		}),
	}
}

func (d *Daemon) handleListForks(msg *Message) *Message {
	startTime := time.Now()
	debugLog("handleListForks started for message ID=%s", msg.ID)
	
	// Check if we have valid cached data
	d.forksCacheMu.RLock()
	cacheValid := time.Since(d.forksCacheTime) < d.forksCacheTTL && len(d.forksCache) > 0
	cachedForks := d.forksCache
	d.forksCacheMu.RUnlock()
	
	if cacheValid {
		debugLog("Returning cached forks (cache age: %v)", time.Since(d.forksCacheTime))
		debugLog("handleListForks completed for message ID=%s (total time: %v, from cache)", msg.ID, time.Since(startTime))
		
		return &Message{
			Type: MsgForkList,
			ID:   msg.ID,
			Payload: mustMarshal(ListForksResponse{
				Forks: cachedForks,
			}),
		}
	}
	
	// Cache miss or expired - rebuild cache
	debugLog("Cache miss or expired, rebuilding...")
	
	// Get current forks from memory (fast operation)
	lockStart := time.Now()
	d.forksMu.RLock()
	forks := make([]ForkInfo, 0, len(d.forks))
	for _, fork := range d.forks {
		forks = append(forks, *fork)
	}
	d.forksMu.RUnlock()
	debugLog("Read %d forks from map (lock held for %v)", len(forks), time.Since(lockStart))
	
	// Update cache
	d.forksCacheMu.Lock()
	d.forksCache = forks
	d.forksCacheTime = time.Now()
	d.forksCacheMu.Unlock()
	
	debugLog("handleListForks completed for message ID=%s (total time: %v, cache rebuilt)", msg.ID, time.Since(startTime))
	
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

func (d *Daemon) handleGetVersion(msg *Message) *Message {
	versionInfo := version.GetInfo()
	
	return &Message{
		Type: MsgVersion,
		ID:   msg.ID,
		Payload: mustMarshal(GetVersionResponse{
			Version:   versionInfo.Version,
			BuildTime: versionInfo.BuildTime,
			GitCommit: versionInfo.GitCommit,
			StartTime: d.startTime.Format(time.RFC3339),
		}),
	}
}


// validateAndCleanupForks checks if containers still exist for registered forks
func (d *Daemon) validateAndCleanupForks() error {
	startTime := time.Now()
	debugLog("validateAndCleanupForks started")
	
	// Create Docker client
	clientStart := time.Now()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()
	debugLog("Docker client created (took %v)", time.Since(clientStart))

	// List all containers with worklet.session label
	filters := filters.NewArgs()
	filters.Add("label", "worklet.session=true")
	
	listStart := time.Now()
	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	debugLog("Listed %d containers with worklet.session label (took %v)", len(containers), time.Since(listStart))

	// Create a map of existing session IDs for quick lookup
	existingSessionIDs := make(map[string]bool)
	for _, c := range containers {
		if sessionID, ok := c.Labels["worklet.session.id"]; ok && sessionID != "" {
			existingSessionIDs[sessionID] = true
		}
	}

	// Check each fork
	lockStart := time.Now()
	d.forksMu.Lock()
	debugLog("Acquired write lock for validation (took %v)", time.Since(lockStart))
	
	var forksToRemove []string
	for forkID := range d.forks {
		// Check if container with this session ID exists
		if !existingSessionIDs[forkID] {
			log.Printf("Container with session ID %s not found, removing fork %s", forkID, forkID)
			forksToRemove = append(forksToRemove, forkID)
		}
	}

	// Remove stale forks
	for _, forkID := range forksToRemove {
		delete(d.forks, forkID)
	}
	
	// Release the lock before calling updateNginxConfig to avoid deadlock
	d.forksMu.Unlock()
	debugLog("Released write lock after validation (lock held for %v)", time.Since(lockStart))

	if len(forksToRemove) > 0 {
		// Invalidate cache since we modified forks
		d.invalidateCache()
		
		// Update nginx configuration (now safe to call)
		nginxStart := time.Now()
		d.updateNginxConfig()
		debugLog("Updated nginx config after cleanup (took %v)", time.Since(nginxStart))
		
		log.Printf("Cleaned up %d stale fork(s)", len(forksToRemove))
	}

	debugLog("validateAndCleanupForks completed (total time: %v)", time.Since(startTime))
	return nil
}

// discoverContainers finds running worklet containers by labels and registers them
func (d *Daemon) discoverContainers() error {
	startTime := time.Now()
	debugLog("discoverContainers started")
	
	// Create Docker client
	clientStart := time.Now()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()
	debugLog("Docker client created (took %v)", time.Since(clientStart))
	
	// List containers with worklet.session=true label
	filters := filters.NewArgs()
	filters.Add("label", "worklet.session=true")
	
	listStart := time.Now()
	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	debugLog("Listed %d containers (took %v)", len(containers), time.Since(listStart))
	
	// Prepare fork information without holding the lock
	type pendingFork struct {
		forkID      string
		projectName string
		containerID string
		workDir     string
		services    []ServiceInfo
		containerName string
	}
	
	var pendingForks []pendingFork
	
	processStart := time.Now()
	debugLog("Starting to process %d containers", len(containers))
	
	for i, container := range containers {
		containerStart := time.Now()
		containerName := "(unnamed)"
		if len(container.Names) > 0 {
			containerName = container.Names[0]
		}
		debugLog("Processing container %d/%d: %s (state: %s)", i+1, len(containers), containerName, container.State)
		
		// Skip if container is not running
		if container.State != "running" {
			debugLog("  Skipping non-running container %s", containerName)
			continue
		}
		
		// Extract fork information from labels
		forkID := container.Labels["worklet.session.id"]
		projectName := container.Labels["worklet.project.name"]
		workDir := container.Labels["worklet.workdir"]
		debugLog("  Container %s: forkID=%s, project=%s, workdir=%s", containerName, forkID, projectName, workDir)
		
		if forkID == "" {
			debugLog("  Skipping container %s: no session ID", containerName)
			continue
		}
		
		// Check if fork is already registered (quick check with read lock)
		lockCheckStart := time.Now()
		d.forksMu.RLock()
		_, exists := d.forks[forkID]
		d.forksMu.RUnlock()
		debugLog("  Checked fork existence for %s: exists=%v (took %v)", forkID, exists, time.Since(lockCheckStart))
		
		if exists {
			debugLog("  Fork %s already registered, skipping", forkID)
			continue
		}
		
		// Load services from .worklet.jsonc if workdir is available
		// This is done OUTSIDE the lock
		var services []ServiceInfo
		
		if workDir != "" {
			// Try to load config from workdir
			configPath := filepath.Join(workDir, ".worklet.jsonc")
			debugLog("  Attempting to read config from %s", configPath)
			readStart := time.Now()
			if configData, err := os.ReadFile(configPath); err == nil {
				debugLog("  Successfully read config file %s (%d bytes, took %v)", configPath, len(configData), time.Since(readStart))
				// Parse the config to get services
				parseStart := time.Now()
				var cfg struct {
					Services []struct {
						Name      string `json:"name"`
						Port      int    `json:"port"`
						Subdomain string `json:"subdomain"`
					} `json:"services"`
				}
				
				if err := json.Unmarshal(configData, &cfg); err == nil {
					debugLog("  Parsed config successfully, found %d services (took %v)", len(cfg.Services), time.Since(parseStart))
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
					debugLog("  Config parse failed (took %v): %v", time.Since(parseStart), err)
				}
			} else {
				log.Printf("Failed to read config for fork %s: %v", forkID, err)
				debugLog("  Config read failed (took %v): %v", time.Since(readStart), err)
			}
		} else {
			debugLog("  No workdir specified, skipping config file load")
		}
		
		// If we couldn't load from config, fall back to labels (for backward compatibility)
		if len(services) == 0 {
			labelStart := time.Now()
			serviceMap := make(map[string]*ServiceInfo)
			serviceLabels := 0
			
			for label, value := range container.Labels {
				if strings.HasPrefix(label, "worklet.service.") {
					serviceLabels++
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
			
			debugLog("  Found %d service labels, extracted %d services (took %v)", serviceLabels, len(serviceMap), time.Since(labelStart))
			
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
		
		// Store pending fork info to register later
		pendingForks = append(pendingForks, pendingFork{
			forkID:      forkID,
			projectName: projectName,
			containerID: container.ID,
			workDir:     workDir,
			services:    services,
			containerName: containerName,
		})
		debugLog("  Added fork %s to pending registration list (container processing took %v)", forkID, time.Since(containerStart))
	}
	
	debugLog("Finished processing all containers (took %v, %d pending forks)", time.Since(processStart), len(pendingForks))
	
	// Now acquire the lock and register all pending forks
	lockStart := time.Now()
	d.forksMu.Lock()
	debugLog("Acquired write lock for registration (took %v)", time.Since(lockStart))
	
	// Ensure forks map is initialized (defensive check)
	if d.forks == nil {
		d.forks = make(map[string]*ForkInfo)
	}
	
	discoveredCount := 0
	for _, pending := range pendingForks {
		// Double-check fork doesn't exist (in case it was added while we were preparing)
		if _, exists := d.forks[pending.forkID]; !exists {
			d.forks[pending.forkID] = &ForkInfo{
				ForkID:       pending.forkID,
				ProjectName:  pending.projectName,
				ContainerID:  pending.containerID,
				WorkDir:      pending.workDir,
				Services:     pending.services,
				RegisteredAt: time.Now(),
				LastSeenAt:   time.Now(),
			}
			discoveredCount++
			log.Printf("Discovered and registered fork %s from container %s", pending.forkID, pending.containerName)
		}
	}
	
	// Release the lock before calling other methods
	d.forksMu.Unlock()
	debugLog("Released write lock after registration (lock held for %v)", time.Since(lockStart))
	
	if discoveredCount > 0 {
		// Invalidate cache since we modified forks
		d.invalidateCache()
		
		// Update nginx configuration (now safe to call)
		nginxStart := time.Now()
		d.updateNginxConfig()
		debugLog("Updated nginx config (took %v)", time.Since(nginxStart))
		
		// Ensure nginx is connected to all discovered session networks
		if d.nginxManager != nil {
			if err := d.nginxManager.EnsureConnectedToAllNetworks(context.Background()); err != nil {
				log.Printf("Warning: failed to connect nginx to all networks: %v", err)
			}
		}
		
		log.Printf("Discovered and registered %d fork(s)", discoveredCount)
	}
	
	debugLog("discoverContainers completed (total time: %v)", time.Since(startTime))
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
	// Get fork info with read lock first
	d.forksMu.RLock()
	fork, exists := d.forks[forkID]
	d.forksMu.RUnlock()
	
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
	
	// Inspect container to get current information (outside of lock)
	containerInfo, err := cli.ContainerInspect(context.Background(), containerName)
	
	// Now update with write lock
	d.forksMu.Lock()
	defer d.forksMu.Unlock()
	
	// Re-check that fork still exists (it might have been removed while we were checking Docker)
	currentFork, stillExists := d.forks[forkID]
	if !stillExists {
		return false, nil
	}
	
	if err != nil {
		// Container might not exist anymore
		delete(d.forks, forkID)
		return true, nil
	}
	
	// Update fork information
	currentFork.LastSeenAt = time.Now()
	currentFork.ContainerID = containerInfo.ID
	
	// Note: We do NOT auto-discover services from container ports
	// Services should only come from .worklet.jsonc via RegisterFork or discoverContainers
	// This prevents Docker daemon ports (2375/2376) from being exposed through nginx
	
	// Save updated fork info
	d.forks[forkID] = currentFork
	
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
	// Acquire lock to check and remove fork
	d.forksMu.Lock()
	
	fork, exists := d.forks[sessionID]
	if exists {
		log.Printf("Container for session %s was removed, cleaning up fork registration", sessionID)
		delete(d.forks, sessionID)
	}
	
	// Release lock before calling updateNginxConfig to avoid deadlock
	d.forksMu.Unlock()
	
	// Update nginx configuration if a fork was removed (now safe to call)
	if exists {
		// Invalidate cache since we modified forks
		d.invalidateCache()
		
		d.updateNginxConfig()
		
		// Clean up the session network if no containers are using it
		if err := docker.RemoveSessionNetworkSafe(sessionID); err != nil {
			log.Printf("Warning: failed to remove network for session %s: %v", sessionID, err)
		} else {
			log.Printf("Cleaned up network for session %s", sessionID)
		}
		
		// Log the removal
		if fork.ProjectName != "" {
			log.Printf("Removed fork %s (project: %s) due to container removal", sessionID, fork.ProjectName)
		} else {
			log.Printf("Removed fork %s due to container removal", sessionID)
		}
	}
}

// invalidateCache marks the forks cache as invalid
func (d *Daemon) invalidateCache() {
	d.forksCacheMu.Lock()
	d.forksCacheTime = time.Time{} // Zero time means cache is invalid
	d.forksCacheMu.Unlock()
	debugLog("Fork cache invalidated")
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

// startPeriodicDiscovery periodically discovers containers in the background
func (d *Daemon) startPeriodicDiscovery() {
	// Initial delay to let the daemon start up
	time.Sleep(5 * time.Second)
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			debugLog("Running periodic container discovery")
			if err := d.discoverContainers(); err != nil {
				log.Printf("Periodic container discovery failed: %v", err)
			}
			if err := d.validateAndCleanupForks(); err != nil {
				log.Printf("Periodic fork validation failed: %v", err)
			}
			
			// Clean up orphaned networks
			if removedCount, err := docker.CleanupOrphanedNetworks(); err != nil {
				log.Printf("Failed to cleanup orphaned networks: %v", err)
			} else if removedCount > 0 {
				log.Printf("Cleaned up %d orphaned network(s)", removedCount)
			}
		case <-d.ctx.Done():
			debugLog("Stopping periodic discovery")
			return
		}
	}
}

// startNginxHealthCheck periodically checks nginx health and restarts if needed
func (d *Daemon) startNginxHealthCheck() {
	if d.nginxManager == nil {
		return
	}
	
	// Initial delay to let nginx start properly
	time.Sleep(10 * time.Second)
	
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	
	var consecutiveFailures int
	const maxConsecutiveFailures = 3
	
	for {
		select {
		case <-ticker.C:
			// Check if nginx is healthy
			healthy, err := d.nginxManager.IsHealthy(d.ctx)
			if err != nil {
				log.Printf("nginx health check error: %v", err)
				consecutiveFailures++
			} else if !healthy {
				log.Printf("nginx proxy is not healthy, attempting to restart...")
				consecutiveFailures++
				
				// Attempt to restart nginx
				if err := d.nginxManager.Restart(d.ctx); err != nil {
					log.Printf("Failed to restart nginx: %v", err)
					
					// If we've failed too many times, wait longer before retrying
					if consecutiveFailures >= maxConsecutiveFailures {
						log.Printf("nginx has failed %d consecutive health checks, backing off for 1 minute", consecutiveFailures)
						time.Sleep(1 * time.Minute)
						consecutiveFailures = 0 // Reset counter after backoff
					}
				} else {
					log.Printf("nginx proxy restarted successfully")
					
					// Update configuration after restart
					d.updateNginxConfig()
					
					// Ensure nginx is connected to all networks
					if err := d.nginxManager.EnsureConnectedToAllNetworks(d.ctx); err != nil {
						log.Printf("Warning: failed to connect nginx to all networks after restart: %v", err)
					}
					
					consecutiveFailures = 0 // Reset on successful restart
				}
			} else {
				// nginx is healthy, reset failure counter
				if consecutiveFailures > 0 {
					debugLog("nginx is healthy again, resetting failure counter")
					consecutiveFailures = 0
				}
			}
		case <-d.ctx.Done():
			log.Printf("Stopping nginx health check")
			return
		}
	}
}

// startPIDChecker periodically checks for duplicate daemons
func (d *Daemon) startPIDChecker() {
	// Initial PID write
	if err := d.updatePIDFile(); err != nil {
		log.Printf("Failed to update PID file: %v", err)
	}
	
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if err := d.checkAndUpdatePIDFile(); err != nil {
				log.Printf("PID check error: %v", err)
				// If another daemon is running, shut down
				if err.Error() == "another daemon is running" {
					log.Printf("Another daemon instance detected, shutting down")
					d.Stop()
					os.Exit(1)
				}
			}
		case <-d.ctx.Done():
			return
		}
	}
}

// checkAndUpdatePIDFile checks for other daemons and updates PID file
func (d *Daemon) checkAndUpdatePIDFile() error {
	// Read current PID file
	data, err := os.ReadFile(d.pidFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read PID file: %w", err)
	}
	
	myPID := os.Getpid()
	var pids []int
	
	if len(data) > 0 {
		// Parse existing PIDs
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for _, line := range lines {
			if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
				// Check if process is alive
				if isProcessAlive(pid) {
					if pid != myPID {
						// Another daemon is running
						return fmt.Errorf("another daemon is running")
					}
					pids = append(pids, pid)
				}
			}
		}
	}
	
	// If we're not in the list, add ourselves
	found := false
	for _, pid := range pids {
		if pid == myPID {
			found = true
			break
		}
	}
	
	if !found {
		pids = append(pids, myPID)
		return d.writePIDFile(pids)
	}
	
	return nil
}

// updatePIDFile writes the current PID to file
func (d *Daemon) updatePIDFile() error {
	myPID := os.Getpid()
	return d.writePIDFile([]int{myPID})
}

// writePIDFile writes PIDs to the file
func (d *Daemon) writePIDFile(pids []int) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(d.pidFile), 0755); err != nil {
		return err
	}
	
	var lines []string
	for _, pid := range pids {
		lines = append(lines, strconv.Itoa(pid))
	}
	
	data := []byte(strings.Join(lines, "\n") + "\n")
	return os.WriteFile(d.pidFile, data, 0644)
}

// removePIDFromFile removes current PID from the file
func (d *Daemon) removePIDFromFile() {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return
	}
	
	myPID := os.Getpid()
	var remainingPIDs []int
	
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			if pid != myPID && isProcessAlive(pid) {
				remainingPIDs = append(remainingPIDs, pid)
			}
		}
	}
	
	if len(remainingPIDs) > 0 {
		d.writePIDFile(remainingPIDs)
	} else {
		// No other daemons, remove the file
		os.Remove(d.pidFile)
	}
}

// isProcessAlive checks if a process with given PID is running
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	
	// On Unix, sending signal 0 checks if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}