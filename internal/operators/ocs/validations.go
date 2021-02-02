package ocs

import (
	"encoding/json"
	"fmt"

	"github.com/docker/go-units"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validations.go -package=ocs -destination=mock_validations.go
type OcsValidator interface {
	ValidateOCSRequirements(cluster *models.Cluster) bool
}

type ocsValidator struct {
	*Config
	log     logrus.FieldLogger
	hostApi host.API
}

func NewOcsValidator(log logrus.FieldLogger, hostApi host.API, cfg *Config) OcsValidator {
	return &ocsValidator{
		log:     log,
		Config:  cfg,
		hostApi: hostApi,
	}
}

type Config struct {
	OCSMinimumCPUCount             int64  `envconfig:"OCS_MINIMUM_CPU_COUNT" default:"24"`
	OCSMinimumRAMGB                int64  `envconfig:"OCS_MINIMUM_RAM_GB" default:"66"`
	OCSRequiredDisk                int64  `envconfig:"OCS_MINIMUM_DISK" default:"3"`
	OCSRequiredHosts               int64  `envconfig:"OCS_MINIMUM_HOST" default:"3"`
	OCSRequiredCPUCount            int64  `envconfig:"OCS_REQUIRED_CPU_COUNT" default:"30"`
	OCSRequiredRAMGB               int64  `envconfig:"OCS_REQUIRED_RAM_GB" default:"72"`
	OCSRequiredCompactModeCPUCount int64  `envconfig:"OCS_REQUIRED_COMAPCT_MODE_CPU_COUNT" default:"36"` // compact mode cpu requirements for 3 nodes( 4*3(master)+8*3(OCS) cpus)
	OCSRequiredCompactModeRAMGB    int64  `envconfig:"OCS_REQUIRED_COMAPCT_MODE_RAM_GB" default:"96"`    //compact mode ram requirements (8*3(master)+72(OCS) GB)
	OCSMasterWorkerHosts           int    `envconfig:"OCS_REQUIRED_MASTER_WORKER_HOSTS" default:"5"`
	OCSMinimalDeployment           bool   `envconfig:"OCS_MINIMAL_DEPLOYMENT" default:"false"`
	OCSDisksAvailable              int64  `envconfig:"OCS_DISKS_AVAILABLE" default:"3"`
	OCSDeploymentType              string `envconfig:"OCS_DEPLOYMENT_TYPE" default:"None"`
}

func setOperatorStatus(cluster *models.Cluster, status string) error {
	var operators models.Operators
	err := json.Unmarshal([]byte(cluster.Operators), &operators)
	if err != nil {
		return err
	}
	for _, operator := range operators {
		if operator.OperatorType == models.OperatorTypeOcs {
			operator.Status = status
			break
		}
	}
	clusterOperators, err := json.Marshal(operators)
	if err != nil {
		return err
	}

	cluster.Operators = string(clusterOperators)
	return nil
}

// ValidateOCSRequirements is used to validate min requirements of OCS
func (o *ocsValidator) ValidateOCSRequirements(cluster *models.Cluster) bool {
	var status string
	hosts := cluster.Hosts

	if int64(len(hosts)) < o.OCSRequiredHosts {
		status = "Insufficient hosts to deploy OCS. A minimum of 3 hosts is required to deploy OCS. "
		err := setOperatorStatus(cluster, status)
		if err != nil {
			o.log.Fatal("Failed to set Operator status ", err)
			return false
		}
		o.log.Info("Setting Operator Status ", status)
		return false
	}
	var CPUCount int64 = 0                       //count the total CPUs on the cluster
	var TotalRAM int64 = 0                       // count the total available RAM on the cluster
	var DiskCount int64 = 0                      // count the total disks on all the hosts
	var HostsWithDisks int64 = 0                 // to determine total number of hosts with disks. OCS requires atleast 3 hosts with non-bootable disks                           // to claim for the PV created by LocalVolumeSet
	if int64(len(hosts)) == o.OCSRequiredHosts { // for only 3 hosts, we need to install OCS in compact mode
		for _, host := range hosts {
			err := o.nodeResources(host, &CPUCount, &TotalRAM, &DiskCount, &HostsWithDisks)
			if err != nil {
				o.log.Fatal("Error occured while calculating Node requirements ", err)
				return false
			}
		}

	} else if len(hosts) <= o.OCSMasterWorkerHosts { // not supporting OCS installation for 2 Workers and 3 Masters
		status = "Not supporting OCS Installation for 3 Masters and 2 Workers"
		o.log.Info(status)
		o.log.Info("Setting Operator Status")
		err := setOperatorStatus(cluster, status)
		if err != nil {
			o.log.Fatal("Failed to set Operator status ", err)
			return false
		}
		o.log.Info(status)
		return false
	} else {
		for _, host := range hosts { // if the worker nodes >=3 , install OCS on all the worker nodes if they satisfy OCS requirements
			if host.Role == models.HostRoleWorker {
				err := o.nodeResources(host, &CPUCount, &TotalRAM, &DiskCount, &HostsWithDisks)
				if err != nil {
					o.log.Fatal("Error occured while calculating Node requirements ", err)
					return false
				}
			}
		}
	}
	// Hosts with disks must be a multiple of 3
	o.OCSDisksAvailable = DiskCount
	canDeployOCS, status := o.validateOCS(o.log, hosts, CPUCount, TotalRAM, DiskCount, HostsWithDisks)

	err := setOperatorStatus(cluster, status)
	if err != nil {
		o.log.Fatal("Failed to set Operator status ", err)
		return false
	}

	return canDeployOCS
}

func (o *ocsValidator) nodeResources(host *models.Host, CPUCount *int64, TotalRAM *int64, DiskCount *int64, HostsWithDisks *int64) error {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return err
	}

	disks, err := o.hostApi.GetHostValidDisks(host)
	if err != nil {
		o.log.Error("failed to get valid disks due to ", err.Error())
		return err
	}

	if len(disks) > 1 { // OCS must use the non-boot disks
		*DiskCount += (int64)(len(disks) - 1) // not counting the boot disk
		*HostsWithDisks++
	}

	*CPUCount += inventory.CPU.Count
	*TotalRAM += inventory.Memory.UsableBytes
	return nil
}

