package db

import (
	"fmt"
	"strings"
	"time"
)

type Submission struct {
	ID          int
	UserID      int
	ChallengeID int
	Flag        string
	Correct     bool
	Timestamp   time.Time
}

func SubmitFlag(userID, challengeID int, flag string) (bool, error) {
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

// Returns a map of challenge_id to username for the first solver on the team
func GetTeamChallengeSolvers(teamID int) (map[int]string, error) {
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
