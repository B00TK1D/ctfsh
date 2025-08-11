package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"ctfsh/internal/db"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

var scoreboardPalette = []lipgloss.Color{
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

func (m model) renderScoreboardView() string {
	title := titleStyle.Render("Scoreboard")

	filtered := m.scoreboard.filteredScoreboard()
	var b strings.Builder
	b.WriteString(title + "\n\n")

	maxSeries := min(10, len(filtered))
	chartWidth := max(m.width-22, 20)
	graphHeight := m.height - (20 + maxSeries)

	if maxSeries > 0 && chartWidth > 0 && graphHeight > 0 {
		b.WriteString(m.renderScoreboardGraph(filtered[:maxSeries]) + "\n\n")
	}

	if m.scoreboard.searchMode {
		b.WriteString("Search: " + m.scoreboard.search + "\n")
	} else {
		b.WriteString("Press '/' to search\n")
	}

	b.WriteString(m.renderScoreboardTable(filtered))

	help := ""
	if m.showHelp {
		help = "\n" + helpStyle.Render("↑/↓: scroll  /: search  q/Esc: back  ?: toggle help")
	} else {
		help = "\n" + helpStyle.Render("Press '?' for help.")
	}

	return b.String() + help
}

func (m model) renderScoreboardGraph(filtered []scoreboardTeam) string {
	maxSeries := min(10, len(filtered))
	chartWidth := max(m.width-22, 20)
	chartHeight := 10

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

	type series struct {
		name string
		id   int
		ts   []timeserieslinechart.TimePoint
		last float64
	}
	var seriesList []series

	for i := range maxSeries {
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

		sortedName := fmt.Sprintf("%04d-%s", len(m.scoreboard.teams)-t.place, t.Name)
		if m.scoreboard.tbl.Cursor()+1 == t.ID || m.scoreboard.tbl.Cursor()+1 == -t.ID {
			sortedName = fmt.Sprintf("_%s", sortedName) // Ensure selected team is first
		}
		seriesList = append(seriesList, series{name: sortedName, id: t.ID, ts: ts, last: last})
	}

	if haveTimeRange {
		for _, s := range seriesList {
			ts := s.ts
			if len(ts) == 0 || ts[0].Time.After(minTime) {
				ts = append([]timeserieslinechart.TimePoint{{Time: minTime, Value: 0}}, ts...)
			}

			if len(ts) == 0 || ts[len(ts)-1].Time.Before(maxTime) {
				ts = append(ts, timeserieslinechart.TimePoint{Time: maxTime, Value: s.last})
				if s.last > maxY {
					maxY = s.last
				}
			}

			for _, tp := range ts {
				chart.PushDataSet(s.name, tp)
			}

			idx := 0
			for i := range seriesList {
				if seriesList[i].name == s.name {
					idx = i
					break
				}
			}

			st := lipgloss.NewStyle().Foreground(scoreboardPalette[idx%len(scoreboardPalette)])
			if m.scoreboard.tbl.Cursor()+1 == s.id || m.scoreboard.tbl.Cursor()+1 == -s.id {
				st = st.Foreground(lipgloss.Color("255"))
			}
			chart.SetDataSetStyle(s.name, st)
		}
	}

	if haveTimeRange {
		if !maxTime.After(minTime) {
			maxTime = minTime.Add(time.Second)
		}
		chart.SetTimeRange(minTime, maxTime)
		chart.SetViewTimeRange(minTime, maxTime)
	}
	upper := math.Max(1, maxY)
	chart.SetYRange(0, upper)
	chart.SetViewYRange(0, upper)

	chart.DrawBrailleAll()
	return chart.View()
}

func (m model) renderScoreboardTable(filtered []scoreboardTeam) string {

	var b strings.Builder

	contentWidth := max(m.width-18, 20)
	maxScore := 0
	for _, t := range m.scoreboard.teams {
		if t.Score > maxScore {
			maxScore = t.Score
		}
	}
	scoreWidth := max(len("Score"), len(fmt.Sprintf("%d", maxScore)))
	rankWidth := 4
	teamWidth := max(6, contentWidth-rankWidth-scoreWidth-6)

	m.scoreboard.tbl.SetColumns([]table.Column{
		{Title: "Rank", Width: rankWidth},
		{Title: "Team", Width: teamWidth},
		{Title: "Score", Width: scoreWidth},
	})
	m.scoreboard.tbl.SetWidth(contentWidth)
	visibleRows := max(5, min(20, len(filtered)))
	m.scoreboard.tbl.SetHeight(visibleRows + 1)

	rows := make([]table.Row, 0, len(filtered))

	selectedIdx := -1
	if m.scoreboard.tbl.Cursor() >= 0 && m.scoreboard.tbl.Cursor() < len(filtered) {
		selectedIdx = m.scoreboard.tbl.Cursor()
	}
	for i, t := range filtered {
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

		base := t.Name
		if i < 10 {
			base = lipgloss.NewStyle().Foreground(scoreboardPalette[i%len(scoreboardPalette)]).Render(base)
		}

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

	if m.scoreboard.searchMode {
		m.scoreboard.tbl.Blur()
	} else {
		m.scoreboard.tbl.Focus()
	}
	b.WriteString(m.scoreboard.tbl.View())

	return b.String()
}
