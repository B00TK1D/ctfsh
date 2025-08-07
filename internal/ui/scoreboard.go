package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"ctfsh/internal/db"
)

// scoreboardModel handles the scoreboard view
type scoreboardModel struct {
	teams      []db.Team
	cursor     int
	search     string
	searchMode bool
}

func newScoreboardModel() *scoreboardModel {
	sm := &scoreboardModel{
		teams: []db.Team{}, // Will be populated when needed
	}
	sm.loadScoreboard()
	return sm
}

func (sm *scoreboardModel) loadScoreboard() {
	teams, err := db.GetScoreboard()
	if err != nil {
		// If there's an error, just use empty list
		sm.teams = []db.Team{}
		return
	}
	sm.teams = teams
}

func (sm *scoreboardModel) update(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if sm.searchMode {
		switch msg.Type {
		case tea.KeyRunes:
			sm.search += msg.String()
			return nil, nil
		case tea.KeyBackspace:
			if len(sm.search) > 0 {
				sm.search = sm.search[:len(sm.search)-1]
			}
			return nil, nil
		case tea.KeyEsc, tea.KeyEnter:
			sm.searchMode = false
			sm.search = ""
			sm.cursor = 0
			return nil, nil
		case tea.KeyUp:
			if sm.cursor > 0 {
				sm.cursor--
			}
			return nil, nil
		case tea.KeyDown:
			filtered := sm.filteredScoreboard()
			if sm.cursor < len(filtered)-1 {
				sm.cursor++
			}
			return nil, nil
		}
		return nil, nil
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		sm.searchMode = true
		sm.search = ""
		sm.cursor = 0
		return nil, nil
	case key.Matches(msg, keys.Up):
		if sm.cursor > 0 {
			sm.cursor--
		}
		return nil, nil
	case key.Matches(msg, keys.Down):
		filtered := sm.filteredScoreboard()
		if sm.cursor < len(filtered)-1 {
			sm.cursor++
		}
		return nil, nil
	}
	return nil, nil
}

func (sm *scoreboardModel) filteredScoreboard() []db.Team {
	if sm.search == "" {
		return sm.teams
	}
	var filtered []db.Team
	for _, t := range sm.teams {
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(sm.search)) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (sm *scoreboardModel) scoreboardRows() int {
	// Always show up to min(20, height-13) teams
	maxRows := 20 // This should be passed from the main model
	if maxRows > 20 {
		return 20
	}
	if maxRows < 1 {
		return 1
	}
	return maxRows
}
