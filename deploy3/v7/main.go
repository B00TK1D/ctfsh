package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
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

const (
	forwardedTCPChannelType = "forwarded-tcpip"
	directTCPChannelType    = "direct-tcpip"
)

// example usage: ssh -N -R 23236:localhost:23235 -p 23234 localhost

type remoteForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardSuccess struct {
	BindPort uint32
}

type remoteForwardCancelRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type directTCPChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type ForwardedTCPHandler struct {
	forwards map[string]net.Listener
	sync.Mutex
}

var h = &ForwardedTCPHandler{
	forwards: make(map[string]net.Listener),
}

func forwardHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	log.Info("Handling SSH request", "type", req.Type)
	h.Lock()
	if h.forwards == nil {
		h.forwards = make(map[string]net.Listener)
	}
	conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
	h.Unlock()
	switch req.Type {
	case "tcpip-forward":
		var reqPayload remoteForwardRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			// TODO: log parse failure
			return false, []byte{}
		}
		if srv.ReversePortForwardingCallback == nil || !srv.ReversePortForwardingCallback(ctx, reqPayload.BindAddr, reqPayload.BindPort) {
			return false, []byte("port forwarding is disabled")
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// TODO: log listen failure
			return false, []byte{}
		}
		_, destPortStr, _ := net.SplitHostPort(ln.Addr().String())
		destPort, _ := strconv.Atoi(destPortStr)
		go func() {
			<-ctx.Done()
			h.Lock()
			ln, ok := h.forwards[addr]
			h.Unlock()
			if ok {
				ln.Close()
			}
		}()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					// TODO: log accept failure
					break
				}
				originAddr, orignPortStr, _ := net.SplitHostPort(c.RemoteAddr().String())
				originPort, _ := strconv.Atoi(orignPortStr)
				payload := gossh.Marshal(&remoteForwardChannelData{
					DestAddr:   reqPayload.BindAddr,
					DestPort:   uint32(destPort),
					OriginAddr: originAddr,
					OriginPort: uint32(originPort),
				})
				log.Info("Accepted connection", "origin", originAddr, "port", originPort, "dest", reqPayload.BindAddr, "destPort", destPort)
				go func() {
					ch, reqs, err := conn.OpenChannel(forwardedTCPChannelType, payload)
					if err != nil {
						// TODO: log failure to open channel
						log.Error("Failed to open channel", "error", err)
						c.Close()
						return
					}
					go gossh.DiscardRequests(reqs)
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(ch, c)
					}()
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(c, ch)
					}()
				}()
			}
			h.Lock()
			delete(h.forwards, addr)
			h.Unlock()
		}()
		return true, gossh.Marshal(&remoteForwardSuccess{uint32(destPort)})

	case "cancel-tcpip-forward":
		var reqPayload remoteForwardCancelRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			// TODO: log parse failure
			return false, []byte{}
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		h.Lock()
		ln, ok := h.forwards[addr]
		h.Unlock()
		if ok {
			ln.Close()
		}
		return true, nil
	default:
		return false, nil
	}
}


// Handle direct-tcpip channels for local port forwarding (-L)
func directTCPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var payload directTCPChannelData
	if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		log.Error("Failed to parse direct-tcpip payload", "error", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to parse payload")
		return
	}

	log.Info("Direct TCP connection request", "dest", payload.DestAddr, "port", payload.DestPort)

	// Check if local port forwarding is allowed
	if srv.LocalPortForwardingCallback != nil && !srv.LocalPortForwardingCallback(ctx, payload.DestAddr, payload.DestPort) {
		newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}

	// Connect to the destination
	destAddr := net.JoinHostPort(payload.DestAddr, strconv.Itoa(int(payload.DestPort)))
	destConn, err := net.Dial("tcp", destAddr)
	if err != nil {
		log.Error("Failed to connect to destination", "addr", destAddr, "error", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to connect to destination")
		return
	}

	// Accept the channel
	channel, requests, err := newChan.Accept()
	if err != nil {
		log.Error("Failed to accept channel", "error", err)
		destConn.Close()
		return
	}

	// Discard any requests on this channel
	go gossh.DiscardRequests(requests)

	// Start forwarding data
	go func() {
		defer channel.Close()
		defer destConn.Close()
		io.Copy(channel, destConn)
	}()

	go func() {
		defer channel.Close()
		defer destConn.Close()
		io.Copy(destConn, channel)
	}()
}

func main() {
	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		func(s *ssh.Server) error {
			// Set up both local and reverse port forwarding
			s.LocalPortForwardingCallback = func(ctx ssh.Context, bindHost string, bindPort uint32) bool {
				log.Info("local port forwarding allowed", "host", bindHost, "port", bindPort)
				return true
			}

			s.ReversePortForwardingCallback = func(ctx ssh.Context, bindHost string, bindPort uint32) bool {
				log.Info("reverse port forwarding allowed", "host", bindHost, "port", bindPort)
				return true
			}

			// Handle reverse port forwarding requests
			s.RequestHandlers = map[string]ssh.RequestHandler{
				"tcpip-forward":        forwardHandler,
				"cancel-tcpip-forward": forwardHandler,
			}

			// Handle local port forwarding channels
			s.ChannelHandlers = map[string]ssh.ChannelHandler{
				directTCPChannelType: directTCPChannelHandler,
				"session":           ssh.DefaultSessionHandler,
			}

			return nil
		},

		wish.WithMiddleware(
			func(h ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					wish.Println(s, "Port forwarding available!")
					wish.Println(s, "Local forwarding (client -> server -> destination):")
					wish.Println(s, "  ssh -L 8000:127.0.0.1:8000 -p 23234 127.0.0.1")
					wish.Println(s, "Reverse forwarding (server -> client -> destination):")
					wish.Println(s, "  ssh -R 8000:127.0.0.1:8000 -p 23234 127.0.0.1")
					wish.Println(s, "Press Ctrl+C to exit...")

					c := make([]byte, 1)
					for {
						_, err := s.Read(c)
						log.Info("Read from session", "char", c)
						if err != nil {
							if errors.Is(err, io.EOF) {
								log.Info("Session closed")
								return
							}
							log.Error("Error reading from session", "error", err)
							return
						}
						if c[0] == 3 { // Ctrl+C
							log.Info("Ctrl+C pressed, exiting")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}
