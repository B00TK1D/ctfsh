package ui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textinput"

	"ctfsh/internal/db"
)

type sessionState int

const (
	authView sessionState = iota
	menuView
	challengeView
	challengeDetailView
	scoreboardView
	teamView
	teamMembersView
	genericInputView
	flagResultView
	confirmDeleteTeamView
	promptJoinTeamView
)

type joinPromptState int

const (
	noJoinPrompt joinPromptState = iota
	promptJoinTeam
	promptAlreadyOnTeam
)

type joinPromptInfo struct {
	team  *db.Team
	state joinPromptState
}

// categoryListItem represents a category in the challenge list
type categoryListItem struct {
	name       string
	total      int
	solved     int
	isExpanded bool
}

// Main model that coordinates all views
type model struct {
	// User data
	user   *db.User
	sshKey string // For registration flow

	// Session state
	state  sessionState
	width  int
	height int

	// Global UI state
	message     string
	messageType string
	help        help.Model
	showHelp    bool
	confirmQuit bool
	inputFocus  bool

	// Registration flow
	usernameInput textinput.Model
	joinPrompt    joinPromptInfo

	// Menu state
	menuCursor int

	// Generic input state
	inputTitle  string
	inputModel  *textinput.Model
	onSubmit    func(string) (string, string) // input -> (message, messageType)
	onBackState sessionState

	// View-specific models
	challenges  *challengeModel
	scoreboard  *scoreboardModel
	team        *teamModel
	teamMembers *teamMembersModel
}

// Initialize a new model for authenticated users
func initialModel(user *db.User) model {
	m := model{
		user:  user,
		state: menuView,
		help:  help.New(),
	}
	m.finishInitialization()
	return m
}

// Initialize a new model for registration flow
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
	// Initialize view-specific models
	m.challenges = newChallengeModel(m.user)
	m.scoreboard = newScoreboardModel()
	m.team = newTeamModel(m.user)
	m.teamMembers = newTeamMembersModel(m.user)
}
