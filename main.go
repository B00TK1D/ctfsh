package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"io"
	"io/fs"
	"path/filepath"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/scp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/sftp"
)

var (
	// Styles
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1)

	categoryStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

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
			Margin(1, 2.)
)

// Centered style for confirmation messages
var confirmStyle = lipgloss.NewStyle().Align(lipgloss.Center)

// Configurable global for SSH domain and port
var (
	sshDomain = "ctfsh.com"
	sshPort   = 2223 // set to 22 to omit -p
	// Configurable download root
	downloadRoot = "./downloads"
)

// Represents an item in the challenge list (either a category or a challenge)
type categoryListItem struct {
	name       string
	total      int
	solved     int
	isExpanded bool
}

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Enter  key.Binding
	Back   key.Binding
	Cancel key.Binding
	Quit   key.Binding
	Help   key.Binding
	Tab    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Select, k.Enter},
		{k.Back, k.Tab, k.Quit},
	}
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
	Select: key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter/space", "select")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	Back:   key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "back")),
	Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Quit:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
	Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch view")),
}

type sessionState int

const (
	authView sessionState = iota
	menuView
	challengeView
	challengeDetailView
	scoreboardView
	teamView
	genericInputView
	flagResultView
	confirmDeleteTeamView
	promptJoinTeamView sessionState = 1001 // pick a high value to avoid collision
)

type model struct {
	user                 *User
	sshKey               string // For registration flow
	state                sessionState
	width                int
	height               int
	challenges           []Challenge
	categories           []string
	cursor               int
	menuCursor           int
	teamMenuCursor       int
	scoreboard           []Team
	scoreboardCursor     int
	scoreboardSearch     string
	scoreboardSearchMode bool
	selectedChal         Challenge
	usernameInput        textinput.Model
	flagInput            textinput.Model
	teamInput            textinput.Model
	message              string
	messageType          string
	help                 help.Model
	showHelp             bool
	expandedCats         map[string]bool
	confirmQuit          bool
	// For generic input view
	inputTitle   string
	inputModel   *textinput.Model
	onSubmit     func(string) (string, string) // input -> (message, messageType)
	onBackState  sessionState
	teamSolvers  map[int]string // challenge_id -> username
	teamJoinCode string
	joinPrompt   joinPromptInfo
}

type joinPromptState int

const (
	noJoinPrompt joinPromptState = iota
	promptJoinTeam
	promptAlreadyOnTeam
)

type joinPromptInfo struct {
	team  *Team
	state joinPromptState
}

// initialModel is for users who are already authenticated.
func initialModel(user *User) model {
	m := model{user: user, state: menuView}
	m.finishInitialization()

	return m
}

// newRegistrationModel is for new users who need to pick a username.
func newRegistrationModel(sshKey string, joinPrompt joinPromptInfo) model {
	unInput := textinput.New()
	unInput.Focus()
	unInput.CharLimit = 32

	return model{
		sshKey:        sshKey,
		state:         authView,
		usernameInput: unInput,
		help:          help.New(),
		joinPrompt:    joinPrompt,
	}
}

