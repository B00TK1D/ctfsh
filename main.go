package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db *sql.DB

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
)

// #region Structs and Enums
type User struct {
	ID       int
	Username string
	SSHKey   string
	TeamID   *int
}

type Team struct {
	ID    int
	Name  string
	Score int
}

type Challenge struct {
	ID          int
	Title       string
	Description string
	Category    string
	Points      int
	Flag        string
	Solved      bool
}

type Submission struct {
	ID          int
	UserID      int
	ChallengeID int
	Flag        string
	Correct     bool
	Timestamp   time.Time
}

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
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
		{k.Up, k.Down, k.Enter},
		{k.Back, k.Tab, k.Quit},
	}
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
	Enter:  key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter/space", "select")),
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
)

// Represents an item in the challenge list (either a category or a challenge)
type categoryListItem struct {
	name       string
	total      int
	solved     int
	isExpanded bool
}

type model struct {
	user           *User
	sshKey         string // For registration flow
	state          sessionState
	width          int
	height         int
	challenges     []Challenge
	categories     []string
	cursor         int
	menuCursor     int
	teamMenuCursor int
	scoreboard     []Team
	selectedChal   Challenge
	usernameInput  textinput.Model
	flagInput      textinput.Model
	teamInput      textinput.Model
	message        string
	messageType    string
	help           help.Model
	showHelp       bool
	expandedCats   map[string]bool
	confirmQuit    bool
	// For generic input view
	inputTitle  string
	inputModel  *textinput.Model
	onSubmit    func(string) (string, string) // input -> (message, messageType)
	onBackState sessionState
}

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./ctf.db")
	if err != nil {
		return err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		ssh_key TEXT NOT NULL UNIQUE,
		team_id INTEGER,
		FOREIGN KEY(team_id) REFERENCES teams(id)
	);

	CREATE TABLE IF NOT EXISTS teams (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		score INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS challenges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		category TEXT NOT NULL,
		points INTEGER NOT NULL,
		flag TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS submissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		challenge_id INTEGER NOT NULL,
		flag TEXT NOT NULL,
		correct BOOLEAN NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(challenge_id) REFERENCES challenges(id)
	);
	`

	_, err = db.Exec(schema)
	if err != nil {
		return err
	}

	// Insert sample challenges
	sampleChallenges := []struct {
		title, desc, category, flag string
		points                      int
	}{
		{"Easy Crypto", "Simple Caesar cipher with shift of 3", "Crypto", "CTF{hello_world}", 100},
		{"Web Basic", "Find the hidden flag in the HTML", "Web", "CTF{inspect_element}", 150},
		{"Rev Intro", "Basic reverse engineering challenge", "Reverse", "CTF{strings_command}", 200},
		{"Pwn Buffer", "Classic buffer overflow", "Pwn", "CTF{stack_smashing}", 300},
		{"Forensics 1", "Analyze the image metadata", "Forensics", "CTF{hidden_data}", 250},
		{"Crypto Hard", "RSA with small exponent", "Crypto", "CTF{small_e_attack}", 400},
		{"Web XSS", "Cross-site scripting vulnerability", "Web", "CTF{alert_box}", 350},
		{"Rev Advanced", "Anti-debugging techniques", "Reverse", "CTF{debugger_detected}", 500},
	}

	for _, ch := range sampleChallenges {
		var exists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM challenges WHERE title = ?)", ch.title).Scan(&exists)
		if err != nil || exists {
			continue
		}
		_, err = db.Exec("INSERT INTO challenges (title, description, category, points, flag) VALUES (?, ?, ?, ?, ?)",
			ch.title, ch.desc, ch.category, ch.points, ch.flag)
		if err != nil {
			log.Printf("Error inserting sample challenge %s: %v", ch.title, err)
		}
	}
	return nil
}

func getUserBySSHKey(sshKey string) (*User, error) {
	user := &User{}
	err := db.QueryRow("SELECT id, username, ssh_key, team_id FROM users WHERE ssh_key = ?", sshKey).
		Scan(&user.ID, &user.Username, &user.SSHKey, &user.TeamID)
	return user, err
}

func getUserByUsername(username string) (*User, error) {
	user := &User{}
	err := db.QueryRow("SELECT id, username, ssh_key, team_id FROM users WHERE username = ?", username).
		Scan(&user.ID, &user.Username, &user.SSHKey, &user.TeamID)
	return user, err
}

func createUser(username, sshKey string) (*User, error) {
	result, err := db.Exec("INSERT INTO users (username, ssh_key) VALUES (?, ?)", username, sshKey)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: int(id), Username: username, SSHKey: sshKey}, nil
}

func getChallenges() ([]Challenge, error) {
	rows, err := db.Query("SELECT id, title, description, category, points, flag FROM challenges ORDER BY category, points")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var challenges []Challenge
	for rows.Next() {
		var ch Challenge
		if err := rows.Scan(&ch.ID, &ch.Title, &ch.Description, &ch.Category, &ch.Points, &ch.Flag); err != nil {
			return nil, err
		}
		challenges = append(challenges, ch)
	}
	return challenges, nil
}

func getChallengesSolvedByUser(userID int) (map[int]bool, error) {
	rows, err := db.Query("SELECT DISTINCT challenge_id FROM submissions WHERE user_id = ? AND correct = 1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	solved := make(map[int]bool)
	for rows.Next() {
		var challengeID int
		if err := rows.Scan(&challengeID); err != nil {
			return nil, err
		}
		solved[challengeID] = true
	}
	return solved, nil
}

func getScoreboard() ([]Team, error) {
	rows, err := db.Query(`
		SELECT t.id, t.name, COALESCE(SUM(c.points), 0) as score
		FROM teams t
		LEFT JOIN users u ON t.id = u.team_id
		LEFT JOIN (
			SELECT s.user_id, s.challenge_id
			FROM submissions s
			WHERE s.correct = 1
			GROUP BY s.user_id, s.challenge_id
		) as solved_challs ON u.id = solved_challs.user_id
		LEFT JOIN challenges c ON solved_challs.challenge_id = c.id
		GROUP BY t.id, t.name
		ORDER BY score DESC, t.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var team Team
		if err := rows.Scan(&team.ID, &team.Name, &team.Score); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, nil
}