func gbToBytes(gb int64) int64 {
	return gb * int64(units.GB)
}

func (o *ocsValidator) validateOCS(log logrus.FieldLogger, hosts []*models.Host, cpu int64, RAM int64, disk int64, hostsWithDisks int64) (bool, string) {

	var status string
	if int64(len(hosts)) == o.OCSRequiredHosts { // for 3 hosts
		if cpu < o.OCSRequiredCompactModeCPUCount || RAM < gbToBytes(o.OCSRequiredCompactModeRAMGB) || disk < o.OCSRequiredDisk || hostsWithDisks < o.OCSRequiredHosts { // check for master nodes requirements
			status = "Insufficient Resources to deploy OCS in Compact mode.A minimum of "
			if cpu < o.OCSRequiredCompactModeCPUCount {
				status = status + fmt.Sprint(o.OCSRequiredCompactModeCPUCount, " CPUs, ")
			}
			if RAM < gbToBytes(o.OCSRequiredCompactModeRAMGB) {
				status = status + fmt.Sprint(o.OCSRequiredCompactModeRAMGB, " RAM, ")
			}
			if disk < o.OCSRequiredDisk {
				status = status + fmt.Sprint(o.OCSRequiredDisk, " Disks, ")
			}
			if hostsWithDisks < o.OCSRequiredHosts {
				status = status + fmt.Sprint(o.OCSRequiredHosts, " Hosts with disks, ")
			}
			status = status + "is required."
			log.Info(status)
			return false, status
		}
		o.OCSDeploymentType = "Compact"
		status = "OCS Requirements for Compact Mode are satisfied"
		return true, status
	}
	if disk < o.OCSRequiredDisk || cpu < o.OCSMinimumCPUCount || RAM < gbToBytes(o.OCSMinimumRAMGB) || hostsWithDisks < o.OCSRequiredHosts { // check for worker nodes requirements
		status = "Insufficient resources to deploy OCS on worker hosts. A minimum of "
		if cpu < o.OCSMinimumCPUCount {
			status = status + fmt.Sprint(o.OCSMinimumCPUCount, " CPUs, ")
		}
		if RAM < gbToBytes(o.OCSMinimumRAMGB) {
			status = status + fmt.Sprint(o.OCSMinimumRAMGB, " RAM, ")
		}
		if disk < o.OCSRequiredDisk {
			status = status + fmt.Sprint(o.OCSRequiredDisk, " Disks, ")
		}
		if hostsWithDisks < o.OCSRequiredHosts {
			status = status + fmt.Sprint(o.OCSRequiredHosts, " Hosts with disks, ")
		}
		status = status + "is required."
		log.Info(status)
		return false, status
	}

	if cpu < o.OCSRequiredCPUCount || RAM < gbToBytes(o.OCSRequiredRAMGB) { // conditions for minimal deployment
		status = "Requirements for OCS Minimal Deployment are satisfied"
		o.OCSDeploymentType = "Minimal"
		o.OCSMinimalDeployment = true
		return true, status
	}

	status = "OCS Requirements for Standard Deployment are satisfied"
	o.OCSDeploymentType = "Standard"
	return true, status
}
