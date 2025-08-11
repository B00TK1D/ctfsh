package ui

import (
	"fmt"
	"strings"

	"ctfsh/internal/config"
)

func (m model) renderChallengeView() string {
	title := titleStyle.Render("Challenges")
	renderList := m.challenges.buildChallengeRenderList()

	if len(renderList) == 0 {
		return title + "\n\nNo challenges available."
	}

	var content strings.Builder
	for i, item := range renderList {
		cursor := "  "
		if i == m.challenges.cursor {
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
		case challengeWrapper:
			status := ""
			if v.solved {
				status = successStyle.Render(" ✓")
				// Show solver if on a team
				if m.challenges.teamSolvers != nil {
					if solver, ok := m.challenges.teamSolvers[v.ID]; ok {
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
	ch := m.challenges.selectedChal
	titleStr := ch.Name
	if ch.Author != "" {
		titleStr += authorStyle.Render(fmt.Sprintf(" (by %s)", ch.Author))
	}
	title := titleStyle.Render(titleStr)

	status := "Unsolved"
	if ch.solved {
		status = successStyle.Render("✓ Solved")
		// Show solver if on a team
		if m.challenges.teamSolvers != nil {
			if solver, ok := m.challenges.teamSolvers[ch.ID]; ok {
				status += successStyle.Render(fmt.Sprintf(" (%s)", solver))
			}
		}
	}

	details := fmt.Sprintf(
		"%s - %d pts",
		categoryStyle.Render(ch.Category),
		ch.Points,
	)
	if ch.solved {
		details += successStyle.Render(" ✓ Solved")
		// Show solver if on a team
		if m.challenges.teamSolvers != nil {
			if solver, ok := m.challenges.teamSolvers[ch.ID]; ok {
				details += successStyle.Render(fmt.Sprintf(" by %s", solver))
			}
		}
	}
	details += fmt.Sprintf("\n\n%s\n", ch.Description)

	if len(ch.Downloads) > 0 {
		scpCmd := "scp"
		if config.Port != 22 {
			scpCmd += fmt.Sprintf(" -P %d", config.Port)
		}
		scpCmd += fmt.Sprintf(" -r %s:%s .", config.Host, ch.Name)
		details += fmt.Sprintf("\nDownload: %s", commandStyle.Render(scpCmd))
	}

	if len(ch.Ports) > 0 {
		tunnelCmd := "ssh"
		if config.Port != 22 {
			tunnelCmd += fmt.Sprintf(" -p %d", config.Port)
		}
		for _, port := range ch.Ports {
			tunnelCmd += fmt.Sprintf(" -L %d:%s:%d", port, ch.Name, port)
		}
		tunnelCmd += fmt.Sprintf(" %s@%s", ch.Name, config.Host)
		details += fmt.Sprintf("\nInstance: %s", commandStyle.Render(tunnelCmd))
	}

	help := ""
	if !ch.solved {
		if m.showHelp {
			help = "\n" + helpStyle.Render("Enter/Space: submit flag  q/Esc: back  ?: toggle help")
		} else {
			help = "\n" + helpStyle.Render("Press Enter to submit flag or '?' for help.")
		}
	}
	return fmt.Sprintf("%s\n\n%s\n%s", title, details, help)
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
