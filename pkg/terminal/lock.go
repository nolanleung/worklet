package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type LockInfo struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

func GetLockFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	
	workletDir := filepath.Join(homeDir, ".worklet")
	if err := os.MkdirAll(workletDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .worklet directory: %w", err)
	}
	
	return filepath.Join(workletDir, "terminal.lock"), nil
}

func IsTerminalRunning() (*LockInfo, bool, error) {
	lockPath, err := GetLockFilePath()
	if err != nil {
		return nil, false, err
	}
	
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read lock file: %w", err)
	}
	
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, false, fmt.Errorf("failed to parse lock file: %w", err)
	}
	
	// Check if process is still running
	process, err := os.FindProcess(info.PID)
	if err != nil {
		return nil, false, nil
	}
	
	// Send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process doesn't exist
		return nil, false, nil
	}
	
	return &info, true, nil
}

func CreateLockFile(port int) error {
	lockPath, err := GetLockFilePath()
	if err != nil {
		return err
	}
	
	info := LockInfo{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now(),
	}
	
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal lock info: %w", err)
	}
	
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}
	
	return nil
}

func RemoveLockFile() error {
	lockPath, err := GetLockFilePath()
	if err != nil {
		return err
	}
	
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}
	
	return nil
}

func CleanStaleLockFile() error {
	info, running, err := IsTerminalRunning()
	if err != nil {
		return err
	}
	
	if info != nil && !running {
		// Lock file exists but process is not running - clean it up
		return RemoveLockFile()
	}
	
	return nil
}