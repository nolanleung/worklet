package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// Manager manages the reverse proxy for worklet forks
type Manager struct {
	mu       sync.RWMutex
	mappings map[string]*ForkMapping // key is fork ID
}

// ForkMapping represents the proxy mapping for a single fork
type ForkMapping struct {
	ForkID       string
	RandomHost   string // e.g., "x7k9m2p4"
	ServicePorts map[string]ServicePort
}

// ServicePort represents a service and its port mapping
type ServicePort struct {
	ServiceName   string
	ContainerPort int
	Subdomain     string
	ContainerName string // e.g., "worklet-fork-abc123-api"
}

// NewManager creates a new proxy manager
func NewManager() *Manager {
	return &Manager{
		mappings: make(map[string]*ForkMapping),
	}
}

// RegisterFork registers a new fork with the proxy
func (m *Manager) RegisterFork(forkID string, services []ServicePort) (*ForkMapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate random host
	randomHost, err := generateRandomHost()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random host: %w", err)
	}

	// Create service mappings
	servicePorts := make(map[string]ServicePort)
	for _, service := range services {
		service.ContainerName = fmt.Sprintf("worklet-%s-%s", forkID, service.ServiceName)
		servicePorts[service.ServiceName] = service
	}

	mapping := &ForkMapping{
		ForkID:       forkID,
		RandomHost:   randomHost,
		ServicePorts: servicePorts,
	}

	m.mappings[forkID] = mapping
	return mapping, nil
}

// UnregisterFork removes a fork from the proxy
func (m *Manager) UnregisterFork(forkID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.mappings[forkID]; !exists {
		return fmt.Errorf("fork %s not found", forkID)
	}

	delete(m.mappings, forkID)
	return nil
}

// GetMapping returns the mapping for a fork
func (m *Manager) GetMapping(forkID string) (*ForkMapping, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mapping, exists := m.mappings[forkID]
	if !exists {
		return nil, fmt.Errorf("fork %s not found", forkID)
	}

	return mapping, nil
}

// GetAllMappings returns all current mappings
func (m *Manager) GetAllMappings() map[string]*ForkMapping {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[string]*ForkMapping)
	for k, v := range m.mappings {
		result[k] = v
	}
	return result
}

// GetServiceURL returns the full URL for a service
func (m *ForkMapping) GetServiceURL(serviceName string) (string, error) {
	service, exists := m.ServicePorts[serviceName]
	if !exists {
		return "", fmt.Errorf("service %s not found", serviceName)
	}

	return fmt.Sprintf("http://%s.%s.fork.local.worklet.sh", service.Subdomain, m.RandomHost), nil
}

// generateRandomHost generates a random 8-character hostname
func generateRandomHost() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}