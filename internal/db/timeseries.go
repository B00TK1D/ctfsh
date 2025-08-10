package db

import (
	"time"
)

// ScorePoint represents a cumulative score value at a specific time.
type ScorePoint struct {
	Time  time.Time
	Score int
}

// GetTeamScoreTimeSeries returns a cumulative score time series for a team.
// The scoring matches the scoreboard logic that counts a challenge once per user.
func GetTeamScoreTimeSeries(teamID int) ([]ScorePoint, error) {
	// Fetch all correct submissions for users on the team, in timestamp order,
	// including challenge points and the user and challenge identifiers so we can
	// de-duplicate repeats by the same user on the same challenge.
	rows, err := db.Query(`
		SELECT s.timestamp, c.points, s.user_id, s.challenge_id
		FROM submissions s
		JOIN users u ON s.user_id = u.id
		JOIN challenges c ON s.challenge_id = c.id
		WHERE s.correct = 1 AND u.team_id = ?
		ORDER BY s.timestamp ASC
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[[2]int]bool) // key: [user_id, challenge_id]
	cumulative := 0
	var series []ScorePoint
	for rows.Next() {
		var ts time.Time
		var points, userID, challengeID int
		if err := rows.Scan(&ts, &points, &userID, &challengeID); err != nil {
			return nil, err
		}
		key := [2]int{userID, challengeID}
		if seen[key] {
			continue
		}
		seen[key] = true
		cumulative += points
		series = append(series, ScorePoint{Time: ts, Score: cumulative})
	}
	return series, nil
}

// GetUserScoreTimeSeries returns a cumulative score time series for a solo user.
// The scoring matches the scoreboard logic that counts a challenge once per user.
func GetUserScoreTimeSeries(userID int) ([]ScorePoint, error) {
	rows, err := db.Query(`
		SELECT s.timestamp, c.points, s.challenge_id
		FROM submissions s
		JOIN challenges c ON s.challenge_id = c.id
		WHERE s.correct = 1 AND s.user_id = ?
		ORDER BY s.timestamp ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seenChallenges := make(map[int]bool)
	cumulative := 0
	var series []ScorePoint
	for rows.Next() {
		var ts time.Time
		var points, challengeID int
		if err := rows.Scan(&ts, &points, &challengeID); err != nil {
			return nil, err
		}
		if seenChallenges[challengeID] {
			continue
		}
		seenChallenges[challengeID] = true
		cumulative += points
		series = append(series, ScorePoint{Time: ts, Score: cumulative})
	}
	return series, nil
}
