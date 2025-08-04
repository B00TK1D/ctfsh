package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	"github.com/lxc/go-lxc"
	gossh "golang.org/x/crypto/ssh"
)

const (
	SSH_PORT         = 2222
	LXC_BASE_NAME    = "ctf-template"
	LXC_NETWORK_NAME = "ctf-network"
	CHAL_DIR         = "./chal"
)

type directTCPChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type ContainerSession struct {
	ContainerName string
	Context       context.Context
	Cancel        context.CancelFunc
	Ready         chan struct{}
	ReadyOnce     sync.Once
	mu            sync.Mutex
}

func directTCPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var payload directTCPChannelData
	if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		log.Printf("Failed to parse direct-tcpip payload: %v", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to parse payload")
		return
	}

	log.Printf("Direct TCP connection request: %s:%d", payload.DestAddr, payload.DestPort)

	if srv.LocalPortForwardingCallback != nil && !srv.LocalPortForwardingCallback(ctx, payload.DestAddr, payload.DestPort) {
		newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}

	channel, requests, err := newChan.Accept()
	if err != nil {
		log.Printf("Failed to accept channel: %v", err)
		return
	}
	defer channel.Close()

	go gossh.DiscardRequests(requests)

	// Get session data
	sessionData, ok := ctx.Value("sessionData").(*ContainerSession)
	if !ok {
		log.Printf("No session data found")
		return
	}

	// Wait for container to be ready
	select {
	case <-sessionData.Ready:
		// Container is ready
	case <-sessionData.Context.Done():
		log.Printf("Session context cancelled while waiting for container")
		return
	}

	// Get container IP
	containerIP, err := getContainerIP(sessionData.ContainerName)
	if err != nil {
		log.Printf("Failed to get container IP: %v", err)
		return
	}

	// Connect to the target service in the container
	target, err := net.Dial("tcp", fmt.Sprintf("%s:%d", containerIP, payload.DestPort))
	if err != nil {
		log.Printf("Failed to connect to target: %v", err)
		return
	}
	defer target.Close()

	log.Printf("Forwarding connection to container %s: %s:%d", sessionData.ContainerName, containerIP, payload.DestPort)

	// Pipe the connections
	done := make(chan struct{}, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(target, channel)
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(channel, target)
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	// Wait for either direction to close or session context to be done
	select {
	case <-done:
	case <-sessionData.Context.Done():
	}

	// Close connections to stop the other goroutine
	target.Close()
	channel.Close()

	// Wait for both goroutines to finish
	wg.Wait()

	log.Printf("Connection closed: %s", sessionData.ContainerName)
}

func sessionHandler(s ssh.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	containerName := "ctf-" + generateRandomString(8)

	sessionData := &ContainerSession{
		ContainerName: containerName,
		Context:       ctx,
		Cancel:        cancel,
		Ready:         make(chan struct{}),
	}

	// Store session data in context
	s.Context().SetValue("sessionData", sessionData)

	wish.Println(s, "🚀 Spinning up your CTF container...")
	wish.Println(s, "Container name:", containerName)

	// Create container from template
	err := createContainerFromTemplate(containerName)
	if err != nil {
		wish.Printf(s, "❌ Failed to create container: %v\n", err)
		return
	}

	// Clean up container when session ends
	defer func() {
		log.Printf("Cleaning up container: %s", containerName)
		cleanupContainer(containerName)
	}()

	// Start container
	err = startContainer(containerName)
	if err != nil {
		wish.Printf(s, "❌ Failed to start container: %v\n", err)
		return
	}

	// Wait for container to be ready in a goroutine
	go func() {
		spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerIdx := 0

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wish.Printf(s, "\r%s Waiting for container to be ready...", spinner[spinnerIdx])
				spinnerIdx = (spinnerIdx + 1) % len(spinner)
			default:
				err := waitForContainerReady(containerName)
				if err != nil {
					wish.Printf(s, "\r❌ Failed to wait for container: %v\n", err)
					return
				}

				// Start Docker Compose in the container
				err = startDockerComposeInContainer(containerName)
				if err != nil {
					wish.Printf(s, "\r❌ Failed to start Docker Compose: %v\n", err)
					return
				}

				ticker.Stop()
				wish.Printf(s, "\r✅ Container is ready!                    \n")
				wish.Println(s, "")
				wish.Println(s, "🔗 You can now create port forwards:")
				wish.Println(s, "   ssh -L 8000:localhost:8000 user@localhost")
				wish.Println(s, "")
				wish.Println(s, "Press Ctrl+C to exit and cleanup the container...")

				sessionData.ReadyOnce.Do(func() {
					close(sessionData.Ready)
				})
				return
			}
		}
	}()

	// Wait for user input or context cancellation
	c := make([]byte, 1)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, err := s.Read(c)
			if err != nil {
				if errors.Is(err, io.EOF) {
					log.Printf("Session closed")
					return
				}
				log.Printf("Error reading from session: %v", err)
				return
			}
			if c[0] == 3 { // Ctrl+C
				wish.Println(s, "\n👋 Goodbye! Cleaning up your container...")
				return
			}
		}
	}
}

