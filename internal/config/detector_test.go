package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name: "basic parsing",
			content: `KEY1=value1
KEY2=value2
KEY3=value3`,
			expected: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
				"KEY3": "value3",
			},
		},
		{
			name: "with quotes",
			content: `KEY1="value with spaces"
KEY2='single quotes'
KEY3=no_quotes`,
			expected: map[string]string{
				"KEY1": "value with spaces",
				"KEY2": "single quotes",
				"KEY3": "no_quotes",
			},
		},
		{
			name: "with comments and empty lines",
			content: `# This is a comment
KEY1=value1

# Another comment
KEY2=value2
`,
			expected: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseEnvFile(tt.content)
			
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d keys, got %d", len(tt.expected), len(result))
			}
			
			for key, expectedValue := range tt.expected {
				if actualValue, exists := result[key]; !exists {
					t.Errorf("Missing key: %s", key)
				} else if actualValue != expectedValue {
					t.Errorf("Key %s: expected '%s', got '%s'", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestMergeEnvMaps(t *testing.T) {
	existing := map[string]string{
		"KEEP_ME":     "original",
		"OVERRIDE_ME": "old_value",
		"UNIQUE_KEY":  "unique_value",
		"HAS_VALUE":   "actual_secret",
		"EMPTY_KEY":   "",
	}
	
	updates := map[string]string{
		"OVERRIDE_ME": "new_value",
		"NEW_KEY":     "new_value",
		"HAS_VALUE":   "", // Empty value should not overwrite non-empty
		"EMPTY_KEY":   "", // Empty can overwrite empty
		"ANOTHER_EMPTY": "",
	}
	
	merged := mergeEnvMaps(existing, updates)
	
	// Check that unique keys from existing are preserved
	if merged["KEEP_ME"] != "original" {
		t.Errorf("Expected KEEP_ME to be 'original', got '%s'", merged["KEEP_ME"])
	}
	if merged["UNIQUE_KEY"] != "unique_value" {
		t.Errorf("Expected UNIQUE_KEY to be 'unique_value', got '%s'", merged["UNIQUE_KEY"])
	}
	
	// Check that overridden keys use new values
	if merged["OVERRIDE_ME"] != "new_value" {
		t.Errorf("Expected OVERRIDE_ME to be 'new_value', got '%s'", merged["OVERRIDE_ME"])
	}
	
	// Check that new keys are added
	if merged["NEW_KEY"] != "new_value" {
		t.Errorf("Expected NEW_KEY to be 'new_value', got '%s'", merged["NEW_KEY"])
	}
	
	// Check that empty values don't overwrite non-empty values
	if merged["HAS_VALUE"] != "actual_secret" {
		t.Errorf("Expected HAS_VALUE to remain 'actual_secret', got '%s'", merged["HAS_VALUE"])
	}
	
	// Check that empty can overwrite empty
	if merged["EMPTY_KEY"] != "" {
		t.Errorf("Expected EMPTY_KEY to remain empty, got '%s'", merged["EMPTY_KEY"])
	}
	
	// Check that new empty keys are added
	if _, exists := merged["ANOTHER_EMPTY"]; !exists {
		t.Error("Expected ANOTHER_EMPTY to be added even though it's empty")
	}
}

func TestFormatEnvFile(t *testing.T) {
	envMap := map[string]string{
		"KEY1": "value1",
		"KEY2": "value with spaces",
		"KEY3": "value3",
	}
	
	originalContent := `# Header comment
KEY1=old_value
KEY2="old value with quotes"
# Middle comment

KEY_TO_REMOVE=remove_me
KEY3='single quotes'
`
	
	result := formatEnvFile(envMap, originalContent)
	
	// Check that comments are preserved
	if !strings.Contains(result, "# Header comment") {
		t.Error("Header comment not preserved")
	}
	if !strings.Contains(result, "# Middle comment") {
		t.Error("Middle comment not preserved")
	}
	
	// Check that values are updated
	if !strings.Contains(result, "KEY1=value1") {
		t.Error("KEY1 not updated correctly")
	}
	if !strings.Contains(result, `KEY2="value with spaces"`) {
		t.Error("KEY2 not updated correctly with quotes preserved")
	}
	
	// Check that KEY_TO_REMOVE is not in result
	if strings.Contains(result, "KEY_TO_REMOVE") {
		t.Error("KEY_TO_REMOVE should have been removed")
	}
}

func TestProcessEnvFilesWithTemplatingMerge(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "worklet-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	
	// Create an existing .env file with actual values
	existingEnv := `# Existing config
SOME_KEY=original_value
ANOTHER_KEY=keep_this
DATABASE_URL=old_database_url
UNIQUE_KEY=should_remain
SECRET_KEY=actual_secret_value
API_TOKEN=real_token_123
`
	err = os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(existingEnv), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	// Create a .env.example file with template and empty placeholders
	envExample := `# Example config
SOME_KEY={{services.app.url}}
DATABASE_URL={{services.db.url}}
NEW_KEY=new_value
SECRET_KEY=
API_TOKEN=
EMPTY_PLACEHOLDER=
`
	err = os.WriteFile(filepath.Join(tmpDir, ".env.example"), []byte(envExample), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	// Define services for templating
	services := []ServiceConfig{
		{
			Name:      "app",
			Port:      3000,
			Subdomain: "app",
		},
		{
			Name:      "db",
			Port:      5432,
			Subdomain: "database",
		},
	}
	
	// Process the files
	err = ProcessEnvFilesWithTemplating(tmpDir, tmpDir, "test-session", "test-project", services)
	if err != nil {
		t.Fatal(err)
	}
	
	// Read the resulting .env file
	resultContent, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	
	result := string(resultContent)
	
	// Check that templated values replaced old values
	if !strings.Contains(result, "SOME_KEY=http://app.test-project-test-session.local.worklet.sh") {
		t.Error("SOME_KEY was not templated correctly")
	}
	if !strings.Contains(result, "DATABASE_URL=http://database.test-project-test-session.local.worklet.sh") {
		t.Error("DATABASE_URL was not templated correctly")
	}
	
	// Check that unique existing keys are preserved
	if !strings.Contains(result, "ANOTHER_KEY=keep_this") {
		t.Error("ANOTHER_KEY was not preserved")
	}
	if !strings.Contains(result, "UNIQUE_KEY=should_remain") {
		t.Error("UNIQUE_KEY was not preserved")
	}
	
	// Check that new keys from .env.example are added
	if !strings.Contains(result, "NEW_KEY=new_value") {
		t.Error("NEW_KEY was not added")
	}
	
	// Check that empty values in .env.example don't overwrite non-empty values
	if !strings.Contains(result, "SECRET_KEY=actual_secret_value") {
		t.Error("SECRET_KEY should have kept its original value")
	}
	if !strings.Contains(result, "API_TOKEN=real_token_123") {
		t.Error("API_TOKEN should have kept its original value")
	}
	
	// Check that empty placeholder is added
	if !strings.Contains(result, "EMPTY_PLACEHOLDER=") {
		t.Error("EMPTY_PLACEHOLDER should be added even though it's empty")
	}
	
	// Check that comments are preserved
	if !strings.Contains(result, "# Existing config") {
		t.Error("Comments were not preserved")
	}
}