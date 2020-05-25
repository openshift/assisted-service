package connectivity

import (
	"fmt"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validator.go -package=connectivity -destination=mock_connectivity_validator.go
type Validator interface {
	IsSufficient(host *models.Host, cluster *common.Cluster) (*common.IsSufficientReply, error)
}

func NewValidator(log logrus.FieldLogger) Validator {
	return &validator{
		log: log,
	}
}

type validator struct {
	log logrus.FieldLogger
}

func (v *validator) IsSufficient(host *models.Host, cluster *common.Cluster) (*common.IsSufficientReply, error) {
	var reason string
	isSufficient := true
	if !common.IsHostInMachineNetCidr(v.log, cluster, host) {
		isSufficient = false
		reason = fmt.Sprintf(", host %s does not belong to cluster machine network %s, The machine network is set by configuring the API-VIP", *host.ID, cluster.MachineNetworkCidr)
	}

	return &common.IsSufficientReply{
		Type:         "connectivity",
		IsSufficient: isSufficient,
		Reason:       reason,
	}, nil
}
