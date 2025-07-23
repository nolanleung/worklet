package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyWorkspace(t *testing.T) {
	// Create a temporary test directory structure
	srcDir, err := os.MkdirTemp("", "worklet-test-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "worklet-test-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	// Create test file structure
	testFiles := map[string]string{
		"README.md":                      "# Test Project",
		"main.go":                        "package main",
		"test.log":                       "log content",
		"src/app.go":                     "package src",
		"src/debug.log":                  "debug log",
		"node_modules/package/index.js":  "module content",
		"vendor/github.com/lib/lib.go":   "vendor content",
		"build/output.exe":               "binary content",
		".env":                           "SECRET=value",
		".env.example":                   "SECRET=example",
		"temp/cache.tmp":                 "temp data",
		"docs/api.md":                    "api docs",
		"coverage/report.html":           "coverage report",
		".DS_Store":                      "mac file",
		"subdir/node_modules/pkg/a.js":   "nested node_modules",
		"logs/app.log":                   "app log",
		"src/logs/debug.log":             "nested log",
	}

	// Create all test files
	for path, content := range testFiles {
		fullPath := filepath.Join(srcDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create .dockerignore file
	dockerignoreContent := `# Test dockerignore
*.log
node_modules/
**/node_modules/
vendor/
build/
.env
!.env.example
temp/
coverage/
.DS_Store
logs/
**/*.tmp
`
	if err := os.WriteFile(filepath.Join(srcDir, ".dockerignore"), []byte(dockerignoreContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test copyWorkspace with config excludes
	configExcludes := []string{".git", "*.bak"}
	
	// Run copyWorkspace
	if err := copyWorkspace(srcDir, dstDir, configExcludes); err != nil {
		t.Fatalf("copyWorkspace failed: %v", err)
	}

	// Define expected files (files that should be copied)
	expectedFiles := []string{
		"README.md",
		"main.go",
		"src/app.go",
		".env.example", // Should be included due to negation pattern
		"docs/api.md",
	}

	// Define files that should NOT be copied
	excludedFiles := []string{
		"test.log",                     // *.log pattern
		"src/debug.log",                // *.log pattern
		"node_modules/package/index.js", // node_modules/ pattern
		"vendor/github.com/lib/lib.go",  // vendor/ pattern
		"build/output.exe",              // build/ pattern
		".env",                          // .env pattern (but not .env.example)
		"temp/cache.tmp",                // temp/ pattern
		"coverage/report.html",          // coverage/ pattern
		".DS_Store",                     // .DS_Store pattern
		"subdir/node_modules/pkg/a.js",  // **/node_modules/ pattern
		"logs/app.log",                  // logs/ pattern
		"src/logs/debug.log",            // **/*.log pattern
		".dockerignore",                 // Should not copy dockerignore itself
	}

	// Check that expected files exist
	for _, file := range expectedFiles {
		dstPath := filepath.Join(dstDir, file)
		if _, err := os.Stat(dstPath); os.IsNotExist(err) {
			t.Errorf("Expected file not copied: %s", file)
		}
	}

	// Check that excluded files do NOT exist
	for _, file := range excludedFiles {
		dstPath := filepath.Join(dstDir, file)
		if _, err := os.Stat(dstPath); err == nil {
			t.Errorf("File should have been excluded but was copied: %s", file)
		}
	}
}

func TestCopyWorkspaceWithoutDockerignore(t *testing.T) {
	// Create a temporary test directory structure
	srcDir, err := os.MkdirTemp("", "worklet-test-src2-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "worklet-test-dst2-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	// Create test files
	testFiles := map[string]string{
		"main.go":       "package main",
		"test.log":      "log content",
		"node_modules/pkg/index.js": "module content",
		"dist/output.js": "built file",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(srcDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test with config excludes only (no .dockerignore file)
	configExcludes := []string{"node_modules", "*.log", "dist"}
	
	// Run copyWorkspace
	if err := copyWorkspace(srcDir, dstDir, configExcludes); err != nil {
		t.Fatalf("copyWorkspace failed: %v", err)
	}

	// Check that main.go was copied
	if _, err := os.Stat(filepath.Join(dstDir, "main.go")); os.IsNotExist(err) {
		t.Error("main.go should have been copied")
	}

	// Check that excluded files were not copied
	excludedFiles := []string{"test.log", "node_modules/pkg/index.js", "dist/output.js"}
	for _, file := range excludedFiles {
		if _, err := os.Stat(filepath.Join(dstDir, file)); err == nil {
			t.Errorf("File should have been excluded: %s", file)
		}
	}
}