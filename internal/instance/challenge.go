package instance

import (
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/lxc/incus/shared/api"

	"ctfsh/internal/config"
	"ctfsh/internal/util"
)

func CreateChallengeImage(name string, challengePath string) {
	c := getIncusConnection()
	builderName := name + "-builder"

	// Check if image already exists
	images, err := c.GetImages()
	util.Must(err)
	for _, img := range images {
		for _, alias := range img.Aliases {
			if alias.Name == "ctfsh/"+name {
				return
			}
		}
	}

	ensureNetworkExists("chals")

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
				"eth0": {
					"type":   "nic",
					"network": "chals",
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
	util.Must(err)
	util.Must(op.Wait())

	op, err = c.UpdateInstanceState(builderName, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	util.Must(err)
	util.Must(op.Wait())

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
	util.Must(err)
	util.Must(op.Wait())

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
	util.Must(err)
	util.Must(op.Wait())

	util.Must(err)
	util.Must(op.Wait())

	op, err = c.DeleteInstance(builderName)
	util.Must(err)
}

func StartChallenge(image string, name string) {
	c := getIncusConnection()
	CreateChallengeImage(image, getChallengePath(image))
	deleteInstanceIfExists(name)

	op, err := c.CreateInstance(api.InstancesPost{
		Name: name,
		InstancePut: api.InstancePut{
			Architecture: "x86_64",
			Config: map[string]string{
				"security.nesting": "true",
			},
			Devices: map[string]map[string]string{
				"eth0": {
					"type":   "nic",
					"network": "chals",
				},
			},
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: "ctfsh/" + image,
		},
	})
	util.Must(err)
	util.Must(op.Wait())

	startOp, err := c.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}, "")
	util.Must(err)
	util.Must(startOp.Wait())

	runCmdInContainer(c, name, `cd /chal && until docker info >/dev/null 2>&1; do sleep 1; done; docker compose up -d`)
}

func getChallengePath(name string) string {
	p, err := filepath.Abs(config.ChallengeDir + "/" + name)
	if err != nil {
		log.Error("Failed to get absolute path for challenge", "name", name, "error", err)
		return ""
	}
	if _, err = os.Stat(p); os.IsNotExist(err) {
		return ""
	}
	return p
}
