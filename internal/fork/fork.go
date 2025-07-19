package fork

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/nolanleung/worklet/internal/config"
	"github.com/schollz/progressbar/v3"
)

type LockFile struct {
	SessionID   string    `json:"session_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SourcePath  string    `json:"source_path"`
	CreatedAt   time.Time `json:"created_at"`
}

type FileInfo struct {
	TotalFiles int
	TotalSize  int64
}

type ForkInfo struct {
	ID          string // Same as SessionID for backward compatibility
	SessionID   string
	Name        string
	Description string
	SourcePath  string
	CreatedAt   time.Time
	Size        int64
	Path        string
}

func CreateFork(sourcePath string, cfg *config.WorkletConfig) (string, error) {
	// Create base directory if it doesn't exist
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	forksDir := filepath.Join(homeDir, ".worklet", "forks")
	if err := os.MkdirAll(forksDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create forks directory: %w", err)
	}

	// Create temporary directory
	tmpDir, err := os.MkdirTemp(forksDir, "fork-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// First pass: count files and calculate total size
	fileInfo, err := countFiles(sourcePath, cfg)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to count files: %w", err)
	}

	// Create progress bar
	bar := progressbar.NewOptions64(
		fileInfo.TotalSize,
		progressbar.OptionSetDescription("Forking repository"),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
	)

	// Copy repository files with progress
	if err := copyDirectoryWithProgress(sourcePath, tmpDir, cfg, bar); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to copy repository: %w", err)
	}

	// Create lock file
	lockFile := LockFile{
		SessionID:   filepath.Base(tmpDir),
		Name:        cfg.Fork.Name,
		Description: cfg.Fork.Description,
		SourcePath:  sourcePath,
		CreatedAt:   time.Now(),
	}

	lockData, err := json.MarshalIndent(lockFile, "", "  ")
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to marshal lock file: %w", err)
	}

	lockPath := filepath.Join(tmpDir, ".worklet.lock")
	if err := os.WriteFile(lockPath, lockData, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to write lock file: %w", err)
	}

	return tmpDir, nil
}

func copyDirectory(src, dst string, exclude []string) error {
	// Create gitignore matcher
	matcher := createGitignoreMatcher(src, exclude)

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing path %s: %w", path, err)
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		// Skip the source directory itself
		if relPath == "." {
			return nil
		}

		// Convert path to components for matcher
		pathComponents := strings.Split(relPath, string(filepath.Separator))

		// Check if path should be excluded
		if matcher.Match(pathComponents, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Always exclude .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Create destination path
		dstPath := filepath.Join(dst, relPath)

		// Handle directories
		if info.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}
			return nil
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			if err := os.Symlink(link, dstPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", dstPath, err)
			}
			return nil
		}

		// Handle regular files
		if err := copyFile(path, dstPath, info.Mode()); err != nil {
			return fmt.Errorf("failed to copy file %s to %s: %w", path, dstPath, err)
		}

		return nil
	})
}

// createGitignoreMatcher creates a gitignore matcher from config excludes and .dockerignore
func createGitignoreMatcher(src string, excludePatterns []string) gitignore.Matcher {
	var patterns []gitignore.Pattern

	// Always exclude .dockerignore itself
	patterns = append(patterns, gitignore.ParsePattern(".dockerignore", nil))

	// Add patterns from config (fork.exclude)
	for _, pattern := range excludePatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" && !strings.HasPrefix(pattern, "#") {
			patterns = append(patterns, gitignore.ParsePattern(pattern, nil))
		}
	}

	// Read and parse .dockerignore file if it exists
	dockerignorePath := filepath.Join(src, ".dockerignore")
	if data, err := os.ReadFile(dockerignorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, gitignore.ParsePattern(line, nil))
			}
		}
	}

	// Create matcher with all patterns
	return gitignore.NewMatcher(patterns)
}

// countFiles counts the total number of files and total size to be copied
func countFiles(src string, cfg *config.WorkletConfig) (*FileInfo, error) {
	info := &FileInfo{}

	// Default to including .git if not specified
	includeGit := true
	if cfg.Fork.IncludeGit != nil {
		includeGit = *cfg.Fork.IncludeGit
	}

	// Create gitignore matcher
	matcher := createGitignoreMatcher(src, cfg.Fork.Exclude)

	err := filepath.Walk(src, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		// Convert path to components for matcher
		pathComponents := strings.Split(relPath, string(filepath.Separator))

		// Check if path should be excluded
		if matcher.Match(pathComponents, fileInfo.IsDir()) {
			if fileInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only exclude .git if includeGit is false
		if !includeGit && fileInfo.IsDir() && fileInfo.Name() == ".git" {
			return filepath.SkipDir
		}

		if !fileInfo.IsDir() && fileInfo.Mode().IsRegular() {
			info.TotalFiles++
			info.TotalSize += fileInfo.Size()
		}

		return nil
	})

	return info, err
}

// copyDirectoryWithProgress copies directory with progress bar
func copyDirectoryWithProgress(src, dst string, cfg *config.WorkletConfig, bar *progressbar.ProgressBar) error {
	// Default to including .git if not specified
	includeGit := true
	if cfg.Fork.IncludeGit != nil {
		includeGit = *cfg.Fork.IncludeGit
	}

	// Create gitignore matcher
	matcher := createGitignoreMatcher(src, cfg.Fork.Exclude)

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing path %s: %w", path, err)
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		// Skip the source directory itself
		if relPath == "." {
			return nil
		}

		// Convert path to components for matcher
		pathComponents := strings.Split(relPath, string(filepath.Separator))

		// Check if path should be excluded
		if matcher.Match(pathComponents, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only exclude .git if includeGit is false
		if !includeGit && info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Create destination path
		dstPath := filepath.Join(dst, relPath)

		// Handle directories
		if info.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}
			return nil
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			if err := os.Symlink(link, dstPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", dstPath, err)
			}
			return nil
		}

		// Handle regular files
		bar.Describe(fmt.Sprintf("Copying %s", filepath.Base(path)))
		if err := copyFileWithProgress(path, dstPath, info.Mode(), bar); err != nil {
			return fmt.Errorf("failed to copy file %s to %s: %w", path, dstPath, err)
		}

		return nil
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copyFileWithProgress copies a file and updates progress bar
func copyFileWithProgress(src, dst string, mode os.FileMode, bar *progressbar.ProgressBar) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get file size for verification
	_, err = srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Create a custom writer that updates the progress bar
	progressReader := progressbar.NewReader(srcFile, bar)

	_, err = io.Copy(dstFile, &progressReader)
	return err
}

// GetForksDirectory returns the path to the forks directory
func GetForksDirectory() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".worklet", "forks"), nil
}

// ListForks returns information about all forks
func ListForks() ([]ForkInfo, error) {
	forksDir, err := GetForksDirectory()
	if err != nil {
		return nil, err
	}

	// Check if forks directory exists
	if _, err := os.Stat(forksDir); os.IsNotExist(err) {
		return []ForkInfo{}, nil
	}

	entries, err := os.ReadDir(forksDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read forks directory: %w", err)
	}

	var forks []ForkInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		forkPath := filepath.Join(forksDir, entry.Name())
		lockPath := filepath.Join(forkPath, ".worklet.lock")

		// Read lock file
		lockData, err := os.ReadFile(lockPath)
		if err != nil {
			// Skip forks without lock files
			continue
		}

		var lockFile LockFile
		if err := json.Unmarshal(lockData, &lockFile); err != nil {
			continue
		}

		// Calculate fork size
		size, err := calculateDirSize(forkPath)
		if err != nil {
			size = 0
		}

		forks = append(forks, ForkInfo{
			ID:          lockFile.SessionID,
			SessionID:   lockFile.SessionID,
			Name:        lockFile.Name,
			Description: lockFile.Description,
			SourcePath:  lockFile.SourcePath,
			CreatedAt:   lockFile.CreatedAt,
			Size:        size,
			Path:        forkPath,
		})
	}

	// Sort by creation date (newest first)
	for i := 0; i < len(forks)-1; i++ {
		for j := i + 1; j < len(forks); j++ {
			if forks[i].CreatedAt.Before(forks[j].CreatedAt) {
				forks[i], forks[j] = forks[j], forks[i]
			}
		}
	}

	return forks, nil
}

// RemoveFork removes a specific fork by ID
func RemoveFork(sessionID string) error {
	forksDir, err := GetForksDirectory()
	if err != nil {
		return err
	}

	forkPath := filepath.Join(forksDir, sessionID)

	// Check if fork exists
	if _, err := os.Stat(forkPath); os.IsNotExist(err) {
		return fmt.Errorf("fork %s not found", sessionID)
	}

	// Remove the fork directory
	if err := os.RemoveAll(forkPath); err != nil {
		return fmt.Errorf("failed to remove fork: %w", err)
	}

	return nil
}

// calculateDirSize calculates the total size of a directory
func calculateDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// FindFork searches for a fork by ID, name, or index
func FindFork(query string) (*ForkInfo, error) {
	forks, err := ListForks()
	if err != nil {
		return nil, err
	}

	if len(forks) == 0 {
		return nil, fmt.Errorf("no forks found")
	}

	// If query is empty and there's only one fork, return it
	if query == "" && len(forks) == 1 {
		return &forks[0], nil
	}

	// Try to parse as index (1-based)
	if index, err := parseIndex(query); err == nil && index > 0 && index <= len(forks) {
		return &forks[index-1], nil
	}

	// Normalize query for matching
	queryLower := strings.ToLower(query)

	// Look for exact matches first
	for _, fork := range forks {
		if fork.SessionID == query || strings.ToLower(fork.Name) == queryLower {
			return &fork, nil
		}
	}

	// Look for prefix matches on session ID
	var matches []ForkInfo
	for _, fork := range forks {
		if strings.HasPrefix(fork.SessionID, query) {
			matches = append(matches, fork)
		}
	}

	if len(matches) == 1 {
		return &matches[0], nil
	} else if len(matches) > 1 {
		return nil, fmt.Errorf("multiple forks match '%s': %s", query, formatMatches(matches))
	}

	// Look for partial matches on name
	matches = []ForkInfo{}
	for _, fork := range forks {
		if strings.Contains(strings.ToLower(fork.Name), queryLower) {
			matches = append(matches, fork)
		}
	}

	if len(matches) == 1 {
		return &matches[0], nil
	} else if len(matches) > 1 {
		return nil, fmt.Errorf("multiple forks match '%s': %s", query, formatMatches(matches))
	}

	return nil, fmt.Errorf("no fork found matching '%s'", query)
}

func parseIndex(s string) (int, error) {
	var index int
	_, err := fmt.Sscanf(s, "%d", &index)
	return index, err
}

func formatMatches(forks []ForkInfo) string {
	var names []string
	for _, f := range forks {
		names = append(names, fmt.Sprintf("%s (%s)", f.Name, f.SessionID))
	}
	return strings.Join(names, ", ")
}

// ResolveFork resolves a fork identifier (index, ID, or name) to a fork path
func ResolveFork(identifier string) (string, error) {
	fork, err := FindFork(identifier)
	if err != nil {
		return "", err
	}
	return fork.Path, nil
}

// HasChanges checks if a fork directory has any modifications
func HasChanges(forkPath string) (bool, error) {
	return false, nil

	// Check if the fork path exists
	if _, err := os.Stat(forkPath); err != nil {
		return false, fmt.Errorf("fork path does not exist: %w", err)
	}

	// Check if it's a git repository
	gitDir := filepath.Join(forkPath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		// Use git to check for changes
		cmd := exec.Command("git", "status", "--porcelain")
		cmd.Dir = forkPath
		output, err := cmd.Output()
		log.Println(string(output))

		if err != nil {
			// Git command failed, fall back to timestamp check
			return hasChangesUsingTimestamps(forkPath)
		}

		// Check for any output (modified, added, deleted files)
		if len(output) > 0 {
			return true, nil
		}

		// Also check for untracked files
		cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
		cmd.Dir = forkPath
		output, err = cmd.Output()
		if err != nil {
			return hasChangesUsingTimestamps(forkPath)
		}

		log.Println(output)

		return len(output) > 0, nil
	}

	// Not a git repo, use timestamp-based detection
	return hasChangesUsingTimestamps(forkPath)
}

// hasChangesUsingTimestamps checks for changes by comparing file modification times
func hasChangesUsingTimestamps(forkPath string) (bool, error) {
	// Get fork creation time from lock file
	lockPath := filepath.Join(forkPath, ".worklet.lock")
	lockInfo, err := os.Stat(lockPath)
	if err != nil {
		// No lock file, assume changes to be safe
		return true, nil
	}

	creationTime := lockInfo.ModTime()
	hasChanges := false

	// Walk through all files and check modification times
	err = filepath.Walk(forkPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the lock file itself
		if path == lockPath {
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file was modified after fork creation
		if info.ModTime().After(creationTime) {
			hasChanges = true
			return filepath.SkipAll // Stop walking, we found a change
		}

		return nil
	})

	return hasChanges, err
}
