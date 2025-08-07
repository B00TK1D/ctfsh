package ui

import (
	"sort"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"ctfsh/internal/db"
)

// challengeWrapper wraps db.Challenge with UI-specific fields
type challengeWrapper struct {
	db.Challenge
	solved bool
	solver string
}

// challengeModel handles the challenge list and detail views
type challengeModel struct {
	user         *db.User
	challenges   map[string]challengeWrapper
	categories   []string
	cursor       int
	selectedChal challengeWrapper
	expandedCats map[string]bool
	flagInput    textinput.Model
	teamSolvers  map[int]string // challenge_id -> username
}

// Custom messages for challenge view
type switchToDetailView struct{}
type submitFlagRequest struct{}

func newChallengeModel(user *db.User) *challengeModel {
	flagInput := textinput.New()
	flagInput.CharLimit = 100

	cm := &challengeModel{
		user:         user,
		flagInput:    flagInput,
		expandedCats: make(map[string]bool),
	}

	// Load challenges and categories
	rawChallenges := db.GetChallenges()
	cm.challenges = make(map[string]challengeWrapper)
	for name, chal := range rawChallenges {
		cm.challenges[name] = challengeWrapper{Challenge: chal}
	}
	cm.categories = db.GetChallengeCategories()

	// Initialize expanded state for categories
	for _, category := range cm.categories {
		cm.expandedCats[category] = false
	}

	// Load solved status
	cm.loadSolvedStatus()

	// Load team solvers if user is on a team
	if user.TeamID != nil {
		solvers, _ := db.GetTeamChallengeSolvers(*user.TeamID)
		cm.teamSolvers = solvers
	}

	return cm
}

func (cm *challengeModel) loadSolvedStatus() {
	solvedMap, _ := db.GetChallengesSolvedByUser(cm.user.ID)
	for name, chal := range cm.challenges {
		if solvedMap[chal.ID] {
			solvedChal := chal
			solvedChal.solved = true
			cm.challenges[name] = solvedChal
		}
	}
}

func (cm *challengeModel) buildChallengeRenderList() []any {
	var items []any
	categoryMap := make(map[string][]challengeWrapper)
	solvedByCategory := make(map[string]int)

	for _, ch := range cm.challenges {
		categoryMap[ch.Category] = append(categoryMap[ch.Category], ch)
		if ch.solved {
			solvedByCategory[ch.Category]++
		}
	}

	// Sort the challenges by point value within each category (break ties by name)
	for cat, challenges := range categoryMap {
		sort.Slice(challenges, func(i, j int) bool {
			if challenges[i].Points == challenges[j].Points {
				return challenges[i].Name < challenges[j].Name
			}
			return challenges[i].Points > challenges[j].Points
		})
		categoryMap[cat] = challenges
	}

	for _, category := range cm.categories {
		items = append(items, categoryListItem{
			name:       category,
			total:      len(categoryMap[category]),
			solved:     solvedByCategory[category],
			isExpanded: cm.expandedCats[category],
		})

		if cm.expandedCats[category] {
			for _, ch := range categoryMap[category] {
				items = append(items, ch)
			}
		}
	}
	return items
}

func (cm *challengeModel) update(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	renderList := cm.buildChallengeRenderList()

	switch {
	case key.Matches(msg, keys.Up):
		if cm.cursor > 0 {
			cm.cursor--
		}
	case key.Matches(msg, keys.Down):
		if cm.cursor < len(renderList)-1 {
			cm.cursor++
		}
	case key.Matches(msg, keys.Select):
		if len(renderList) == 0 {
			break
		}
		selectedItem := renderList[cm.cursor]
		if cat, ok := selectedItem.(categoryListItem); ok {
			cm.expandedCats[cat.name] = !cm.expandedCats[cat.name]
		} else if chal, ok := selectedItem.(challengeWrapper); ok {
			cm.selectedChal = chal
			return nil, func() tea.Msg { return switchToDetailView{} }
		}
	}

	// Clamp cursor after expanding/collapsing
	renderList = cm.buildChallengeRenderList()
	if cm.cursor >= len(renderList) {
		cm.cursor = len(renderList) - 1
	}
	if cm.cursor < 0 {
		cm.cursor = 0
	}

	return nil, nil
}

func (cm *challengeModel) submitFlag(flag string) (string, string) {
	if flag == "" {
		return "", ""
	}
	correct, err := db.SubmitFlag(cm.user.ID, cm.selectedChal.ID, flag)
	if err != nil {
		return err.Error(), "error"
	}
	if correct {
		// Update solved status
		solvedChal := cm.selectedChal
		solvedChal.solved = true
		cm.selectedChal = solvedChal

		// Also update the challenge in the main list
		for name, chal := range cm.challenges {
			if chal.ID == cm.selectedChal.ID {
				solvedChal := chal
				solvedChal.solved = true
				cm.challenges[name] = solvedChal
				break
			}
		}

		// Refresh all challenge and solver state
		cm.loadSolvedStatus()
		if cm.user.TeamID != nil {
			solvers, _ := db.GetTeamChallengeSolvers(*cm.user.TeamID)
			cm.teamSolvers = solvers
		}

		return "Correct! Flag accepted.", "success"
	}
	return "Incorrect flag. Try again.", "error"
}
