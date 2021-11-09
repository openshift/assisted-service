package hostcommands

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
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

	logsCommand, err := i.createUploadLogsCmd(host, i.instructionConfig.ServiceBaseURL,
		i.instructionConfig.AgentImage, strings.Join(mastersIPs, ","),
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

func (i *logsCmd) createUploadLogsCmd(host *models.Host, baseURL, agentImage, mastersIPs string, skipCertVerification, preservePreviousCommandReturnCode,
	withInstallerGatherLogging bool) (string, error) {

	cmdArgsTmpl := ""
	if preservePreviousCommandReturnCode {
		cmdArgsTmpl = "( returnCode=$?; "
	}

	data := map[string]string{
		"BASE_URL":               strings.TrimSpace(baseURL),
		"CLUSTER_ID":             host.ClusterID.String(),
		"HOST_ID":                host.ID.String(),
		"INFRA_ENV_ID":           host.InfraEnvID.String(),
		"AGENT_IMAGE":            strings.TrimSpace(agentImage),
		"SKIP_CERT_VERIFICATION": strconv.FormatBool(skipCertVerification),
		"BOOTSTRAP":              strconv.FormatBool(host.Bootstrap),
		"INSTALLER_GATHER":       strconv.FormatBool(withInstallerGatherLogging),
		"MASTERS_IPS":            mastersIPs,
	}

	if i.instructionConfig.ServiceCACertPath != "" {
		data["CACERTPATH"] = common.HostCACertPath
	}

	cmdArgsTmpl += "timeout 1h podman run --rm --privileged --net=host " +
		"-v /run/systemd/journal/socket:/run/systemd/journal/socket -v /var/log:/var/log " +
		"{{if .CACERTPATH}} -v {{.CACERTPATH}}:{{.CACERTPATH}} {{end}}" +
		"{{if .BOOTSTRAP}} -v /root/.ssh:/root/.ssh -v /tmp:/tmp {{end}}" +
		"--env PULL_SECRET_TOKEN --name logs-sender --pid=host {{.AGENT_IMAGE}} logs_sender " +
		"-url {{.BASE_URL}} -cluster-id {{.CLUSTER_ID}} -host-id {{.HOST_ID}} -infra-env-id {{.INFRA_ENV_ID}} " +
		"--insecure={{.SKIP_CERT_VERIFICATION}} -bootstrap={{.BOOTSTRAP}} -with-installer-gather-logging={{.INSTALLER_GATHER}}" +
		"{{if .MASTERS_IPS}} -masters-ips={{.MASTERS_IPS}} {{end}}" +
		"{{if .CACERTPATH}} --cacert {{.CACERTPATH}} {{end}}"

	if preservePreviousCommandReturnCode {
		cmdArgsTmpl = cmdArgsTmpl + "; exit $returnCode; )"
	}

	t, err := template.New("cmd").Parse(cmdArgsTmpl)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	if err := t.Execute(buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (i *logsCmd) getNonBootstrapMastersIPsInHostCluster(ctx context.Context, host *models.Host) ([]string, error) {
	cluster, err := common.GetClusterFromDB(i.db, *host.ClusterID, common.UseEagerLoading)
	if err != nil {
		i.log.WithError(err).Errorf("failed to get cluster for host %s", host.ID)
		return nil, err
	}

	if swag.BoolValue(cluster.UserManagedNetworking) {
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
