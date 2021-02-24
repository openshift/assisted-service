package ocs

import (
	"encoding/json"
	"fmt"

	"github.com/docker/go-units"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=validations.go -package=ocs -destination=mock_validations.go
type OcsValidator interface {
	ValidateOCSRequirements(cluster *models.Cluster) string
}

type ocsValidator struct {
	*Config
	log logrus.FieldLogger
}

func NewOCSValidator(log logrus.FieldLogger, cfg *Config) OcsValidator {
	return &ocsValidator{
		log:    log,
		Config: cfg,
	}
}

type Config struct {
	OCSMinimumCPUCount             int64  `envconfig:"OCS_MINIMUM_CPU_COUNT" default:"18"`
	OCSMinimumRAMGB                int64  `envconfig:"OCS_MINIMUM_RAM_GB" default:"51"` // disable deployment if less
	OCSRequiredDisk                int64  `envconfig:"OCS_MINIMUM_DISK" default:"3"`
	OCSRequiredDiskCPUCount        int64  `envconfig:"OCS_REQUIRED_DISK_CPU_COUNT" default:"2"` // each disk requires 2 cpus
	OCSRequiredDiskRAMGB           int64  `envconfig:"OCS_REQUIRED_DISK_RAM_GB" default:"5"`    // each disk requires 5GB ram
	OCSRequiredHosts               int64  `envconfig:"OCS_MINIMUM_HOST" default:"3"`
	OCSRequiredCPUCount            int64  `envconfig:"OCS_REQUIRED_CPU_COUNT" default:"24"`              // Standard Mode 8*3
	OCSRequiredRAMGB               int64  `envconfig:"OCS_REQUIRED_RAM_GB" default:"57"`                 // Standard Mode
	OCSRequiredCompactModeCPUCount int64  `envconfig:"OCS_REQUIRED_COMAPCT_MODE_CPU_COUNT" default:"30"` // Compact Mode cpu requirements for 3 nodes( 4*3(master)+6*3(OCS) cpus)
	OCSRequiredCompactModeRAMGB    int64  `envconfig:"OCS_REQUIRED_COMAPCT_MODE_RAM_GB" default:"81"`    //Compact Mode ram requirements (8*3(master)+57(OCS) GB)
	OCSMasterWorkerHosts           int    `envconfig:"OCS_REQUIRED_MASTER_WORKER_HOSTS" default:"6"`
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
func (o *ocsValidator) ValidateOCSRequirements(cluster *models.Cluster) string {
	var status string
	var err error
	var inventoryMissing bool
	hosts := cluster.Hosts

	if int64(len(hosts)) < o.OCSRequiredHosts {
		status = "Insufficient hosts to deploy OCS. A minimum of 3 hosts is required to deploy OCS. "
		err = setOperatorStatus(cluster, status)
		if err != nil {
			o.log.Error("Failed to set Operator status ", err)
			return validationFailure
		}
		o.log.Info("Setting Operator Status ", status)
		return validationFailure
	}
	var cpuCount int64 = 0       //count the total CPUs on the cluster
	var totalRAM int64 = 0       // count the total available RAM on the cluster
	var diskCount int64 = 0      // count the total disks on all the hosts
	var hostsWithDisks int64 = 0 // to determine total number of hosts with disks. OCS requires atleast 3 hosts with non-bootable disks
	var insufficientHosts []string
	if int64(len(hosts)) == o.OCSRequiredHosts { // for only 3 hosts, we need to install OCS in compact mode
		for _, host := range hosts {
			inventoryMissing, err = o.nodeResources(host, &cpuCount, &totalRAM, &diskCount, &hostsWithDisks, &insufficientHosts)
			if err != nil {
				o.log.Fatal("Error occured while calculating Node requirements ", err)
				return validationFailure
			} else if inventoryMissing {
				return validationPending
			}
		}

	} else if len(hosts) < o.OCSMasterWorkerHosts { // not supporting OCS installation for 2 Workers and 3 Masters
		status = "Not supporting OCS Installation for 3 Masters and 2 Workers"
		o.log.Info("Setting Operator Status", status)
		err = setOperatorStatus(cluster, status)
		if err != nil {
			o.log.Error("Failed to set Operator status ", err)
		}
		return validationFailure
	} else {
		for _, host := range hosts { // if the worker nodes >=3 , install OCS on all the worker nodes if they satisfy OCS requirements
			/* If the Role is set to Auto-assign for a host, it is not possible to determine whether the node will end up as a master or worker node.
			As OCS will use only worker nodes for non-compact deployments, the OCS validations cannot be performed as it cannot know which nodes will be worker nodes.
			We ignore the role check for a cluster of 3 nodes as they will all be master nodes. OCS validations will proceed as for a compact deployment.
			*/
			if host.Role == models.HostRoleAutoAssign {
				status = "All host roles must be assigned to enable OCS."
				o.log.Info("Setting Operator Status ", status)
				err = setOperatorStatus(cluster, status)
				if err != nil {
					o.log.Error("Failed to set Operator status", err)
				}
				return validationFailure
			}
			if host.Role == models.HostRoleWorker {
				inventoryMissing, err = o.nodeResources(host, &cpuCount, &totalRAM, &diskCount, &hostsWithDisks, &insufficientHosts)
				if err != nil {
					o.log.Error("Error occured while calculating Node requirements ", err)
					return validationFailure
				} else if inventoryMissing {
					return validationPending
				}

			}
		}
	}

	if len(insufficientHosts) > 0 {
		for _, hostStatus := range insufficientHosts {
			status = status + hostStatus + ".\n"
		}
		err = setOperatorStatus(cluster, status)
		if err != nil {
			o.log.Error("Failed to set Operator status ", err)
			return validationFailure
		}
		o.log.Info("Setting Operator Status ", status)
		return validationFailure
	}

	// total disks excluding boot disk must be a multiple of 3
	if diskCount%3 != 0 {
		status = "Total disks on the cluster must be a multiple of 3"
		o.log.Info(status)
		err = setOperatorStatus(cluster, status)
		if err != nil {
			o.log.Error("Failed to set Operator status ", err)
		}
		return validationFailure
	}

	// this will be used to set count of StorageDevices in StorageCluster manifest
	o.OCSDisksAvailable = diskCount
	canDeployOCS, status := o.validateOCS(o.log, hosts, cpuCount, totalRAM, diskCount, hostsWithDisks)

	o.log.Info(status)
	err = setOperatorStatus(cluster, status)
	if err != nil {
		o.log.Error("Failed to set Operator status ", err)
		return validationFailure
	}

	if canDeployOCS {
		return validationSuccess
	}
	return validationFailure
}

func (o *ocsValidator) nodeResources(host *models.Host, cpuCount *int64, totalRAM *int64, diskCount *int64, hostsWithDisks *int64, insufficientHosts *[]string) (bool, error) {
	var inventory models.Inventory
	// if inventory is empty
	if host.Inventory == "" {
		o.log.Info("Empty Inventory of host with hostID ", *host.ID)
		return true, nil // to indicate that inventory is empty and the ValidationStatus must be Pending
	} else if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return false, err
	}

	disks := getValidOCSDiskCount(inventory.Disks)

	if disks > 1 { // OCS must use the non-boot disks
		requiredDiskCPU := int64(disks-1) * o.OCSRequiredDiskCPUCount
		requiredDiskRAM := int64(disks-1) * o.OCSRequiredDiskRAMGB

		if inventory.CPU.Count < requiredDiskCPU || inventory.Memory.UsableBytes < gbToBytes(requiredDiskRAM) {
			status := fmt.Sprint("Insufficient resources on host with host ID ", *host.ID, " to deploy OCS. The hosts has ", disks, " disks that require ", requiredDiskCPU, " CPUs, ", requiredDiskRAM, " RAMGB")
			*insufficientHosts = append(*insufficientHosts, status)
			o.log.Info(status)
		} else {
			*cpuCount += (inventory.CPU.Count - requiredDiskCPU)                     // cpus excluding the cpus required for disks
			*totalRAM += (inventory.Memory.UsableBytes - gbToBytes(requiredDiskRAM)) // ram excluding the ram required for disks
			*diskCount += (int64)(disks - 1)                                         // not counting the boot disk
			*hostsWithDisks++
		}
	} else {
		*cpuCount += inventory.CPU.Count
		*totalRAM += inventory.Memory.UsableBytes
	}
	return false, nil
}

func gbToBytes(gb int64) int64 {
	return gb * int64(units.GB)
}

// count all disks of drive type ssd or hdd
func getValidOCSDiskCount(disks []*models.Disk) int {

	var countDisks int

	for _, disk := range disks {
		if disk.DriveType == ssdDrive || disk.DriveType == hddDrive {
			countDisks++
		}
	}

	return countDisks

}

// used to validate resource requirements for OCS excluding disk requirements and set a status message
func (o *ocsValidator) validateOCS(log logrus.FieldLogger, hosts []*models.Host, cpu int64, ram int64, disk int64, hostsWithDisks int64) (bool, string) {
	var TotalCPUs int64
	var TotalRAM int64
	var status string
	if int64(len(hosts)) == o.OCSRequiredHosts { // for 3 hosts
		TotalCPUs = o.OCSRequiredCompactModeCPUCount
		TotalRAM = o.OCSRequiredCompactModeRAMGB
		if cpu < TotalCPUs || ram < gbToBytes(TotalRAM) || disk < o.OCSRequiredDisk || hostsWithDisks < o.OCSRequiredHosts { // check for master nodes requirements
			status = o.setStatusInsufficientResources(cpu, ram, disk, hostsWithDisks, "Compact Mode")
			return false, status
		}
		o.OCSDeploymentType = "Compact"
		status = "OCS Requirements for Compact Mode are satisfied"
		return true, status
	}
	TotalCPUs = o.OCSMinimumCPUCount
	TotalRAM = o.OCSMinimumRAMGB
	if disk < o.OCSRequiredDisk || cpu < TotalCPUs || ram < gbToBytes(TotalRAM) || hostsWithDisks < o.OCSRequiredHosts { // check for worker nodes requirements
		status = o.setStatusInsufficientResources(cpu, ram, disk, hostsWithDisks, "Minimal Deployment Mode")
		return false, status
	}

	TotalCPUs = o.OCSRequiredCPUCount
	TotalRAM = o.OCSRequiredRAMGB
	if cpu < TotalCPUs || ram < gbToBytes(TotalRAM) { // conditions for minimal deployment
		status = "Requirements for OCS Minimal Deployment are satisfied"
		o.OCSDeploymentType = "Minimal"
		o.OCSMinimalDeployment = true
		return true, status
	}

	status = "OCS Requirements for Standard Deployment are satisfied"
	o.OCSDeploymentType = "Standard"
	return true, status
}

func (o *ocsValidator) setStatusInsufficientResources(cpu int64, ram int64, disk int64, hostsWithDisks int64, mode string) string {
	var TotalCPUs int64
	var TotalRAMGB int64
	if mode == "Compact Mode" {
		TotalCPUs = o.OCSRequiredCompactModeCPUCount
		TotalRAMGB = o.OCSRequiredCompactModeRAMGB
	} else {
		TotalCPUs = o.OCSMinimumCPUCount
		TotalRAMGB = o.OCSMinimumRAMGB
	}
	status := fmt.Sprint("Insufficient Resources to deploy OCS in ", mode, ". A minimum of ")
	if cpu < TotalCPUs {
		status = status + fmt.Sprint(TotalCPUs, " CPUs, excluding disk CPU resources ")
	}
	if ram < gbToBytes(TotalRAMGB) {
		status = status + fmt.Sprint(TotalRAMGB, " RAM, excluding disk RAM resources ")
	}
	if disk < o.OCSRequiredDisk {
		status = status + fmt.Sprint(o.OCSRequiredDisk, " Disks, ")
	}
	if hostsWithDisks < o.OCSRequiredHosts {
		status = status + fmt.Sprint(o.OCSRequiredHosts, " Hosts with disks, ")
	}
	status = status + "is required."

	return status

}
