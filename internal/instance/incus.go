package instance

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"

	"ctfsh/internal/util"
)

var incusConn incus.InstanceServer

func getIncusConnection() incus.InstanceServer {
	if incusConn == nil {
		conn, err := incus.ConnectIncusUnix("", nil)
		util.Must(err)
		incusConn = conn
	}
	return incusConn
}

func deleteInstanceIfExists(name string) {
	c := getIncusConnection()
	_, _, err := c.GetInstance(name)
	if err == nil {
		fmt.Printf("Instance %s already exists, deleting...\n", name)
		inst, _, err := c.GetInstanceState(name)
		util.Must(err)
		if inst.StatusCode == api.Running {
			op, err := c.UpdateInstanceState(name, api.InstanceStatePut{
				Action:  "stop",
				Timeout: 0,
			}, "")
			util.Must(err)
			util.Must(op.Wait())
		}
		op, err := c.DeleteInstance(name)
		util.Must(err)
		util.Must(op.Wait())
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
	util.Must(err)
	util.Must(op.Wait())
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
		util.Must(err)
		util.Must(op.Wait())
	}

	op, err := c.DeleteInstance(name)
	util.Must(err)
	util.Must(op.Wait())
	log.Info("Challenge stopped and instance deleted", "name", name)
}

func ensureNetworkExists(name string) {
	c := getIncusConnection()
	_, _, err := c.GetNetwork(name)
	if err == nil {
		log.Info("Network already exists", "name", name)
		return
	}

	log.Info("Creating network", "name", name)
	err = c.CreateNetwork(api.NetworksPost{
		Name: name,
		Type: "bridge",
	})
	if err != nil {
		log.Error("Failed to create network", "name", name, "error", err)
		return
	}
	log.Info("Network created successfully", "name", name)
}
