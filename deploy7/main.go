package main

import (
	"log"
	"time"

	"github.com/lxc/go-lxc"
)

func main() {

	name := "mycontainer"

	c, err := lxc.NewContainer(name, lxc.DefaultConfigPath())
	if err != nil {
		log.Fatalf("ERROR: %s\n", err.Error())
	}
	defer c.Release()

	log.Printf("Creating container...\n")
	c.SetVerbosity(lxc.Verbose)

	var backend lxc.BackendStore
	if err := (&backend).Set("dir"); err != nil {
		log.Fatalf("ERROR: %s\n", err.Error())
	}

	var bdevSize lxc.ByteSize

	options := lxc.TemplateOptions{
		Template:             "download",
		Distro:               "alpine",
		Release:              "3.19",
		Arch:                 "amd64",
		FlushCache:           false,
		DisableGPGValidation: false,
		Backend:              backend,
		BackendSpecs: &lxc.BackendStoreSpecs{
			FSSize: uint64(bdevSize),
		},
	}

	c.SetLogFile("log")
	c.SetLogLevel(lxc.DEBUG)

	if err := c.Create(options); err != nil {
		log.Printf("ERROR: %s\n", err.Error())
	}

	// Start the container
	if err := c.Start(); err != nil {
		log.Printf("ERROR: %s\n", err.Error())
	} else {
		log.Printf("Container %s started successfully.\n", name)
	}

	log.Printf("Container %s created successfully.\n", name)

	// Wait for container networking
	if _, err := c.WaitIPAddresses(5 * time.Second); err != nil {
		log.Fatalf("ERROR: %s\n", err.Error())
	}
	log.Printf("Container %s is ready with IP addresses.\n", name)
}
