package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	incus "github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"
	gossh "golang.org/x/crypto/ssh"
)

type directTCPChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

var incusConn incus.InstanceServer

func getIncusConnection() incus.InstanceServer {
	if incusConn == nil {
		conn, err := incus.ConnectIncusUnix("", nil)
		must(err)
		incusConn = conn
	}
	return incusConn
}

func createChallengeImage(name string, challengePath string) {
	c := getIncusConnection()
	builderName := name + "-builder"

	// Check if image already exists
	images, err := c.GetImages()
	must(err)
	for _, img := range images {
		for _, alias := range img.Aliases {
			if alias.Name == "ctfsh/"+name {
				return
			}
		}
	}

	op, err := c.CreateInstance(api.InstancesPost{
		Name: builderName,
		InstancePut: api.InstancePut{
			Architecture: "x86_64",
			Config: map[string]string{
				"security.nesting": "true",
			},
			Devices: map[string]map[string]string{
				"chal": {
					"type":   "disk",
					"source": challengePath,
					"path":   "/mnt/chal",
				},
			},
		},
		Source: api.InstanceSource{
			Type:     "image",
			Alias:    "alpine/edge",
			Server:   "https://images.linuxcontainers.org",
			Protocol: "simplestreams",
		},
	})
	must(err)
	must(op.Wait())

	op, err = c.UpdateInstanceState(builderName, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	must(err)
	must(op.Wait())

	runCmdInContainer(c, builderName, `while ! ip addr show eth0 | grep -q "inet "; do echo "Waiting for IP..."; sleep 1; done`)
	runCmdInContainer(c, builderName, `apk add docker docker-compose`)
	runCmdInContainer(c, builderName, `rc-update add docker default`)
	runCmdInContainer(c, builderName, `service docker start`)
	runCmdInContainer(c, builderName, `mkdir -p /chal && cp -r /mnt/chal/* /chal/`)
	runCmdInContainer(c, builderName, `cd /chal && docker compose build && docker compose create`)

	op, err = c.UpdateInstanceState(builderName, api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}, "")
	must(err)
	must(op.Wait())

	op, err = c.CreateImage(api.ImagesPost{
		Source: &api.ImagesPostSource{
			Type: "container",
			Name: builderName,
		},
		Aliases: []api.ImageAlias{{
			Name:        "ctfsh/" + name,
			Description: "CTFsh container for " + name,
		}},
	}, nil)
	must(err)
	must(op.Wait())

	must(err)
	must(op.Wait())

	op, err = c.DeleteInstance(builderName)
	must(err)
}

func deleteInstanceIfExists(name string) {
	c := getIncusConnection()
	_, _, err := c.GetInstance(name)
	if err == nil {
		fmt.Printf("Instance %s already exists, deleting...\n", name)
		inst, _, err := c.GetInstanceState(name)
		must(err)
		if inst.StatusCode == api.Running {
			op, err := c.UpdateInstanceState(name, api.InstanceStatePut{
				Action:  "stop",
				Timeout: 0,
			}, "")
			must(err)
			must(op.Wait())
		}
		op, err := c.DeleteInstance(name)
		must(err)
		must(op.Wait())
		fmt.Printf("Instance %s deleted successfully.\n", name)
	}
}

func runCmdInContainer(c incus.InstanceServer, name, command string) {
	execReq := api.InstanceExecPost{
		Command:     []string{"sh", "-c", command},
		WaitForWS:   true,
		Interactive: false,
	}

	args := incus.InstanceExecArgs{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		DataDone: make(chan bool),
	}

	op, err := c.ExecInstance(name, execReq, &args)
	must(err)
	must(op.Wait())
	<-args.DataDone
}

func getContainerIp(name string) string {
	c := getIncusConnection()
	inst, _, err := c.GetInstanceState(name)
	if err != nil || len(inst.Network) == 0 {
		log.Error("Failed to get instance state", "name", name, "error", err)
		return ""
	}

	for _, net := range inst.Network {
		for _, addr := range net.Addresses {
			if addr.Family == "inet" {
				if addr.Address[:2] == "10" {
					return addr.Address
				}
			}
		}
	}

	log.Error("No IPv4 address found for instance", "name", name)
	return ""
}

func startChallenge(image string, name string) {
	c := getIncusConnection()
	createChallengeImage(image, getChallengePath(image))
	deleteInstanceIfExists(name)

	op, err := c.CreateInstance(api.InstancesPost{
		Name: name,
		InstancePut: api.InstancePut{
			Architecture: "x86_64",
			Config: map[string]string{
				"security.nesting": "true",
			},
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "ctfsh/" + image,
		},
	})
	must(err)
	must(op.Wait())

	startOp, err := c.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	must(err)
	must(startOp.Wait())

	runCmdInContainer(c, name, `cd /chal && until docker info >/dev/null 2>&1; do sleep 1; done; docker compose up -d`)
}

func stopContainer(name string) {
	c := getIncusConnection()
	inst, _, err := c.GetInstanceState(name)
	if err != nil {
		log.Error("Failed to get instance state", "name", name, "error", err)
		return
	}

	if inst.StatusCode == api.Running {
		op, err := c.UpdateInstanceState(name, api.InstanceStatePut{
			Action:  "stop",
			Timeout: -1,
		}, "")
		must(err)
		must(op.Wait())
	}

	op, err := c.DeleteInstance(name)
	must(err)
	must(op.Wait())
	log.Info("Challenge stopped and instance deleted", "name", name)
}

func getChallengePath(name string) string {
	p, err := filepath.Abs(challengeDir + "/" + name)
	if err != nil {
		log.Error("Failed to get absolute path for challenge", "name", name, "error", err)
		return ""
	}
	if _, err = os.Stat(p); os.IsNotExist(err) {
		return ""
	}
	return p
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
	target, err := net.Dial("tcp", fmt.Sprintf("%s:%d", getContainerIp(containerName), payload.DestPort))
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

func handleInstanceRequest(s ssh.Session, user *User, chal Challenge) {
	log.Printf("Loading instancer for %s", chal.Name)

	containerName := fmt.Sprintf("%s-%s", chal.Name, randHex(6))
	s.Context().SetValue("containerName", containerName)
	readyChan := make(chan struct{})
	go func() {
		startChallenge(chal.Name, containerName)
		close(readyChan)
	}()
	defer func() {
		go stopContainer(containerName)
	}()

	fmt.Fprintf(s, "\x1b[?25l\n   %s\n\n", titleStyle.Render(chal.Name))
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
			fmt.Fprintf(s, "\r %s %s", successStyle.Render(spinner[spinnerIdx]), helpStyle.Render("Loading instance..."))
			spinnerIdx = (spinnerIdx + 1) % len(spinner)
		}
	}

	fmt.Fprintf(s, "\r %s %s\n\n", successStyle.Render("✔"), "Instance ready. To connect:")
	for _, port := range chal.Ports {
		fmt.Fprintf(s, commandStyle.Render(fmt.Sprintf("     nc 127.0.0.1 %d        \n\r", port)))
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
				wish.Println(s, "\n   Exiting instance...\x1b[?25h\n")
				break exit
			}
		}
	}

}
