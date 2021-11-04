package hostcommands

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type inventoryCmd struct {
	baseCmd
	inventoryImage string
}

func NewInventoryCmd(log logrus.FieldLogger, inventoryImage string) *inventoryCmd {
	return &inventoryCmd{
		baseCmd:        baseCmd{log: log},
		inventoryImage: inventoryImage,
	}
}

func (h *inventoryCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	// Copying mounts file, which is not available by podman's PID
	// We incorporate the host's ID in the copied mtab file path to allow multiple agents
	// to run on the same host during load testing easily without fighting over the same
	// path (each of them has a different fake host ID)
	mtabPath := fmt.Sprintf("/root/mtab-%s", host.ID)
	mtabCopy := fmt.Sprintf("cp /etc/mtab %s", mtabPath)
	mtabMount := fmt.Sprintf("%s:/host/etc/mtab:ro", mtabPath)

	podmanRunCmd := strings.Join([]string{
		"podman", "run", "--privileged", "--net=host", "--rm", "--quiet",
		"-v", "/var/log:/var/log",
		"-v", "/run/udev:/run/udev",
		"-v", "/dev/disk:/dev/disk",
		"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",

		// Enable capturing host's HW using a different root path for GHW library
		"-v", "/var/log:/host/var/log:ro",
		"-v", "/proc/meminfo:/host/proc/meminfo:ro",
		"-v", "/sys/kernel/mm/hugepages:/host/sys/kernel/mm/hugepages:ro",
		"-v", "/proc/cpuinfo:/host/proc/cpuinfo:ro",
		"-v", mtabMount,
		"-v", "/sys/block:/host/sys/block:ro",
		"-v", "/sys/devices:/host/sys/devices:ro",
		"-v", "/sys/bus:/host/sys/bus:ro",
		"-v", "/sys/class:/host/sys/class:ro",
		"-v", "/run/udev:/host/run/udev:ro",
		"-v", "/dev/disk:/host/dev/disk:ro",

		h.inventoryImage,
		"inventory",
	}, " ")

	inventoryCmd := &models.Step{
		StepType: models.StepTypeInventory,
		Command:  "sh",
		Args: []string{
			"-c",
			fmt.Sprintf("%v && %v", mtabCopy, podmanRunCmd),
		},
	}

	return []*models.Step{inventoryCmd}, nil
}