func submitFlag(userID, challengeID int, flag string) (bool, error) {
	var correctFlag string
	err := db.QueryRow("SELECT flag FROM challenges WHERE id = ?", challengeID).Scan(&correctFlag)
	if err != nil {
		return false, err
	}

	correct := strings.TrimSpace(flag) == strings.TrimSpace(correctFlag)

	var alreadySolved bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM submissions WHERE user_id = ? AND challenge_id = ? AND correct = 1)",
		userID, challengeID).Scan(&alreadySolved)
	if err != nil {
		return false, err
	}

	if alreadySolved {
		return false, fmt.Errorf("you have already solved this challenge")
	}

	_, err = db.Exec("INSERT INTO submissions (user_id, challenge_id, flag, correct) VALUES (?, ?, ?, ?)",
		userID, challengeID, flag, correct)

	return correct, err
}

func createAndJoinTeam(creatorID int, teamName string) (*Team, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // Rollback on error

	res, err := tx.Exec("INSERT INTO teams (name) VALUES (?)", teamName)
	if err != nil {
		return nil, fmt.Errorf("team name likely already exists")
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("UPDATE users SET team_id = ? WHERE id = ?", id, creatorID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Team{ID: int(id), Name: teamName}, nil
}

func joinTeam(userID int, teamName string) (int, error) {
	var teamID int
	err := db.QueryRow("SELECT id FROM teams WHERE name = ?", teamName).Scan(&teamID)
	if err != nil {
		return 0, fmt.Errorf("team not found")
	}

	_, err = db.Exec("UPDATE users SET team_id = ? WHERE id = ?", teamID, userID)
	if err != nil {
		return 0, err
	}
	return teamID, nil
}

func leaveTeam(userID int) error {
	_, err := db.Exec("UPDATE users SET team_id = NULL WHERE id = ?", userID)
	return err
}

func getTeamName(teamID int) (string, error) {
	var name string
	err := db.QueryRow("SELECT name FROM teams WHERE id = ?", teamID).Scan(&name)
	return name, err
}

// initialModel is for users who are already authenticated.
func initialModel(user *User) model {
	m := model{user: user, state: menuView}
	m.finishInitialization()

	return m
}

// newRegistrationModel is for new users who need to pick a username.
func newRegistrationModel(sshKey string) model {
	unInput := textinput.New()
	unInput.Focus()
	unInput.CharLimit = 32

	return model{
		sshKey:        sshKey,
		state:         authView,
		usernameInput: unInput,
		help:          help.New(),
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
		}
	}
	return m, nil
}

func (m model) View() string {
	var s string
	// The quit confirmation overrides any other view
	if m.confirmQuit {
		return "\n  Are you sure you want to quit? (y/n)\n"
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
	default:
		s = "Unknown view state."
	}

	// Don't show help during auth
	if m.state == authView {
		return s
	}

	helpView := ""
	if m.showHelp {
		helpView = "\n" + helpStyle.Render(m.help.View(keys))
	} else {
		helpView = "\n" + helpStyle.Render("Press '?' for help.")
	}

	return s + helpView
}

func (m model) updateAuthView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
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
		m.state = menuView
		m.message = ""
		m.messageType = ""
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
	case key.Matches(msg, keys.Enter):
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

func (m *model) buildChallengeRenderList() []interface{} {
	var items []interface{}
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
	case key.Matches(msg, keys.Enter):
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
	case key.Matches(msg, keys.Enter):
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
					return "Correct! Flag accepted.", "success"
				}
				return "Incorrect flag. Try again.", "error"
			}
		}
	}
	return m, nil
}

