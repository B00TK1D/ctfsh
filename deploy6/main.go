package main

import (
	"fmt"
	"log"
	"os"
	"time"
	"strings"

	"github.com/lxc/go-lxc"
)

func main() {
	// Container names
	containerName := "alpine-docker-base"
	cloneName := "alpine-docker-clone"

	// Create the base container
	fmt.Println("Creating Alpine container with isolated network...")
	container, err := createAlpineContainer(containerName)
	if err != nil {
		log.Fatalf("Failed to create container: %v", err)
	}
	defer container.Release()

	// Start the container
	fmt.Println("Starting container...")
	if err := container.Start(); err != nil {
		log.Fatalf("Failed to start container: %v", err)
	}

	// Wait for container to be fully started
	if !container.Wait(lxc.RUNNING, 30) {
		log.Fatalf("Container failed to start within 30 seconds")
	}

	// Wait a bit more for network to be ready
	fmt.Println("Waiting for container network to be ready...")
	time.Sleep(10 * time.Second)

	// Install Docker in the container
	fmt.Println("Installing Docker in container...")
	if err := installDockerInContainer(container); err != nil {
		log.Fatalf("Failed to install Docker: %v", err)
	}

	// Stop the container before cloning
	fmt.Println("Stopping container for cloning...")
	if err := container.Stop(); err != nil {
		log.Fatalf("Failed to stop container: %v", err)
	}

	if !container.Wait(lxc.STOPPED, 30) {
		log.Fatalf("Container failed to stop within 30 seconds")
	}

	// Clone the container
	fmt.Println("Cloning container...")
	clonedContainer, err := cloneContainer(container, cloneName)
	if err != nil {
		log.Fatalf("Failed to clone container: %v", err)
	}
	defer clonedContainer.Release()

	fmt.Println("Successfully created and cloned Alpine containers with Docker!")
	fmt.Printf("Base container: %s\n", containerName)
	fmt.Printf("Cloned container: %s\n", cloneName)
	fmt.Println("Both containers are stopped and ready for use.")
}

func createAlpineContainer(name string) (*lxc.Container, error) {
	// Create container
	container, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		return nil, fmt.Errorf("failed to create container object: %w", err)
	}

	// Check if container already exists
	if container.Defined() {
		fmt.Printf("Container %s already exists, using existing container\n", name)
		return container, nil
	}

	// Create container with Alpine template
	options := lxc.TemplateOptions{
		Template: "alpine",
		Release:  "3.18",
		Arch:     "amd64",
	}

	if err := container.Create(options); err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Configure network isolation
	if err := configureNetworkIsolation(container); err != nil {
		return nil, fmt.Errorf("failed to configure network: %w", err)
	}

	return container, nil
}

func configureNetworkIsolation(container *lxc.Container) error {
	// Set network configuration for isolated network with internet access
	// This creates a private network that can reach the internet through NAT
	// but isolates containers from each other

	networkConfig := []string{
		// Clear any existing network config
		"lxc.net.0.type = veth",
		"lxc.net.0.link = lxcbr0",
		"lxc.net.0.flags = up",
		"lxc.net.0.hwaddr = 00:16:3e:xx:xx:xx", // LXC will generate unique MAC
		// Use a specific subnet for isolation
		"lxc.net.0.ipv4.address = 10.0.10.0/24",
		"lxc.net.0.ipv4.gateway = 10.0.10.1",
	}

	for _, config := range networkConfig {
		if err := container.SetConfigItem(config[:strings.Index(config, " = ")],
			config[strings.Index(config, " = ")+3:]); err != nil {
			return fmt.Errorf("failed to set network config %s: %w", config, err)
		}
	}

	// Additional security and isolation settings
	securityConfig := []string{
		"lxc.apparmor.profile = generated",
		"lxc.seccomp.profile = /usr/share/lxc/config/common.seccomp",
		// Prevent access to host devices
		"lxc.cgroup.devices.deny = a",
		// Allow basic devices needed for container operation
		"lxc.cgroup.devices.allow = c 1:3 rwm", // /dev/null
		"lxc.cgroup.devices.allow = c 1:5 rwm", // /dev/zero
		"lxc.cgroup.devices.allow = c 5:0 rwm", // /dev/tty
		"lxc.cgroup.devices.allow = c 5:1 rwm", // /dev/console
		"lxc.cgroup.devices.allow = c 1:8 rwm", // /dev/random
		"lxc.cgroup.devices.allow = c 1:9 rwm", // /dev/urandom
	}

	for _, config := range securityConfig {
		parts := strings.SplitN(config, " = ", 2)
		if len(parts) == 2 {
			if err := container.SetConfigItem(parts[0], parts[1]); err != nil {
				return fmt.Errorf("failed to set security config %s: %w", config, err)
			}
		}
	}

	return nil
}

func installDockerInContainer(container *lxc.Container) error {
	commands := []string{
		// Update package index
		"apk update",
		// Install Docker
		"apk add docker docker-compose",
		// Add Docker to startup
		"rc-update add docker boot",
		// Create docker group (if it doesn't exist)
		"addgroup docker || true",
		// Start Docker daemon
		"service docker start",
		// Test Docker installation
		"docker --version",
	}

	for i, cmd := range commands {
		fmt.Printf("Executing command %d/%d: %s\n", i+1, len(commands), cmd)

		// Execute command in container
		exitCode, err := container.RunCommandStatus([]string{"/bin/sh", "-c", cmd}, lxc.AttachOptions{})
		if err != nil {
			return fmt.Errorf("failed to execute command '%s': %w", cmd, err)
		}

		if exitCode != 0 {
			return fmt.Errorf("command '%s' failed with exit code %d", cmd, exitCode)
		}

		// Small delay between commands
		time.Sleep(2 * time.Second)
	}

	fmt.Println("Docker installation completed successfully!")
	return nil
}

func cloneContainer(source *lxc.Container, cloneName string) (*lxc.Container, error) {
	// Create clone container object
	clone, err := lxc.NewContainer(cloneName, lxc.DefaultConfigPath())
	if err != nil {
		return nil, fmt.Errorf("failed to create clone container object: %w", err)
	}

	// Check if clone already exists
	if clone.Defined() {
		fmt.Printf("Clone container %s already exists, using existing container\n", cloneName)
		return clone, nil
	}

	// Clone the container
	if err := source.Clone(cloneName, lxc.CloneOptions{
		KeepName:      false,
		KeepMAC:       false,
		Snapshot:      false, // Full clone, not snapshot
	}); err != nil {
		return nil, fmt.Errorf("failed to clone container: %w", err)
	}

	fmt.Printf("Successfully cloned %s to %s\n", source.Name(), cloneName)
	return clone, nil
}

// Helper function to check if running as root
func init() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root to manage LXC containers")
	}
}