// finishInitialization populates the model with data that requires a user object.
func (m *model) finishInitialization() {
	flagInput := textinput.New()
	flagInput.CharLimit = 100
	m.flagInput = flagInput

	teamInput := textinput.New()
	teamInput.CharLimit = 50
	m.teamInput = teamInput

	m.help = help.New()

	challenges, _ := getChallenges()
	if m.user != nil {
		solvedMap, _ := getChallengesSolvedByUser(m.user.ID)
		for i := range challenges {
			if solvedMap[challenges[i].ID] {
				challenges[i].Solved = true
			}
		}
		// If user is on a team, get solvers for each challenge and join code
		if m.user.TeamID != nil {
			solvers, _ := getTeamChallengeSolvers(*m.user.TeamID)
			m.teamSolvers = solvers
			_, code, _ := getTeamNameAndCode(*m.user.TeamID)
			m.teamJoinCode = code
		} else {
			// For solo users, treat their username as the team name and only show their solves
			solvers := make(map[int]string)
			solvedMap, _ := getChallengesSolvedByUser(m.user.ID)
			for cid := range solvedMap {
				solvers[cid] = m.user.Username
			}
			m.teamSolvers = solvers
			m.teamJoinCode = ""
		}
	}
	m.challenges = challenges
	m.expandedCats = make(map[string]bool)

	categoryMap := make(map[string]bool)
	for _, ch := range challenges {
		categoryMap[ch.Category] = true
	}
	var categories []string
	for cat := range categoryMap {
		categories = append(categories, cat)
	}
	sort.Strings(categories)
	m.categories = categories
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		return m, nil

	case tea.KeyMsg:
		// Global quit, even if in confirmation
		if key.Matches(msg, keys.Quit) {
			return m, tea.Quit
		}

		// Handle quit confirmation if active
		if m.confirmQuit {
			switch msg.String() {
			case "y", "Y":
				return m, tea.Quit
			case "n", "N", "esc", "q":
				m.confirmQuit = false
				return m, nil
			}
			return m, nil
		}

		switch m.state {
		case authView:
			return m.updateAuthView(msg)
		case menuView:
			return m.updateMenuView(msg)
		case challengeView:
			return m.updateChallengeView(msg)
		case challengeDetailView:
			return m.updateChallengeDetailView(msg)
		case scoreboardView:
			return m.updateScoreboardView(msg)
		case teamView:
			return m.updateTeamView(msg)
		case genericInputView:
			return m.updateGenericInputView(msg)
		case flagResultView:
			return m.updateFlagResultView(msg)
		case confirmDeleteTeamView:
			return m.updateConfirmDeleteTeamView(msg)
		case promptJoinTeamView:
			return m.updatePromptJoinTeamView(msg)
		}
	}
	return m, nil
}

func (m model) View() string {
	var s string
	// The quit confirmation overrides any other view
	if m.confirmQuit {
		msg := "Are you sure you want to quit? (y/n)"
		centered := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(msg)
		verticalPad := max((m.height - 1) / 2, 0)
		return strings.Repeat("\n", verticalPad) + centered
	}

	switch m.state {
	case authView:
		s = m.renderAuthView()
	case menuView:
		s = m.renderMenuView()
	case challengeView:
		s = m.renderChallengeView()
	case challengeDetailView:
		s = m.renderChallengeDetailView()
	case scoreboardView:
		s = m.renderScoreboardView()
	case teamView:
		s = m.renderTeamView()
	case genericInputView:
		s = m.renderGenericInputView()
	case flagResultView:
		s = m.renderFlagResultView()
	case confirmDeleteTeamView:
		msg := m.renderConfirmDeleteTeamView()
		centered := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(msg)
		verticalPad := max((m.height - 1) / 2, 0)
		return strings.Repeat("\n", verticalPad) + centered
	case promptJoinTeamView:
		s = m.renderPromptJoinTeamView()
	default:
		s = "Unknown view state."
	}

	// Always horizontally center the window based on current m.width
	window := windowStyle.Width(m.width / 2).MaxWidth(m.width - 4).Render(s)
	windowLines := strings.Split(window, "\n")
	maxLineWidth := 0
	for _, line := range windowLines {
		w := lipgloss.Width(line)
		if w > maxLineWidth {
			maxLineWidth = w
		}
	}
	leftPad := max((m.width - maxLineWidth) / 2, 0)
	padStr := strings.Repeat(" ", leftPad)
	for i, line := range windowLines {
		windowLines[i] = padStr + line
	}
	window = strings.Join(windowLines, "\n")
	windowHeight := lipgloss.Height(window)
	if windowHeight < m.height {
		verticalPad := max((m.height - windowHeight) / 2, 0)
		return strings.Repeat("\n", verticalPad) + window
	}
	return window
}

