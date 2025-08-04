package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"
)

func createContainerTemplate(c incus.InstanceServer, name string, challengePath string, templateFile string) {
	builderName := name + "-builder"

	op, err := c.CreateInstance(api.InstancesPost{
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
					"pool": poolName,
				},
				"chal": {
					"type":   "disk",
					"source": challengePath,
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
	runCmdInContainer(c, builderName, `cd /chal && docker compose build && docker compose create`)

	op, err = c.UpdateInstanceState(builderName, api.InstanceStatePut{
		Action:  "stop",
		Timeout: -1,
	}, "")
	must(err)
	must(op.Wait())

	op, err = c.CreateInstanceBackup(builderName, api.InstanceBackupsPost{
		Name: builderName,
	})

	must(err)
	must(op.Wait())

	func() {
		f, err := os.Create(templateFile)
		must(err)
		defer f.Close()
		_, err = c.GetInstanceBackupFile(builderName, builderName, &incus.BackupFileRequest{
			BackupFile: f,
		})
		must(err)
	}()
	fmt.Printf("Backup of '%s' exported to '%s'.\n", builderName, templateFile)

	op, err = c.DeleteInstance(builderName)
	must(err)
}

func ensurePoolExists(c incus.InstanceServer, poolName string) {
	pools, err := c.GetStoragePools()
	must(err)

	for _, pool := range pools {
		if pool.Name == poolName {
			return
		}
	}

	err = c.CreateStoragePool(api.StoragePoolsPost{
		Name:   poolName,
		Driver: "btrfs",
	})
	must(err)
}

func deleteInstanceIfExists(c incus.InstanceServer, name string) {
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

func getContainerIp(c incus.InstanceServer, name string) (string, error) {
	inst, _, err := c.GetInstanceState(name)
	if err != nil || len(inst.Network) == 0 {
		return "", fmt.Errorf("failed to get instance state: %w", err)
	}

	for _, net := range inst.Network {
		for _, addr := range net.Addresses {
			if addr.Family == "inet" {
				return addr.Address, nil
			}
		}
	}

	return "", fmt.Errorf("no IPv4 address found for instance %s", name)
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
			ip, err := getContainerIp(c, name)
			if err != nil {
				log.Println("Failed to get container IP:", err)
				return
			}

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
