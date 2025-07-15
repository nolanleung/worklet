package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	globalServer *Server
	globalMu     sync.Mutex
)

// InitGlobalProxy initializes the global proxy server
func InitGlobalProxy() error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalServer != nil {
		return nil // Already initialized
	}

	// Get worklet home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".worklet", "proxy")

	server, err := NewServer(configDir)
	if err != nil {
		return fmt.Errorf("failed to create proxy server: %w", err)
	}

	globalServer = server
	return nil
}

// GetGlobalProxy returns the global proxy server
func GetGlobalProxy() (*Server, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalServer == nil {
		return nil, fmt.Errorf("proxy not initialized")
	}

	return globalServer, nil
}

// StartGlobalProxy starts the global proxy server
func StartGlobalProxy(ctx context.Context) error {
	server, err := GetGlobalProxy()
	if err != nil {
		return err
	}

	return server.Start(ctx)
}

// StopGlobalProxy stops the global proxy server
func StopGlobalProxy() error {
	server, err := GetGlobalProxy()
	if err != nil {
		return err
	}

	return server.Stop()
}

// RegisterForkWithProxy registers a fork with the global proxy
func RegisterForkWithProxy(forkID string, services []ServicePort) (*ForkMapping, error) {
	server, err := GetGlobalProxy()
	if err != nil {
		return nil, err
	}

	return server.RegisterFork(forkID, services)
}

// UnregisterForkFromProxy removes a fork from the global proxy
func UnregisterForkFromProxy(forkID string) error {
	server, err := GetGlobalProxy()
	if err != nil {
		return err
	}

	return server.UnregisterFork(forkID)
}
