package worklet

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mergestat/timediff"
	"github.com/nolanleung/worklet/cmd/worklet/cli"
	"github.com/nolanleung/worklet/internal/docker"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table   table.Model
	session *cli.SessionModel
	view    string
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
		rows = append(rows, table.Row{
			name,
			session.SessionID,
			"http://" + session.Services[0].Subdomain + "." + session.ProjectName + "-" + session.SessionID + ".local.worklet.sh",
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

	// Handle session view updates first
	if m.view == "session" && m.session != nil {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q":
				m.view = ""
				m.session = nil
				m.refresh()
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
		}

		// Update the session model
		var sessionModel tea.Model
		sessionModel, cmd = m.session.Update(msg)
		m.session = sessionModel.(*cli.SessionModel)
		return m, cmd
	}

	// Handle main view updates
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

		case "enter":
			// attach to the selected project
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

			m.session = &cli.SessionModel{
				SessionID: sessionID,
			}
			m.view = "session"

			// Initialize the session model
			return m, m.session.Init()
		}
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m model) View() string {
	switch m.view {
	case "session":
		if m.session != nil {
			return m.session.View()
		}
		return "Loading session..."
	default:
		return baseStyle.Render(m.table.View()) + "\n"
	}
}

func RunCLI() error {
	m := model{}
	m.refresh()
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}

	return nil
}
