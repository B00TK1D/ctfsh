package db

type User struct {
	ID       int
	Username string
	SSHKey   string
	TeamID   *int
}

func GetUserBySSHKey(sshKey string) (*User, error) {
	user := &User{}
	err := db.QueryRow("SELECT id, username, ssh_key, team_id FROM users WHERE ssh_key = ?", sshKey).
		Scan(&user.ID, &user.Username, &user.SSHKey, &user.TeamID)
	return user, err
}

func GetUserByUsername(username string) (*User, error) {
	user := &User{}
	err := db.QueryRow("SELECT id, username, ssh_key, team_id FROM users WHERE username = ?", username).
		Scan(&user.ID, &user.Username, &user.SSHKey, &user.TeamID)
	return user, err
}

func CreateUser(username, sshKey string) (*User, error) {
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

func GetChallengesSolvedByUser(userID int) (map[int]bool, error) {
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
