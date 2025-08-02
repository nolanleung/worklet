package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Project represents a worklet project
type Project struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	LastAccessed time.Time `json:"last_accessed"`
	RunCount     int       `json:"run_count"`
	ForkID       string    `json:"fork_id,omitempty"`
	IsRunning    bool      `json:"is_running,omitempty"`
}

// Manager manages the project history
type Manager struct {
	storePath string
	projects  []Project
	mu        sync.RWMutex
}

// NewManager creates a new project manager
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	storePath := filepath.Join(homeDir, ".worklet", "projects.json")
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(storePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	m := &Manager{
		storePath: storePath,
		projects:  []Project{},
	}

	// Load existing projects
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load projects: %w", err)
	}

	return m, nil
}

// AddOrUpdate adds a new project or updates an existing one
func (m *Manager) AddOrUpdate(path, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clean the path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if project already exists
	found := false
	for i, p := range m.projects {
		if p.Path == absPath {
			m.projects[i].LastAccessed = time.Now()
			m.projects[i].RunCount++
			if name != "" {
				m.projects[i].Name = name
			}
			found = true
			break
		}
	}

	// Add new project if not found
	if !found {
		m.projects = append(m.projects, Project{
			Path:         absPath,
			Name:         name,
			LastAccessed: time.Now(),
			RunCount:     1,
		})
	}

	return m.save()
}

// List returns all projects sorted by last accessed time
func (m *Manager) List() []Project {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Make a copy to avoid race conditions
	projects := make([]Project, len(m.projects))
	copy(projects, m.projects)

	// Sort by last accessed time (most recent first)
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].LastAccessed.After(projects[j].LastAccessed)
	})

	return projects
}

// Remove removes a project from the history
func (m *Manager) Remove(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Find and remove the project
	newProjects := []Project{}
	for _, p := range m.projects {
		if p.Path != absPath {
			newProjects = append(newProjects, p)
		}
	}

	m.projects = newProjects
	return m.save()
}

// Clear removes all projects from history
func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.projects = []Project{}
	return m.save()
}

// CleanStale removes projects with non-existent directories
func (m *Manager) CleanStale() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newProjects := []Project{}
	for _, p := range m.projects {
		if _, err := os.Stat(p.Path); err == nil {
			newProjects = append(newProjects, p)
		}
	}

	m.projects = newProjects
	return m.save()
}

// GetProject returns a project by path
func (m *Manager) GetProject(path string) (*Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	for _, p := range m.projects {
		if p.Path == absPath {
			proj := p
			return &proj, nil
		}
	}

	return nil, fmt.Errorf("project not found")
}

// UpdateForkStatus updates the fork status for a project
func (m *Manager) UpdateForkStatus(path, forkID string, isRunning bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	for i, p := range m.projects {
		if p.Path == absPath {
			m.projects[i].ForkID = forkID
			m.projects[i].IsRunning = isRunning
			return m.save()
		}
	}

	return fmt.Errorf("project not found")
}

// save persists the projects to disk
func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.projects, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal projects: %w", err)
	}

	return os.WriteFile(m.storePath, data, 0644)
}

// load reads the projects from disk
func (m *Manager) load() error {
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &m.projects)
}