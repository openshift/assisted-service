package hostcommands

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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

func (i *logsCmd) prepareParam(ctx context.Context, host *models.Host) (string, error) {
	var mastersIPs []string
	var err error
	if host.Bootstrap {
		mastersIPs, err = i.getNonBootstrapMastersIPsInHostCluster(ctx, host)
		if err != nil {
			i.log.WithError(err).Errorf("Failed to get non-bootstrap masters IPs from cluster %s", host.ClusterID)
			return "", err
		}
	}

	request := models.LogsGatherCmdRequest{
		ClusterID:       host.ClusterID,
		HostID:          host.ID,
		InfraEnvID:      &host.InfraEnvID,
		Bootstrap:       swag.Bool(host.Bootstrap),
		InstallerGather: true,
		MasterIps:       mastersIPs,
	}

	b, err := json.Marshal(&request)
	if err != nil {
		i.log.WithError(err).Warn("Json marshal")
		return "", err
	}
	return string(b), nil
}

func (i *logsCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	// added to run upload logs if install command fails
	if !time.Time(host.LogsCollectedAt).Equal(time.Time{}) {
		return nil, nil
	}

	logsCommandAsArgs, err := i.prepareParam(ctx, host)
	if err != nil {
		return nil, err
	}
	step := &models.Step{
		StepType: models.StepTypeLogsGather,
		Args:     []string{logsCommandAsArgs},
	}

	return []*models.Step{step}, nil
}

func (i *logsCmd) getNonBootstrapMastersIPsInHostCluster(ctx context.Context, host *models.Host) ([]string, error) {
	cluster, err := common.GetClusterFromDB(i.db, *host.ClusterID, common.UseEagerLoading)
	if err != nil {
		i.log.WithError(err).Errorf("failed to get cluster for host %s", host.ID)
		return nil, err
	}

	if common.IsClusterUmnEnabled(cluster) {
		return i.getHostsIps(*cluster)
	}
	return i.getHostsIpsfromMachineCIDR(*cluster)
}

func (i *logsCmd) getHostsIpsfromMachineCIDR(cluster common.Cluster) ([]string, error) {
	var ips []string
	for _, h := range cluster.Hosts {
		if h.Bootstrap || common.GetEffectiveRole(h) == models.HostRoleWorker {
			continue
		}
		ip, err := network.GetPrimaryMachineCIDRIP(h, &cluster)
		if err != nil {
			i.log.WithError(err).Errorf("failed to get machine cidr IP for host %s", h.ID)
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

func (i *logsCmd) getHostsIps(cluster common.Cluster) ([]string, error) {
	var ips []string
	for _, h := range cluster.Hosts {
		if h.Bootstrap || h.Role == models.HostRoleWorker {
			continue
		}
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			i.log.WithError(err).Warn("Fail to getHostsIps:  Inventory parse")
			return nil, err
		}
		for _, intf := range inventory.Interfaces {
			err = i.parseIps(intf.IPV4Addresses, &ips)
			if err != nil {
				i.log.WithError(err).Warn("Fail to getHostsIps IPV4: Cidr parse")
				return nil, err
			}
			err = i.parseIps(intf.IPV6Addresses, &ips)
			if err != nil {
				i.log.WithError(err).Warn("Fail to getHostsIps IPV6: Cidr parse")
				return nil, err
			}
		}

	}
	return ips, nil
}

func (i *logsCmd) parseIps(ipAddresses []string, ips *[]string) error {
	for _, ipToParse := range ipAddresses {
		var ip net.IP
		ip, _, err := net.ParseCIDR(ipToParse)
		if err != nil {
			i.log.WithError(err).Warn("Cidr parse")
			return err
		}
		*ips = append(*ips, ip.String())
	}
	return nil
}