func (m model) updateScoreboardView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
	case key.Matches(msg, keys.Help):
		m.showHelp = !m.showHelp
	}
	return m, nil
}

func (m model) updateTeamView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.state = menuView
		m.message = ""
		return m, nil
	}

	// If user is already on a team, the only option is to leave
	if m.user.TeamID != nil {
		switch {
		case key.Matches(msg, keys.Enter):
			err := leaveTeam(m.user.ID)
			if err != nil {
				m.message = "Error leaving team: " + err.Error()
				m.messageType = "error"
			} else {
				m.user.TeamID = nil
				m.message = "You have left the team."
				m.messageType = "success"
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
		if m.teamMenuCursor < 1 {
			m.teamMenuCursor++
		}
	case key.Matches(msg, keys.Enter):
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
		} else { // Join Team
			m.inputTitle = "Join Team"
			m.onSubmit = func(name string) (string, string) {
				if name == "" {
					return "", ""
				}
				teamID, err := joinTeam(m.user.ID, name)
				if err != nil {
					return "Failed to join team: " + err.Error(), "error"
				}
				m.user.TeamID = &teamID
				return "Successfully joined team '" + name + "'!", "success"
			}
		}
	}
	return m, nil
}

func (m model) updateGenericInputView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(msg, keys.Cancel):
		m.state = m.onBackState
		m.inputModel.Blur()
	case key.Matches(msg, keys.Enter):
		val := m.inputModel.Value()
		msg, msgType := m.onSubmit(val)
		m.message = msg
		m.messageType = msgType
		if msgType == "success" {
			// On success, go back to previous screen to see result
			m.state = m.onBackState
			m.inputModel.Blur()
		}
		m.inputModel.SetValue("")
	}
	*m.inputModel, cmd = m.inputModel.Update(msg)
	return m, cmd
}

