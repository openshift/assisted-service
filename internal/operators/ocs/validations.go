package ocs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

type Config struct {
	OCSNumMinimumDisks                    int64             `envconfig:"OCS_NUM_MINIMUM_DISK" default:"3"`
	OCSDisksAvailable                     int64             `envconfig:"OCS_DISKS_AVAILABLE" default:"3"`
	OCSPerDiskCPUCount                    int64             `envconfig:"OCS_PER_DISK_CPU_COUNT" default:"2"` // each disk requires 2 cpus
	OCSPerDiskRAMGiB                      int64             `envconfig:"OCS_PER_DISK_RAM_GiB" default:"5"`   // each disk requires 5GiB ram
	OCSNumMinimumHosts                    int64             `envconfig:"OCS_NUM_MINIMUM_HOST" default:"3"`
	OCSCompactModeClusterCPUCount         int64             `envconfig:"OCS_COMPACT_MODE_CLUSTER_CPU_COUNT" default:"18"`
	OCSCompactModeClusterRAMGiB           int64             `envconfig:"OCS_COMPACT_MODE_CLUSTER_RAM_GiB" default:"57"`
	OCSMinimalModeClusterCPUCount         int64             `envconfig:"OCS_MINIMAL_MODE_CLUSTER_CPU_COUNT" default:"12"`
	OCSMinimalModeClusterRAMGiB           int64             `envconfig:"OCS_MINIMAL_MODE_CLUSTER_RAM_GiB" default:"57"`
	OCSStandardModeClusterCPUCount        int64             `envconfig:"OCS_STANDARD_MODE_CLUSTER_CPU_COUNT" default:"24"`
	OCSStandardModeClusterRAMGiB          int64             `envconfig:"OCS_STANDARD_MODE_CLUSTER_RAM_GiB" default:"57"`
	OCSMinimumHostsMinimalAndStandardMode int64             `envconfig:"OCS_MINIMUM_HOSTS_MINIMAL_AND_STANDARD_MODE" default:"6"`
	OCSDeploymentType                     ocsDeploymentMode `envconfig:"OCS_DEPLOYMENT_TYPE" default:"None"`
	// OCP Resource Elements are temporary and will be removed once we can query those values
	OCPMinCPUCoresWorker int64 `envconfig:"OCP_MIN_CPU_CORES_WORKER" default:"2"`
	OCPMinCPUCoresMaster int64 `envconfig:"OCP_MIN_CPU_CORES_MASTER" default:"4"`
	OCPMinRamGibWorker   int64 `envconfig:"OCP_MIN_RAM_GIB_WORKER" default:"8"`
	OCPMinRamGibMaster   int64 `envconfig:"OCP_MIN_RAM_GIB_MASTER" default:"16"`
}

type ocsClusterResourcesInfo struct {
	cpuCount          int64    //stores the total number of cpu cores in the cluster excluding the cpus required for disks
	ram               int64    //stores the total ram in the cluster excluding the ram required for disks
	numberOfDisks     int64    //number of Valid disks in the cluster
	hostsWithDisks    int64    //number of hosts with Valid disk in the cluster
	numberOfHosts     int64    //number of hosts in the cluster
	insufficientHosts []string //status for the insufficient hosts
	missingInventory  bool     //checks for the missing inventory
}

func (o *operator) validateClusterRequirements(cluster *models.Cluster) (api.ValidationStatus, string) {
	var status string
	hosts := cluster.Hosts
	numAvailableHosts := int64(len(hosts))

	// TODO: Remove this validation once OCS 4.7 is GA
	if strings.HasPrefix(cluster.OpenshiftVersion, "4.8") {
		status = "OCS is not supported on OCP 4.8."
		o.log.Info("OCS requirements validation status ", status)
		return api.Failure, status
	}

	if numAvailableHosts < o.config.OCSNumMinimumHosts {
		status = "Insufficient hosts to deploy OCS. A minimum of 3 hosts is required to deploy OCS."
		o.log.Info("OCS requirements validation status ", status)
		return api.Failure, status
	}
	if numAvailableHosts > o.config.OCSNumMinimumHosts && numAvailableHosts < o.config.OCSMinimumHostsMinimalAndStandardMode {
		status = "Insufficient hosts for OCS installation. A cluster with only 3 masters or with a minimum of 3 workers is required"
		o.log.Info("OCS requirements validation status ", status)
		return api.Failure, status
	}

	ocsClusterResources := &ocsClusterResourcesInfo{}
	status, err := o.computeResourcesAllNodes(cluster, ocsClusterResources)
	if err != nil {
		if ocsClusterResources.missingInventory {
			return api.Pending, status
		}
		return api.Failure, status
	}
	canDeployOCS, status := o.canOCSBeDeployed(hosts, ocsClusterResources)

	o.log.Info(status)

	if canDeployOCS {
		// this will be used to set count of StorageDevices in StorageCluster manifest
		o.config.OCSDisksAvailable = ocsClusterResources.numberOfDisks
		return api.Success, status
	}
	return api.Failure, status
}

