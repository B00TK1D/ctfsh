package ui

import (
	"sort"

	"ctfsh/internal/db"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// teamMember represents a team member with their points
type teamMember struct {
	User   db.User
	Points int
}

// teamMembersModel handles the team members view
type teamMembersModel struct {
	user    *db.User
	members []teamMember
	cursor  int
}

func newTeamMembersModel(user *db.User) *teamMembersModel {
	return &teamMembersModel{
		user:    user,
		members: []teamMember{},
	}
}

func (tmm *teamMembersModel) loadTeamMembers() {
	if tmm.user.TeamID == nil {
		tmm.members = []teamMember{}
		return
	}

	// Get team members
	members, err := db.GetTeamMembers(*tmm.user.TeamID)
	if err != nil {
		tmm.members = []teamMember{}
		return
	}

	// Calculate points for each member
	var teamMembers []teamMember
	for _, member := range members {
		points := tmm.calculateMemberPoints(member.ID)
		teamMembers = append(teamMembers, teamMember{
			User:   member,
			Points: points,
		})
	}

	// Sort by points (highest first)
	sort.Slice(teamMembers, func(i, j int) bool {
		return teamMembers[i].Points > teamMembers[j].Points
	})

	tmm.members = teamMembers
}

func (tmm *teamMembersModel) calculateMemberPoints(userID int) int {
	// Get solved challenges for this user
	solvedMap, err := db.GetChallengesSolvedByUser(userID)
	if err != nil {
		return 0
	}

	// Calculate total points
	totalPoints := 0
	challenges := db.GetChallenges()
	for _, challenge := range challenges {
		if solvedMap[challenge.ID] {
			totalPoints += challenge.Points
		}
	}

	return totalPoints
}

func (tmm *teamMembersModel) update(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if tmm.cursor > 0 {
			tmm.cursor--
		}
	case key.Matches(msg, keys.Down):
		if tmm.cursor < len(tmm.members)-1 {
			tmm.cursor++
		}
	}
	return nil, nil
}
