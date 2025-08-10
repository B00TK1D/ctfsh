package ui

import (
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	_ "github.com/mattn/go-sqlite3"
	"github.com/muesli/termenv"

	"ctfsh/internal/db"
	"ctfsh/internal/instance"
)

func (m model) Quit() tea.Cmd {
	return tea.Quit
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

	case switchToDetailView:
		m.state = challengeDetailView
		return m, nil

	case submitFlagRequest:
		m.state = genericInputView
		m.onBackState = challengeDetailView
		m.inputTitle = fmt.Sprintf("Submit Flag - %s", m.challenges.selectedChal.Name)
		m.inputModel = &m.challenges.flagInput
		m.inputModel.Focus()
		m.message = ""
		m.onSubmit = func(flag string) (string, string) {
			return m.challenges.submitFlag(flag)
		}
		return m, nil

	case teamErrorMsg:
		m.message = msg.message
		m.messageType = "error"
		return m, nil

	case teamSuccessMsg:
		m.message = msg.message
		m.messageType = "success"
		return m, nil

	case confirmDeleteTeamMsg:
		m.state = confirmDeleteTeamView
		return m, nil

	case createTeamRequestMsg:
		m.state = genericInputView
		m.onBackState = teamView
		m.inputModel = &m.team.teamInput
		m.inputModel.Focus()
		m.message = ""
		m.inputTitle = "Create Team"
		m.onSubmit = func(name string) (string, string) {
			return m.team.createTeam(name)
		}
		return m, nil

	case viewTeamMembersMsg:
		m.state = teamMembersView
		m.teamMembers.loadTeamMembers() // Load team members data
		m.teamMembers.cursor = 0
		return m, nil

	case tea.KeyMsg:
		// Global quit, even if in confirmation
		if key.Matches(msg, keys.Quit) {
			return m, m.Quit()
		}

		// Handle quit confirmation if active
		if m.confirmQuit {
			switch msg.String() {
			case "y", "Y":
				return m, m.Quit()
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
		case teamMembersView:
			return m.updateTeamMembersView(msg)
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
		verticalPad := genericMax((m.height-1)/2, 0)
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
	case teamMembersView:
		s = m.renderTeamMembersView()
	case genericInputView:
		s = m.renderGenericInputView()
	case flagResultView:
		s = m.renderFlagResultView()
	case confirmDeleteTeamView:
		msg := m.renderConfirmDeleteTeamView()
		centered := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(msg)
		verticalPad := genericMax((m.height-1)/2, 0)
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
	leftPad := genericMax((m.width-maxLineWidth)/2, 0)
	padStr := strings.Repeat(" ", leftPad)
	for i, line := range windowLines {
		windowLines[i] = padStr + line
	}
	window = strings.Join(windowLines, "\n")
	windowHeight := lipgloss.Height(window)
	if windowHeight < m.height {
		verticalPad := genericMax((m.height-windowHeight)/2, 0)
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

		newUser, err := createUser(username, m.sshKey)
		if err != nil {
			m.message = "Error: " + err.Error()
			m.messageType = "error"
			m.usernameInput.SetValue("")
			return m, nil
		}

		m.user = newUser
		m.finishInitialization()
		m.message = ""
		m.messageType = ""

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
			m.challenges.cursor = 0
			m.challenges.loadSolvedStatus() // Refresh challenge solved status
		case 1:
			m.state = scoreboardView
			m.scoreboard.loadScoreboard() // Refresh scoreboard data
		case 2:
			m.state = teamView
			m.team.cursor = 0
			m.message = ""
			// Refresh team data if user is on a team
			if m.user.TeamID != nil {
				_, code, err := db.GetTeamNameAndCode(*m.user.TeamID)
				if err == nil {
					m.team.teamJoinCode = code
				}
			}
		}
	}
	return m, nil
}

func (m model) updateChallengeView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delegate to challenge model
	newModel, cmd := m.challenges.update(msg)
	if newModel != nil {
		// Handle any messages from the challenge model
		return m, cmd
	}

	// Check if we got a command that should switch to detail view
	if cmd != nil {
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	}
	return m, nil
}

