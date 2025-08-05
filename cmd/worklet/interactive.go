package worklet

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nolanleung/worklet/internal/docker"
	"github.com/nolanleung/worklet/internal/projects"
)

// Styles for the interactive UI
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170")).
			Background(lipgloss.Color("238"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	runningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	emptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)
)

// InteractiveModel represents the state of the interactive project selector
type InteractiveModel struct {
	projects       []projects.Project
	sessions       []docker.SessionInfo               // All sessions
	projectSessions map[string][]docker.SessionInfo   // Map project path to sessions
	cursor         int
	selected       int
	quitting       bool
	manager        *projects.Manager
	width          int
	height         int
	showConfirm    bool
	confirmMsg     string
	confirmPath    string
	action         string  // Track what action to perform after quit
	showSessions   bool    // Show session selector for current project
	sessionCursor  int     // Cursor position in session list
}

// InteractiveMsg types
type projectsLoadedMsg []projects.Project
type sessionsLoadedMsg struct {
	sessions       []docker.SessionInfo
	projectSessions map[string][]docker.SessionInfo
}
type actionCompleteMsg string
type errorMsg error
type tickMsg time.Time

// Init initializes the model
func (m InteractiveModel) Init() tea.Cmd {
	return tea.Batch(
		loadProjects(m.manager),
		loadSessions(),
		tickCmd(), // Start auto-refresh ticker
	)
}

// Update handles messages
func (m InteractiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.showConfirm {
			switch msg.String() {
			case "y", "Y":
				// Check if this is a stop sessions confirmation
				if strings.Contains(m.confirmMsg, "Stop all") {
					// Stop all sessions for the project
					sessions := m.projectSessions[m.confirmPath]
					m.showConfirm = false
					return m, tea.Batch(
						stopAllSessionsDocker(sessions),
						loadSessions(), // Reload sessions after stopping
					)
				} else {
					// Perform the deletion
					if err := m.manager.Remove(m.confirmPath); err == nil {
						m.showConfirm = false
						return m, loadProjects(m.manager)
					}
					m.showConfirm = false
				}
			case "n", "N", "esc":
				m.showConfirm = false
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.showSessions {
				// Navigate sessions
				if m.sessionCursor > 0 {
					m.sessionCursor--
				}
			} else {
				// Navigate projects
				if m.cursor > 0 {
					m.cursor--
					m.sessionCursor = 0 // Reset session cursor
				}
			}

		case "down", "j":
			if m.showSessions {
				// Navigate sessions
				if m.cursor < len(m.projects) {
					project := m.projects[m.cursor]
					absPath, _ := filepath.Abs(project.Path)
					sessions := m.projectSessions[absPath]
					if m.sessionCursor < len(sessions)-1 {
						m.sessionCursor++
					}
				}
			} else {
				// Navigate projects
				if m.cursor < len(m.projects)-1 {
					m.cursor++
					m.sessionCursor = 0 // Reset session cursor
				}
			}

		case "enter":
			if len(m.projects) > 0 {
				project := m.projects[m.cursor]
				absPath, _ := filepath.Abs(project.Path)
				sessions := m.projectSessions[absPath]
				
				// If sessions are expanded and we have sessions, attach to selected one
				if m.showSessions && len(sessions) > 0 {
					selectedSession := sessions[m.sessionCursor]
					m.action = "attach:" + selectedSession.SessionID
					m.quitting = true
					return m, tea.Quit
				} else {
					// Otherwise start a new session in background mode
					return m, startProjectInBackground(m.projects[m.cursor].Path)
				}
			}

		case "a":
			// Attach to running container
			if len(m.projects) > 0 {
				project := m.projects[m.cursor]
				absPath, _ := filepath.Abs(project.Path)
				sessions := m.projectSessions[absPath]
				
				if len(sessions) == 0 {
					// No sessions to attach to
					return m, nil
				} else if len(sessions) == 1 {
					// Single session - attach directly
					m.action = "attach:" + sessions[0].SessionID
					m.quitting = true
					return m, tea.Quit
				} else if m.showSessions {
					// Multiple sessions and session list is shown - attach to selected
					selectedSession := sessions[m.sessionCursor]
					m.action = "attach:" + selectedSession.SessionID
					m.quitting = true
					return m, tea.Quit
				} else {
					// Multiple sessions but list not shown - show the list
					m.showSessions = true
					m.sessionCursor = 0
				}
			}
		
		case "right", "l", "tab":
			// Expand sessions for current project
			if len(m.projects) > 0 {
				project := m.projects[m.cursor]
				absPath, _ := filepath.Abs(project.Path)
				sessions := m.projectSessions[absPath]
				if len(sessions) > 0 && !m.showSessions {
					m.showSessions = true
					m.sessionCursor = 0
				}
			}
		
		case "left", "h":
			// Collapse sessions view
			if m.showSessions {
				m.showSessions = false
				m.sessionCursor = 0
			}
		
		case "esc":
			// Close session list if open, otherwise quit
			if m.showSessions {
				m.showSessions = false
				m.sessionCursor = 0
			} else {
				m.quitting = true
				return m, tea.Quit
			}
		
		case "S":
			// Stop all sessions for current project
			if len(m.projects) > 0 {
				project := m.projects[m.cursor]
				absPath, _ := filepath.Abs(project.Path)
				sessions := m.projectSessions[absPath]
				if len(sessions) > 0 {
					m.confirmMsg = fmt.Sprintf("Stop all %d sessions for %s? (y/n)", len(sessions), project.Name)
					m.showConfirm = true
					m.confirmPath = absPath // Store path for later use
				}
			}

		case "d":
			// Delete project from history
			if len(m.projects) > 0 {
				m.confirmPath = m.projects[m.cursor].Path
				projectName := m.projects[m.cursor].Name
				if projectName == "" {
					projectName = filepath.Base(m.confirmPath)
				}
				m.confirmMsg = fmt.Sprintf("Remove %s from history? (y/n)", projectName)
				m.showConfirm = true
			}

		case "c":
			// Clean stale projects
			if err := m.manager.CleanStale(); err == nil {
				return m, loadProjects(m.manager)
			}

		case "r":
			// Refresh
			return m, tea.Batch(
				loadProjects(m.manager),
				loadSessions(),
			)
		}

	case projectsLoadedMsg:
		m.projects = msg
		if m.cursor >= len(m.projects) {
			m.cursor = len(m.projects) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}

	case sessionsLoadedMsg:
		m.sessions = msg.sessions
		m.projectSessions = msg.projectSessions

	case actionCompleteMsg:
		// Handle various actions
		action := string(msg)
		if strings.HasPrefix(action, "start_background:") {
			path := strings.TrimPrefix(action, "start_background:")
			m.action = "background:" + path
			m.quitting = true
			return m, tea.Quit
		} else if action == "sessions_stopped" {
			// Sessions were stopped, refresh the sessions list
			return m, loadSessions()
		}

	case errorMsg:
		// Handle errors silently for now
	
	case tickMsg:
		// Auto-refresh sessions every 2 seconds
		return m, tea.Batch(
			loadSessions(),  // Refresh sessions list
			tickCmd(),      // Continue ticking
		)
	}

	return m, nil
}

