package db

func GetScoreboard() ([]Team, error) {
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
