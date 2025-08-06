package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// GenerateDefaultConfig generates a default config based on detected project type
func GenerateDefaultConfig(dir string, projectType ProjectType) (*WorkletConfig, error) {
	projectName := filepath.Base(dir)

	switch projectType {
	case ProjectTypeNodeJS:
		command, err := DetectNodeCommand(dir)
		if err != nil {
			return nil, err
		}

		// Detect package manager for install command
		packageManager := DetectPackageManager(dir)
		
		// Build init script with install command
		var initScript []string
		
		// Skip install for Deno as it downloads dependencies on demand
		if packageManager != "deno" {
			initScript = append(initScript, fmt.Sprintf("%s install", packageManager))
		}

		return &WorkletConfig{
			Name: projectName,
			Run: RunConfig{
				Image:      "worklet/base:latest",
				Command:    command,
				InitScript: initScript,
				Privileged: true,
				Isolation:  "full",
			},
		}, nil

	case ProjectTypePython:
		return nil, fmt.Errorf("Python project detected. Python runtime support coming soon! Please create a .worklet.jsonc file for now")

	default:
		return nil, fmt.Errorf("couldn't detect project type. Please create a .worklet.jsonc file")
	}
}