func (m model) updateAuthView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Enter):
		username := m.usernameInput.Value()
		if username == "" {
			m.message = "Username cannot be empty."
			m.messageType = "error"
			return m, nil
		}

		// Check if username is taken
		_, err := getUserByUsername(username)
		if err == nil {
			m.message = "Username '" + username + "' is already taken. Please choose another."
			m.messageType = "error"
			m.usernameInput.SetValue("")
			return m, nil
		}

		// Create the user
		newUser, err := createUser(username, m.sshKey)
		if err != nil {
			m.message = "Failed to create user: " + err.Error()
			m.messageType = "error"
			return m, nil
		}

		log.Printf("New user '%s' created and authenticated.", newUser.Username)
		m.user = newUser
		m.finishInitialization() // Load challenges and other data
		m.message = ""
		m.messageType = ""
		// If joinPrompt is set, prompt to join team
		if m.joinPrompt.state == promptJoinTeam && m.joinPrompt.team != nil {
			m.state = promptJoinTeamView
			return m, nil
		}
		m.state = menuView
		return m, nil
	}

	m.usernameInput, cmd = m.usernameInput.Update(msg)
	return m, cmd
}

func (m model) updateMenuView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.confirmQuit = true
		return m, nil
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Up):
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case key.Matches(msg, keys.Down):
		if m.menuCursor < 2 {
			m.menuCursor++
		}
	case key.Matches(msg, keys.Select):
		switch m.menuCursor {
		case 0:
			m.state = challengeView
			m.cursor = 0
		case 1:
			m.state = scoreboardView
			scoreboard, _ := getScoreboard()
			m.scoreboard = scoreboard
		case 2:
			m.state = teamView
			m.teamMenuCursor = 0
			m.message = ""
		}
	}
	return m, nil
}

func (m *model) buildChallengeRenderList() []any {
	var items []any
	categoryMap := make(map[string][]Challenge)
	solvedByCategory := make(map[string]int)

	for _, ch := range m.challenges {
		categoryMap[ch.Category] = append(categoryMap[ch.Category], ch)
		if ch.Solved {
			solvedByCategory[ch.Category]++
		}
	}

	for _, category := range m.categories {
		items = append(items, categoryListItem{
			name:       category,
			total:      len(categoryMap[category]),
			solved:     solvedByCategory[category],
			isExpanded: m.expandedCats[category],
		})

		if m.expandedCats[category] {
			for _, ch := range categoryMap[category] {
				items = append(items, ch)
			}
		}
	}
	return items
}

func (m model) updateChallengeView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	renderList := m.buildChallengeRenderList()

	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, keys.Down):
		if m.cursor < len(renderList)-1 {
			m.cursor++
		}
	case key.Matches(msg, keys.Select):
		if len(renderList) == 0 {
			break
		}
		selectedItem := renderList[m.cursor]
		if cat, ok := selectedItem.(categoryListItem); ok {
			m.expandedCats[cat.name] = !m.expandedCats[cat.name]
		} else if chal, ok := selectedItem.(Challenge); ok {
			m.selectedChal = chal
			m.state = challengeDetailView
		}
	}

	// Clamp cursor after expanding/collapsing
	renderList = m.buildChallengeRenderList()
	if m.cursor >= len(renderList) {
		m.cursor = len(renderList) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	return m, nil
}

func (m model) updateChallengeDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		// Re-calculate solved status in case a flag was just submitted
		solvedMap, _ := getChallengesSolvedByUser(m.user.ID)
		for i := range m.challenges {
			if solvedMap[m.challenges[i].ID] {
				m.challenges[i].Solved = true
			}
		}
		m.state = challengeView
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Select):
		if !m.selectedChal.Solved {
			m.state = genericInputView
			m.onBackState = challengeDetailView
			m.inputTitle = fmt.Sprintf("Submit Flag - %s", m.selectedChal.Title)
			m.inputModel = &m.flagInput
			m.inputModel.Focus()
			m.message = ""
			m.onSubmit = func(flag string) (string, string) {
				if flag == "" {
					return "", ""
				}
				correct, err := submitFlag(m.user.ID, m.selectedChal.ID, flag)
				if err != nil {
					return err.Error(), "error"
				}
				if correct {
					m.selectedChal.Solved = true
					// Also update the challenge in the main list
					for i := range m.challenges {
						if m.challenges[i].ID == m.selectedChal.ID {
							m.challenges[i].Solved = true
							break
						}
					}
					// Refresh all challenge and solver state
					m.finishInitialization()
					m.selectedChal = Challenge{} // clear selectedChal to force detail view to reload
					return "Correct! Flag accepted.", "success"
				}
				return "Incorrect flag. Try again.", "error"
			}
		}
	}
	return m, nil
}