func createContainerFromTemplate(name string) error {
	log.Printf("Creating container %s from template", name)

	// Create container using go-lxc
	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to create container: %v", err)
	}
	defer container.Release()

	// Clone from template
	err = container.Clone(LXC_BASE_NAME, lxc.CloneOptions{
		Backend:  lxc.Overlayfs,
		Snapshot: true,
	})
	if err != nil {
		return fmt.Errorf("failed to clone container: %v", err)
	}

	return nil
}

func startContainer(name string) error {
	log.Printf("Starting container %s", name)

	// Get container and start it
	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Start the container
	err = container.Start()
	if err != nil {
		return fmt.Errorf("failed to start container: %v", err)
	}

	return nil
}

func waitForContainerReady(name string) error {
	log.Printf("Waiting for container %s to be ready", name)

	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Wait for container to be running
	maxAttempts := 30
	for i := 0; i < maxAttempts; i++ {
		state := container.State()
		if state == lxc.RUNNING {
			// Try to get IP address
			_, err := getContainerIP(name)
			if err == nil {
				return nil
			}
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("container %s not ready within %d seconds", name, maxAttempts)
}

func startDockerComposeInContainer(name string) error {
	log.Printf("Starting Docker Compose in container %s", name)

	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Execute docker-compose command inside the container
	cmd := []string{"sh", "-c", "cd /chal && docker-compose up -d"}
	_, err = container.RunCommand(cmd, lxc.DefaultAttachOptions)
	if err != nil {
		return fmt.Errorf("failed to run docker-compose: %v", err)
	}

	return nil
}

func cleanupContainer(name string) {
	log.Printf("Cleaning up container %s", name)

	// Stop container
	stopContainer(name)

	// Destroy container
	destroyContainer(name)
}

func stopContainer(name string) error {
	log.Printf("Stopping container: %s", name)

	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Stop the container
	err = container.Stop()
	if err != nil {
		return fmt.Errorf("failed to stop container: %v", err)
	}

	return nil
}

func destroyContainer(name string) error {
	log.Printf("Destroying container: %s", name)

	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Destroy the container
	err = container.Destroy()
	if err != nil {
		return fmt.Errorf("failed to destroy container: %v", err)
	}

	return nil
}

func getContainerIP(name string) (string, error) {
	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return "", fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Get container IP addresses - try different methods
	// Method 1: Try to get IP from network interface
	ips, err := container.IPAddresses()
	if err != nil {
		return "", fmt.Errorf("failed to get container IP addresses: %v", err)
	}
	if len(ips) > 0 {
		return ips[0], nil
	}

	// Method 2: Try to get IP from config
	config := container.ConfigItem("lxc.net.0.ipv4.address")
	if len(config) > 0 {
		// Extract IP from config (format: "10.0.3.100/24")
		ip := config[0]
		if idx := strings.Index(ip, "/"); idx != -1 {
			return ip[:idx], nil
		}
		return ip, nil
	}

	// Method 3: Use default IP
	return "10.0.3.100", nil
}

func copyDirectory(src, dst string) error {
	// Create destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	// Read source directory
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

func initialSetup() error {
	log.Println("Performing initial setup...")

	// Check LXC system requirements
	log.Println("Checking LXC system requirements...")

	// Check if running as root (LXC typically requires root)
	if os.Geteuid() != 0 {
		log.Println("Warning: Not running as root. LXC operations may fail.")
	}

	// Check if LXC tools are available
	if err := checkLXCTools(); err != nil {
		return fmt.Errorf("LXC tools check failed: %v", err)
	}

	// Create isolated LXC network
	if err := createLXCNetwork(); err != nil {
		return fmt.Errorf("failed to create LXC network: %v", err)
	}

	// Create base Alpine container
	if err := createBaseContainer(); err != nil {
		return fmt.Errorf("failed to create base container: %v", err)
	}

	// Install Docker in the container
	if err := installDocker(); err != nil {
		return fmt.Errorf("failed to install Docker: %v", err)
	}

	// Copy challenge files
	if err := copyChallengeFiles(); err != nil {
		return fmt.Errorf("failed to copy challenge files: %v", err)
	}

	// Start Docker Compose
	if err := startDockerCompose(); err != nil {
		return fmt.Errorf("failed to start Docker Compose: %v", err)
	}

	// Stop container to use as template
	if err := stopContainer(LXC_BASE_NAME); err != nil {
		return fmt.Errorf("failed to stop template container: %v", err)
	}

	log.Println("Initial setup completed successfully")
	return nil
}

func checkLXCTools() error {
	// Check if lxc-create is available
	if _, err := os.Stat("/usr/bin/lxc-create"); os.IsNotExist(err) {
		return fmt.Errorf("lxc-create not found. Please install LXC tools")
	}

	// Check if lxcbr0 bridge exists
	if _, err := os.Stat("/var/lib/lxc/lxcbr0"); os.IsNotExist(err) {
		log.Println("Warning: lxcbr0 bridge not found. LXC networking may not work properly.")
	}

	return nil
}

func createLXCNetwork() error {
	log.Println("Creating isolated LXC network...")

	// Create network configuration directory
	networkPath := filepath.Join("/var/lib/lxc", LXC_NETWORK_NAME)
	if err := os.MkdirAll(networkPath, 0755); err != nil {
		return err
	}

	// Create network configuration file
	networkConfig := `lxc.net.0.type = veth
lxc.net.0.link = lxcbr0
lxc.net.0.flags = up
lxc.net.0.hwaddr = 00:16:3e:xx:xx:xx
lxc.net.0.ipv4.address = 10.0.3.100/24
lxc.net.0.ipv4.gateway = 10.0.3.1
lxc.net.0.ipv4.dhcp = true`

	configPath := filepath.Join(networkPath, "config")
	return os.WriteFile(configPath, []byte(networkConfig), 0644)
}

func createBaseContainer() error {
	log.Println("Creating base Alpine container...")

	// Check if LXC is properly initialized
	if !lxc.VersionAtLeast(2, 0, 0) {
		return fmt.Errorf("LXC version 2.0.0 or higher is required")
	}

	// Create container using go-lxc
	container, err := lxc.NewContainer(LXC_BASE_NAME, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to create container: %v", err)
	}
	defer container.Release()

	// Check if container already exists
	if container.Defined() {
		log.Printf("Container %s already exists, destroying it first", LXC_BASE_NAME)
		if err := container.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy existing container: %v", err)
		}
	}

	// Create the Alpine container with more detailed options
	err = container.Create(lxc.TemplateOptions{
		Template: "alpine",
		Distro:   "alpine",
		Release:  "latest",
		Arch:     "amd64",
	})
	if err != nil {
		return fmt.Errorf("failed to create Alpine container: %v", err)
	}

	log.Printf("Successfully created container %s", LXC_BASE_NAME)

	// Update container configuration
	return updateContainerConfig(LXC_BASE_NAME)
}

func updateContainerConfig(containerName string) error {
	configPath := filepath.Join("/var/lib/lxc", containerName, "config")

	// Read existing config
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// Add network configuration
	networkConfig := `
# Network configuration
lxc.net.0.type = veth
lxc.net.0.link = lxcbr0
lxc.net.0.flags = up
lxc.net.0.hwaddr = 00:16:3e:xx:xx:xx
lxc.net.0.ipv4.address = 10.0.3.100/24
lxc.net.0.ipv4.gateway = 10.0.3.1

# Additional capabilities for Docker
lxc.cap.keep = sys_admin sys_chroot sys_ptrace sys_pacct net_admin

# Security settings
lxc.seccomp.profile = /usr/share/lxc/config/common.seccomp
lxc.apparmor.profile = lxc-container-default`

	// Append network config
	newConfig := string(configData) + networkConfig

	return os.WriteFile(configPath, []byte(newConfig), 0644)
}

func installDocker() error {
	log.Println("Installing Docker in container...")

	container, err := lxc.NewContainer(LXC_BASE_NAME, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Start the container
	err = container.Start()
	if err != nil {
		return fmt.Errorf("failed to start container for Docker installation: %v", err)
	}

	// Wait for container to be ready
	if err := waitForContainerReady(LXC_BASE_NAME); err != nil {
		return fmt.Errorf("failed to wait for container to be ready: %v", err)
	}

	// Install Docker commands
	dockerCommands := []string{
		"apk update",
		"apk add docker docker-compose",
		"rc-update add docker default",
		"service docker start",
	}

	for _, cmd := range dockerCommands {
		command := []string{"sh", "-c", cmd}
		_, err = container.RunCommand(command, lxc.DefaultAttachOptions)
		if err != nil {
			return fmt.Errorf("failed to execute %s: %v", cmd, err)
		}
	}

	return nil
}

func copyChallengeFiles() error {
	log.Println("Copying challenge files to container...")

	// Copy the entire chal directory to the container
	srcDir := CHAL_DIR
	dstDir := filepath.Join("/var/lib/lxc", LXC_BASE_NAME, "rootfs", "chal")

	return copyDirectory(srcDir, dstDir)
}

func startDockerCompose() error {
	log.Println("Starting Docker Compose in container...")

	container, err := lxc.NewContainer(LXC_BASE_NAME, lxc.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}
	defer container.Release()

	// Execute docker-compose command inside the container
	cmd := []string{"sh", "-c", "cd /chal && docker-compose up -d"}
	_, err = container.RunCommand(cmd, lxc.DefaultAttachOptions)
	if err != nil {
		return fmt.Errorf("failed to run docker-compose: %v", err)
	}

	return nil
}

func main() {
	// Perform initial setup
	if err := initialSetup(); err != nil {
		log.Fatalf("Failed to perform initial setup: %v", err)
	}

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf(":%d", SSH_PORT)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		func(s *ssh.Server) error {
			// Handle local port forwarding channels
			s.ChannelHandlers = map[string]ssh.ChannelHandler{
				"direct-tcpip": directTCPChannelHandler,
				"session":      ssh.DefaultSessionHandler,
			}
			return nil
		},
		wish.WithMiddleware(
			func(h ssh.Handler) ssh.Handler {
				return sessionHandler
			},
			logging.Middleware(),
		),
	)

	if err != nil {
		log.Printf("Could not start server: %v", err)
		os.Exit(1)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("Starting SSH server on port %d", SSH_PORT)

	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Printf("Could not start server: %v", err)
			done <- nil
		}
	}()

	<-done
	log.Printf("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.Shutdown(ctx)
}
