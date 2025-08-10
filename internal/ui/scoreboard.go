package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"ctfsh/internal/db"
)

type scoreboardTeam struct {
	db.Team
	place int
}
type scoreboardModel struct {
	teams      []scoreboardTeam
	cursor     int
	search     string
	searchMode bool
}

func newScoreboardModel() *scoreboardModel {
	sm := &scoreboardModel{
		teams: []scoreboardTeam{}, // Will be populated when needed
	}
	sm.loadScoreboard()
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
				sm.cursor = 0
			}
		case tea.KeyEsc, tea.KeyEnter:
			sm.searchMode = false
			sm.search = ""
			sm.cursor = 0
		case tea.KeyUp:
			if sm.cursor > 0 {
				sm.cursor--
			}
		case tea.KeyDown:
			filtered := sm.filteredScoreboard()
			if sm.cursor < len(filtered)-1 {
				sm.cursor++
			}
		}
		return
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		sm.searchMode = true
		sm.search = ""
		sm.cursor = 0
	case key.Matches(msg, keys.Up):
		if sm.cursor > 0 {
			sm.cursor--
		}
	case key.Matches(msg, keys.Down):
		filtered := sm.filteredScoreboard()
		if sm.cursor < len(filtered)-1 {
			sm.cursor++
		}
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
