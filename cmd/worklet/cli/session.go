package cli

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nolanleung/worklet/internal/docker"
)

type SessionModel struct {
	SessionID string
	logs      []string
	stream    chan string
	listening bool
}

type logsMsg struct {
	lines  []string
	status string
	err    error
}

// Init implements tea.Model.
func (s *SessionModel) Init() tea.Cmd {
	// Initialize the stream channel if not already done
	if s.stream == nil {
		s.stream = make(chan string, 100)
	}
	s.listening = true
	return listenForLogs(s.SessionID, s.stream)
}

// Update implements tea.Model.
func (s *SessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case logsMsg:
		if msg.err != nil {
			// Handle error - could add error display
			s.listening = false
			return s, nil
		}

		if msg.lines != nil {
			// Append new log lines
			s.logs = append(s.logs, msg.lines...)

			// Keep only last 1000 lines for performance
			if len(s.logs) > 1000 {
				s.logs = s.logs[len(s.logs)-1000:]
			}
		}

		if msg.status == "Stream ended" {
			s.listening = false
			return s, nil
		}

		// Continue listening for more logs
		if s.listening {
			return s, waitForLogs(s.stream)
		}
	}

	return s, nil
}

// View implements tea.Model.
func (s *SessionModel) View() string {
	if len(s.logs) == 0 {
		return "Waiting for logs from session " + s.SessionID + "..."
	}

	var sb strings.Builder
	// Show last 50 lines in the view for better performance
	start := 0
	if len(s.logs) > 50 {
		start = len(s.logs) - 50
	}

	for i := start; i < len(s.logs); i++ {
		sb.WriteString(s.logs[i] + "\n")
	}
	return sb.String()
}

func listenForLogs(sessionID string, stream chan string) tea.Cmd {
	return func() tea.Msg {
		// Start tailing logs in a goroutine
		go func() {
			ctx := context.Background()
			err := docker.TailLogs(ctx, sessionID, stream)
			if err != nil {
				// Send error through channel as a special marker
				stream <- "ERROR: " + err.Error()
			}
		}()

		// Wait for first log line
		return waitForLogs(stream)()
	}
}

// waitForLogs waits for the next log line from the stream
func waitForLogs(stream chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-stream
		if !ok {
			return logsMsg{status: "Stream ended"}
		}

		if strings.HasPrefix(line, "ERROR: ") {
			return logsMsg{err: fmt.Errorf(strings.TrimPrefix(line, "ERROR: "))}
		}

		return logsMsg{lines: []string{line}}
	}
}

var _ tea.Model = (*SessionModel)(nil)
