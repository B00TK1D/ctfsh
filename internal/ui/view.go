package ui

import (
	"fmt"
	"strings"

	"ctfsh/internal/db"
)

func (m model) renderMenuView() string {
	title := titleStyle.Render("🚩 CTFsh")

	var teamName string
	var err error
	if m.user.TeamID != nil {
		teamName, err = db.GetTeamName(*m.user.TeamID)
		if err != nil {
			teamName = "Error"
		} else {
			teamName = fmt.Sprintf("Team: %s", teamName)
		}
	} else {
		teamName = "No team"
	}

	userInfo := fmt.Sprintf("User: %s | Team: %s", m.user.Username, teamName)

	options := []string{"Challenges", "Scoreboard", "Team Management"}
	var menu strings.Builder
	for i, option := range options {
		cursor := "  "
		if i == m.menuCursor {
			cursor = selectedStyle.Render("> ")
		}
		menu.WriteString(cursor + option + "\n")
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: move  Enter/Space: select  q/Esc: quit  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return fmt.Sprintf("%s\n\n%s\n\n%s%s", title, userInfo, menu.String(), help)
}