func (m model) updateScoreboardView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scoreboardSearchMode {
		switch msg.Type {
		case tea.KeyRunes:
			m.scoreboardSearch += msg.String()
			return m, nil
		case tea.KeyBackspace:
			if len(m.scoreboardSearch) > 0 {
				m.scoreboardSearch = m.scoreboardSearch[:len(m.scoreboardSearch)-1]
			}
			return m, nil
		case tea.KeyEsc, tea.KeyEnter:
			m.scoreboardSearchMode = false
			m.scoreboardSearch = ""
			m.scoreboardCursor = 0
			return m, nil
		case tea.KeyUp:
			if m.scoreboardCursor > 0 {
				m.scoreboardCursor--
			}
			return m, nil
		case tea.KeyDown:
			filtered := m.filteredScoreboard()
			if m.scoreboardCursor < len(filtered)-1 {
				m.scoreboardCursor++
			}
			return m, nil
		}
		return m, nil
	}
	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		m.scoreboardSearchMode = true
		m.scoreboardSearch = ""
		m.scoreboardCursor = 0
		return m, nil
	case key.Matches(msg, keys.Up):
		if m.scoreboardCursor > 0 {
			m.scoreboardCursor--
		}
		return m, nil
	case key.Matches(msg, keys.Down):
		filtered := m.filteredScoreboard()
		if m.scoreboardCursor < len(filtered)-1 {
			m.scoreboardCursor++
		}
		return m, nil
	}
	return m, nil
}

// Helper: number of visible scoreboard rows
func (m model) scoreboardRows() int {
	// Always show up to min(20, m.height-13) teams
	maxRows := m.height - 13
	if maxRows > 20 {
		return 20
	}
	if maxRows < 1 {
		return 1
	}
	return maxRows
}

// Helper: filtered scoreboard
func (m model) filteredScoreboard() []Team {
	if m.scoreboardSearch == "" {
		return m.scoreboard
	}
	var filtered []Team
	for _, t := range m.scoreboard {
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(m.scoreboardSearch)) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (m model) updateTeamView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
		m.message = ""
		return m, nil
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, keys.Up):
		if m.teamMenuCursor > 0 {
			m.teamMenuCursor--
		}
		return m, nil
	case key.Matches(msg, keys.Down):
		if m.user.TeamID != nil && m.teamMenuCursor < 1 {
			m.teamMenuCursor++
		} else if m.user.TeamID == nil && m.teamMenuCursor < 0 {
			m.teamMenuCursor++
		}
		return m, nil
	}

	// If user is already on a team, the only option is to leave or regenerate join code
	if m.user.TeamID != nil {
		switch {
		case key.Matches(msg, keys.Select):
			if m.teamMenuCursor == 0 {
				// Leave team logic (as before)
				count, err := countTeamMembers(*m.user.TeamID)
				if err != nil {
					m.message = "Error checking team members: " + err.Error()
					m.messageType = "error"
					return m, nil
				}
				if count == 1 {
					m.state = confirmDeleteTeamView
					return m, nil
				} else {
					err := leaveTeam(m.user.ID)
					if err != nil {
						m.message = "Error leaving team: " + err.Error()
						m.messageType = "error"
					} else {
						m.user.TeamID = nil
						m.message = "You have left the team."
						m.messageType = "success"
					}
					return m, nil
				}
			} else if m.teamMenuCursor == 1 {
				// Regenerate join code
				if m.user.TeamID != nil {
					newCode, err := regenerateTeamJoinCode(*m.user.TeamID)
					if err != nil {
						m.message = "Error regenerating join code: " + err.Error()
						m.messageType = "error"
					} else {
						m.teamJoinCode = newCode
						m.finishInitialization() // refresh join code and view
						m.message = "Join code regenerated!"
						m.messageType = "success"
					}
				}
				return m, nil
			}
		}
		return m, nil
	}

	// User is not on a team, handle create/join
	switch {
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Up):
		if m.teamMenuCursor > 0 {
			m.teamMenuCursor--
		}
	case key.Matches(msg, keys.Down):
		if m.teamMenuCursor < 0 {
			m.teamMenuCursor++
		}
	case key.Matches(msg, keys.Select):
		m.state = genericInputView
		m.onBackState = teamView
		m.inputModel = &m.teamInput
		m.inputModel.Focus()
		m.message = ""

		if m.teamMenuCursor == 0 { // Create Team
			m.inputTitle = "Create Team"
			m.onSubmit = func(name string) (string, string) {
				if name == "" {
					return "", ""
				}
				team, err := createAndJoinTeam(m.user.ID, name)
				if err != nil {
					return "Team creation failed: " + err.Error(), "error"
				}
				m.user.TeamID = &team.ID
				return "Team '" + name + "' created and joined!", "success"
			}
		}
	}
	return m, nil
}

