package proxy

import (
	"fmt"
	"os"
	"time"
)

// DebugLogger provides debug logging functionality
type DebugLogger struct {
	enabled bool
}

var globalDebugLogger = &DebugLogger{enabled: false}

// EnableDebug enables debug logging
func EnableDebug() {
	globalDebugLogger.enabled = true
}

// DisableDebug disables debug logging
func DisableDebug() {
	globalDebugLogger.enabled = false
}

// IsDebugEnabled returns whether debug logging is enabled
func IsDebugEnabled() bool {
	return globalDebugLogger.enabled
}

// Debug logs a debug message if debug mode is enabled
func Debug(format string, args ...interface{}) {
	if globalDebugLogger.enabled {
		timestamp := time.Now().Format("15:04:05.000")
		prefix := fmt.Sprintf("[DEBUG %s]", timestamp)
		message := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "%s %s\n", prefix, message)
	}
}

// DebugError logs an error with additional context in debug mode
func DebugError(context string, err error) {
	if globalDebugLogger.enabled && err != nil {
		Debug("ERROR in %s: %v", context, err)
	}
}

// DebugDuration logs the duration of an operation
func DebugDuration(operation string, start time.Time) {
	if globalDebugLogger.enabled {
		duration := time.Since(start)
		Debug("%s took %v", operation, duration)
	}
}