func (m model) renderAuthView() string {
	var b strings.Builder
	b.WriteString("\n  Welcome to the CTF!\n")
	b.WriteString("  Please choose a username to register your public key.\n\n")
	b.WriteString("  " + m.usernameInput.View() + "\n\n")

	if m.message != "" {
		style := errorStyle
		b.WriteString("  " + style.Render(m.message) + "\n")
	}

	b.WriteString("\n  " + helpStyle.Render("Press Enter to confirm, Ctrl+C to quit."))
	return b.String()
}

func (m model) renderMenuView() string {
	title := titleStyle.Render("🚩 CTF Platform")

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

	return fmt.Sprintf("%s\n\n%s\n\n%s", title, userInfo, menu.String())
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
			}
			content.WriteString(fmt.Sprintf("  %s%s (%d pts)%s\n", cursor, v.Title, v.Points, status))
		}
	}

	return fmt.Sprintf("%s\n\n%s", title, content.String())
}

func (m model) renderChallengeDetailView() string {
	ch := m.selectedChal
	title := titleStyle.Render(ch.Title)

	status := "Unsolved"
	if ch.Solved {
		status = successStyle.Render("✓ SOLVED")
	}

	details := fmt.Sprintf(
		"Category: %s\nPoints: %d\nStatus: %s\n\nDescription:\n%s\n\n",
		categoryStyle.Render(ch.Category),
		ch.Points,
		status,
		ch.Description,
	)

	action := "Press Enter to submit flag"
	if ch.Solved {
		action = "You have already completed this challenge!"
	}

	return fmt.Sprintf("%s\n\n%s%s", title, details, action)
}

func (m model) renderScoreboardView() string {
	title := titleStyle.Render("Scoreboard")

	if len(m.scoreboard) == 0 {
		return title + "\n\nNo teams on the board yet!"
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("%-4s %-20s %s\n", "Rank", "Team", "Score"))
	content.WriteString(strings.Repeat("─", 35) + "\n")

	for i, team := range m.scoreboard {
		content.WriteString(fmt.Sprintf("%-4d %-20s %d\n", i+1, team.Name, team.Score))
	}

	return fmt.Sprintf("%s\n\n%s", title, content.String())
}

func (m model) renderTeamView() string {
	title := titleStyle.Render("Team Management")

	var content string
	// If user is on a team, show leave option
	if m.user.TeamID != nil {
		teamName, err := getTeamName(*m.user.TeamID)
		if err != nil {
			teamName = "Error fetching name"
		}
		content = fmt.Sprintf("Current team: %s\n\n%s",
			teamName,
			selectedStyle.Render("> ")+"Leave Team (Press Enter)")
	} else {
		// User not on a team, show create/join options
		options := []string{"Create a new Team", "Join an existing Team"}
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

	return fmt.Sprintf("%s\n\n%s%s", title, content, message)
}

func (m model) renderGenericInputView() string {
	title := titleStyle.Render(m.inputTitle)
	input := m.inputModel.View()

	message := ""
	if m.message != "" {
		style := successStyle
		if m.messageType == "error" {
			style = errorStyle
		}
		message = "\n\n" + style.Render(m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s\n\n%s%s",
		title,
		"Enter value below:",
		input,
		"Press Esc to go back.",
		message)
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

	// 1. Check if a user exists with the provided public key.
	user, err := getUserBySSHKey(sshKeyStr)
	if err == nil {
		// User found with this key. Log them in.
		log.Printf("User '%s' authenticated via public key.", user.Username)
		m := initialModel(user)
		m.width = pty.Window.Width
		m.height = pty.Window.Height
		return m, []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	}

	// 2. If key not found, start the registration flow.
	log.Printf("New public key detected. Starting registration flow.")
	m := newRegistrationModel(sshKeyStr)
	m.width = pty.Window.Width
	m.height = pty.Window.Height
	return m, []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
}

func main() {
	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

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
		// We allow all public key connections to proceed to our teaHandler,
		// where we implement the custom authentication and user creation logic.
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}),
		wish.WithMiddleware(
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
