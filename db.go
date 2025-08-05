// db.go
// Package db contains all database models and functions for the CTFsh application.
package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/exp/maps"
	"gopkg.in/yaml.v3"
)

var db *sql.DB

// User represents a player in the system.
type User struct {
	ID       int
	Username string
	SSHKey   string
	TeamID   *int
}

// Team represents a team in the system.
type Team struct {
	ID          int
	Name        string
	Score       int
	PlayerCount int
	JoinCode    string
}

// Challenge represents a CTF challenge.
type Challenge struct {
	ID          int
	Name        string
	Description string
	Category    string
	Points      int
	Flag        string
	Solved      bool
	Author      string
	Downloads   []string
	Ports       []int
	BuildDir    string
}

// Submission represents a flag submission.
type Submission struct {
	ID          int
	UserID      int
	ChallengeID int
	Flag        string
	Correct     bool
	Timestamp   time.Time
}

var challenges map[string]Challenge

// --- DB functions ---

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
		score INTEGER DEFAULT 0,
 		join_code TEXT UNIQUE NOT NULL
	);

	CREATE TABLE IF NOT EXISTS challenges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		category TEXT NOT NULL
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

	return nil
}

// Generate a random 8-letter lowercase join code
func generateJoinCode() string {
	letters := []rune("abcdefghjkmnpqrstuvwxyz")
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[randInt(len(letters))]
	}
	return string(b)
}

// Helper for random int
func randInt(n int) int {
	b := make([]byte, 1)
	_, err := rand.Read(b)
	if err != nil {
		return 0
	}
	return int(b[0]) % n
}

func getTeamNameAndCode(teamID int) (string, string, error) {
	var name, code string
	err := db.QueryRow("SELECT name, join_code FROM teams WHERE id = ?", teamID).Scan(&name, &code)
	return name, code, err
}

func getTeamByJoinCode(code string) (*Team, error) {
	team := &Team{}
	err := db.QueryRow("SELECT id, name, score, join_code FROM teams WHERE join_code = ?", code).
		Scan(&team.ID, &team.Name, &team.Score, &team.JoinCode)
	if err != nil {
		return nil, err
	}
	return team, nil
}

