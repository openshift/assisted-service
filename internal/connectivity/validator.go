package connectivity

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/filanov/bm-inventory/internal/network"

	"github.com/filanov/bm-inventory/internal/validators"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validator.go -package=connectivity -destination=mock_connectivity_validator.go
type Validator interface {
	IsSufficient(host *models.Host, cluster *common.Cluster) (*validators.IsSufficientReply, error)
	GetHostValidInterfaces(host *models.Host) ([]*models.Interface, error)
}

func NewValidator(log logrus.FieldLogger) Validator {
	return &validator{
		log: log,
	}
}

type validator struct {
	log logrus.FieldLogger
}

/*
	This method validates if connectivity host is sufficient by checking the following:
    - the connectivity data in the hw info of the host exists
	- the user choose machine network CIDR
	- the host is in the same network as the machine network CIDR
*/
func (v *validator) IsSufficient(host *models.Host, cluster *common.Cluster) (*validators.IsSufficientReply, error) {
	var reasons []string
	isSufficient := true

	_, err := v.GetHostValidInterfaces(host)
	if err != nil {
		isSufficient = false
		reasons = append(reasons, "Waiting to receive connectivity information")
	}

	if cluster.MachineNetworkCidr == "" {
		isSufficient = false
		reasons = append(reasons, "Could not determine connectivity because API VIP not set")
	}

	if !network.IsHostInMachineNetCidr(v.log, cluster, host) {
		isSufficient = false
		reasons = append(reasons, fmt.Sprintf("host %s does not belong to cluster machine network %s, The machine network is set by configuring the API-VIP", *host.ID, cluster.MachineNetworkCidr))
	}

	return &validators.IsSufficientReply{
		Type:         "connectivity",
		IsSufficient: isSufficient,
		Reason:       strings.Join(reasons[:], ","),
	}, nil
}

func (v *validator) GetHostValidInterfaces(host *models.Host) ([]*models.Interface, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	if len(inventory.Interfaces) == 0 {
		return nil, fmt.Errorf("host %s doesn't have interfaces", host.ID)
	}
	return inventory.Interfaces, nil
}
