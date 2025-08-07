package db

import (
	"fmt"
	"math/rand/v2"
)

type Team struct {
	ID          int
	Name        string
	Score       int
	PlayerCount int
	JoinCode    string
}

func GetTeamNameAndCode(teamID int) (string, string, error) {
	var name, code string
	err := db.QueryRow("SELECT name, join_code FROM teams WHERE id = ?", teamID).Scan(&name, &code)
	return name, code, err
}

func GetTeamByJoinCode(code string) (*Team, error) {
	team := &Team{}
	err := db.QueryRow("SELECT id, name, score, join_code FROM teams WHERE join_code = ?", code).
		Scan(&team.ID, &team.Name, &team.Score, &team.JoinCode)
	if err != nil {
		return nil, err
	}
	return team, nil
}

// Generate a random 8-letter lowercase join code
func GenerateJoinCode() string {
	letters := []rune("abcdefghjkmnpqrstuvwxyz")
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[rand.IntN(len(letters))]
	}
	return string(b)
}

func RegenerateTeamJoinCode(teamID int) (string, error) {
	newCode := GenerateJoinCode()
	_, err := db.Exec("UPDATE teams SET join_code = ? WHERE id = ?", newCode, teamID)
	return newCode, err
}

func CreateAndJoinTeam(creatorID int, teamName string) (*Team, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // Rollback on error

	joinCode := GenerateJoinCode()
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

func JoinTeam(userID int, teamName string) (int, error) {
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

func LeaveTeam(userID int) error {
	_, err := db.Exec("UPDATE users SET team_id = NULL WHERE id = ?", userID)
	return err
}

func GetTeamName(teamID int) (string, error) {
	name, _, err := GetTeamNameAndCode(teamID)
	return name, err
}

// Returns all users on a team
func GetTeamMembers(teamID int) ([]User, error) {
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

// Returns the number of members in a team
func CountTeamMembers(teamID int) (int, error) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE team_id = ?", teamID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// Deletes a team by ID
func DeleteTeam(teamID int) error {
	_, err := db.Exec("DELETE FROM teams WHERE id = ?", teamID)
	return err
}

// Generate test teams for scoreboard testing
func GenerateTestTeams(n int) error {
	nameRunes := []rune("abcdefghjkmnpqrstuvwxyz")
	for range n {
		// Generate random team name (6-10 chars)
		nameLen := rand.IntN(5) + 6
		nameRunesSlice := make([]rune, nameLen)
		for j := range nameRunesSlice {
			nameRunesSlice[j] = nameRunes[rand.IntN(len(nameRunes))]
		}
		teamName := string(nameRunesSlice)
		team, err := CreateAndJoinTeam(-1, teamName) // -1: we'll update users below
		if err != nil {
			continue // skip duplicates
		}
		// Add 1-5 users to the team
		userCount := rand.IntN(5) + 1
		for u := range userCount {
			uname := fmt.Sprintf("%s_user%d", teamName, u+1)
			sshKey := fmt.Sprintf("testkey_%s_%d", teamName, u+1)
			user, err := CreateUser(uname, sshKey)
			if err != nil {
				continue
			}
			// Assign user to team
			db.Exec("UPDATE users SET team_id = ? WHERE id = ?", team.ID, user.ID)
		}
	}
	return nil
}
