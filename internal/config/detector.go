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

// DetectPythonPackageManager detects which Python package manager to use
func DetectPythonPackageManager(dir string) string {
	// Check in order of preference
	if _, err := os.Stat(filepath.Join(dir, "uv.lock")); err == nil {
		return "uv"
	}
	if _, err := os.Stat(filepath.Join(dir, "poetry.lock")); err == nil {
		return "poetry"
	}
	if _, err := os.Stat(filepath.Join(dir, "Pipfile.lock")); err == nil {
		return "pipenv"
	}
	if _, err := os.Stat(filepath.Join(dir, "Pipfile")); err == nil {
		return "pipenv"
	}
	// Check for pyproject.toml with poetry section
	if data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
		if strings.Contains(string(data), "[tool.poetry]") {
			return "poetry"
		}
	}
	// Default to pip
	return "pip"
}

// DetectPythonCommand detects the appropriate command for a Python project
func DetectPythonCommand(dir string) []string {
	// Common Python entry points in order of preference
	entryPoints := []string{
		"main.py",
		"app.py",
		"manage.py",
		"wsgi.py",
		"asgi.py",
		"run.py",
		"server.py",
		"__main__.py",
	}

	for _, entry := range entryPoints {
		if _, err := os.Stat(filepath.Join(dir, entry)); err == nil {
			// Special handling for Django manage.py
			if entry == "manage.py" {
				return []string{"python", "manage.py", "runserver", "0.0.0.0:8000"}
			}
			// Special handling for WSGI apps
			if entry == "wsgi.py" {
				return []string{"gunicorn", "--bind", "0.0.0.0:8000", "wsgi:application"}
			}
			// Special handling for ASGI apps
			if entry == "asgi.py" {
				return []string{"uvicorn", "asgi:application", "--host", "0.0.0.0", "--port", "8000"}
			}
			// Default Python execution
			return []string{"python", entry}
		}
	}

	// Check for common framework patterns in subdirectories
	if _, err := os.Stat(filepath.Join(dir, "src", "main.py")); err == nil {
		return []string{"python", "src/main.py"}
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "main.py")); err == nil {
		return []string{"python", "app/main.py"}
	}

	// Default to Python REPL if no entry point found
	return []string{"python"}
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

// parseEnvFile parses environment file content into a map
func parseEnvFile(content string) map[string]string {
	envMap := make(map[string]string)
	lines := strings.Split(content, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Find the first = sign
		equalIndex := strings.Index(line, "=")
		if equalIndex == -1 {
			continue
		}
		
		key := strings.TrimSpace(line[:equalIndex])
		value := strings.TrimSpace(line[equalIndex+1:])
		
		// Remove surrounding quotes if present
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
			   (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		
		envMap[key] = value
	}
	
	return envMap
}

// formatEnvFile formats a map back into environment file content
func formatEnvFile(envMap map[string]string, originalContent string) string {
	// Parse original content to preserve order and comments
	var result []string
	processedKeys := make(map[string]bool)
	
	if originalContent != "" {
		lines := strings.Split(originalContent, "\n")
		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			
			// Preserve empty lines and comments
			if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
				result = append(result, line)
				continue
			}
			
			// Check if this is a key-value pair
			equalIndex := strings.Index(line, "=")
			if equalIndex == -1 {
				result = append(result, line)
				continue
			}
			
			key := strings.TrimSpace(line[:equalIndex])
			
			// If this key exists in our map, use the new value
			if newValue, exists := envMap[key]; exists {
				// Preserve the original formatting style
				if strings.Contains(line, "\"") {
					result = append(result, fmt.Sprintf("%s=\"%s\"", key, newValue))
				} else if strings.Contains(line, "'") {
					result = append(result, fmt.Sprintf("%s='%s'", key, newValue))
				} else {
					result = append(result, fmt.Sprintf("%s=%s", key, newValue))
				}
				processedKeys[key] = true
			} else {
				// Key was removed, skip it
				continue
			}
		}
	}
	
	// Add any new keys that weren't in the original
	for key, value := range envMap {
		if !processedKeys[key] {
			// Use double quotes for new values that contain spaces or special characters
			if strings.ContainsAny(value, " \t\n'\"\\$") {
				result = append(result, fmt.Sprintf("%s=\"%s\"", key, value))
			} else {
				result = append(result, fmt.Sprintf("%s=%s", key, value))
			}
		}
	}
	
	return strings.Join(result, "\n")
}

