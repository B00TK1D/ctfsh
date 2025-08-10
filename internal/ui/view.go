package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ctfsh/internal/config"
	"ctfsh/internal/db"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
)

// genericMin returns the smaller of two ints.
func genericMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// genericMax returns the larger of two ints.
func genericMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

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
	var selected *scoreboardTeam
	if m.scoreboard.cursor >= 0 && m.scoreboard.cursor < len(filtered) {
		t := filtered[m.scoreboard.cursor]
		selected = &t
	}
	var b strings.Builder
	// Always show title
	b.WriteString(title + "\n\n")

	// Render time series chart of scores for the top entries
	// Choose up to 5 top entries based on current scoreboard order
	maxSeries := min(5, len(m.scoreboard.teams))
	// Determine chart dimensions to fit within the centered window
	// Window width is m.width/2; subtract border (2) + padding (8)
	chartWidth := genericMax((m.width/2)-10, 20)
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

		// Increase to top 10
		if maxSeries > 10 {
			maxSeries = 10
		}
		for i := 0; i < maxSeries; i++ {
			t := m.scoreboard.teams[i]
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
			ts := make([]timeserieslinechart.TimePoint, 0, genericMax(1, len(points)))
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
				// seriesList is in order of scoreboard rank among selected teams
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
			// If selected team is not in the top N, add a bold line just for that team
			if selected != nil {
				found := false
				for _, s := range seriesList {
					if s.id == selected.ID {
						found = true
						break
					}
				}
				if !found {
					// Fetch selected team series
					var pts []db.ScorePoint
					var err error
					if selected.ID < 0 {
						pts, err = db.GetUserScoreTimeSeries(-selected.ID)
					} else {
						pts, err = db.GetTeamScoreTimeSeries(selected.ID)
					}
					if err == nil {
						ts := make([]timeserieslinechart.TimePoint, 0, genericMax(2, len(pts)))
						last := 0.0
						for _, p := range pts {
							v := float64(p.Score)
							ts = append(ts, timeserieslinechart.TimePoint{Time: p.Time, Value: v})
							last = v
							if v > maxY {
								maxY = v
							}
						}
						if len(ts) == 0 || ts[0].Time.After(minTime) {
							ts = append([]timeserieslinechart.TimePoint{{Time: minTime, Value: 0}}, ts...)
						}
						if len(ts) == 0 || ts[len(ts)-1].Time.Before(maxTime) {
							ts = append(ts, timeserieslinechart.TimePoint{Time: maxTime, Value: last})
						}
						for _, tp := range ts {
							chart.PushDataSet(selected.Name, tp)
						}
						// Color selected extra series brightly; no bold on braille
						chart.SetDataSetStyle(selected.Name, lipgloss.NewStyle().Foreground(lipgloss.Color("15")))
					}
				}
			}
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

	// Compute dynamic column widths to fit the window
	contentWidth := genericMax((m.width/2)-10, 20) // account for window border/padding
	cursorWidth := 2
	rowBaseWidth := contentWidth - cursorWidth
	rankWidth := 4
	// Determine numeric column widths from data
	maxPlayers := 0
	maxScore := 0
	for _, t := range m.scoreboard.teams {
		if t.PlayerCount > maxPlayers {
			maxPlayers = t.PlayerCount
		}
		if t.Score > maxScore {
			maxScore = t.Score
		}
	}
	playersWidth := genericMax(len("Players"), len(strconv.Itoa(maxPlayers)))
	scoreWidth := genericMax(len("Score"), len(strconv.Itoa(maxScore)))
	// Spaces: 1 (rank-team) + 1 (team-players) + 3 (players-score) + 2 (right pad) = 7
	teamWidth := max(rowBaseWidth-rankWidth-playersWidth-scoreWidth-7, 8)

	// Header (no cursor on header line)
	header := fmt.Sprintf("%-*s %-*s   %*s   %*s\n",
		rankWidth, "Rank",
		teamWidth, "Team",
		playersWidth, "Players",
		scoreWidth, "Score",
	)
	b.WriteString(header)
	b.WriteString(strings.Repeat("─", contentWidth) + "\n")

	// Show up to 20 rows, or as many as fit on the screen
	windowSize := genericMin(len(m.scoreboard.teams), 20)
	teamRows := 0
	if len(filtered) == 0 {
		b.WriteString(helpStyle.Render("(no teams match search)\n"))
		teamRows++
	}
	// Scrolling window logic: ensure cursor is always visible
	start := 0
	if m.scoreboard.cursor >= windowSize {
		start = m.scoreboard.cursor - windowSize + 1
	}
	if start > len(filtered)-windowSize {
		start = len(filtered) - windowSize
	}
	if start < 0 {
		start = 0
	}

	end := genericMin(start+windowSize, len(filtered))
	for i := start; i < end; i++ {
		if i < len(filtered) {
			team := filtered[i]
			baseName := team.Name
			paddingLen := 20 - len(baseName)
			suffix := ""

			if team.ID < 0 {
				suffix = " " + helpStyle.Render("(solo)")
				paddingLen -= 7
			}

			// Name color is applied to the entire padded cell below
			cursor := "  "
			if i == m.scoreboard.cursor {
				cursor = selectedStyle.Render("> ")
			}

			// Build styled team cell and pad using lipgloss.Width to ignore ANSI codes
			nameStyled := baseName
			if team.place <= 10 {
				pal := []lipgloss.Color{
					lipgloss.Color("205"), lipgloss.Color("39"), lipgloss.Color("208"), lipgloss.Color("82"), lipgloss.Color("196"),
					lipgloss.Color("45"), lipgloss.Color("226"), lipgloss.Color("135"), lipgloss.Color("50"), lipgloss.Color("201"),
				}
				nameStyled = lipgloss.NewStyle().Foreground(pal[(team.place-1)%len(pal)]).Render(baseName)
			}
			styledSuffix := suffix
			combined := nameStyled + styledSuffix

			// If too wide, drop suffix first
			if lipgloss.Width(combined) > teamWidth {
				combined = nameStyled
			}

			// If still too wide, truncate base name and re-style
			if lipgloss.Width(combined) > teamWidth {
				// truncate plain base name by runes, append ellipsis
				r := []rune(baseName)
				if teamWidth > 1 && len(r) >= teamWidth-1 {
					r = r[:teamWidth-1]
					truncated := string(r) + "…"
					if team.place <= 10 {
						pal := []lipgloss.Color{
							lipgloss.Color("205"), lipgloss.Color("39"), lipgloss.Color("208"), lipgloss.Color("82"), lipgloss.Color("196"),
							lipgloss.Color("45"), lipgloss.Color("226"), lipgloss.Color("135"), lipgloss.Color("50"), lipgloss.Color("201"),
						}
						combined = lipgloss.NewStyle().Foreground(pal[(team.place-1)%len(pal)]).Render(truncated)
					} else {
						combined = truncated
					}
				} else if teamWidth > 0 {
					r = r[:teamWidth]
					truncated := string(r)
					if team.place <= 10 {
						pal := []lipgloss.Color{
							lipgloss.Color("205"), lipgloss.Color("39"), lipgloss.Color("208"), lipgloss.Color("82"), lipgloss.Color("196"),
							lipgloss.Color("45"), lipgloss.Color("226"), lipgloss.Color("135"), lipgloss.Color("50"), lipgloss.Color("201"),
						}
						combined = lipgloss.NewStyle().Foreground(pal[(team.place-1)%len(pal)]).Render(truncated)
					} else {
						combined = truncated
					}
				}
			}

			if lipgloss.Width(combined) < teamWidth {
				combined += strings.Repeat(" ", teamWidth-lipgloss.Width(combined))
			}
			teamCell := combined

			line := fmt.Sprintf("%s%-*d %s %*d   %*d  ",
				cursor,
				rankWidth, team.place,
				teamCell,
				playersWidth, team.PlayerCount,
				scoreWidth, team.Score,
			)

			b.WriteString(line)
			b.WriteString("\n")
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
