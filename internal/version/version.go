package version

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// Version is the current version of worklet
	// Format: major.minor.patch
	Version = "0.2.0"
	
	// BuildTime can be set at build time using ldflags
	BuildTime = "unknown"
	
	// GitCommit can be set at build time using ldflags
	GitCommit = "unknown"
)

// Info contains version information
type Info struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`
}

// GetInfo returns the current version information
func GetInfo() Info {
	return Info{
		Version:   Version,
		BuildTime: BuildTime,
		GitCommit: GitCommit,
	}
}

// String returns a formatted version string
func (i Info) String() string {
	return fmt.Sprintf("Version: %s\nBuild Time: %s\nGit Commit: %s", i.Version, i.BuildTime, i.GitCommit)
}

// CompareVersions compares two semantic versions
// Returns:
//   -1 if v1 < v2
//    0 if v1 == v2
//    1 if v1 > v2
func CompareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")
	
	parts1 := parseVersion(v1)
	parts2 := parseVersion(v2)
	
	// Compare major, minor, patch
	for i := 0; i < 3; i++ {
		if parts1[i] < parts2[i] {
			return -1
		}
		if parts1[i] > parts2[i] {
			return 1
		}
	}
	
	return 0
}

// parseVersion parses a semantic version string into [major, minor, patch]
func parseVersion(version string) [3]int {
	var result [3]int
	
	// Split by '.' and parse each part
	parts := strings.Split(version, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		// Handle pre-release versions (e.g., "1.0.0-alpha")
		part := strings.Split(parts[i], "-")[0]
		if num, err := strconv.Atoi(part); err == nil {
			result[i] = num
		}
	}
	
	return result
}

// IsNewer returns true if v1 is newer than v2
func IsNewer(v1, v2 string) bool {
	return CompareVersions(v1, v2) > 0
}

// IsOlder returns true if v1 is older than v2
func IsOlder(v1, v2 string) bool {
	return CompareVersions(v1, v2) < 0
}