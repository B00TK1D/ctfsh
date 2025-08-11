package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ctfsh/internal/db"
)

type scoreboardTeam struct {
	db.Team
	place int
}
type scoreboardModel struct {
	teams      []scoreboardTeam
	search     string
	searchMode bool
	tbl        table.Model
}

func newScoreboardModel() *scoreboardModel {
	sm := &scoreboardModel{
		teams: []scoreboardTeam{}, // Will be populated when needed
	}
	sm.loadScoreboard()
	// Initialize a basic table; width/height and rows are set in render
	columns := []table.Column{
		{Title: "Rank", Width: 4},
		{Title: "Team", Width: 20},
		{Title: "Players", Width: 8},
		{Title: "Score", Width: 8},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(true),
	)
	// Simple styles
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("63")).
		BorderBottom(true)
	s.Selected = lipgloss.NewStyle().Background(lipgloss.Color("235"))
	t.SetStyles(s)
	sm.tbl = t
	return sm
}

func (sm *scoreboardModel) loadScoreboard() {
	dbTeams, err := db.GetScoreboard()
	if err != nil {
		// If there's an error, just use empty list
		sm.teams = []scoreboardTeam{}
		return
	}
	teams := make([]scoreboardTeam, 0, len(dbTeams))
	for i, t := range dbTeams {
		teams = append(teams, scoreboardTeam{
			Team:  t,
			place: i + 1,
		})
	}
	sm.teams = teams
}

func (sm *scoreboardModel) update(msg tea.KeyMsg) {
	if sm.searchMode {
		switch msg.Type {
		case tea.KeyRunes, tea.KeySpace:
			sm.search += msg.String()
		case tea.KeyBackspace:
			if len(sm.search) > 0 {
				sm.search = sm.search[:len(sm.search)-1]
			} else {
				sm.searchMode = false
			}
		case tea.KeyEsc, tea.KeyEnter:
			sm.searchMode = false
			sm.search = ""
			// leave table cursor as-is
		case tea.KeyUp, tea.KeyDown:
			// ignore; table will handle when not in search mode
		}
		return
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		sm.searchMode = true
		sm.search = ""
	default:
		// Delegate navigation and scrolling to table
		sm.tbl, _ = sm.tbl.Update(msg)
	}
}

func (sm *scoreboardModel) filteredScoreboard() []scoreboardTeam {
	if sm.search == "" {
		return sm.teams
	}
	var filtered []scoreboardTeam
	for _, t := range sm.teams {
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(sm.search)) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
