package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log"
	"os"
	"sync"
	"fmt"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/scp"
	_ "github.com/mattn/go-sqlite3"

	"ctfsh/internal/config"
	"ctfsh/internal/db"
	"ctfsh/internal/download"
	"ctfsh/internal/instance"
	"ctfsh/internal/ui"
)

func main() {
	log.Println("Starting CTF SSH server...")
	if err := db.InitDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	log.Println("Preparing challenge downloads...")
	challenges := db.GetChallenges()
	if err := download.PrepareChallengeFS(challenges); err != nil {
		log.Fatal("Failed to prepare challenge FS: ", err)
	}

	log.Println("Building challenge images...")
	wg := &sync.WaitGroup{}
	for _, ch := range challenges {
		if len(ch.Ports) > 0 {
			wg.Add(1)
			go func() {
				instance.CreateChallengeImage(ch.Name, config.ChallengeDir+"/"+ch.Name)
				wg.Done()
			}()
		}
	}
	wg.Wait()
	log.Println("All challenges ready.")

	handler := scp.NewFileSystemHandler(config.DownloadRoot)

	hostKeyPath := "host_key"
	if _, err := os.Stat(hostKeyPath); os.IsNotExist(err) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatal("Failed to generate host key:", err)
		}
		keyBytes := x509.MarshalPKCS1PrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes})
		if err := os.WriteFile(hostKeyPath, keyPEM, 0600); err != nil {
			log.Fatal("Failed to write host key:", err)
		}
		log.Println("Generated new host key.")
	}

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf(":%d", config.Port)),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true
		}),
		func(s *ssh.Server) error {
			// Handle local port forwarding channels
			s.ChannelHandlers = map[string]ssh.ChannelHandler{
				"direct-tcpip": instance.DirectTCPChannelHandler,
				"session":      ssh.DefaultSessionHandler,
			}
			return nil
		},
		wish.WithSubsystem("sftp", download.SftpSubsystem(config.DownloadRoot)),
		wish.WithMiddleware(
			scp.Middleware(handler, handler),
			bubbletea.Middleware(ui.TeaHandler),
			logging.Middleware(),
		),
	)
	if err != nil {
		log.Fatal("Could not create server:", err)
	}
	log.Printf("CTF SSH server listening on %s:%d", config.Host, config.Port)
	log.Fatal(s.ListenAndServe())
}