func (m model) updateChallengeDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		// Re-calculate solved status in case a flag was just submitted
		m.challenges.loadSolvedStatus()
		m.state = challengeView
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	case key.Matches(msg, keys.Select):
		if !m.challenges.selectedChal.solved {
			m.state = genericInputView
			m.onBackState = challengeDetailView
			m.inputTitle = fmt.Sprintf("Submit Flag - %s", m.challenges.selectedChal.Name)
			m.inputModel = &m.challenges.flagInput
			m.inputModel.Focus()
			m.message = ""
			m.onSubmit = func(flag string) (string, string) {
				return m.challenges.submitFlag(flag)
			}
		}
	}
	return m, nil
}

func (m model) updateScoreboardView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.scoreboard.update(msg)
	m.inputFocus = m.scoreboard.searchMode

	switch {
	case key.Matches(msg, keys.Back):
		if !m.inputFocus {
			m.state = menuView
		}
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	}
	return m, nil

}

func (m model) updateTeamView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delegate to team model
	newModel, cmd := m.team.update(msg)
	if newModel != nil {
		// Handle any messages from the team model
		return m, cmd
	}

	// Check if we got a command that should be handled
	if cmd != nil {
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
		m.message = ""
		return m, nil
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	}
	return m, nil
}

func (m model) updateTeamMembersView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delegate to team members model
	newModel, cmd := m.teamMembers.update(msg)
	if newModel != nil {
		// Handle any messages from the team members model
		return m, cmd
	}

	// Check if we got a command that should be handled
	if cmd != nil {
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Back):
		m.state = teamView
		return m, nil
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
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
				m.team.cursor = 0
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
		solvers, _ := db.GetTeamChallengeSolvers(*m.user.TeamID)
		m.challenges.teamSolvers = solvers
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
			err := db.LeaveTeam(m.user.ID)
			if err != nil {
				m.message = "Error leaving team: " + err.Error()
				m.messageType = "error"
				m.state = teamView
				return m, nil
			}
			err = db.DeleteTeam(teamID)
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
			_, err := db.JoinTeam(m.user.ID, m.joinPrompt.team.Name)
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

// teaHandler is responsible for the entire lifecycle of a user session,
// including authentication, user creation, and initializing the TUI.
func TeaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
	pty, _, active := s.Pty()
	if !active {
		wish.Fatalln(s, "No PTY requested.")
		return nil, nil
	}

	lipgloss.SetColorProfile(termenv.TrueColor)

	if s.PublicKey() == nil {
		wish.Fatalln(s, "No public key provided, to use CTFsh please first run `ssh-keygen` to generate a key pair and then try reconnecting.")
		return nil, nil
	}

	sshKeyBytes := s.PublicKey().Marshal()
	sshKeyStr := string(sshKeyBytes)
	sshUser := s.User()
	var joinPrompt joinPromptInfo
	team, err := db.GetTeamByJoinCode(sshUser)
	if err == nil {
		joinPrompt = joinPromptInfo{team: team, state: promptJoinTeam}
	} else {
		joinPrompt = joinPromptInfo{team: nil, state: noJoinPrompt}
	}

	//  Check if a user exists with the provided public key.
	user, err := authenticateUser(sshKeyStr)
	if err == nil {
		// User found with this key. Log them in.
		m := initialModel(user)
		m.width = pty.Window.Width
		m.height = pty.Window.Height

		// Check if username matches challenge (for instancer)
		if user.Username != sshUser {
			chal, isChal := db.GetChallenges()[sshUser]
			if isChal {
				instance.HandleInstanceRequest(s, user, chal)
				return nil, nil
			}
		}

		// If user is not on a team and joinPrompt is set, prompt to join
		if user.TeamID == nil && joinPrompt.state == promptJoinTeam && joinPrompt.team != nil {
			m.joinPrompt = joinPrompt
			m.state = promptJoinTeamView
		}
		return m, []tea.ProgramOption{tea.WithAltScreen()}
	}

	// If key not found, start the registration flow.
	log.Printf("New public key detected. Starting registration flow.")
	m := newRegistrationModel(sshKeyStr, joinPrompt)
	m.width = pty.Window.Width
	m.height = pty.Window.Height
	return m, []tea.ProgramOption{tea.WithAltScreen()}
}
