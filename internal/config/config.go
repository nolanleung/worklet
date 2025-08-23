package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	SSH    bool `json:"ssh,omitempty"`    // Mount SSH credentials volume
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

// LoadConfigOrDetect loads config from .worklet.jsonc or detects project type
func LoadConfigOrDetect(dir string, isClonedRepo bool) (*WorkletConfig, error) {
	// First try to load existing config
	config, err := LoadConfig(dir)
	if err == nil {
		// If it's a cloned repo and Claude is not enabled, enable it if credentials exist
		if isClonedRepo && (config.Run.Credentials == nil || !config.Run.Credentials.Claude) {
			// Check if Claude credentials are available
			if hasClaudeCredentials() {
				if config.Run.Credentials == nil {
					config.Run.Credentials = &CredentialConfig{}
				}
				config.Run.Credentials.Claude = true
			}
		}
		return config, nil
	}

	// If config doesn't exist, try to detect project type
	if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
		projectType, detectErr := DetectProjectType(dir)
		if detectErr != nil {
			return nil, fmt.Errorf("failed to detect project type: %w", detectErr)
		}

		// Generate default config based on detected type
		defaultConfig, genErr := GenerateDefaultConfig(dir, projectType, isClonedRepo)
		if genErr != nil {
			return nil, genErr
		}

		// Log what we detected
		if projectType == ProjectTypeNodeJS {
			packageManager := DetectPackageManager(dir)
			scriptName := defaultConfig.Run.Command[len(defaultConfig.Run.Command)-1]
			if packageManager != "deno" {
				fmt.Printf("No .worklet.jsonc found. Detected Node.js project, will run '%s install' then '%s run %s'\n",
					packageManager, packageManager, scriptName)
			} else {
				fmt.Printf("No .worklet.jsonc found. Detected Deno project, using 'deno task %s'\n", scriptName)
			}
		}

		return defaultConfig, nil
	}

	// If it's another error, return it
	return nil, err
}

// hasClaudeCredentials checks if Claude credentials are configured
func hasClaudeCredentials() bool {
	// Import cycle prevention - we'll check this differently
	// For now, we'll use a simple volume check
	cmd := exec.Command("docker", "volume", "inspect", "worklet-claude-credentials")
	err := cmd.Run()
	return err == nil
}
