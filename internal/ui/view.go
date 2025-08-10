package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"ctfsh/internal/config"
	"ctfsh/internal/db"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

func (m model) renderMenuView() string {
	title := titleStyle.Render("🚩 CTFsh")

	var teamName string
	var err error
	if m.user.TeamID != nil {
		teamName, err = db.GetTeamName(*m.user.TeamID)
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

func (m model) renderScoreboardView() string {
	title := titleStyle.Render("Scoreboard")

	filtered := m.scoreboard.filteredScoreboard()
	// Cursor comes from table; selection used only for later features if needed
	var b strings.Builder
	// Always show title
	b.WriteString(title + "\n\n")

	// Render time series chart of scores for the top entries
	// Choose up to 10 entries based on current visible (filtered) table order
	maxSeries := min(10, len(filtered))
	// Determine chart dimensions to fit within the centered window
	// Window width is m.width/2; subtract border (2) + padding (8)
	chartWidth := max(m.width-22, 20)
	chartHeight := 10
	// Build datasets
	if maxSeries > 0 && chartWidth > 0 && chartHeight > 0 {
		var minTime, maxTime time.Time
		haveTimeRange := false
		maxY := 0.0

		// Create chart with default options; set time range later when known
		chart := timeserieslinechart.New(
			chartWidth,
			chartHeight,
			timeserieslinechart.WithXLabelFormatter(timeserieslinechart.HourTimeLabelFormatter()),
			timeserieslinechart.WithUpdateHandler(timeserieslinechart.SecondUpdateHandler(60)),
		)

		// Assign distinct styles (top 10)
		palette := []lipgloss.Color{
			lipgloss.Color("205"), // magenta
			lipgloss.Color("39"),  // blue
			lipgloss.Color("208"), // orange
			lipgloss.Color("82"),  // green
			lipgloss.Color("196"), // red
			lipgloss.Color("45"),  // cyan
			lipgloss.Color("226"), // yellow
			lipgloss.Color("135"), // purple
			lipgloss.Color("50"),  // teal
			lipgloss.Color("201"), // pink
		}
		type series struct {
			name string
			id   int
			ts   []timeserieslinechart.TimePoint
			last float64
		}
		var seriesList []series

		for i := 0; i < maxSeries; i++ {
			t := filtered[i]
			var points []db.ScorePoint
			var err error
			if t.ID < 0 {
				points, err = db.GetUserScoreTimeSeries(-t.ID)
			} else {
				points, err = db.GetTeamScoreTimeSeries(t.ID)
			}
			if err != nil {
				continue
			}
			// Convert to chart TimePoints
			ts := make([]timeserieslinechart.TimePoint, 0, max(1, len(points)))
			var last float64
			for _, p := range points {
				v := float64(p.Score)
				ts = append(ts, timeserieslinechart.TimePoint{Time: p.Time, Value: v})
				last = v
				if v > maxY {
					maxY = v
				}
				if !haveTimeRange {
					minTime, maxTime = p.Time, p.Time
					haveTimeRange = true
				} else {
					if p.Time.Before(minTime) {
						minTime = p.Time
					}
					if p.Time.After(maxTime) {
						maxTime = p.Time
					}
				}
			}
			seriesList = append(seriesList, series{name: t.Name, id: t.ID, ts: ts, last: last})
		}

		// Anchor the time range to the first and last solve across all teams
		if haveTimeRange {
			// Prepend first-solve anchor and append last-solve anchor
			for _, s := range seriesList {
				ts := s.ts
				// Prepend zero at global min if this team didn't solve at that time
				if len(ts) == 0 || ts[0].Time.After(minTime) {
					ts = append([]timeserieslinechart.TimePoint{{Time: minTime, Value: 0}}, ts...)
				}
				// Append last value at global max if needed
				if len(ts) == 0 || ts[len(ts)-1].Time.Before(maxTime) {
					ts = append(ts, timeserieslinechart.TimePoint{Time: maxTime, Value: s.last})
					if s.last > maxY {
						maxY = s.last
					}
				}
				// Push into chart after anchoring
				for _, tp := range ts {
					chart.PushDataSet(s.name, tp)
				}
				// Determine palette index by rank (place) if available from order
				// seriesList is in order of visible table (filtered) among selected teams
				// Use that index for color mapping
				idx := 0
				for i := range seriesList {
					if seriesList[i].name == s.name {
						idx = i
						break
					}
				}
				// Apply color (braille thickness is fixed; bold has no effect on braille patterns)
				st := lipgloss.NewStyle().Foreground(palette[idx%len(palette)])
				chart.SetDataSetStyle(s.name, st)
			}
			// No extra series: graph strictly matches top 10 visible rows
		}

		if haveTimeRange {
			if !maxTime.After(minTime) {
				maxTime = minTime.Add(time.Second)
			}
			chart.SetTimeRange(minTime, maxTime)
			// Tighten view to the full span so labels tick at seconds/minutes
			chart.SetViewTimeRange(minTime, maxTime)
		}
		// Ensure a reasonable Y range so lines are visible
		upper := math.Max(1, maxY)
		chart.SetYRange(0, upper)
		chart.SetViewYRange(0, upper)

		// Draw all datasets using braille for smoother lines
		chart.DrawBrailleAll()
		b.WriteString(chart.View())
		b.WriteString("\n\n")
	}
	if m.scoreboard.searchMode {
		b.WriteString("Search: " + m.scoreboard.search + "\n")
	} else {
		b.WriteString("Press '/' to search\n")
	}

	// Build a bubbles table instead of manual formatting
	contentWidth := max(m.width-18, 20)
	// Determine columns dynamically
	maxScore := 0
	for _, t := range m.scoreboard.teams {
		if t.Score > maxScore {
			maxScore = t.Score
		}
	}
	scoreWidth := max(len("Score"), len(fmt.Sprintf("%d", maxScore)))
	rankWidth := 4
	teamWidth := max(6, contentWidth-rankWidth-scoreWidth-6)

	// Update and size the table (do not colorize here so Selected style applies a grey background only)
	m.scoreboard.tbl.SetColumns([]table.Column{
		{Title: "Rank", Width: rankWidth},
		{Title: "Team", Width: teamWidth},
		{Title: "Score", Width: scoreWidth},
	})
	m.scoreboard.tbl.SetWidth(contentWidth)
	// Auto-resize height: show at least 5 rows (plus header), at most all filtered rows
	visibleRows := max(5, min(20, len(filtered)))
	m.scoreboard.tbl.SetHeight(visibleRows + 1)

	// Populate rows with colorized team names for top 10 visible rows to match graph palette
	rows := make([]table.Row, 0, len(filtered))
	palette := []lipgloss.Color{
		lipgloss.Color("205"), // magenta
		lipgloss.Color("39"),  // blue
		lipgloss.Color("208"), // orange
		lipgloss.Color("82"),  // green
		lipgloss.Color("196"), // red
		lipgloss.Color("45"),  // cyan
		lipgloss.Color("226"), // yellow
		lipgloss.Color("135"), // purple
		lipgloss.Color("50"),  // teal
		lipgloss.Color("201"), // pink
	}
	// Determine selected row index so we can avoid styling conflicts there
	selectedIdx := -1
	if m.scoreboard.tbl.Cursor() >= 0 && m.scoreboard.tbl.Cursor() < len(filtered) {
		selectedIdx = m.scoreboard.tbl.Cursor()
	}
	for i, t := range filtered {
		// When selected, avoid inner ANSI styles entirely so the table's Selected background
		// applies across the entire row (including the (solo) suffix).
		if i == selectedIdx {
			base := t.Name
			suffix := ""
			if t.ID < 0 {
				suffix = " (solo)"
			}
			rows = append(rows, table.Row{
				fmt.Sprintf("%d", t.place),
				base + suffix,
				fmt.Sprintf("%d", t.Score),
			})
			continue
		}

		// Non-selected: color-match top 10 names to graph lines
		base := t.Name
		if i < 10 {
			base = lipgloss.NewStyle().Foreground(palette[i%len(palette)]).Render(base)
		}
		// Keep solo suffix greyed out
		suffix := ""
		if t.ID < 0 {
			suffix = " " + helpStyle.Render("(solo)")
		}

		rows = append(rows, table.Row{
			fmt.Sprintf("%d", t.place),
			base + suffix,
			fmt.Sprintf("%d", t.Score),
		})
	}
	m.scoreboard.tbl.SetRows(rows)
	// Focused so it handles scrolling and highlighting; when searching, blur to avoid conflict
	if m.scoreboard.searchMode {
		m.scoreboard.tbl.Blur()
	} else {
		m.scoreboard.tbl.Focus()
	}
	b.WriteString(m.scoreboard.tbl.View())

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
