package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SessionInfo represents information about a worklet session container
type SessionInfo struct {
	SessionID     string            `json:"session_id"`
	ProjectName   string            `json:"project_name"`
	ContainerID   string            `json:"container_id"`
	ContainerName string            `json:"container_name"`
	WorkDir       string            `json:"workdir"`
	Status        string            `json:"status"`
	Services      []ServiceInfo     `json:"services"`
	Labels        map[string]string `json:"labels"`
	CreatedAt     time.Time         `json:"created_at"`
}

// ListSessions returns all running worklet sessions discovered via Docker API
func ListSessions(ctx context.Context) ([]SessionInfo, error) {
	return listSessionsWithFilter(ctx, false)
}

// ListAllSessions returns all worklet sessions (including stopped) discovered via Docker API
func ListAllSessions(ctx context.Context) ([]SessionInfo, error) {
	return listSessionsWithFilter(ctx, true)
}

// listSessionsWithFilter is the internal implementation that can list running or all sessions
func listSessionsWithFilter(ctx context.Context, includesStopped bool) ([]SessionInfo, error) {
	// Build docker command based on whether we want all containers or just running ones
	args := []string{"ps"}
	if includesStopped {
		args = append(args, "-a")
	}
	args = append(args, "--filter", "label=worklet.session=true", "--format", "json")

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list docker containers: %w", err)
	}

	var sessions []SessionInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		var container struct {
			ID        string `json:"ID"`
			Names     string `json:"Names"`
			State     string `json:"State"`
			Status    string `json:"Status"`
			Labels    string `json:"Labels"`
			CreatedAt string `json:"CreatedAt"`
		}

		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue // Skip malformed lines
		}

		// Parse labels
		labels := parseLabels(container.Labels)

		// Extract session info from labels
		sessionID := labels["worklet.session.id"]
		if sessionID == "" {
			continue // Skip containers without session ID
		}

		session := SessionInfo{
			SessionID:     sessionID,
			ProjectName:   labels["worklet.project.name"],
			ContainerID:   container.ID,
			ContainerName: container.Names,
			WorkDir:       labels["worklet.workdir"],
			Status:        container.State,
			Labels:        labels,
		}

		// Parse creation time
		if container.CreatedAt != "" {
			session.CreatedAt, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", container.CreatedAt)
		} else {
			session.CreatedAt = time.Now() // Fallback to now if parsing fails
		}

		// Extract services from labels
		session.Services = extractServicesFromLabels(labels)

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// GetSessionInfo returns information about a specific session
func GetSessionInfo(ctx context.Context, sessionID string) (*SessionInfo, error) {
	sessions, err := ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	for _, session := range sessions {
		if session.SessionID == sessionID {
			return &session, nil
		}
	}

	return nil, fmt.Errorf("session %s not found", sessionID)
}

// ListSessionsByProject returns all sessions for a specific project
func ListSessionsByProject(ctx context.Context, projectName string) ([]SessionInfo, error) {
	sessions, err := ListSessions(ctx)
	if err != nil {
		return nil, err
	}

	var projectSessions []SessionInfo
	for _, session := range sessions {
		if session.ProjectName == projectName {
			projectSessions = append(projectSessions, session)
		}
	}

	return projectSessions, nil
}

// StopSession stops a worklet session container
func StopSession(ctx context.Context, sessionID string) error {
	session, err := GetSessionInfo(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "stop", session.ContainerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	return nil
}

// RemoveSession removes a worklet session container
func RemoveSession(ctx context.Context, sessionID string) error {
	session, err := GetSessionInfo(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", session.ContainerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// AttachToSession attaches to a running session container
func AttachToSession(ctx context.Context, sessionID string) error {
	session, err := GetSessionInfo(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	// Check if container is running
	if session.Status != "running" {
		// Start the container if it's not running
		startCmd := exec.CommandContext(ctx, "docker", "start", session.ContainerID)
		if err := startCmd.Run(); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	}

	// Get TERM from host environment, or use a sensible default
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}

	// Use docker exec -it for a full interactive terminal experience with a new shell
	cmd := exec.Command("docker", "exec", "-it", "-e", "TERM="+term, session.ContainerID, "/bin/sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// parseLabels parses Docker labels from a comma-separated string
func parseLabels(labelStr string) map[string]string {
	labels := make(map[string]string)
	if labelStr == "" {
		return labels
	}

	pairs := strings.Split(labelStr, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			labels[parts[0]] = parts[1]
		}
	}

	return labels
}

// extractServicesFromLabels extracts service information from container labels
func extractServicesFromLabels(labels map[string]string) []ServiceInfo {
	serviceMap := make(map[string]*ServiceInfo)

	for key, value := range labels {
		if strings.HasPrefix(key, "worklet.service.") {
			parts := strings.Split(key, ".")
			if len(parts) >= 4 {
				serviceName := parts[2]
				property := parts[3]

				if _, exists := serviceMap[serviceName]; !exists {
					serviceMap[serviceName] = &ServiceInfo{Name: serviceName}
				}

				switch property {
				case "port":
					var port int
					fmt.Sscanf(value, "%d", &port)
					serviceMap[serviceName].Port = port
				case "subdomain":
					serviceMap[serviceName].Subdomain = value
				}
			}
		}
	}

	var services []ServiceInfo
	for _, svc := range serviceMap {
		services = append(services, *svc)
	}

	return services
}

// GetSessionDNSName generates the DNS name for a session service
func GetSessionDNSName(session SessionInfo, service ServiceInfo) string {
	subdomain := service.Subdomain
	if subdomain == "" {
		subdomain = service.Name
	}
	return fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", subdomain, session.ProjectName, session.SessionID)
}

func TailLogs(ctx context.Context, containerID string, output chan<- string) error {
	session, err := GetSessionInfo(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get session info: %w", err)
	}

	// Show last 10 lines and follow
	cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", "10", "-tf", session.ContainerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start log command: %w", err)
	}

	go func() {
		defer close(output)

		// Read from both stdout and stderr
		stdoutScanner := bufio.NewScanner(stdout)
		stderrScanner := bufio.NewScanner(stderr)

		// Use a goroutine for stderr
		go func() {
			for stderrScanner.Scan() {
				text := stderrScanner.Text()
				output <- text
			}
		}()

		// Read stdout in main goroutine
		for stdoutScanner.Scan() {
			text := stdoutScanner.Text()
			output <- text
		}
	}()

	return cmd.Wait()
}

// ExecShell creates an interactive shell session in a container
func ExecShell(ctx context.Context, sessionID string) (*exec.Cmd, error) {
	session, err := GetSessionInfo(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session info: %w", err)
	}

	// Check if container is running
	if session.Status != "running" {
		// Start the container if it's not running
		startCmd := exec.CommandContext(ctx, "docker", "start", session.ContainerID)
		if err := startCmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to start container: %w", err)
		}
	}

	// Get TERM from host environment, or use a sensible default
	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}

	// Create an interactive shell command without -t flag (PTY will handle this)
	// Using -i flag for interactive input and -e to set TERM environment variable
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", "-e", "TERM="+term, session.ContainerID, "/bin/sh")
	
	return cmd, nil
}
