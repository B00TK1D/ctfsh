package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	gossh "golang.org/x/crypto/ssh"
)

// ChatSession represents a chat room for a specific SSH tunnel
type ChatSession struct {
	ID       string
	clients  map[string]*ChatClient
	messages chan ChatMessage
	mu       sync.RWMutex
}

// ChatClient represents a connected client in a chat session
type ChatClient struct {
	ID          string
	conn        net.Conn
	chatSession *ChatSession
	username    string
}

// ChatMessage represents a message in the chat
type ChatMessage struct {
	From      string
	Content   string
	Timestamp time.Time
}

// Global chat sessions manager
var (
	sessions   = make(map[string]*ChatSession)
	sessionsMu sync.RWMutex
)

// NewChatSession creates a new chat session
func NewChatSession(id string) *ChatSession {
	session := &ChatSession{
		ID:       id,
		clients:  make(map[string]*ChatClient),
		messages: make(chan ChatMessage, 100),
	}

	// Start message broadcaster
	go session.broadcastMessages()

	return session
}

// broadcastMessages handles broadcasting messages to all clients in the session
func (cs *ChatSession) broadcastMessages() {
	for msg := range cs.messages {
		cs.mu.RLock()
		clients := make([]*ChatClient, 0, len(cs.clients))
		for _, client := range cs.clients {
			clients = append(clients, client)
		}
		cs.mu.RUnlock()

		formattedMsg := fmt.Sprintf("[%s] %s: %s\n",
			msg.Timestamp.Format("15:04:05"),
			msg.From,
			msg.Content)

		for _, client := range clients {
			_, err := client.conn.Write([]byte(formattedMsg))
			if err != nil {
				log.Printf("Error sending message to client %s: %v", client.ID, err)
				cs.removeClient(client.ID)
			}
		}
	}
}

// addClient adds a new client to the chat session
func (cs *ChatSession) addClient(client *ChatClient) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.clients[client.ID] = client

	// Send welcome message
	welcomeMsg := fmt.Sprintf("Welcome to chat session %s! You are connected as %s.\n", cs.ID, client.username)
	client.conn.Write([]byte(welcomeMsg))

	// Notify other clients
	cs.messages <- ChatMessage{
		From:      "System",
		Content:   fmt.Sprintf("%s joined the chat", client.username),
		Timestamp: time.Now(),
	}
}

// removeClient removes a client from the chat session
func (cs *ChatSession) removeClient(clientID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if client, exists := cs.clients[clientID]; exists {
		client.conn.Close()
		delete(cs.clients, clientID)

		// Notify other clients
		cs.messages <- ChatMessage{
			From:      "System",
			Content:   fmt.Sprintf("%s left the chat", client.username),
			Timestamp: time.Now(),
		}

		// If no clients left, close the session
		if len(cs.clients) == 0 {
			close(cs.messages)
			sessionsMu.Lock()
			delete(sessions, cs.ID)
			sessionsMu.Unlock()
		}
	}
}

// handleChat handles the chat functionality for a client
func handleChat(conn net.Conn, sessionID string, username string) {
	clientID := fmt.Sprintf("%s-%s", sessionID, username)

	// Get or create chat session
	sessionsMu.Lock()
	chatSession, exists := sessions[sessionID]
	if !exists {
		chatSession = NewChatSession(sessionID)
		sessions[sessionID] = chatSession
	}
	sessionsMu.Unlock()

	client := &ChatClient{
		ID:          clientID,
		conn:        conn,
		chatSession: chatSession,
		username:    username,
	}

	chatSession.addClient(client)
	defer chatSession.removeClient(clientID)

	// Send initial instructions
	instructions := "Chat session started! Type your messages and press Enter.\nType 'quit' to disconnect.\n\n"
	conn.Write([]byte(instructions))

	// Handle incoming messages
	buffer := make([]byte, 1024)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			log.Printf("Client %s disconnected: %v", clientID, err)
			break
		}

		message := string(buffer[:n])
		// Remove newline characters
		if len(message) > 0 && (message[len(message)-1] == '\n' || message[len(message)-1] == '\r') {
			message = message[:len(message)-1]
		}
		if len(message) > 0 && (message[len(message)-1] == '\n' || message[len(message)-1] == '\r') {
			message = message[:len(message)-1]
		}

		if message == "quit" {
			conn.Write([]byte("Goodbye!\n"))
			break
		}

		if message != "" {
			chatSession.messages <- ChatMessage{
				From:      username,
				Content:   message,
				Timestamp: time.Now(),
			}
		}
	}
}

// portForwardHandler handles SSH port forward requests
func portForwardHandler(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
	log.Printf("Port forward request: %s:%d", destinationHost, destinationPort)
	return true
}