// View renders the UI
func (m InteractiveModel) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder

	// Title
	s.WriteString(titleStyle.Render("🚀 Worklet Projects"))
	s.WriteString("\n\n")

	if m.showConfirm {
		s.WriteString(m.confirmMsg)
		s.WriteString("\n")
		return s.String()
	}

	if len(m.projects) == 0 {
		s.WriteString(emptyStyle.Render("No projects found. Run 'worklet run' in a project directory to add it."))
		s.WriteString("\n\n")
		s.WriteString(helpStyle.Render("Press 'q' to quit"))
		return s.String()
	}

	// Project list
	for i, project := range m.projects {
		cursor := "  "
		if m.cursor == i {
			cursor = "> "
		}

		// Format project name
		name := project.Name
		if name == "" {
			name = filepath.Base(project.Path)
		}

		// Format time
		timeAgo := formatTimeAgo(project.LastAccessed)

		// Check for running sessions
		absPath, _ := filepath.Abs(project.Path)
		sessions := m.projectSessions[absPath]
		sessionCount := len(sessions)

		// Build the line with expansion indicator
		var expandIndicator string
		if sessionCount > 0 {
			if m.cursor == i && m.showSessions {
				expandIndicator = "▼ " // Expanded
			} else if sessionCount > 0 {
				expandIndicator = "▶ " // Collapsed
			}
		} else {
			expandIndicator = "  " // No sessions
		}
		
		line := fmt.Sprintf("%s%s%-28s", cursor, expandIndicator, name)
		
		if sessionCount > 0 {
			if sessionCount == 1 {
				line += runningStyle.Render(" ● 1 session")
			} else {
				line += runningStyle.Render(fmt.Sprintf(" ● %d sessions", sessionCount))
			}
		}
		
		info := fmt.Sprintf(" %s, %d runs", timeAgo, project.RunCount)
		line += infoStyle.Render(info)

		// Apply style
		if m.cursor == i {
			s.WriteString(selectedStyle.Render(line))
		} else {
			s.WriteString(normalStyle.Render(line))
		}
		s.WriteString("\n")

		// Show path and sessions for selected item
		if m.cursor == i {
			s.WriteString(infoStyle.Render(fmt.Sprintf("    %s", project.Path)))
			s.WriteString("\n")
			
			// Show sessions if any
			if sessionCount > 0 && m.showSessions {
				for j, session := range sessions {
					sessionLine := "      "  // Extra indentation for hierarchy
					if j == m.sessionCursor {
						sessionLine += "▸ "  // Small right arrow for selected session
					} else {
						sessionLine += "• "  // Bullet for unselected session
					}
					
					// Format session info
					sessionAge := formatTimeAgo(session.CreatedAt)
					sessionLine += fmt.Sprintf("Session %s • started %s", session.SessionID, sessionAge)
					
					if j == m.sessionCursor {
						s.WriteString(selectedStyle.Render(sessionLine))
					} else {
						s.WriteString(infoStyle.Render(sessionLine))
					}
					s.WriteString("\n")
				}
			}
		}
	}

	// Help text
	s.WriteString("\n")
	if m.showSessions {
		helpText := "↑/↓ Navigate • Enter: Attach • ← Collapse • q: Quit"
		s.WriteString(helpStyle.Render(helpText))
	} else {
		helpText := "↑/↓ Navigate • Enter: Start new • a: Attach • d: Remove • r: Refresh • q: Quit"
		s.WriteString(helpStyle.Render(helpText))
		
		// Additional tips based on current project state
		if m.cursor < len(m.projects) {
			project := m.projects[m.cursor]
			absPath, _ := filepath.Abs(project.Path)
			sessions := m.projectSessions[absPath]
			
			if len(sessions) == 1 {
				s.WriteString("\n")
				s.WriteString(helpStyle.Render("Press 'a' to attach to the running session"))
			} else if len(sessions) > 1 {
				s.WriteString("\n")
				s.WriteString(helpStyle.Render(fmt.Sprintf("Press → to expand %d sessions • Press 'a' to attach", len(sessions))))
			}
		}
	}

	return s.String()
}

