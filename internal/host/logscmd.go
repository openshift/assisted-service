package host

import (
	"context"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"

	"github.com/sirupsen/logrus"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
)

type logsCmd struct {
	baseCmd
	instructionConfig InstructionConfig
	db                *gorm.DB
}

func NewLogsCmd(log logrus.FieldLogger, db *gorm.DB, instructionConfig InstructionConfig) *logsCmd {
	return &logsCmd{
		baseCmd:           baseCmd{log: log},
		instructionConfig: instructionConfig,
		db:                db,
	}
}

func (i *logsCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	// added to run upload logs if install command fails
	if host.LogsCollectedAt != strfmt.DateTime(time.Time{}) {
		return nil, nil
	}

	var mastersIPs []string
	var err error
	if host.Bootstrap {
		mastersIPs, err = i.getNonBootstrapMastersIPsInHostCluster(ctx, host)
		if err != nil {
			i.log.WithError(err).Errorf("Failed to get non-bootstrap masters IPs from cluster %s", host.ClusterID)
			return nil, err
		}
	}

	logsCommand, err := CreateUploadLogsCmd(host, i.instructionConfig.ServiceBaseURL,
		i.instructionConfig.InventoryImage, strings.Join(mastersIPs, ","),
		i.instructionConfig.SkipCertVerification, false, true)
	if err != nil {
		return nil, err
	}
	logsCommandAsArgs := strings.Fields(logsCommand)
	step := &models.Step{
		StepType: models.StepTypeExecute,
		Command:  logsCommandAsArgs[0],
		Args:     logsCommandAsArgs[1:],
	}

	return []*models.Step{step}, nil
}

func (i *logsCmd) getNonBootstrapMastersIPsInHostCluster(ctx context.Context, host *models.Host) ([]string, error) {

	var cluster common.Cluster
	if err := i.db.Preload("Hosts", "cluster_id = ?", host.ClusterID).First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		i.log.WithError(err).Errorf("failed to get cluster for host %s", host.ID)
		return nil, err
	}

	var ips []string
	for _, h := range cluster.Hosts {
		if h.Bootstrap || h.Role == models.HostRoleWorker {
			continue
		}
		ip, err := network.GetMachineCIDRIP(h, &cluster)
		if err != nil {
			i.log.WithError(err).Errorf("failed to get machine cidr IP for host %s", host.ID)
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, nil
}
