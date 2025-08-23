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

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type model struct {
	table  table.Model
	width  int
	height int
}

// Init implements tea.Model.
func (m model) Init() tea.Cmd {
	// Request initial window size
	return tea.EnterAltScreen
}

// Init implements tea.Model.
func (m *model) refresh() {
	// Calculate dynamic column widths based on terminal width
	// Default to 120 if width not yet set
	termWidth := m.width
	if termWidth == 0 {
		termWidth = 120
	}

	// Reserve space for borders and padding (approximately 10 chars)
	availableWidth := termWidth - 10
	if availableWidth < 80 {
		availableWidth = 80 // Minimum usable width
	}

	// Calculate proportional widths
	// Approximate ratios: Project(15%), SessionID(20%), URL(50%), Created(15%)
	projectWidth := max(12, availableWidth*15/100)
	sessionWidth := max(16, availableWidth*20/100)
	urlWidth := max(30, availableWidth*50/100)
	createdWidth := max(10, availableWidth*15/100)

	// Adjust to fit exactly
	totalWidth := projectWidth + sessionWidth + urlWidth + createdWidth
	if totalWidth < availableWidth {
		// Add extra space to URL column
		urlWidth += availableWidth - totalWidth
	}

	columns := []table.Column{
		{Title: "Project", Width: projectWidth},
		{Title: "Session ID", Width: sessionWidth},
		{Title: "URL", Width: urlWidth},
		{Title: "Created", Width: createdWidth},
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

	// Calculate table height based on terminal height
	tableHeight := 10
	if m.height > 15 {
		// Use most of the terminal height, leaving room for borders and help text
		tableHeight = m.height - 5
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(tableHeight),
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
	case tea.WindowSizeMsg:
		// Update terminal dimensions
		m.width = msg.Width
		m.height = msg.Height
		// Refresh the table with new dimensions
		m.refresh()
		return m, nil

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

			// Create the docker exec command
			c := exec.Command("docker", "exec", "-it", "-e", "TERM="+term, session.ContainerID, "/bin/sh")

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
	// Make the border width responsive to terminal width
	tableView := m.table.View()
	
	// Apply border styling with dynamic width
	if m.width > 0 {
		// Adjust border to terminal width
		styledTable := baseStyle.
			Width(m.width - 2). // Account for terminal padding
			Render(tableView)
		
		helpText := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Width(m.width - 2).
			Render("\nEnter: Attach to shell • O: Open in browser • L: View logs • Q: Quit")
		
		return styledTable + helpText + "\n"
	}
	
	// Fallback for when dimensions aren't set yet
	helpText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("\nEnter: Attach to shell • O: Open in browser • L: View logs • Q: Quit")
	return baseStyle.Render(tableView) + helpText + "\n"
}

func RunCLI() error {
	m := model{}
	m.refresh()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}

	return nil
}