// channelHandler handles SSH channels including port forwarding
func channelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	switch newChan.ChannelType() {
	case "direct-tcpip":
		// Handle port forwarding
		var req struct {
			Addr       string
			Port       uint32
			OriginAddr string
			OriginPort uint32
		}
		if err := gossh.Unmarshal(newChan.ExtraData(), &req); err != nil {
			newChan.Reject(gossh.ConnectionFailed, "invalid request")
			return
		}

		// Accept the channel
		ch, reqs, err := newChan.Accept()
		if err != nil {
			log.Printf("Failed to accept channel: %v", err)
			return
		}
		defer ch.Close()

		// Handle requests (should be empty for direct-tcpip)
		go gossh.DiscardRequests(reqs)

		log.Printf("Port forward request: %s:%d -> %s:%d", req.OriginAddr, req.OriginPort, req.Addr, req.Port)

		// Create a unique session ID based on the forwarded port
		sessionID := fmt.Sprintf("tunnel-%d", req.Port)

		// Connect to the destination
		destConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", req.Addr, req.Port))
		if err != nil {
			log.Printf("Failed to connect to destination %s:%d: %v", req.Addr, req.Port, err)
			ch.Close()
			return
		}
		defer destConn.Close()

		// Create a chat session for this connection
		username := fmt.Sprintf("user-%d", time.Now().UnixNano()%10000)
		clientID := fmt.Sprintf("%s-%s", sessionID, username)

		// Get or create chat session
		sessionsMu.Lock()
		chatSession, exists := sessions[sessionID]
		if !exists {
			chatSession = NewChatSession(sessionID)
			sessions[sessionID] = chatSession
		}
		sessionsMu.Unlock()

		client := &ChatClient{
			ID:          clientID,
			conn:        destConn,
			chatSession: chatSession,
			username:    username,
		}

		chatSession.addClient(client)
		defer chatSession.removeClient(clientID)

		// Send initial instructions
		instructions := fmt.Sprintf("Chat session started on port %d! Type your messages and press Enter.\nType 'quit' to disconnect.\n\n", req.Port)
		destConn.Write([]byte(instructions))

		// Handle bidirectional communication
		go func() {
			// Copy from SSH channel to destination
			io.Copy(destConn, ch)
		}()

		// Copy from destination to SSH channel (this is the chat input)
		buffer := make([]byte, 1024)
		for {
			n, err := destConn.Read(buffer)
			if err != nil {
				log.Printf("Client %s disconnected: %v", clientID, err)
				break
			}

			message := string(buffer[:n])
			// Remove newline characters
			if len(message) > 0 && (message[len(message)-1] == '\n' || message[len(message)-1] == '\r') {
				message = message[:len(message)-1]
			}
			if len(message) > 0 && (message[len(message)-1] == '\n' || message[len(message)-1] == '\r') {
				message = message[:len(message)-1]
			}

			if message == "quit" {
				destConn.Write([]byte("Goodbye!\n"))
				break
			}

			if message != "" {
				chatSession.messages <- ChatMessage{
					From:      username,
					Content:   message,
					Timestamp: time.Now(),
				}
			}
		}

	default:
		// For session channels and other types, let the default handler deal with them
		// This allows the wish server to handle shell sessions properly
		return
	}
}

// sessionHandler handles regular SSH sessions
func sessionHandler(s ssh.Session) {
	io.WriteString(s, "SSH Chat Server\n")
	io.WriteString(s, "Use port forwarding to create chat sessions.\n")
	io.WriteString(s, "Example: ssh -L 8080:localhost:8080 user@localhost -p 2222\n")
	io.WriteString(s, "Then connect to localhost:8080 to join the chat.\n\n")

	// Keep the session alive
	<-s.Context().Done()
}

func main() {
	// Create SSH server with custom configuration
	s, err := wish.NewServer(
		wish.WithAddress(":2222"),
		wish.WithHostKeyPath(".ssh/term_info_ed25519"),
		wish.WithMiddleware(
			func(next ssh.Handler) ssh.Handler {
				return func(s ssh.Session) {
					// Handle regular sessions
					sessionHandler(s)
				}
			},
		),
	)
	if err != nil {
		log.Fatalf("Failed to create SSH server: %v", err)
	}

	// Set up port forwarding handlers
	s.LocalPortForwardingCallback = portForwardHandler
	s.ChannelHandlers = map[string]ssh.ChannelHandler{
		"direct-tcpip": channelHandler,
	}

	log.Printf("SSH Chat Server starting on :2222")
	log.Printf("Connect with: ssh -L <local_port>:localhost:<local_port> user@localhost -p 2222")
	log.Printf("Then connect to localhost:<local_port> to join the chat session")

	if err := s.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start SSH server: %v", err)
	}
}