func (m model) updateGenericInputView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Cancel):
		m.state = m.onBackState
		m.inputModel.Blur()
	case key.Matches(msg, keys.Enter):
		val := m.inputModel.Value()
		msg, msgType := m.onSubmit(val)
		m.message = msg
		m.messageType = msgType
		if msgType == "success" {
			// If we just created a team, go to teamView and refresh join code
			if m.inputTitle == "Create Team" {
				m.finishInitialization()
				m.state = teamView
				m.teamMenuCursor = 0
				m.inputModel.Blur()
				return m, nil
			}
			// On other success, go back to previous screen to see result
			m.state = m.onBackState
			m.inputModel.Blur()
		}
		m.inputModel.SetValue("")
	}
	*m.inputModel, cmd = m.inputModel.Update(msg)
	return m, cmd
}

func (m model) updateFlagResultView(_ tea.KeyMsg) (tea.Model, tea.Cmd) {
	// On any key, return to challenge list
	// Refresh teamSolvers if on a team
	if m.user != nil && m.user.TeamID != nil {
		solvers, _ := getTeamChallengeSolvers(*m.user.TeamID)
		m.teamSolvers = solvers
	}
	m.state = challengeView
	m.message = ""
	m.messageType = ""
	return m, nil
}

func (m model) updateConfirmDeleteTeamView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.Quit) || key.Matches(msg, keys.Back) {
		m.state = teamView
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		// Delete team and leave
		if m.user.TeamID != nil {
			teamID := *m.user.TeamID
			err := leaveTeam(m.user.ID)
			if err != nil {
				m.message = "Error leaving team: " + err.Error()
				m.messageType = "error"
				m.state = teamView
				return m, nil
			}
			err = deleteTeam(teamID)
			if err != nil {
				m.message = "Error deleting team: " + err.Error()
				m.messageType = "error"
				m.state = teamView
				return m, nil
			}
			m.user.TeamID = nil
			m.message = "You have left and deleted the team."
			m.messageType = "success"
			m.state = teamView
			return m, nil
		}
	case "n", "N":
		// Cancel
		m.state = teamView
		return m, nil
	}
	return m, nil
}

func (m model) updatePromptJoinTeamView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.user != nil && m.joinPrompt.team != nil {
			_, err := joinTeam(m.user.ID, m.joinPrompt.team.Name)
			if err != nil {
				m.message = "Failed to join team: " + err.Error()
				m.messageType = "error"
				m.state = menuView
				return m, nil
			}
			m.user.TeamID = &m.joinPrompt.team.ID
			m.finishInitialization()
			m.message = "Joined team '" + m.joinPrompt.team.Name + "'!"
			m.messageType = "success"
		}
		m.state = menuView
		return m, nil
	case "n", "N":
		m.state = menuView
		return m, nil
	}
	return m, nil
}

// sftpHandler implements a readonly SFTP handler for the chals/ directory
// (based on the provided example)
type sftpHandler struct {
	root string
}

