package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nolanleung/worklet/internal/env"
)

// ProjectType represents the detected project type
type ProjectType string

const (
	ProjectTypeNodeJS  ProjectType = "nodejs"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeUnknown ProjectType = "unknown"
)

// PackageJSON represents a minimal package.json structure
type PackageJSON struct {
	Name    string            `json:"name"`
	Scripts map[string]string `json:"scripts"`
	Main    string            `json:"main"`
}

// DetectProjectType detects the type of project in the given directory
func DetectProjectType(dir string) (ProjectType, error) {
	// Check for Node.js indicators
	nodeFiles := []string{
		"package.json",
		"bun.lockb",
		"deno.json",
		"deno.jsonc",
		"pnpm-lock.yaml",
		"yarn.lock",
		"package-lock.json",
	}

	for _, file := range nodeFiles {
		if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
			return ProjectTypeNodeJS, nil
		}
	}

	// Check for Python indicators
	pythonFiles := []string{
		"requirements.txt",
		"setup.py",
		"pyproject.toml",
		"Pipfile",
		"poetry.lock",
	}

	for _, file := range pythonFiles {
		if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
			return ProjectTypePython, nil
		}
	}

	return ProjectTypeUnknown, nil
}

// DetectPackageManager detects which package manager to use based on lock files
func DetectPackageManager(dir string) string {
	// Check in order of preference
	if _, err := os.Stat(filepath.Join(dir, "bun.lockb")); err == nil {
		return "bun"
	}
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	if _, err := os.Stat(filepath.Join(dir, "deno.json")); err == nil {
		return "deno"
	}
	if _, err := os.Stat(filepath.Join(dir, "deno.jsonc")); err == nil {
		return "deno"
	}
	// Default to npm
	return "npm"
}

// ReadPackageJSON reads and parses package.json
func ReadPackageJSON(dir string) (*PackageJSON, error) {
	packagePath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read package.json: %w", err)
	}

	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse package.json: %w", err)
	}

	return &pkg, nil
}

// DetectNodeCommand detects the appropriate command for a Node.js project
func DetectNodeCommand(dir string) ([]string, error) {
	pkg, err := ReadPackageJSON(dir)
	if err != nil {
		return nil, err
	}

	packageManager := DetectPackageManager(dir)

	// Check for TypeScript
	isTypeScript := false
	if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err == nil {
		isTypeScript = true
	}

	// Priority for scripts
	var scriptName string
	if isTypeScript {
		// For TypeScript, prefer 'dev' over 'start'
		if _, ok := pkg.Scripts["dev"]; ok {
			scriptName = "dev"
		} else if _, ok := pkg.Scripts["start"]; ok {
			scriptName = "start"
		}
	} else {
		// For regular Node.js, prefer 'start' over 'dev'
		if _, ok := pkg.Scripts["start"]; ok {
			scriptName = "start"
		} else if _, ok := pkg.Scripts["dev"]; ok {
			scriptName = "dev"
		}
	}

	if scriptName == "" {
		return nil, fmt.Errorf("no 'dev' or 'start' script found in package.json")
	}

	// Build the command based on package manager
	switch packageManager {
	case "bun":
		return []string{"bun", "run", scriptName}, nil
	case "deno":
		// Deno uses task instead of run
		return []string{"deno", "task", scriptName}, nil
	case "pnpm":
		return []string{"pnpm", "run", scriptName}, nil
	case "yarn":
		return []string{"yarn", "run", scriptName}, nil
	default:
		return []string{"npm", "run", scriptName}, nil
	}
}

// DetectEnvExampleFiles finds all .env.example files in the project directory
func DetectEnvExampleFiles(dir string) ([]string, error) {
	var envFiles []string
	
	// Patterns to look for
	patterns := []string{
		".env.example",
		".env.sample",
		".env.template",
	}
	
	// Walk through the directory to find env example files
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}
		
		// Skip directories
		if info.IsDir() {
			// Skip node_modules and other common directories
			if info.Name() == "node_modules" || info.Name() == ".git" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		
		// Check if file matches any of our patterns
		fileName := info.Name()
		for _, pattern := range patterns {
			if fileName == pattern {
				// Get relative path from the project root
				relPath, err := filepath.Rel(dir, path)
				if err != nil {
					continue
				}
				envFiles = append(envFiles, relPath)
				break
			}
		}
		
		return nil
	})
	
	if err != nil {
		return nil, err
	}
	
	return envFiles, nil
}

