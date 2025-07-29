package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	gossh "golang.org/x/crypto/ssh"
)

const (
	host = "localhost"
	port = "23234"
)

type directTCPChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func directTCPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var payload directTCPChannelData
	if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		log.Error("Failed to parse direct-tcpip payload", "error", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to parse payload")
		return
	}

	log.Info("Direct TCP connection request", "dest", payload.DestAddr, "port", payload.DestPort)

	if srv.LocalPortForwardingCallback != nil && !srv.LocalPortForwardingCallback(ctx, payload.DestAddr, payload.DestPort) {
		newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}

	channel, requests, err := newChan.Accept()
	if err != nil {
		log.Error("Failed to accept channel", "error", err)
		return
	}

	go gossh.DiscardRequests(requests)

	// TODO: Replace this with a connection to the kubernetes pod once it is created
	defer channel.Close()
	channel.Write([]byte("Hi there! This is a direct-tcpip channel.\n"))
}

func main() {
	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		func(s *ssh.Server) error {
			// Handle local port forwarding channels
			s.ChannelHandlers = map[string]ssh.ChannelHandler{
				"direct-tcpip": directTCPChannelHandler,
				"session":           ssh.DefaultSessionHandler,
			}

			return nil
		},

		wish.WithMiddleware(
			func(h ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					// TODO: Here, create kubernetes pod, showing a spinner until it's ready
					// Then, save the pod's IP address and port in the session context for reference when forwards are received

					wish.Println(s, "ssh -L 5555:chal:0 ctfsh.com")
					wish.Println(s, "Press Ctrl+C to exit...")

					c := make([]byte, 1)
					for {
						_, err := s.Read(c)
						if err != nil {
							if errors.Is(err, io.EOF) {
								log.Info("Session closed")
								return
							}
							log.Error("Error reading from session", "error", err)
							return
						}
						if c[0] == 3 { // Ctrl+C
							return
						}
					}
				}
			},
			logging.Middleware(),
		),
	)

	if err != nil {
		log.Error("Could not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH server", "host", host, "port", port)
	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer func() { cancel() }()
	s.Shutdown(ctx)
}
