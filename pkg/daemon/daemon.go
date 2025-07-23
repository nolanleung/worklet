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
	"strings"
	"sync"
	"time"
	
	"github.com/docker/docker/api/types/container"
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
	
	// Load state
	if err := d.loadState(); err != nil {
		log.Printf("Failed to load state: %v", err)
	}
	
	// Validate and cleanup stale forks
	if err := d.validateAndCleanupForks(); err != nil {
		log.Printf("Failed to validate forks: %v", err)
	}
	
	// Start accepting connections
	go d.acceptConnections()
	
	// Start cleanup routine
	go d.cleanupRoutine()
	
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
	case MsgHealthCheck:
		return &Message{
			Type: MsgSuccess,
			ID:   msg.ID,
		}
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
	
	// Save state after registration
	go d.saveState()
	
	// Update nginx configuration
	d.updateNginxConfig()
	
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
	
	// Save state after unregistration
	go d.saveState()
	
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

// cleanupRoutine periodically cleans up stale fork registrations
func (d *Daemon) cleanupRoutine() {
	ticker := time.NewTicker(5 * time.Minute)
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
		// Save updated state
		go d.saveState()
		
		// Update nginx configuration
		d.updateNginxConfig()
		
		log.Printf("Cleaned up %d stale fork(s)", len(forksToRemove))
	}

	return nil
}

// State persistence methods
func (d *Daemon) saveState() error {
	d.forksMu.RLock()
	defer d.forksMu.RUnlock()
	
	data, err := json.MarshalIndent(d.forks, "", "  ")
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
	
	return json.Unmarshal(data, &d.forks)
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