package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PersistentMapping represents a stored fork mapping with metadata
type PersistentMapping struct {
	ForkID       string                    `json:"fork_id"`
	RandomHost   string                    `json:"random_host"`
	ServicePorts map[string]ServicePort    `json:"service_ports"`
	CreatedAt    time.Time                 `json:"created_at"`
	UpdatedAt    time.Time                 `json:"updated_at"`
	IsActive     bool                      `json:"is_active"`
}

// PersistenceStore handles saving and loading proxy mappings
type PersistenceStore struct {
	filePath string
}

// NewPersistenceStore creates a new persistence store
func NewPersistenceStore(configDir string) *PersistenceStore {
	return &PersistenceStore{
		filePath: filepath.Join(configDir, "mappings.json"),
	}
}

// Load reads persisted mappings from disk
func (ps *PersistenceStore) Load() (map[string]*PersistentMapping, error) {
	Debug("Loading mappings from %s", ps.filePath)
	mappings := make(map[string]*PersistentMapping)
	
	// If file doesn't exist, return empty map
	if _, err := os.Stat(ps.filePath); os.IsNotExist(err) {
		Debug("Mappings file does not exist (first run or reset)")
		return mappings, nil
	}
	
	data, err := os.ReadFile(ps.filePath)
	if err != nil {
		DebugError("read mappings file", err)
		return nil, fmt.Errorf("failed to read mappings file: %w", err)
	}
	
	Debug("Read %d bytes from mappings file", len(data))
	
	if len(data) == 0 {
		Debug("Mappings file is empty")
		return mappings, nil
	}
	
	if err := json.Unmarshal(data, &mappings); err != nil {
		Debug("Failed to unmarshal mappings: %s", string(data))
		DebugError("unmarshal mappings", err)
		return nil, fmt.Errorf("failed to unmarshal mappings: %w", err)
	}
	
	Debug("Loaded %d mappings from persistence", len(mappings))
	for forkID, mapping := range mappings {
		Debug("  Fork %s: %d services, host=%s", forkID, len(mapping.ServicePorts), mapping.RandomHost)
	}
	
	return mappings, nil
}

// Save writes mappings to disk
func (ps *PersistenceStore) Save(mappings map[string]*PersistentMapping) error {
	Debug("Saving %d mappings to %s", len(mappings), ps.filePath)
	
	// Ensure directory exists
	dir := filepath.Dir(ps.filePath)
	Debug("Ensuring directory exists: %s", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		DebugError("create directory", err)
		return fmt.Errorf("failed to create directory: %w", err)
	}
	
	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		DebugError("marshal mappings", err)
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}
	
	Debug("Writing %d bytes to mappings file", len(data))
	if err := os.WriteFile(ps.filePath, data, 0644); err != nil {
		DebugError("write mappings file", err)
		return fmt.Errorf("failed to write mappings file: %w", err)
	}
	
	Debug("Mappings saved successfully")
	return nil
}

// ConvertToPersistent converts a ForkMapping to PersistentMapping
func ConvertToPersistent(fm *ForkMapping, isActive bool) *PersistentMapping {
	return &PersistentMapping{
		ForkID:       fm.ForkID,
		RandomHost:   fm.RandomHost,
		ServicePorts: fm.ServicePorts,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		IsActive:     isActive,
	}
}

// ConvertFromPersistent converts a PersistentMapping to ForkMapping
func ConvertFromPersistent(pm *PersistentMapping) *ForkMapping {
	return &ForkMapping{
		ForkID:       pm.ForkID,
		RandomHost:   pm.RandomHost,
		ServicePorts: pm.ServicePorts,
	}
}