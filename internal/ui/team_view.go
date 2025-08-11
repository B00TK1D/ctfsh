package ui

import (
	"fmt"
	"strings"

	"ctfsh/internal/config"
	"ctfsh/internal/db"
)

func (m model) renderTeamView() string {
	title := titleStyle.Render("Team Management")

	var content string
	// If user is on a team, show leave/regenerate options and join command
	if m.user.TeamID != nil {
		teamName, err := db.GetTeamName(*m.user.TeamID)
		if err != nil {
			teamName = "Error fetching name"
		}
		joinCode := m.team.teamJoinCode
		sshCmd := ""
		if joinCode != "" {
			if config.Port == 22 {
				sshCmd = fmt.Sprintf("ssh %s@%s", joinCode, config.Host)
			} else {
				sshCmd = fmt.Sprintf("ssh %s@%s -p %d", joinCode, config.Host, config.Port)
			}
		}
		options := []string{"Leave Team", "Regenerate Join Code", "View Team Members"}
		var menu strings.Builder
		for i, option := range options {
			cursor := "  "
			if i == m.team.cursor {
				cursor = selectedStyle.Render("> ")
			}
			menu.WriteString(cursor + option + "\n")
		}
		content = fmt.Sprintf("Current team: %s\n\n%s\nJoin team:\n%s\n",
			teamName,
			menu.String(),
			sshCmd,
		)
	} else {
		// User not on a team, show only create option
		options := []string{"Create a new Team"}
		var menu strings.Builder
		menu.WriteString("You have not joined a team.\n\n")
		for i, option := range options {
			cursor := "  "
			if i == m.team.cursor {
				cursor = selectedStyle.Render("> ")
			}
			menu.WriteString(cursor + option + "\n")
		}
		content = menu.String()
	}

	message := ""
	if m.message != "" {
		style := successStyle
		if m.messageType == "error" {
			style = errorStyle
		}
		message = style.Render(m.message)
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: move  Enter/Space: select  q/Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return fmt.Sprintf("%s\n\n%s%s%s", title, content, message, help)
}

func (m model) renderConfirmDeleteTeamView() string {
	return "You are the last member of your team.\nLeaving will delete the team.\nAre you sure you want to proceed? (y/n)"
}

func (m model) renderPromptJoinTeamView() string {
	if m.joinPrompt.team != nil {
		return confirmStyle.Render(fmt.Sprintf("\n  Join team '%s'? (y/n)\n", m.joinPrompt.team.Name))
	}
	return confirmStyle.Render("\n  Invalid team join code.\n")
}

func (m model) renderTeamMembersView() string {
	title := titleStyle.Render("Team Members")

	if m.user.TeamID == nil {
		return title + "\n\nYou are not on a team."
	}

	if len(m.teamMembers.members) == 0 {
		return title + "\n\nNo team members found."
	}

	var content strings.Builder
	content.WriteString(title + "\n\n")
	content.WriteString(fmt.Sprintf("%-20s %-10s\n", "Username", "Points"))
	content.WriteString(strings.Repeat("─", 35) + "\n")

	for i, member := range m.teamMembers.members {
		cursor := "  "
		if i == m.teamMembers.cursor {
			cursor = selectedStyle.Render("  ")
		}
		content.WriteString(fmt.Sprintf("%s%-20s %-10d\n", cursor, member.User.Username, member.Points))
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: scroll  q/Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return content.String() + help
}
