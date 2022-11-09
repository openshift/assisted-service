package hostcommands

import (
	"context"
	"encoding/json"
	"net"

	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type freeAddressesCmd struct {
	baseCmd
	freeAddressesImage string
}

func newFreeAddressesCmd(log logrus.FieldLogger, freeAddressesImage string) CommandGetter {
	return &freeAddressesCmd{
		baseCmd:            baseCmd{log: log},
		freeAddressesImage: freeAddressesImage,
	}
}

func hasIPv6Addresses(inventory *models.Inventory) bool {
	for _, intf := range inventory.Interfaces {
		if len(intf.IPV6Addresses) > 0 {
			return true
		}
	}
	return false
}

func (f *freeAddressesCmd) prepareParam(host *models.Host) (string, error) {
	var inventory models.Inventory
	err := json.Unmarshal([]byte(host.Inventory), &inventory)
	if err != nil {
		f.log.WithError(err).Warn("Inventory parse")
		return "", err
	}

	cidrDedupSet := make(map[string]struct{})
	ipv4InterfaceSkipped := false
	for _, intf := range inventory.Interfaces {
		for _, ipv4 := range intf.IPV4Addresses {
			var cidr *net.IPNet
			_, cidr, err = net.ParseCIDR(ipv4)

			ones, bits := cidr.Mask.Size()

			// Ignore subnets with size 8192 or more
			if bits-ones > 12 {
				f.log.Warnf("Skipping address scan for IPv4 CIDR %s for host %s because it contains more than 4096 addresses",
					cidr.String(), host.ID.String())
				ipv4InterfaceSkipped = true
				continue
			}

			if err != nil {
				f.log.WithError(err).Warn("Cidr parse")
				return "", err
			}
			cidrDedupSet[cidr.String()] = struct{}{}
		}
	}
	if len(cidrDedupSet) == 0 {
		if hasIPv6Addresses(&inventory) || ipv4InterfaceSkipped {
			return "", nil
		}
		err = errors.Errorf("No networks found for host %s", host.ID.String())
		f.log.WithError(err).Warn("Missing networks")
		return "", err
	}
	request := models.FreeAddressesRequest{}
	for cidr := range cidrDedupSet {
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
	if param == "" || err != nil {
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeFreeNetworkAddresses,
		Args: []string{
			param,
		},
	}

	return []*models.Step{step}, nil

}
