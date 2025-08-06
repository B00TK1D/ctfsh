package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	authorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("204"))

	categoryStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	commandStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46"))

	// Main window style
	windowStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 4).
			Margin(1, 2)
)

func (m model) renderAuthView() string {
	var b strings.Builder
	b.WriteString("\n  Welcome to the CTF!\n")
	b.WriteString("  Please choose a username to register your public key.\n\n")
	b.WriteString("  " + m.usernameInput.View() + "\n\n")

	if m.message != "" {
		style := errorStyle
		b.WriteString("  " + style.Render(m.message) + "\n")
	}

	if m.showHelp {
		b.WriteString("\n" + helpStyle.Render("Enter: confirm  Ctrl+C: quit  ?: toggle help"))
	} else {
		b.WriteString("\n" + helpStyle.Render("Press '?' for help."))
	}
	return b.String()
}

func (m model) renderMenuView() string {
	title := titleStyle.Render("🚩 CTFsh")

	var teamName string
	var err error
	if m.user.TeamID != nil {
		teamName, err = getTeamName(*m.user.TeamID)
		if err != nil {
			teamName = "Error"
		}
	}

	var userInfo string
	if m.user.TeamID != nil {
		userInfo = fmt.Sprintf("User: %s | Team: %s", m.user.Username, teamName)
	} else {
		userInfo = fmt.Sprintf("User: %s | No team", m.user.Username)
	}

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

func (m model) renderChallengeView() string {
	title := titleStyle.Render("Challenges")
	renderList := m.buildChallengeRenderList()

	if len(renderList) == 0 {
		return title + "\n\nNo challenges available."
	}

	var content strings.Builder
	for i, item := range renderList {
		cursor := "  "
		if i == m.cursor {
			cursor = selectedStyle.Render("> ")
		}

		switch v := item.(type) {
		case categoryListItem:
			arrow := "▶"
			if v.isExpanded {
				arrow = "▼"
			}
			content.WriteString(fmt.Sprintf("%s%s %s (%d/%d)\n",
				cursor, arrow, categoryStyle.Render(v.name), v.solved, v.total))
		case Challenge:
			status := ""
			if v.Solved {
				status = successStyle.Render(" ✓")
				// Show solver if on a team
				if m.teamSolvers != nil {
					if solver, ok := m.teamSolvers[v.ID]; ok {
						status += successStyle.Render(fmt.Sprintf(" (%s)", solver))
					}
				}
			}
			content.WriteString(fmt.Sprintf("  %s%s (%d pts)%s\n", cursor, v.Name, v.Points, status))
		}
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: move  Enter/Space: expand/select  q/Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return fmt.Sprintf("%s\n\n%s%s", title, content.String(), help)
}

func (m model) renderChallengeDetailView() string {
	ch := m.selectedChal
	titleStr := ch.Name
	if ch.Author != "" {
		titleStr += authorStyle.Render(fmt.Sprintf(" (by %s)", ch.Author))
	}
	title := titleStyle.Render(titleStr)

	status := "Unsolved"
	if ch.Solved {
		status = successStyle.Render("✓ Solved")
		// Show solver if on a team
		if m.teamSolvers != nil {
			if solver, ok := m.teamSolvers[ch.ID]; ok {
				status += successStyle.Render(fmt.Sprintf(" (%s)", solver))
			}
		}
	}

	details := fmt.Sprintf(
		"%s - %d pts",
		categoryStyle.Render(ch.Category),
		ch.Points,
	)
	if ch.Solved {
		details += successStyle.Render(" ✓ Solved")
		// Show solver if on a team
		if m.teamSolvers != nil {
			if solver, ok := m.teamSolvers[ch.ID]; ok {
				details += successStyle.Render(fmt.Sprintf(" by %s", solver))
			}
		}
	}
	details += fmt.Sprintf("\n\n%s\n", ch.Description)

	if len(ch.Downloads) > 0 {
		scpCmd := "scp"
		if sshPort != 22 {
			scpCmd += fmt.Sprintf(" -P %d", sshPort)
		}
		scpCmd += fmt.Sprintf(" -r %s:%s .", sshDomain, ch.Name)
		details += fmt.Sprintf("\nDownload: %s", commandStyle.Render(scpCmd))
	}

	if len(ch.Ports) > 0 {
		tunnelCmd := "ssh"
		if sshPort != 22 {
			tunnelCmd += fmt.Sprintf(" -p %d", sshPort)
		}
		for _, port := range ch.Ports {
			tunnelCmd += fmt.Sprintf(" -L %d:%s:%d", port, ch.Name, port)
		}
		tunnelCmd += fmt.Sprintf(" %s@%s", ch.Name, sshDomain)
		details += fmt.Sprintf("\nInstance: %s", commandStyle.Render(tunnelCmd))
	}

	help := ""
	if !ch.Solved {
		if m.showHelp {
			help = "\n" + helpStyle.Render("Enter/Space: submit flag  q/Esc: back  ?: toggle help")
		} else {
			help = "\n" + helpStyle.Render("Press Enter to submit flag or '?' for help.")
		}
	}
	return fmt.Sprintf("%s\n\n%s\n%s", title, details, help)
}

func (m model) renderScoreboardView() string {
	title := titleStyle.Render("Scoreboard")

	filtered := m.filteredScoreboard()
	var b strings.Builder
	// Always show title and search bar
	b.WriteString(title + "\n\n")
	if m.scoreboardSearchMode {
		b.WriteString("Search: " + m.scoreboardSearch + "\n")
	} else {
		b.WriteString("Press '/' to search\n")
	}
	// Always show header
	b.WriteString(fmt.Sprintf("%-4s %-20s %-8s %s\n", "Rank", "Team", "Players", "Score"))
	b.WriteString(strings.Repeat("─", 45) + "\n")

	// Show up to 20 rows, or as many as fit on the screen
	windowSize := min(m.scoreboardRows(), 20)
	teamRows := 0
	if len(filtered) == 0 {
		b.WriteString(helpStyle.Render("(no teams match search)\n"))
		teamRows++
	}
	// Scrolling window logic: ensure cursor is always visible
	start := 0
	if m.scoreboardCursor >= windowSize {
		start = m.scoreboardCursor - windowSize + 1
	}
	if start > len(filtered)-windowSize {
		start = len(filtered) - windowSize
	}
	if start < 0 {
		start = 0
	}
	end := min(start+windowSize, len(filtered))
	for i := start; i < end; i++ {
		if i < len(filtered) {
			team := filtered[i]
			teamName := team.Name
			if team.ID < 0 {
				teamName = fmt.Sprintf("%s %s", team.Name, helpStyle.Render("(solo)"))
			}
			cursor := "  "
			if i == m.scoreboardCursor {
				cursor = selectedStyle.Render("> ")
			}
			b.WriteString(fmt.Sprintf("%s%-4d %-20s %-8d %d\n", cursor, i+1, teamName, team.PlayerCount, team.Score))
			teamRows++
		} else {
			break
		}
	}
	// Pad with blank lines if needed
	for i := teamRows; i < windowSize; i++ {
		b.WriteString("\n")
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: scroll  /: search  q/Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return b.String() + help
}

func (m model) renderTeamView() string {
	title := titleStyle.Render("Team Management")

	var content string
	// If user is on a team, show leave/regenerate options and join command
	if m.user.TeamID != nil {
		teamName, err := getTeamName(*m.user.TeamID)
		if err != nil {
			teamName = "Error fetching name"
		}
		joinCode := m.teamJoinCode
		sshCmd := ""
		if joinCode != "" {
			if sshPort == 22 {
				sshCmd = fmt.Sprintf("ssh %s@%s", joinCode, sshDomain)
			} else {
				sshCmd = fmt.Sprintf("ssh %s@%s -p %d", joinCode, sshDomain, sshPort)
			}
		}
		options := []string{"Leave Team", "Regenerate Join Code"}
		var menu strings.Builder
		for i, option := range options {
			cursor := "  "
			if i == m.teamMenuCursor {
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
			if i == m.teamMenuCursor {
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
		message = "\n\n" + style.Render(m.message)
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: move  Enter/Space: select  q/Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return fmt.Sprintf("%s\n\n%s%s%s", title, content, message, help)
}

func (m model) renderGenericInputView() string {
	title := titleStyle.Render(m.inputTitle)
	input := m.inputModel.View()

	message := "\n"
	if m.message != "" {
		style := successStyle
		if m.messageType == "error" {
			style = errorStyle
		}
		message = "\n" + style.Render(m.message)
	}

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("Enter: submit  Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}
	return fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s%s%s",
		title,
		"Enter value below:",
		input,
		"Press Esc to go back.",
		message,
		help)
}

func (m model) renderFlagResultView() string {
	var b strings.Builder
	b.WriteString("\n  ")
	if m.messageType == "success" {
		b.WriteString(successStyle.Render(m.message))
	} else {
		b.WriteString(errorStyle.Render(m.message))
	}
	b.WriteString("\n\n  Press any key to return to the challenge list.")
	return b.String()
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
