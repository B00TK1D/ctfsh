package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/wish"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

type ChatRoom struct {
	mu      sync.Mutex
	clients map[net.Conn]struct{}
}

func NewChatRoom() *ChatRoom {
	return &ChatRoom{clients: make(map[net.Conn]struct{})}
}

func (r *ChatRoom) Add(conn net.Conn) {
	r.mu.Lock()
	r.clients[conn] = struct{}{}
	r.mu.Unlock()

	go r.handle(conn)
}

func (r *ChatRoom) Remove(conn net.Conn) {
	r.mu.Lock()
	delete(r.clients, conn)
	r.mu.Unlock()
	conn.Close()
}

func (r *ChatRoom) Broadcast(from net.Conn, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.clients {
		if c != from {
			fmt.Fprintf(c, "%s\n", msg)
		}
	}
}

func (r *ChatRoom) handle(conn net.Conn) {
	defer r.Remove(conn)
	fmt.Fprintln(conn, "Welcome to the chat room!")
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			r.Broadcast(conn, text)
		}
	}
}

var (
	roomLock sync.Mutex
	rooms    = make(map[string]*ChatRoom)
)

func getRoom(dest string) *ChatRoom {
	roomLock.Lock()
	defer roomLock.Unlock()
	if room, ok := rooms[dest]; ok {
		return room
	}
	room := NewChatRoom()
	rooms[dest] = room
	return room
}

type dummyAddr string

func (a dummyAddr) Network() string { return string(a) }
func (a dummyAddr) String() string  { return string(a) }

type sshConn struct {
	gossh.Channel
}

func (c *sshConn) Read(b []byte) (int, error)         { return c.Channel.Read(b) }
func (c *sshConn) Write(b []byte) (int, error)        { return c.Channel.Write(b) }
func (c *sshConn) Close() error                       { return c.Channel.Close() }
func (c *sshConn) LocalAddr() net.Addr                { return dummyAddr("local") }
func (c *sshConn) RemoteAddr() net.Addr               { return dummyAddr("remote") }
func (c *sshConn) SetDeadline(t time.Time) error      { return nil }
func (c *sshConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *sshConn) SetWriteDeadline(t time.Time) error { return nil }

func main() {
	s := &ssh.Server{
		Addr:             ":2222",
		Handler:          nil, // not needed for direct-tcpip
		LocalPortForwardingCallback: func(ctx ssh.Context, dhost string, dport uint32) bool {
			log.Printf("Allowing local port forward to %s:%d\n", dhost, dport)
			return true
		},
	}

	// Custom channel handler for direct-tcpip (used by -L)
	s.ChannelHandlers = map[string]ssh.ChannelHandler{
		"direct-tcpip": func(sess ssh.Session, newChan ssh.NewChannel) {
			var req struct {
				DestAddr   string
				DestPort   uint32
				OriginAddr string
				OriginPort uint32
			}
			if err := gossh.Unmarshal(newChan.ExtraData(), &req); err != nil {
				newChan.Reject(gossh.ConnectionFailed, "invalid payload")
				return
			}

			channel, reqs, err := newChan.Accept()
			if err != nil {
				return
			}
			go gossh.DiscardRequests(reqs)

			dest := fmt.Sprintf("%s:%d", req.DestAddr, req.DestPort)
			room := getRoom(dest)

			room.Add(&sshConn{Channel: channel})
		},
	}

	// Add Wish options (e.g. host key)
	s, err := wish.NewServer(
		wish.WithAddress(":2222"),
		wish.WithHostKeyPath(".ssh/hostkey"),
		wish.WithMiddleware(func(h ssh.Handler) ssh.Handler {
			return func(sess ssh.Session) {
				// not used, but required
			}
		}),
		wish.WithServerConfigFunc(func(cfg *ssh.Server) {
			cfg.ChannelHandlers = s.ChannelHandlers
			cfg.LocalPortForwardingCallback = s.LocalPortForwardingCallback
		}),
	)
	if err != nil {
		log.Fatalf("failed to start wish server: %v", err)
	}

	log.Println("Listening on :2222")
	if err := s.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