// mergeEnvMaps merges two environment maps, with updates taking precedence
// Empty values from updates will not overwrite non-empty existing values
func mergeEnvMaps(existing, updates map[string]string) map[string]string {
	merged := make(map[string]string)
	
	// Copy existing values
	for k, v := range existing {
		merged[k] = v
	}
	
	// Override with updates, but only if:
	// - The update value is non-empty, OR
	// - The existing value is also empty
	for k, v := range updates {
		if v != "" || existing[k] == "" {
			merged[k] = v
		}
		// If v is empty and existing[k] is non-empty, keep existing value
	}
	
	return merged
}

// ProcessEnvFilesWithTemplating processes .env.example files and applies templating
// srcDir is the source directory to read .env.example files from
// targetDir is the directory where processed .env files will be written (can be different from srcDir)
func ProcessEnvFilesWithTemplating(srcDir, targetDir string, sessionID string, projectName string, services []ServiceConfig) error {
	// Find all .env.example files in source directory
	envExampleFiles, err := DetectEnvExampleFiles(srcDir)
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
		// Read the example file from source directory
		examplePath := filepath.Join(srcDir, exampleFile)
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

		// Write to target directory (which may be different from source)
		targetPath := filepath.Join(targetDir, targetFile)

		// Create parent directories in target if needed
		targetSubdir := filepath.Dir(targetFile)
		if targetSubdir != "." {
			fullTargetDir := filepath.Join(targetDir, targetSubdir)
			if err := os.MkdirAll(fullTargetDir, 0755); err != nil {
				return fmt.Errorf("failed to create target directory %s: %w", fullTargetDir, err)
			}
		}

		// Process template
		processedContent := env.ProcessTemplate(string(content), ctx)
		
		// Parse the processed .env.example into a map
		exampleEnvMap := parseEnvFile(processedContent)
		
		var finalContent string
		
		// Check if target .env already exists
		if existingContent, err := os.ReadFile(targetPath); err == nil {
			// Target exists, merge with existing content
			existingEnvMap := parseEnvFile(string(existingContent))
			
			// Merge maps: existing values are kept, but overridden by example values
			mergedEnvMap := mergeEnvMaps(existingEnvMap, exampleEnvMap)
			
			// Format back to env file, preserving original structure where possible
			finalContent = formatEnvFile(mergedEnvMap, string(existingContent))
		} else {
			// Target doesn't exist, use processed content directly
			finalContent = formatEnvFile(exampleEnvMap, processedContent)
		}

		// Write final content to target file
		if err := os.WriteFile(targetPath, []byte(finalContent), 0644); err != nil {
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
		command := DetectPythonCommand(dir)
		packageManager := DetectPythonPackageManager(dir)

		// Build init script with install command
		var initScript []string

		// Add package installation based on detected package manager
		switch packageManager {
		case "uv":
			initScript = append(initScript, "uv pip install -r requirements.txt")
		case "poetry":
			initScript = append(initScript, "poetry install")
		case "pipenv":
			initScript = append(initScript, "pipenv install")
		default:
			// Check for requirements.txt
			if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
				initScript = append(initScript, "pip install -r requirements.txt")
			} else if _, err := os.Stat(filepath.Join(dir, "requirements")); err == nil {
				// Check for requirements directory
				initScript = append(initScript, "pip install -r requirements/requirements.txt")
			} else if _, err := os.Stat(filepath.Join(dir, "setup.py")); err == nil {
				// Install from setup.py
				initScript = append(initScript, "pip install -e .")
			} else if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
				// Install from pyproject.toml
				initScript = append(initScript, "pip install -e .")
			}
		}

		// Install common web frameworks if detected in command
		for _, cmd := range command {
			if strings.Contains(cmd, "gunicorn") {
				initScript = append(initScript, "pip install gunicorn")
			}
			if strings.Contains(cmd, "uvicorn") {
				initScript = append(initScript, "pip install uvicorn")
			}
		}

		config := &WorkletConfig{
			Name: projectName,
			Run: RunConfig{
				Image:      "worklet/base:latest",
				Command:    command,
				InitScript: initScript,
				Environment: map[string]string{
					"PYTHONUNBUFFERED":                "1",
					"COREPACK_ENABLE_DOWNLOAD_PROMPT": "0",
					"DOCKER_TLS_CERTDIR":              "",
					"DOCKER_DRIVER":                   "overlay2",
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

		// Log what we detected
		fmt.Printf("No .worklet.jsonc found. Detected Python project using %s\n", packageManager)
		if len(command) > 0 && command[0] != "python" {
			fmt.Printf("Will run: %s\n", strings.Join(command, " "))
		}

		return config, nil

	default:
		return nil, fmt.Errorf("couldn't detect project type. Please create a .worklet.jsonc file")
	}
}