// ProcessEnvFilesWithTemplating processes .env.example files and applies templating
func ProcessEnvFilesWithTemplating(dir string, sessionID string, projectName string, services []ServiceConfig) error {
	// Find all .env.example files
	envExampleFiles, err := DetectEnvExampleFiles(dir)
	if err != nil {
		return err
	}

	// Convert ServiceConfig to env.ServiceInfo
	var serviceInfos []env.ServiceInfo
	for _, svc := range services {
		serviceInfos = append(serviceInfos, env.ServiceInfo{
			Name:      svc.Name,
			Port:      svc.Port,
			Subdomain: svc.Subdomain,
		})
	}

	// Create template context
	ctx := env.TemplateContext{
		SessionID:   sessionID,
		ProjectName: projectName,
		Services:    serviceInfos,
	}

	// Process each .env.example file
	for _, exampleFile := range envExampleFiles {
		// Read the example file
		examplePath := filepath.Join(dir, exampleFile)
		content, err := os.ReadFile(examplePath)
		if err != nil {
			continue // Skip if can't read
		}

		// Get the target .env file path
		var targetFile string
		if strings.HasSuffix(exampleFile, ".example") {
			targetFile = strings.TrimSuffix(exampleFile, ".example")
		} else if strings.HasSuffix(exampleFile, ".sample") {
			targetFile = strings.TrimSuffix(exampleFile, ".sample")
		} else if strings.HasSuffix(exampleFile, ".template") {
			targetFile = strings.TrimSuffix(exampleFile, ".template")
		} else {
			continue
		}

		targetPath := filepath.Join(dir, targetFile)

		// Check if target already exists
		if _, err := os.Stat(targetPath); err == nil {
			// Target exists, skip
			continue
		}

		// Process template
		processedContent := env.ProcessTemplate(string(content), ctx)

		// Write processed content to target file
		if err := os.WriteFile(targetPath, []byte(processedContent), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", targetFile, err)
		}
	}

	return nil
}

// GenerateDefaultConfig generates a default config based on detected project type
func GenerateDefaultConfig(dir string, projectType ProjectType, isClonedRepo bool) (*WorkletConfig, error) {
	projectName := filepath.Base(dir)

	switch projectType {
	case ProjectTypeNodeJS:
		// Try to use package.json name if available
		if pkg, err := ReadPackageJSON(dir); err == nil && pkg.Name != "" {
			projectName = pkg.Name
		}
		
		command, err := DetectNodeCommand(dir)
		if err != nil {
			return nil, err
		}

		// Detect package manager for install command
		packageManager := DetectPackageManager(dir)
		
		// Build init script with install command
		var initScript []string
		
		// Detect and process .env.example files with templating
		// Note: This creates a basic copy command, actual templating happens at runtime
		envExampleFiles, _ := DetectEnvExampleFiles(dir)
		for _, exampleFile := range envExampleFiles {
			// Get the target .env file path by removing the .example/.sample/.template suffix
			var targetFile string
			if strings.HasSuffix(exampleFile, ".example") {
				targetFile = strings.TrimSuffix(exampleFile, ".example")
			} else if strings.HasSuffix(exampleFile, ".sample") {
				targetFile = strings.TrimSuffix(exampleFile, ".sample")
			} else if strings.HasSuffix(exampleFile, ".template") {
				targetFile = strings.TrimSuffix(exampleFile, ".template")
			}
			
			// Add copy command that only copies if target doesn't exist
			// The actual templating will be done at container runtime with session context
			copyCmd := fmt.Sprintf("[ ! -f %s ] && cp %s %s && echo 'Created %s from %s' || true", 
				targetFile, exampleFile, targetFile, targetFile, exampleFile)
			initScript = append(initScript, copyCmd)
		}
		
		// Skip install for Deno as it downloads dependencies on demand
		if packageManager != "deno" {
			initScript = append(initScript, fmt.Sprintf("%s install", packageManager))
		}

		config := &WorkletConfig{
			Name: projectName,
			Run: RunConfig{
				Image:      "worklet/base:latest",
				Command:    command,
				InitScript: initScript,
				Environment: map[string]string{
					"COREPACK_ENABLE_DOWNLOAD_PROMPT": "0",
				},
				Privileged: true,
				Isolation:  "full",
			},
		}

		// Enable Claude for cloned repos if credentials are available
		if isClonedRepo && hasClaudeCredentials() {
			config.Run.Credentials = &CredentialConfig{
				Claude: true,
			}
		}

		return config, nil

	case ProjectTypePython:
		return nil, fmt.Errorf("Python project detected. Python runtime support coming soon! Please create a .worklet.jsonc file for now")

	default:
		return nil, fmt.Errorf("couldn't detect project type. Please create a .worklet.jsonc file")
	}
}