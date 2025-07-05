package fork

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nolanleung/worklet/internal/config"
)

func TestCopyDirectory(t *testing.T) {
	// Create test source directory structure
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create test file structure
	testFiles := map[string]string{
		"file1.txt":                      "content1",
		"file2.log":                      "should be excluded",
		"subdir/file3.txt":               "content3",
		"subdir/nested/file4.txt":        "content4",
		"node_modules/package.json":      "should be excluded",
		"node_modules/lib/index.js":      "should be excluded",
		"build/output.js":                "should be excluded",
		".DS_Store":                      "should be excluded",
		"src/main.go":                    "package main",
		"src/test/main_test.go":          "package main",
	}

	// Create test files
	for path, content := range testFiles {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}

	// Create a symlink
	srcFile := filepath.Join(srcDir, "file1.txt")
	linkFile := filepath.Join(srcDir, "link_to_file1.txt")
	if err := os.Symlink(srcFile, linkFile); err != nil {
		t.Logf("Warning: Failed to create symlink (might not be supported): %v", err)
	}

	// Test exclusion patterns
	exclude := []string{"*.log", "node_modules", "build", ".DS_Store"}

	// Copy directory
	if err := copyDirectory(srcDir, dstDir, exclude); err != nil {
		t.Fatalf("Failed to copy directory: %v", err)
	}

	// Verify files that should exist
	expectedFiles := []string{
		"file1.txt",
		"subdir/file3.txt",
		"subdir/nested/file4.txt",
		"src/main.go",
		"src/test/main_test.go",
	}

	for _, file := range expectedFiles {
		dstFile := filepath.Join(dstDir, file)
		if _, err := os.Stat(dstFile); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not copied", file)
		} else {
			// Verify content
			content, err := os.ReadFile(dstFile)
			if err != nil {
				t.Errorf("Failed to read copied file %s: %v", file, err)
			} else if string(content) != testFiles[file] {
				t.Errorf("File %s content mismatch. Got %s, want %s", file, string(content), testFiles[file])
			}
		}
	}

	// Verify files that should NOT exist
	excludedFiles := []string{
		"file2.log",
		"node_modules/package.json",
		"node_modules/lib/index.js",
		"build/output.js",
		".DS_Store",
	}

	for _, file := range excludedFiles {
		dstFile := filepath.Join(dstDir, file)
		if _, err := os.Stat(dstFile); err == nil {
			t.Errorf("File %s should have been excluded but was copied", file)
		}
	}

	// Test symlink if it was created
	if _, err := os.Lstat(linkFile); err == nil {
		dstLink := filepath.Join(dstDir, "link_to_file1.txt")
		if info, err := os.Lstat(dstLink); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				t.Error("Symlink was not preserved")
			}
		}
	}
}

func TestCreateFork(t *testing.T) {
	// Create test source directory
	srcDir := t.TempDir()

	// Create test config
	cfg := &config.WorkletConfig{
		Fork: config.ForkConfig{
			Name:        "test-fork",
			Description: "Test fork description",
			Exclude:     []string{"*.tmp", "cache"},
		},
	}

	// Create some test files
	testFiles := map[string]string{
		"main.go":       "package main",
		"README.md":     "# Test Project",
		"cache/tmp.txt": "should be excluded",
		"data.tmp":      "should be excluded",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Redirect stdout to capture progress bar output
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	
	// Create fork
	forkPath, err := CreateFork(srcDir, cfg)
	
	// Restore stdout
	w.Close()
	os.Stdout = oldStdout
	
	if err != nil {
		t.Fatalf("Failed to create fork: %v", err)
	}
	defer os.RemoveAll(forkPath)

	// Verify fork directory exists
	if _, err := os.Stat(forkPath); os.IsNotExist(err) {
		t.Fatal("Fork directory was not created")
	}

	// Verify lock file
	lockPath := filepath.Join(forkPath, ".worklet.lock")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	var lockFile LockFile
	if err := json.Unmarshal(lockData, &lockFile); err != nil {
		t.Fatalf("Failed to parse lock file: %v", err)
	}

	// Verify lock file contents
	if lockFile.Name != cfg.Fork.Name {
		t.Errorf("Lock file name mismatch. Got %s, want %s", lockFile.Name, cfg.Fork.Name)
	}
	if lockFile.Description != cfg.Fork.Description {
		t.Errorf("Lock file description mismatch. Got %s, want %s", lockFile.Description, cfg.Fork.Description)
	}
	if lockFile.SourcePath != srcDir {
		t.Errorf("Lock file source path mismatch. Got %s, want %s", lockFile.SourcePath, srcDir)
	}

	// Verify files were copied correctly
	expectedFiles := []string{"main.go", "README.md"}
	for _, file := range expectedFiles {
		dstFile := filepath.Join(forkPath, file)
		if _, err := os.Stat(dstFile); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not copied", file)
		}
	}

	// Verify excluded files
	excludedFiles := []string{"cache/tmp.txt", "data.tmp"}
	for _, file := range excludedFiles {
		dstFile := filepath.Join(forkPath, file)
		if _, err := os.Stat(dstFile); err == nil {
			t.Errorf("File %s should have been excluded but was copied", file)
		}
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{
			name:     "exact match",
			path:     "test.log",
			patterns: []string{"*.log"},
			want:     true,
		},
		{
			name:     "directory match",
			path:     "node_modules/package.json",
			patterns: []string{"node_modules"},
			want:     true,
		},
		{
			name:     "nested directory match",
			path:     "src/node_modules/lib/index.js",
			patterns: []string{"node_modules"},
			want:     true,
		},
		{
			name:     "no match",
			path:     "src/main.go",
			patterns: []string{"*.log", "node_modules"},
			want:     false,
		},
		{
			name:     "DS_Store anywhere",
			path:     "subdir/.DS_Store",
			patterns: []string{".DS_Store"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExclude(tt.path, tt.patterns)
			if got != tt.want {
				t.Errorf("shouldExclude(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}