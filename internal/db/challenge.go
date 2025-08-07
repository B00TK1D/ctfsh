package db

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"ctfsh/internal/config"
)

type Challenge struct {
	ID          int
	Name        string
	Title       string
	Description string
	Category    string
	Points      int
	Flag        string
	Author      string
	BuildDir    string
	Downloads   []string
	Ports       []int
}

type challengeConfig struct {
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

func LoadChallenges() {
	filepath.WalkDir(config.ChallengeDir, func(path string, d fs.DirEntry, err error) error {
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
			chalConfig := challengeConfig{}
			if err := yaml.Unmarshal(data, &chalConfig); err != nil {
				return err
			}
			if chalConfig.Challenge.Points <= 0 {
				chalConfig.Challenge.Points = config.DefaultPoints
			}

			CreateChallenge(Challenge{
				Name:        strings.ReplaceAll(strings.ToLower(strings.TrimSpace(chalConfig.Challenge.Name)), " ", "_"),
				Title:       chalConfig.Challenge.Name,
				Description: chalConfig.Challenge.Description,
				Category:    chalConfig.Challenge.Category,
				Points:      chalConfig.Challenge.Points,
				Flag:        chalConfig.Challenge.Flag,
				Author:      chalConfig.Challenge.Author,
				Downloads:   chalConfig.Challenge.Downloads,
				Ports:       chalConfig.Challenge.Instance.Ports,
				BuildDir:    chalConfig.Challenge.Instance.Build,
			})
		}
		return nil

	})
}

func CreateChallenge(chal Challenge) {
	result, err := db.Exec("INSERT INTO challenges (name, title, description, category, points, flag, author, build_dir) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		chal.Name, chal.Title, chal.Description, chal.Category, chal.Points, chal.Flag, chal.Author, chal.BuildDir)
	if err != nil {
		log.Printf("Failed to insert challenge: %v\n", err)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		log.Printf("Failed to get last insert ID: %v\n", err)
		return
	}
	chal.ID = int(id)
	if len(chal.Downloads) > 0 {
		for _, download := range chal.Downloads {
			_, err := db.Exec("INSERT INTO challenge_downloads (path, challenge_id) VALUES (?, ?)", download, chal.ID)
			if err != nil {
				log.Printf("Failed to insert challenge download: %v\n", err)
			}
		}
	}
	if len(chal.Ports) > 0 {
		for _, port := range chal.Ports {
			_, err := db.Exec("INSERT INTO challenge_ports (port, challenge_id) VALUES (?, ?)", port, chal.ID)
			if err != nil {
				log.Printf("Failed to insert challenge port: %v\n", err)
			}
		}
	}
}

func GetChallenges() map[string]Challenge {
	// Get all challenges from the database, including downloads and ports
	rows, err := db.Query("SELECT id, name, title, description, category, points, flag, author, build_dir FROM challenges")
	if err != nil {
		log.Printf("Failed to query challenges: %v\n", err)
		return nil
	}
	defer rows.Close()
	challenges := make(map[string]Challenge)
	for rows.Next() {
		var chal Challenge
		if err := rows.Scan(&chal.ID, &chal.Name, &chal.Title, &chal.Description, &chal.Category, &chal.Points, &chal.Flag, &chal.Author, &chal.BuildDir); err != nil {
			log.Printf("Failed to scan challenge: %v\n", err)
			continue
		}
		chal.Downloads = GetChallengeDownloads(chal.ID)
		chal.Ports = GetChallengePorts(chal.ID)
		challenges[chal.Name] = chal
	}
	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over challenges: %v\n", err)
		return nil
	}
	for name, chal := range challenges {
		chal.Name = strings.ToLower(strings.TrimSpace(name))
		challenges[chal.Name] = chal
	}
	return challenges
}

func GetChallengeDownloads(chalId int) []string {
	rows, err := db.Query("SELECT path FROM challenge_downloads WHERE challenge_id = ?", chalId)
	if err != nil {
		log.Printf("Failed to query challenge downloads: %v\n", err)
		return nil
	}
	defer rows.Close()
	var downloads []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			log.Printf("Failed to scan download path: %v\n", err)
			continue
		}
		downloads = append(downloads, path)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over downloads: %v\n", err)
		return nil
	}
	return downloads
}

func GetChallengePorts(chalId int) []int {
	rows, err := db.Query("SELECT port FROM challenge_ports WHERE challenge_id = ?", chalId)
	if err != nil {
		log.Printf("Failed to query challenge ports: %v\n", err)
		return nil
	}
	defer rows.Close()
	var ports []int
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			log.Printf("Failed to scan port: %v\n", err)
			continue
		}
		ports = append(ports, port)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over ports: %v\n", err)
		return nil
	}
	return ports
}

func GetChallengeCategories() []string {
	rows, err := db.Query("SELECT DISTINCT category FROM challenges ORDER BY category")
	if err != nil {
		log.Printf("Failed to query challenge categories: %v\n", err)
		return nil
	}
	defer rows.Close()

	var categories []string
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			log.Printf("Failed to scan category: %v\n", err)
			return nil
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over categories: %v\n", err)
		return nil
	}
	return categories
}