func regenerateTeamJoinCode(teamID int) (string, error) {
	newCode := generateJoinCode()
	_, err := db.Exec("UPDATE teams SET join_code = ? WHERE id = ?", newCode, teamID)
	return newCode, err
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

// getChallenges loads all challenge YAML files from ./chals/*/ctfsh.yml or .yaml
func getChallenges() map[string]Challenge {
	if challenges != nil {
		return maps.Clone(challenges)
	}
	challenges = make(map[string]Challenge)
	var idCounter int
	err := filepath.WalkDir("chals", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name == "ctfsh.yml" || name == "ctfsh.yaml" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var yml struct {
				Challenge struct {
					Name        string   `yaml:"name"`
					Author      string   `yaml:"author"`
					Category    string   `yaml:"category"`
					Description string   `yaml:"description"`
					Flag        string   `yaml:"flag"`
					Points      int      `yaml:"points"`
					Downloads   []string `yaml:"downloads"`
					Instance    struct {
						Build string `yaml:"build"`
						Ports []int  `yaml:"ports"`
					} `yaml:"instance"`
				} `yaml:"challenge"`
			}
			if err := yaml.Unmarshal(data, &yml); err != nil {
				return err
			}
			idCounter++
			ch := Challenge{
				ID:          idCounter,
				Name:        yml.Challenge.Name,
				Description: yml.Challenge.Description,
				Category:    yml.Challenge.Category,
				Points:      yml.Challenge.Points,
				Flag:        yml.Challenge.Flag,
				Author:      yml.Challenge.Author,
				Downloads:   yml.Challenge.Downloads,
				Ports:       yml.Challenge.Instance.Ports,
				BuildDir:    yml.Challenge.Instance.Build,
			}
			challenges[strings.ToLower(ch.Name)] = ch
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return maps.Clone(challenges)
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
	// Get teams and their scores
	rows, err := db.Query(`
		SELECT t.id, t.name, COALESCE(SUM(c.points), 0) as score, COUNT(u.id) as player_count
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
		if err := rows.Scan(&team.ID, &team.Name, &team.Score, &team.PlayerCount); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}

	// Add solo users (users with no team) as their own 'team'
	userRows, err := db.Query(`
		SELECT u.id, u.username, COALESCE(SUM(c.points), 0) as score
		FROM users u
		LEFT JOIN (
			SELECT s.user_id, s.challenge_id
			FROM submissions s
			WHERE s.correct = 1
			GROUP BY s.user_id, s.challenge_id
		) as solved_challs ON u.id = solved_challs.user_id
		LEFT JOIN challenges c ON solved_challs.challenge_id = c.id
		WHERE u.team_id IS NULL
		GROUP BY u.id, u.username
		ORDER BY score DESC, u.username ASC
	`)
	if err != nil {
		return nil, err
	}
	defer userRows.Close()

	for userRows.Next() {
		var id int
		var username string
		var score int
		if err := userRows.Scan(&id, &username, &score); err != nil {
			return nil, err
		}
		teams = append(teams, Team{ID: -id, Name: username, Score: score, PlayerCount: 1}) // negative ID to distinguish solo
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

	joinCode := generateJoinCode()
	res, err := tx.Exec("INSERT INTO teams (name, join_code) VALUES (?, ?)", teamName, joinCode)
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
	name, _, err := getTeamNameAndCode(teamID)
	return name, err
}

// Returns all users on a team
func getTeamMembers(teamID int) ([]User, error) {
	rows, err := db.Query("SELECT id, username, ssh_key, team_id FROM users WHERE team_id = ?", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.SSHKey, &u.TeamID); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// Returns a map of challenge_id to username for the first solver on the team
func getTeamChallengeSolvers(teamID int) (map[int]string, error) {
	query := `
	SELECT s.challenge_id, u.username
	FROM submissions s
	JOIN users u ON s.user_id = u.id
	WHERE u.team_id = ? AND s.correct = 1
	GROUP BY s.challenge_id
	ORDER BY s.timestamp ASC
	`
	rows, err := db.Query(query, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	solvers := make(map[int]string)
	for rows.Next() {
		var challengeID int
		var username string
		if err := rows.Scan(&challengeID, &username); err != nil {
			return nil, err
		}
		solvers[challengeID] = username
	}
	return solvers, nil
}

// Returns the number of members in a team
func countTeamMembers(teamID int) (int, error) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE team_id = ?", teamID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// Deletes a team by ID
func deleteTeam(teamID int) error {
	_, err := db.Exec("DELETE FROM teams WHERE id = ?", teamID)
	return err
}

// Generate test teams for scoreboard testing
func generateTestTeams(n int) error {
	nameRunes := []rune("abcdefghjkmnpqrstuvwxyz")
	for i := 0; i < n; i++ {
		// Generate random team name (6-10 chars)
		nameLen := randInt(5) + 6
		nameRunesSlice := make([]rune, nameLen)
		for j := range nameRunesSlice {
			nameRunesSlice[j] = nameRunes[randInt(len(nameRunes))]
		}
		teamName := string(nameRunesSlice)
		team, err := createAndJoinTeam(-1, teamName) // -1: we'll update users below
		if err != nil {
			continue // skip duplicates
		}
		// Add 1-5 users to the team
		userCount := randInt(5) + 1
		for u := 0; u < userCount; u++ {
			uname := fmt.Sprintf("%s_user%d", teamName, u+1)
			sshKey := fmt.Sprintf("testkey_%s_%d", teamName, u+1)
			user, err := createUser(uname, sshKey)
			if err != nil {
				continue
			}
			// Assign user to team
			db.Exec("UPDATE users SET team_id = ? WHERE id = ?", team.ID, user.ID)
		}
	}
	return nil
}
