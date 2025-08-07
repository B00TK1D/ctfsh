package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"ctfsh/internal/db"
)

type teamModel struct {
	user         *db.User
	cursor       int
	teamInput    textinput.Model
	teamJoinCode string
}

// Custom messages for team view
type teamErrorMsg struct{ message string }
type teamSuccessMsg struct{ message string }
type confirmDeleteTeamMsg struct{}
type createTeamRequestMsg struct{}
type viewTeamMembersMsg struct{}

func newTeamModel(user *db.User) *teamModel {
	teamInput := textinput.New()
	teamInput.CharLimit = 50

	tm := &teamModel{
		user:      user,
		teamInput: teamInput,
	}

	// Load team join code if user is on a team
	if user.TeamID != nil {
		_, code, err := db.GetTeamNameAndCode(*user.TeamID)
		if err == nil {
			tm.teamJoinCode = code
		}
	}

	return tm
}

func (tm *teamModel) update(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if tm.cursor > 0 {
			tm.cursor--
		}
	case key.Matches(msg, keys.Down):
		if tm.user.TeamID != nil && tm.cursor < 2 {
			tm.cursor++
		} else if tm.user.TeamID == nil && tm.cursor < 0 {
			tm.cursor++
		}
	case key.Matches(msg, keys.Select):
		if tm.user.TeamID != nil {
			return tm.handleTeamMemberAction()
		} else {
			return tm.handleCreateTeam()
		}
	}
	return nil, nil
}

func (tm *teamModel) handleTeamMemberAction() (tea.Model, tea.Cmd) {
	switch tm.cursor {
	case 0: // Leave team
		count, err := db.CountTeamMembers(*tm.user.TeamID)
		if err != nil {
			return nil, func() tea.Msg { return teamErrorMsg{err.Error()} }
		}
		if count == 1 {
			return nil, func() tea.Msg { return confirmDeleteTeamMsg{} }
		} else {
			err := db.LeaveTeam(tm.user.ID)
			if err != nil {
				return nil, func() tea.Msg { return teamErrorMsg{err.Error()} }
			}
			tm.user.TeamID = nil
			return nil, func() tea.Msg { return teamSuccessMsg{"You have left the team."} }
		}
	case 1: // Regenerate join code
		if tm.user.TeamID != nil {
			newCode, err := db.RegenerateTeamJoinCode(*tm.user.TeamID)
			if err != nil {
				return nil, func() tea.Msg { return teamErrorMsg{err.Error()} }
			}
			tm.teamJoinCode = newCode
			return nil, func() tea.Msg { return teamSuccessMsg{"Join code regenerated!"} }
		}
	case 2: // View team members
		return nil, func() tea.Msg { return viewTeamMembersMsg{} }
	}
	return nil, nil
}

func (tm *teamModel) handleCreateTeam() (tea.Model, tea.Cmd) {
	return nil, func() tea.Msg { return createTeamRequestMsg{} }
}

func (tm *teamModel) createTeam(name string) (string, string) {
	if name == "" {
		return "", ""
	}
	team, err := db.CreateAndJoinTeam(tm.user.ID, name)
	if err != nil {
		return "Team creation failed: " + err.Error(), "error"
	}
	tm.user.TeamID = &team.ID
	return "Team '" + name + "' created and joined!", "success"
}