// Helper functions
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func loadProjects(manager *projects.Manager) tea.Cmd {
	return func() tea.Msg {
		projects := manager.List()
		return projectsLoadedMsg(projects)
	}
}

func loadSessions() tea.Cmd {
	return func() tea.Msg {
		// Create a context with timeout for the Docker API call
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		sessions, err := docker.ListSessions(ctx)
		if err != nil {
			return sessionsLoadedMsg{}
		}

		// Map sessions by project path
		sessionsByPath := make(map[string][]docker.SessionInfo)
		
		for _, session := range sessions {
			// Normalize the path
			if session.WorkDir != "" {
				absPath, err := filepath.Abs(session.WorkDir)
				if err == nil {
					sessionsByPath[absPath] = append(sessionsByPath[absPath], session)
				}
			}
		}

		return sessionsLoadedMsg{
			sessions:        sessions,
			projectSessions: sessionsByPath,
		}
	}
}

func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		mins := int(duration.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if duration < 7*24*time.Hour {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else if duration < 30*24*time.Hour {
		weeks := int(duration.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	} else {
		months := int(duration.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	}
}

// Command to start project in background
func startProjectInBackground(path string) tea.Cmd {
	return func() tea.Msg {
		// We'll handle the actual start after quitting the interactive mode
		// For now, just mark this project as selected for background start
		return actionCompleteMsg("start_background:" + path)
	}
}

// Command to stop all sessions for a project
func stopAllSessionsDocker(sessions []docker.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		for _, session := range sessions {
			// Stop the session using Docker API
			docker.StopSession(ctx, session.SessionID)
		}
		return actionCompleteMsg("sessions_stopped")
	}
}

// ShowProjectSelector shows the interactive project selector
// Returns: path, action ("attach", "background", or ""), error
func ShowProjectSelector() (string, string, error) {
	manager, err := projects.NewManager()
	if err != nil {
		return "", "", err
	}

	// Clean stale projects on startup
	manager.CleanStale()

	model := InteractiveModel{
		manager:  manager,
		projects: manager.List(),
		selected: -1,
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return "", "", err
	}

	// Safe type assertion to prevent panic
	m, ok := finalModel.(InteractiveModel)
	if !ok {
		return "", "", fmt.Errorf("unexpected model type returned")
	}
	
	// Check if an action was set
	if m.action != "" {
		if strings.HasPrefix(m.action, "background:") {
			path := strings.TrimPrefix(m.action, "background:")
			return path, "background", nil
		} else if strings.HasPrefix(m.action, "attach:") {
			sessionID := strings.TrimPrefix(m.action, "attach:")
			// For attach action, we return the session ID as path and "attach" as action
			return sessionID, "attach", nil
		}
	}
	
	if m.selected >= 0 && m.selected < len(m.projects) {
		project := m.projects[m.selected]
		return project.Path, "", nil // Normal interactive run
	}

	return "", "", fmt.Errorf("no project selected")
}