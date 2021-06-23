package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/alessio/shellescape"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type freeAddressesCmd struct {
	baseCmd
	freeAddressesImage string
}

func newFreeAddressesCmd(log logrus.FieldLogger, freeAddressesImage string, enabled bool) CommandGetter {
	if !enabled {
		return NewNoopCmd()
	}
	return &freeAddressesCmd{
		baseCmd:            baseCmd{log: log},
		freeAddressesImage: freeAddressesImage,
	}
}

func (f *freeAddressesCmd) prepareParam(host *models.Host) (string, error) {
	var inventory models.Inventory
	err := json.Unmarshal([]byte(host.Inventory), &inventory)
	if err != nil {
		f.log.WithError(err).Warn("Inventory parse")
		return "", err
	}
	m := make(map[string]struct{})
	for _, intf := range inventory.Interfaces {
		for _, ipv4 := range intf.IPV4Addresses {
			var cidr *net.IPNet
			_, cidr, err = net.ParseCIDR(ipv4)
			if err != nil {
				f.log.WithError(err).Warn("Cidr parse")
				return "", err
			}
			m[cidr.String()] = struct{}{}
		}
	}
	if len(m) == 0 {
		err = errors.Errorf("No networks found for host %s", host.ID.String())
		f.log.WithError(err).Warn("Missing networks")
		return "", err
	}
	request := models.FreeAddressesRequest{}
	for cidr := range m {
		request = append(request, cidr)
	}
	b, err := json.Marshal(&request)
	if err != nil {
		f.log.WithError(err).Warn("Json marshal")
		return "", err
	}
	return string(b), nil
}

func (f *freeAddressesCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	param, err := f.prepareParam(host)
	if err != nil {
		return nil, err
	}

	const containerName = "free_addresses_scanner"

	podmanRunCmd := shellescape.QuoteCommand([]string{
		"podman", "run", "--privileged", "--net=host", "--rm", "--quiet",
		"--name", containerName,
		"-v", "/var/log:/var/log",
		"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
		f.freeAddressesImage,
		"free_addresses",
		param,
	})

	// Sometimes the address scanning takes longer than the interval we wait between invocations.
	// To avoid flooding the log with "container already exists" errors, we silently fail by manually
	// checking if it exists and only running if it doesn't
	checkAlreadyRunningCmd := fmt.Sprintf("podman ps --format '{{.Names}}' | grep -q '^%s$'", containerName)

	step := &models.Step{
		StepType: models.StepTypeFreeNetworkAddresses,
		Command:  "sh",
		Args: []string{
			"-c",
			fmt.Sprintf("%s || %s", checkAlreadyRunningCmd, podmanRunCmd),
		},
	}

	return []*models.Step{step}, nil

}
