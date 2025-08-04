package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	gossh "golang.org/x/crypto/ssh"
)

func main() {
	host := "localhost"
	port := 2222

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%d", host, port)),
		wish.WithHostKeyPath(".ssh/term_info_ed25519"),
		wish.WithMiddleware(
			logging.Middleware(),
		),
		// Configure port forwarding callbacks
		func(srv *ssh.Server) error {
			srv.LocalPortForwardingCallback = localPortForwardingCallback
			srv.ReversePortForwardingCallback = reversePortForwardingCallback

			// Set up channel handlers - preserve defaults and add ours
			defaultHandlers := map[string]ssh.ChannelHandler{
				"session": ssh.DefaultSessionHandler,
			}

			// Add our custom handlers
			defaultHandlers["direct-tcpip"] = directTcpipHandler
			defaultHandlers["forwarded-tcpip"] = forwardedTcpipHandler

			srv.ChannelHandlers = defaultHandlers

			// Set up request handlers
			srv.RequestHandlers = map[string]ssh.RequestHandler{
				"tcpip-forward":        tcpipForwardHandler,
				"cancel-tcpip-forward": cancelTcpipForwardHandler,
			}

			return nil
		},
	)
	if err != nil {
		log.Fatalln(err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("Starting SSH server on %s:%d", host, port)
	go func() {
		if err = s.ListenAndServe(); err != nil {
			log.Fatalln(err)
		}
	}()

	<-done
	log.Println("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()
	if err := s.Shutdown(ctx); err != nil {
		log.Fatalln(err)
	}
}

// Callback when client requests local port forwarding (-L)
func localPortForwardingCallback(ctx ssh.Context, dstHost string, dstPort uint32) bool {
	log.Printf("🔗 Client %s requested LOCAL port forward (-L) to %s:%d",
		ctx.RemoteAddr(), dstHost, dstPort)
	return true // Allow the tunnel
}

// Callback when client requests reverse port forwarding (-R)
func reversePortForwardingCallback(ctx ssh.Context, bindHost string, bindPort uint32) bool {
	log.Printf("🔄 Client %s requested REVERSE port forward (-R) on %s:%d",
		ctx.RemoteAddr(), bindHost, bindPort)
	return true // Allow the tunnel
}

// Handler for direct-tcpip channels (local port forwarding -L)
func directTcpipHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var payload struct {
		DestHost string
		DestPort uint32
		SrcHost  string
		SrcPort  uint32
	}

	if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		log.Printf("❌ Failed to parse direct-tcpip request: %v", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to parse request")
		return
	}

	log.Printf("📡 New TCP connection over LOCAL tunnel from %s: %s:%d -> %s:%d",
		ctx.RemoteAddr(), payload.SrcHost, payload.SrcPort, payload.DestHost, payload.DestPort)

	// Accept the channel
	channel, requests, err := newChan.Accept()
	if err != nil {
		log.Printf("❌ Failed to accept direct-tcpip channel: %v", err)
		return
	}
	defer func() {
		channel.Close()
		log.Printf("🔚 Closed tunnel connection to %s:%d", payload.DestHost, payload.DestPort)
	}()

	// Handle any requests on this channel
	go gossh.DiscardRequests(requests)

	log.Printf("🔌 Simulating connection to %s:%d (not actually forwarding)",
		payload.DestHost, payload.DestPort)

	// Simulate some activity - in reality you'd proxy data here
	buffer := make([]byte, 1024)
	for {
		// Set a read timeout to avoid blocking forever
		channel.SetReadDeadline(time.Now().Add(30 * time.Second))

		// Try to read from the channel
		n, err := channel.Read(buffer)
		if err != nil {
			if err.Error() != "EOF" {
				log.Printf("📤 Connection to %s:%d closed: %v", payload.DestHost, payload.DestPort, err)
			} else {
				log.Printf("📤 Connection to %s:%d closed by client", payload.DestHost, payload.DestPort)
			}
			break
		}
		if n > 0 {
			log.Printf("📤 Received %d bytes for %s:%d (not forwarding)", n, payload.DestHost, payload.DestPort)
			// In a real implementation, you would forward this data to the destination
			// For now, just send back a simple response to close the connection gracefully
			response := "HTTP/1.1 200 OK\r\nContent-Length: 13\r\n\r\nNot forwarded"
			channel.Write([]byte(response))
			break
		}
	}
}

// Handler for forwarded-tcpip channels (reverse port forwarding -R)
func forwardedTcpipHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var payload struct {
		ConnectedHost string
		ConnectedPort uint32
		OriginHost    string
		OriginPort    uint32
	}

	if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		log.Printf("❌ Failed to parse forwarded-tcpip request: %v", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to parse request")
		return
	}

	log.Printf("📡 New TCP connection over REVERSE tunnel from %s: %s:%d <- %s:%d",
		ctx.RemoteAddr(), payload.ConnectedHost, payload.ConnectedPort,
		payload.OriginHost, payload.OriginPort)

	// Accept the channel
	channel, requests, err := newChan.Accept()
	if err != nil {
		log.Printf("❌ Failed to accept forwarded-tcpip channel: %v", err)
		return
	}
	defer func() {
		channel.Close()
		log.Printf("🔚 Closed reverse tunnel connection")
	}()

	// Handle any requests on this channel
	go gossh.DiscardRequests(requests)

	log.Printf("🔌 Handling reverse tunnel connection (not actually forwarding)")

	// Simulate handling the connection
	buffer := make([]byte, 1024)
	for {
		channel.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := channel.Read(buffer)
		if err != nil {
			log.Printf("📤 Reverse tunnel connection closed: %v", err)
			break
		}
		if n > 0 {
			log.Printf("📤 Received %d bytes from reverse tunnel (not forwarding)", n)
		}
	}
}

// Handler for tcpip-forward requests (setting up reverse port forwarding)
func tcpipForwardHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	var payload struct {
		Host string
		Port uint32
	}

	if err := gossh.Unmarshal(req.Payload, &payload); err != nil {
		log.Printf("❌ Failed to parse tcpip-forward request: %v", err)
		return false, nil
	}

	log.Printf("🔄 Setting up reverse port forward from %s on %s:%d",
		ctx.RemoteAddr(), payload.Host, payload.Port)

	// In a real implementation, you would bind to the port here
	// For now, just log and accept
	return true, nil
}

// Handler for cancel-tcpip-forward requests
func cancelTcpipForwardHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	var payload struct {
		Host string
		Port uint32
	}

	if err := gossh.Unmarshal(req.Payload, &payload); err != nil {
		log.Printf("❌ Failed to parse cancel-tcpip-forward request: %v", err)
		return false, nil
	}

	log.Printf("❌ Canceling reverse port forward from %s on %s:%d",
		ctx.RemoteAddr(), payload.Host, payload.Port)

	// In a real implementation, you would unbind the port here
	return true, nil
}
