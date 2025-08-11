package worklet

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mergestat/timediff"
	"github.com/nolanleung/worklet/internal/docker"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table table.Model
}

// Init implements tea.Model.
func (m model) Init() tea.Cmd {
	return nil
}

// Init implements tea.Model.
func (m *model) refresh() {
	columns := []table.Column{
		{Title: "Project", Width: 16},
		{Title: "Session ID", Width: 20},
		{Title: "URL", Width: 48},
		{Title: "Created", Width: 12},
	}

	sessions, err := docker.ListSessions(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
		os.Exit(1)
	}

	rows := []table.Row{}
	for _, session := range sessions {
		name := session.ProjectName
		if name == "" {
			name = "(no name)"
		}
		
		// Build URL if services exist
		url := "(no services)"
		if len(session.Services) > 0 {
			subdomain := session.Services[0].Subdomain
			if subdomain == "" {
				subdomain = session.Services[0].Name
			}
			url = fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", subdomain, session.ProjectName, session.SessionID)
		}
		
		rows = append(rows, table.Row{
			name,
			session.SessionID,
			url,
			timediff.TimeDiff(session.CreatedAt),
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Background(lipgloss.Color("#ffaa00ff")).
		Foreground(lipgloss.Color("0")).
		Bold(false)
	t.SetStyles(s)

	m.table = t
}

// Update implements tea.Model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.table.Focused() {
				m.table.Blur()
			} else {
				m.table.Focus()
			}

		case "q", "ctrl+c":
			return m, tea.Quit

		case "o", "O":
			// open browser with the service URL
			if !m.table.Focused() {
				return m, nil
			}
			selected := m.table.SelectedRow()
			if len(selected) == 0 {
				return m, nil
			}
			
			// Get the URL from the table (3rd column)
			url := selected[2]
			if url == "" || url == "(no services)" {
				// No services or URL available
				return m, nil
			}

			// Open the URL in the default browser
			openBrowserURL(url)
			return m, nil

		case "l", "L":
			// tail logs of the selected session
			if !m.table.Focused() {
				return m, nil
			}
			selected := m.table.SelectedRow()
			if len(selected) == 0 {
				return m, nil
			}
			sessionID := selected[1]
			if sessionID == "" {
				return m, nil
			}

			// Get session info to get container ID
			session, err := docker.GetSessionInfo(context.Background(), sessionID)
			if err != nil {
				// Could not get session info, just return
				return m, nil
			}

			// Create the docker logs command to tail logs
			c := exec.Command("docker", "logs", "--tail", "50", "-f", session.ContainerID)

			// Use tea.ExecProcess to temporarily leave bubbletea and run the logs command
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				// After viewing logs, we return here
				// Refresh the table in case container states changed
				m.refresh()
				return nil
			})

		case "enter":
			// attach to the selected project with direct terminal
			if !m.table.Focused() {
				return m, nil
			}
			selected := m.table.SelectedRow()
			if len(selected) == 0 {
				return m, nil
			}
			sessionID := selected[1]
			if sessionID == "" {
				return m, nil
			}

			// Get session info to get container ID
			session, err := docker.GetSessionInfo(context.Background(), sessionID)
			if err != nil {
				// Could not get session info, just return
				return m, nil
			}

			// Get TERM from host environment
			term := os.Getenv("TERM")
			if term == "" {
				term = "xterm-256color"
			}

			// Build docker exec arguments
			execArgs := []string{"exec", "-it", "-e", "TERM=" + term}
			
			// Pass SSH_AUTH_SOCK if available
			if sshAuthSock := os.Getenv("SSH_AUTH_SOCK"); sshAuthSock != "" {
				execArgs = append(execArgs, "-e", "SSH_AUTH_SOCK="+sshAuthSock)
			}
			
			execArgs = append(execArgs, session.ContainerID, "/bin/sh")

			// Create the docker exec command
			c := exec.Command("docker", execArgs...)

			// Use tea.ExecProcess to temporarily leave bubbletea and run the shell
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				// After the shell exits, we return here
				// Refresh the table in case container states changed
				m.refresh()
				return nil
			})

		}
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m model) View() string {
	helpText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("\nEnter: Attach to shell • O: Open in browser • L: View logs • Q: Quit")
	return baseStyle.Render(m.table.View()) + helpText + "\n"
}

func RunCLI() error {
	m := model{}
	m.refresh()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}

	return nil
}
