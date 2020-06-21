package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type freeAddressesCmd struct {
	baseCmd
	freeAddressesImage string
}

func NewFreeAddressesCmd(log logrus.FieldLogger, freeAddressesImage string) *freeAddressesCmd {
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
		err = fmt.Errorf("No networks found for host %s", host.ID.String())
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

func (f *freeAddressesCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	param, err := f.prepareParam(host)
	if err != nil {
		return nil, err
	}
	step := &models.Step{
		StepType: models.StepTypeFreeNetworkAddresses,
		Command:  "podman",
		Args: []string{
			"run", "--privileged", "--net=host", "--rm", "--quiet",
			"--name", "free_addresses_scanner",
			"-v", "/var/log:/var/log",
			"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
			f.freeAddressesImage,
			"free_addresses",
			param,
		},
	}
	return step, nil
}
