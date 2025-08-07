package db

import (
	"database/sql"
	"fmt"

	"ctfsh/internal/config"
)

var db *sql.DB

func Init() error {
	var err error
	db, err = sql.Open("sqlite3", config.DBPath)
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
		name TEXT UNIQUE NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		category TEXT NOT NULL,
		points INTEGER DEFAULT 0,
		flag TEXT NOT NULL,
		author TEXT NOT NULL,
		build_dir TEXT
	);

	CREATE TABLE IF NOT EXISTS challenge_downloads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL,
		challenge_id INTEGER NOT NULL,
		FOREIGN KEY(challenge_id) REFERENCES challenges(id)
	);

	CREATE TABLE IF NOT EXISTS challenge_ports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		port INTEGER NOT NULL,
		challenge_id INTEGER NOT NULL,
		FOREIGN KEY(challenge_id) REFERENCES challenges(id)
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

	LoadChallenges()

	return nil
}

func Close() {
	if db != nil {
		err := db.Close()
		if err != nil {
			fmt.Println("Error closing database:", err)
		}
		db = nil
	}
}