func (o *operator) computeResourcesAllNodes(cluster *models.Cluster, ocsClusterResources *ocsClusterResourcesInfo) (string, error) {
	var status string
	var err error

	hosts := cluster.Hosts
	compactMode := int64(len(hosts)) == 3
	for _, host := range hosts { // if the worker nodes >=3 , install OCS on all the worker nodes if they satisfy OCS requirements

		/* If the Role is set to Auto-assign for a host, it is not possible to determine whether the node will end up as a master or worker node.
		As OCS will use only worker nodes for non-compact deployments, the OCS validations cannot be performed as it cannot know which nodes will be worker nodes.
		We ignore the role check for a cluster of 3 nodes as they will all be master nodes. OCS validations will proceed as for a compact deployment.
		*/
		if !compactMode {
			if host.Role == models.HostRoleAutoAssign {
				status = "All host roles must be assigned to enable OCS."
				o.log.Info("Validate Requirements status ", status)
				err = errors.New("Role is set to auto-assign for host ")
				return status, err
			}
			if host.Role == models.HostRoleWorker {
				status, err = o.computeNodeResourceUtil(host, ocsClusterResources)
				if err != nil {
					return status, err
				}
			}
		} else {
			status, err = o.computeNodeResourceUtil(host, ocsClusterResources)
			if err != nil {
				return status, err
			}
		}
	}

	return status, nil
}

func (o *operator) computeNodeResourceUtil(host *models.Host, ocsClusterResources *ocsClusterResourcesInfo) (string, error) {
	var inventory models.Inventory
	var status string

	// if inventory is empty, return an error
	if host.Inventory == "" {
		o.log.Info("Empty Inventory for host with hostID ", *host.ID)
		ocsClusterResources.missingInventory = true
		status = "Missing Inventory in some of the hosts"
		return status, errors.New("Missing Inventory in some of the hosts ") // to indicate that inventory is empty and the ValidationStatus must be Pending
	} else if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return status, err
	}

	disks := getValidDiskCount(inventory.Disks)
	if disks > 1 { // OCS must use only the non-boot disks
		requiredDiskCPU := int64(disks-1) * o.config.OCSPerDiskCPUCount
		requiredDiskRAM := int64(disks-1) * o.config.OCSPerDiskRAMGiB

		if inventory.CPU.Count < requiredDiskCPU || inventory.Memory.UsableBytes < conversions.GibToBytes(requiredDiskRAM) {
			status = fmt.Sprint("Insufficient resources on host with host ID ", *host.ID, " to deploy OCS. The hosts has ", disks, " disks that require ", requiredDiskCPU, " CPUs, ", requiredDiskRAM, " GiB RAM")
			ocsClusterResources.insufficientHosts = append(ocsClusterResources.insufficientHosts, status)
			o.log.Info(status)
		} else {
			ocsClusterResources.cpuCount += inventory.CPU.Count - requiredDiskCPU                             // cpus excluding the cpus required for disks
			ocsClusterResources.ram += inventory.Memory.UsableBytes - conversions.GibToBytes(requiredDiskRAM) // ram excluding the ram required for disks
			ocsClusterResources.numberOfDisks += (int64)(disks - 1)                                           // not counting the boot disk
			ocsClusterResources.hostsWithDisks++
		}
	} else {
		ocsClusterResources.cpuCount += inventory.CPU.Count
		ocsClusterResources.ram += inventory.Memory.UsableBytes
	}
	ocsClusterResources.numberOfHosts++
	return status, nil
}

// used to validate resource requirements for OCS
func (o *operator) canOCSBeDeployed(hosts []*models.Host, ocsClusterResources *ocsClusterResourcesInfo) (bool, string) {
	var status string
	numAvailableHosts := int64(len(hosts))

	if len(ocsClusterResources.insufficientHosts) > 0 {
		for _, hostStatus := range ocsClusterResources.insufficientHosts {
			status = status + hostStatus + ".\n"
		}
		o.log.Info("Validate Requirements status ", status)
		return false, status
	}

	if ocsClusterResources.numberOfDisks%3 != 0 {
		status = fmt.Sprint("The number of disks in the cluster for OCS must be a multiple of 3 with a minimum size of ", MinDiskSize, " GB")
		o.log.Info(status)
		return false, status
	}

	if numAvailableHosts == o.config.OCSNumMinimumHosts { // for 3 hosts
		if !validateRequirementsForMode(o, ocsClusterResources, compactMode) { // check for master nodes requirements
			status = o.setStatusInsufficientResources(ocsClusterResources, compactMode)
			return false, status
		}
		o.config.OCSDeploymentType = compactMode
		status = "OCS Requirements for Compact Mode are satisfied"
		return true, status
	}

	if !validateRequirementsForMode(o, ocsClusterResources, standardMode) { // check for worker nodes requirement
		if !validateRequirementsForMode(o, ocsClusterResources, minimalMode) { // check for worker nodes requirements
			status = o.setStatusInsufficientResources(ocsClusterResources, minimalMode)
			return false, status
		}
		status = "Requirements for OCS Minimal Deployment are satisfied"
		o.config.OCSDeploymentType = minimalMode
		return true, status
	}
	status = "OCS Requirements for Standard Deployment are satisfied"
	o.config.OCSDeploymentType = standardMode
	return true, status
}

