package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/lxc/incus/client"
	"github.com/lxc/incus/shared/api"
)

const (
	poolName  = "default"
	backupDir = "./backups"
)

func startChallenge(c incus.InstanceServer, name string, path string, templateFile string) {
	deleteInstanceIfExists(c, name)

	func() {
		f, err := os.Open(templateFile)
		must(err)
		defer f.Close()
		op, err := c.CreateInstanceFromBackup(incus.InstanceBackupArgs{
			BackupFile: f,
			PoolName:   poolName,
			Name:       name,
		})
		must(err)
		must(op.Wait())
	}()

	startOp, err := c.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	must(err)
	must(startOp.Wait())

	runCmdInContainer(c, name, `cd /chal && until docker info >/dev/null 2>&1; do sleep 1; done; docker compose up -d`)
}

func main() {

	name := "instance"
	path := "/root/workspace/ctfsh/deploy8/chal"

	c, err := incus.ConnectIncusUnix("", nil)
	must(err)
	ensurePoolExists(c, poolName)

	templateFile, err := filepath.Abs(backupDir + "/" + name + ".tar.gz")
	must(err)
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		err = os.MkdirAll(backupDir, 0755)
		must(err)
	}

	if _, err := os.Stat(templateFile); os.IsNotExist(err) {
		createContainerTemplate(c, name, path, templateFile)
	}

	startChallenge(c, name, path, templateFile)

	log.Println("Starting port proxy for container on port 8000...")
	proxyPort8000ToContainer(c, name)
}
