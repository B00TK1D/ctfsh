package instance

import (
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func DirectTCPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
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
	defer channel.Close()

	go gossh.DiscardRequests(requests)

	// Get session data
	containerName, ok := ctx.Value("containerName").(string)
	if !ok {
		log.Error("No container name set")
		return
	}

	// Get requested challenge name from forward host
	if payload.DestAddr == "" || payload.DestPort == 0 {
		log.Error("Invalid destination address or port")
		newChan.Reject(gossh.ConnectionFailed, "invalid destination address or port")
		return
	}

	chalName := payload.DestAddr
	chalPath := getChallengePath(chalName)
	if chalPath == "" {
		log.Error("Challenge does not exist", "challenge", payload.DestAddr)
		newChan.Reject(gossh.ConnectionFailed, "challenge does not exist")
		return
	}

	// Connect to the forwarded port
	target, err := net.Dial("tcp", getContainerIp(containerName)+":"+fmt.Sprint(payload.DestPort))
	if err != nil {
		log.Error("Failed to connect to forwarded port", "error", err)
		return
	}
	defer target.Close()

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
	<-done

	// Close connections to stop the other goroutine
	target.Close()
	channel.Close()

	// Wait for both goroutines to finish
	wg.Wait()

	log.Info("Connection closed", "container", containerName)
}