func validateRequirementsForMode(o *operator, ocsClusterResources *ocsClusterResourcesInfo, mode ocsDeploymentMode) bool {
	var totalCPUs int64
	var totalRAM int64

	if mode == compactMode {
		totalCPUs = o.config.OCSCompactModeClusterCPUCount + (o.config.OCPMinCPUCoresMaster * ocsClusterResources.numberOfHosts)
		totalRAM = o.config.OCSCompactModeClusterRAMGiB + (o.config.OCPMinRamGibMaster * ocsClusterResources.numberOfHosts)
	} else if mode == minimalMode {
		totalCPUs = o.config.OCSMinimalModeClusterCPUCount + (o.config.OCPMinCPUCoresWorker * ocsClusterResources.numberOfHosts)
		totalRAM = o.config.OCSMinimalModeClusterRAMGiB + (o.config.OCPMinRamGibWorker * ocsClusterResources.numberOfHosts)
	} else {
		totalCPUs = o.config.OCSStandardModeClusterCPUCount + (o.config.OCPMinCPUCoresWorker * ocsClusterResources.numberOfHosts)
		totalRAM = o.config.OCSStandardModeClusterRAMGiB + (o.config.OCPMinRamGibWorker * ocsClusterResources.numberOfHosts)
	}
	return ocsClusterResources.cpuCount >= totalCPUs && ocsClusterResources.ram >= conversions.GibToBytes(totalRAM) && ocsClusterResources.numberOfDisks >= o.config.OCSNumMinimumDisks && ocsClusterResources.hostsWithDisks >= o.config.OCSNumMinimumHosts
}

func (o *operator) setStatusInsufficientResources(ocsClusterResources *ocsClusterResourcesInfo, mode ocsDeploymentMode) string {
	var totalCPUs int64
	var totalRAMGiB int64
	if mode == compactMode {
		totalCPUs = o.config.OCSCompactModeClusterCPUCount + (o.config.OCPMinCPUCoresMaster * ocsClusterResources.numberOfHosts)
		totalRAMGiB = o.config.OCSCompactModeClusterRAMGiB + (o.config.OCPMinRamGibMaster * ocsClusterResources.numberOfHosts)
	} else {
		totalCPUs = o.config.OCSMinimalModeClusterCPUCount + (o.config.OCPMinCPUCoresWorker * ocsClusterResources.numberOfHosts)
		totalRAMGiB = o.config.OCSMinimalModeClusterRAMGiB + (o.config.OCPMinRamGibWorker * ocsClusterResources.numberOfHosts)
	}
	status := fmt.Sprint("Insufficient Resources to deploy OCS in ", string(mode), " Mode. A minimum of ")
	if ocsClusterResources.cpuCount < totalCPUs {
		status = status + fmt.Sprint(totalCPUs, " CPUs, excluding disk CPU resources ")
	}
	if ocsClusterResources.ram < conversions.GibToBytes(totalRAMGiB) {
		status = status + fmt.Sprint(totalRAMGiB, " GiB RAM, excluding disk RAM resources ")
	}
	if ocsClusterResources.numberOfDisks < o.config.OCSNumMinimumDisks {
		status = status + fmt.Sprint(o.config.OCSNumMinimumDisks, " Disks of minimum ", MinDiskSize, "GB is required ")
	}
	if ocsClusterResources.hostsWithDisks < o.config.OCSNumMinimumHosts {
		status = status + fmt.Sprint(o.config.OCSNumMinimumHosts, " Hosts with disks, ")
	}
	status = status + "is required."

	return status
}

// count all disks of drive type ssd or hdd
func getValidDiskCount(disks []*models.Disk) int {
	var countDisks int

	for _, disk := range disks {
		if disk.SizeBytes >= conversions.GbToBytes(MinDiskSize) && (disk.DriveType == ssdDrive || disk.DriveType == hddDrive) {
			countDisks++
		}
	}
	return countDisks
}
