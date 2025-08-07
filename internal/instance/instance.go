package instance

import (
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"

	"ctfsh/internal/db"
	"ctfsh/internal/util"
)

type directTCPChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func HandleInstanceRequest(s ssh.Session, user *db.User, chal db.Challenge) {
	log.Printf("Loading instancer for %s", chal.Name)

	containerName := fmt.Sprintf("%s-%s", chal.Name, util.RandHex(6))
	s.Context().SetValue("containerName", containerName)
	readyChan := make(chan struct{})
	go func() {
		StartChallenge(chal.Name, containerName)
		close(readyChan)
	}()
	defer func() {
		go stopContainer(containerName)
	}()

	fmt.Fprintf(s, "\x1b[?25l\n   %s\n\n", chal.Name)
	fmt.Fprintf(s, "   %s\n\n", chal.Description)
	// Show loading spinner
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerIdx := 0
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()

spinner:
	for {
		select {
		case <-readyChan:
			ticker.Stop()
			break spinner
		case <-s.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(s, "\r %s %s", spinner[spinnerIdx], "Loading instance...")
			spinnerIdx = (spinnerIdx + 1) % len(spinner)
		}
	}

	fmt.Fprintf(s, "\r %s %s\n\n", "✔", "Instance ready. To connect:")
	for _, port := range chal.Ports {
		fmt.Fprintf(s, "     nc 127.0.0.1 %d        \n\r", port)
	}

	c := make([]byte, 1)
exit:
	for {
		select {
		case <-s.Context().Done():
			break exit
		default:
			_, err := s.Read(c)
			if err != nil {
				break exit
			}
			if c[0] == 3 { // Ctrl+C
				wish.Printf(s, "\n   Exiting instance...\x1b[?25h\n\n")
				break exit
			}
		}
	}

}