var (
	_ sftp.FileLister = &sftpHandler{}
	_ sftp.FileReader = &sftpHandler{}
)

type listerAt []fs.FileInfo

func (l listerAt) ListAt(ls []fs.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

func (s *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, err := os.Open(filepath.Join(s.root, r.Filepath))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		entries, err := os.ReadDir(filepath.Join(s.root, r.Filepath))
		if err != nil {
			return nil, fmt.Errorf("sftp: %w", err)
		}
		infos := make([]fs.FileInfo, len(entries))
		for i, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			infos[i] = info
		}
		return listerAt(infos), nil
	case "Stat":
		fi, err := os.Stat(filepath.Join(s.root, r.Filepath))
		if err != nil {
			return nil, err
		}
		return listerAt{fi}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

func sftpSubsystem(root string) ssh.SubsystemHandler {
	return func(s ssh.Session) {
		fs := &sftpHandler{root}
		srv := sftp.NewRequestServer(s, sftp.Handlers{
			FileList: fs,
			FileGet:  fs,
		})
		if err := srv.Serve(); err == io.EOF {
			_ = srv.Close()
		} else if err != nil {
			wish.Fatalln(s, "sftp:", err)
		}
	}
}

// teaHandler is responsible for the entire lifecycle of a user session,
// including authentication, user creation, and initializing the TUI.
func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
	pty, _, active := s.Pty()
	if !active {
		wish.Fatalln(s, "No PTY requested.")
		return nil, nil
	}

	sshKeyBytes := s.PublicKey().Marshal()
	sshKeyStr := string(sshKeyBytes)
	sshUser := s.User()
	var joinPrompt joinPromptInfo
	team, err := getTeamByJoinCode(sshUser)
	if err == nil {
		joinPrompt = joinPromptInfo{team: team, state: promptJoinTeam}
	} else {
		joinPrompt = joinPromptInfo{team: nil, state: noJoinPrompt}
	}

	// 1. Check if a user exists with the provided public key.
	user, err := getUserBySSHKey(sshKeyStr)
	if err == nil {
		// User found with this key. Log them in.
		log.Printf("User '%s' authenticated via public key.", user.Username)
		m := initialModel(user)
		m.width = pty.Window.Width
		m.height = pty.Window.Height
		// If user is not on a team and joinPrompt is set, prompt to join
		if user.TeamID == nil && joinPrompt.state == promptJoinTeam && joinPrompt.team != nil {
			m.joinPrompt = joinPrompt
			m.state = promptJoinTeamView
		}
		return m, []tea.ProgramOption{tea.WithAltScreen()}
	}

	// 2. If key not found, start the registration flow.
	log.Printf("New public key detected. Starting registration flow.")
	m := newRegistrationModel(sshKeyStr, joinPrompt)
	m.width = pty.Window.Width
	m.height = pty.Window.Height
	return m, []tea.ProgramOption{tea.WithAltScreen()}
}

func main() {
	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	challenges, _ := getChallenges()
	if err := PrepareChallengeFS(challenges, downloadRoot); err != nil {
		log.Fatal("Failed to prepare challenge FS: ", err)
	}

	root := downloadRoot
	handler := scp.NewFileSystemHandler(root)

	hostKeyPath := "host_key"
	if _, err := os.Stat(hostKeyPath); os.IsNotExist(err) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatal("Failed to generate host key:", err)
		}
		keyBytes := x509.MarshalPKCS1PrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes})
		if err := os.WriteFile(hostKeyPath, keyPEM, 0600); err != nil {
			log.Fatal("Failed to write host key:", err)
		}
		log.Println("Generated new host key.")
	}

	s, err := wish.NewServer(
		wish.WithAddress(":2223"),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}),
		wish.WithSubsystem("sftp", sftpSubsystem(root)),
		wish.WithMiddleware(
			scp.Middleware(handler, handler),
			bubbletea.Middleware(teaHandler),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Fatal("Could not create server:", err)
	}
	log.Println("Starting CTF SSH server on :2223")
	log.Fatal(s.ListenAndServe())
}
