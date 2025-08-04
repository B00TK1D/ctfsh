package main

import (
	"fmt"
	"log"

	"github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"
)

func main() {
	// Connect to local Incus server
	c, err := incus.ConnectIncusUnix("", nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Container name
	name := "example"

	// Configuration
	req := api.InstancesPost{
		Name: name,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "alpine/edge",
			Server: "https://images.linuxcontainers.org",
			Protocol: "simplestreams",
		},
	}

	// Create container
	op, err := c.CreateInstance(req)
	if err != nil {
		log.Fatalf("Failed to create instance: %v", err)
	}

	// Wait for operation to finish
	if err := op.Wait(); err != nil {
		log.Fatalf("Create wait error: %v", err)
	}

	fmt.Println("Container created.")

	// Start container
	op, err = c.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	if err != nil {
		log.Fatalf("Failed to start instance: %v", err)
	}
	if err := op.Wait(); err != nil {
		log.Fatalf("Start wait error: %v", err)
	}

	fmt.Println("Container started.")
}
