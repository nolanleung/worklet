package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"
)

type WorkletConfig struct {
	Name     string          `json:"name"` // Project name used for container naming
	Run      RunConfig       `json:"run"`
	Services []ServiceConfig `json:"services"`
}

type RunConfig struct {
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	Volumes     []string          `json:"volumes"`
	Privileged  bool              `json:"privileged"`
	Isolation   string            `json:"isolation"`  // "full" for DinD, "shared" for socket mount (default: "shared")
	InitScript  []string          `json:"initScript"` // Commands to run on container start
	Credentials *CredentialConfig `json:"credentials,omitempty"`
	ComposePath string            `json:"composePath"` // Path to docker-compose.yml file
}

type CredentialConfig struct {
	Claude bool `json:"claude,omitempty"` // Mount Claude credentials volume
}

type ServiceConfig struct {
	Name      string `json:"name"`      // Service name (e.g., "api", "frontend")
	Port      int    `json:"port"`      // Port the service runs on inside container
	Subdomain string `json:"subdomain"` // Subdomain prefix (e.g., "api" for api.project-name.worklet.sh)
}

func LoadConfig(dir string) (*WorkletConfig, error) {
	configPath := filepath.Join(dir, ".worklet.jsonc")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Strip JSONC comments
	jsonData := jsonc.ToJSON(data)

	var config WorkletConfig
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}
