package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	// Connect to Incus
	c, err := incus.ConnectIncusUnix("", nil)
	must(err)

	// Container name
	name := "instance"

	builderName := name + "-builder"
	backupFile := "/root/workspace/ctfsh/deploy8/chal/" + builderName + ".tar.gz"

	// Check if backup file exists
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		fmt.Printf("Template '%s' does not exist, creating...\n", builderName)
		// Launch new container with mounted chal directory
		req := api.InstancesPost{
			Name: builderName,
			InstancePut: api.InstancePut{
				Architecture: "x86_64",
				Config: map[string]string{
					"security.nesting": "true",
				},
				Devices: map[string]map[string]string{
					"root": {
						"type": "disk",
						"path": "/",
						"pool": "default", // Make sure 'default' storage pool exists
					},
					"chal": {
						"type":   "disk",
						"source": "/root/workspace/ctfsh/deploy8/chal",
						"path":   "/chal",
					},
				},
			},
			Source: api.InstanceSource{
				Type:     "image",
				Alias:    "alpine/edge",
				Server:   "https://images.linuxcontainers.org",
				Protocol: "simplestreams",
			},
		}

		op, err := c.CreateInstance(req)
		must(err)
		must(op.Wait())
		fmt.Printf("Builder instance '%s' created successfully.\n", builderName)

		// Start the container
		op, err = c.UpdateInstanceState(builderName, api.InstanceStatePut{
			Action:  "start",
			Timeout: -1,
		}, "")
		must(err)
		must(op.Wait())

		// Wait for container to boot and get an IP
		fmt.Println("Waiting for container to get IP...")
		time.Sleep(5 * time.Second)

		// Install Docker
		runCmdInContainer(c, builderName, `apk add docker docker-compose`)

		// Start Docker service
		runCmdInContainer(c, builderName, `rc-update add docker default`)
		runCmdInContainer(c, builderName, `service docker start`)

		// Build the docker compose project
		runCmdInContainer(c, builderName, `cd /chal && docker compose build && docker compose create`)

		// Shut down the container
		op, err = c.UpdateInstanceState(builderName, api.InstanceStatePut{
			Action:  "stop",
			Timeout: -1,
		}, "")
		must(err)
		must(op.Wait())

		fmt.Printf("Builder instance '%s' built.\n", builderName)

		// Back up the container
		fmt.Printf("Creating backup of '%s'...\n", builderName)
		op, err = c.CreateInstanceBackup(builderName, api.InstanceBackupsPost{
			Name: builderName,
		})

		// Wait for backup operation to finish
		must(err)
		must(op.Wait())
		fmt.Printf("Backup of '%s' created successfully.\n", builderName)

		// Export the backup to a file
		func() {
			f, err := os.Create(backupFile)
			must(err)
			defer f.Close()
			_, err = c.GetInstanceBackupFile(builderName, builderName, &incus.BackupFileRequest{
				BackupFile: f,
			})
			must(err)
		}()
		fmt.Printf("Backup of '%s' exported to '%s'.\n", builderName, backupFile)

		// Delete the builder instance
		op, err = c.DeleteInstance(builderName)
		must(err)
		must(op.Wait())
		fmt.Printf("Builder instance '%s' deleted after backup.\n", builderName)

	} else {
		fmt.Printf("Template '%s' already exists, skipping creation.\n", builderName)
	}

	// Delete existing container if it exists
	_, _, err = c.GetInstance(name)
	if err == nil {
		fmt.Printf("Instance %s already exists, deleting...\n", name)
		// Stop the instance if it's running
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
	}


	// Get default pool name
	pools, err := c.GetStoragePools()
	must(err)
	if len(pools) == 0 {
		log.Fatal("No storage pools found")
	}
	defaultPool := pools[0].Name

	// Create new container from the backup
	func() {
		fmt.Printf("Creating instance '%s' from backup '%s'...\n", name, builderName)
		f, err := os.Open(backupFile)
		must(err)
		defer f.Close()
		op, err := c.CreateInstanceFromBackup(incus.InstanceBackupArgs{
			BackupFile: f,
			PoolName: defaultPool,
			Name: name,
		})
		must(err)
		must(op.Wait())
		fmt.Printf("Instance '%s' created successfully.\n", name)
	}()

	// Start the new instance
	fmt.Printf("Starting instance '%s'...\n", name)
	startOp, err := c.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	must(err)
	must(startOp.Wait())
	fmt.Printf("Instance '%s' started successfully.\n", name)

	// Start the docker compose project once docker is running
	fmt.Printf("Starting Docker Compose project in instance '%s'...\n", name)
	runCmdInContainer(c, name, `cd /chal && until docker compose up -d; do echo "Waiting for Docker to be ready..."; sleep 1; done`)
	fmt.Printf("Docker Compose project started in instance '%s'.\n", name)

	// Start proxy on port 8000
	fmt.Println("Proxying :8000 to container...")
	proxyPort8000ToContainer(c, name)
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

func proxyPort8000ToContainer(c incus.InstanceServer, name string) {
	l, err := net.Listen("tcp", ":8000")
	must(err)
	defer l.Close()

	for {
		clientConn, err := l.Accept()
		if err != nil {
			log.Println("Accept error:", err)
			continue
		}

		go func(conn net.Conn) {
			defer conn.Close()

			// Get container IP
			inst, _, err := c.GetInstanceState(name)
			if err != nil || len(inst.Network) == 0 {
				log.Println("Failed to get container IP")
				return
			}

			var ip string
			for _, net := range inst.Network {
				for _, addr := range net.Addresses {
					if addr.Family == "inet" {
						ip = addr.Address
						break
					}
				}
				if ip != "" {
					break
				}
			}

			if ip == "" {
				log.Println("No IP found for container")
				return
			}

			// Connect to container port 8000
			containerConn, err := net.Dial("tcp", ip+":8000")
			if err != nil {
				log.Println("Dial container:", err)
				return
			}
			defer containerConn.Close()

			go io.Copy(containerConn, conn)
			io.Copy(conn, containerConn)
		}(clientConn)
	}
}
