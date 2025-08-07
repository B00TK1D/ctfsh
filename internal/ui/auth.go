package ui

import (
	"fmt"
	"log"
	"strings"

	"ctfsh/internal/db"
)

func (m model) renderAuthView() string {
	var b strings.Builder
	b.WriteString("\n  Welcome to the CTF!\n")
	b.WriteString("  Please choose a username to register your public key.\n\n")
	b.WriteString("  " + m.usernameInput.View() + "\n\n")

	if m.message != "" {
		style := errorStyle
		b.WriteString("  " + style.Render(m.message) + "\n")
	}

	if m.showHelp {
		b.WriteString("\n" + helpStyle.Render("Enter: confirm  Ctrl+C: quit  ?: toggle help"))
	} else {
		b.WriteString("\n" + helpStyle.Render("Press '?' for help."))
	}
	return b.String()
}

func createUser(username, sshKey string) (*db.User, error) {
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}

	if sshKey == "" {
		return nil, fmt.Errorf("SSH key cannot be empty")
	}

	_, err := db.GetUserByUsername(username)
	if err == nil {
		return nil, fmt.Errorf("username '%s' is already taken", username)
	}

	// Check if the username matches a challenge name (reserved for instancing)
	_, matchesChallenge := db.GetChallenges()[username]
	if matchesChallenge {
		return nil, fmt.Errorf("username '%s' is already taken", username)
	}

	newUser, err := db.CreateUser(username, sshKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	log.Printf("New user '%s' created and authenticated.", newUser.Username)
	return newUser, nil
}

func authenticateUser(sshKey string) (*db.User, error) {
	user, err := db.GetUserBySSHKey(sshKey)
	if err != nil {
		return nil, err
	}
	log.Printf("User '%s' authenticated via public key.", user.Username)
	return user, nil
}
