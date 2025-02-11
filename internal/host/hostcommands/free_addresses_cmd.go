package hostcommands

import (
	"context"
	"encoding/json"
	"net"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	MaxSmallV4PrefixSize = 10
)

type freeAddressesCmd struct {
	baseCmd
	kubeApiEnabled bool
}

func getAllSmallV4Cidrs(ctx context.Context, host *models.Host, log logrus.FieldLogger) ([]string, error) {
	networksByFamily, err := network.GetInventoryNetworksByFamily(ctx, []*models.Host{host}, log)
	if err != nil {
		return nil, err
	}
	var ret []string
	for _, cidr := range networksByFamily[network.IPv4] {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed parsing %s", cidr)
		}
		ones, bits := ipnet.Mask.Size()
		if ones >= bits-MaxSmallV4PrefixSize {
			ret = append(ret, cidr)
		}
	}
	return ret, nil
}

func getFreeAddressesNetworks(ctx context.Context, host *models.Host, log logrus.FieldLogger) ([]string, error) {
	cidrs, err := getAllSmallV4Cidrs(ctx, host, log)
	if err != nil {
		return nil, err
	}
	return cidrs, nil
}

func newFreeAddressesCmd(log logrus.FieldLogger, kubeApiEnabled bool) CommandGetter {
	return &freeAddressesCmd{
		baseCmd:        baseCmd{log: log},
		kubeApiEnabled: kubeApiEnabled,
	}
}

func (f *freeAddressesCmd) prepareParam(ctx context.Context, host *models.Host) (string, error) {
	if f.kubeApiEnabled {
		return "", nil
	}
	_, err := common.UnmarshalInventory(ctx, host.Inventory)
	if err != nil {
		f.log.WithError(err).Warn("Inventory parse")
		return "", err
	}
	networks, err := getFreeAddressesNetworks(ctx, host, f.log)
	if err != nil {
		f.log.WithError(err).Errorf("find if validate with free addresses")
		return "", err
	}
	if len(networks) == 0 {
		return "", nil
	}
	request := models.FreeAddressesRequest{}
	for _, cidr := range networks {
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
	param, err := f.prepareParam(ctx, host)
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
