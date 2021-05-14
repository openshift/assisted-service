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
	OCSMinimumCPUCount                int64             `envconfig:"OCS_MINIMUM_CPU_COUNT" default:"18"`
	OCSMinimumRAMGiB                  int64             `envconfig:"OCS_MINIMUM_RAM_GiB" default:"57"`
	OCSRequiredDisk                   int64             `envconfig:"OCS_MINIMUM_DISK" default:"3"`
	OCSRequiredDiskCPUCount           int64             `envconfig:"OCS_REQUIRED_DISK_CPU_COUNT" default:"2"` // each disk requires 2 cpus
	OCSRequiredDiskRAMGiB             int64             `envconfig:"OCS_REQUIRED_DISK_RAM_GiB" default:"5"`   // each disk requires 5GiB ram
	OCSRequiredHosts                  int64             `envconfig:"OCS_MINIMUM_HOST" default:"3"`
	OCSStandardAndCompactModeCPUCount int64             `envconfig:"OCS_STANDARD_AND_COMPACT_MODE_CPU_COUNT" default:"30"` // Compact Mode cpu requirements for 3 nodes( 4*3(master)+6*3(OCS) cpus)
	OCSStandardAndCompactModeRAMGiB   int64             `envconfig:"OCS_STANDARD_AND_COMPACT_MODE_RAM_GiB" default:"81"`   // Compact Mode ram requirements (8*3(master)+57(OCS) GiB)
	OCSMasterWorkerHosts              int64             `envconfig:"OCS_REQUIRED_MASTER_WORKER_HOSTS" default:"6"`
	OCSDisksAvailable                 int64             `envconfig:"OCS_DISKS_AVAILABLE" default:"3"`
	OCSDeploymentType                 ocsDeploymentMode `envconfig:"OCS_DEPLOYMENT_TYPE" default:"None"`
}

type resourceInfo struct {
	cpuCount          int64
	ram               int64
	numDisks          int64
	hostsWithDisks    int64
	insufficientHosts []string
	missingInventory  bool
}

func (o *operator) validateRequirements(cluster *models.Cluster) (api.ValidationStatus, string) {
	var status string
	hosts := cluster.Hosts
	numAvailableHosts := int64(len(hosts))

	// TODO: Remove this validation once OCS 4.7 is GA
	if strings.HasPrefix(cluster.OpenshiftVersion, "4.8") {
		status = "OCS is not supported on OCP 4.8."
		o.log.Info("OCS requirements validation status ", status)
		return api.Failure, status
	}

	if numAvailableHosts < o.config.OCSRequiredHosts {
		status = "Insufficient hosts to deploy OCS. A minimum of 3 hosts is required to deploy OCS."
		o.log.Info("OCS requirements validation status ", status)
		return api.Failure, status
	}
	if numAvailableHosts > o.config.OCSRequiredHosts && numAvailableHosts < o.config.OCSMasterWorkerHosts {
		status = "Insufficient hosts for OCS installation. A cluster with only 3 masters or with a minimum of 3 workers is required"
		o.log.Info("OCS requirements validation status ", status)
		return api.Failure, status
	}

	nodesResourceInfo := &resourceInfo{}
	status, err := o.computeResourcesAllNodes(cluster, nodesResourceInfo)
	if err != nil {
		if nodesResourceInfo.missingInventory {
			return api.Pending, status
		}
		return api.Failure, status
	}
	canDeployOCS, status := o.canOCSBeDeployed(hosts, nodesResourceInfo)

	o.log.Info(status)

	if canDeployOCS {
		// this will be used to set count of StorageDevices in StorageCluster manifest
		o.config.OCSDisksAvailable = nodesResourceInfo.numDisks
		return api.Success, status
	}
	return api.Failure, status
}

func (o *operator) computeResourcesAllNodes(cluster *models.Cluster, nodeInfo *resourceInfo) (string, error) {
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
				status, err = o.computeNodeResourceUtil(host, nodeInfo)
				if err != nil {
					return status, err
				}
			}
		} else {
			status, err = o.computeNodeResourceUtil(host, nodeInfo)
			if err != nil {
				return status, err
			}
		}
	}

	return status, nil
}

func (o *operator) computeNodeResourceUtil(host *models.Host, nodeInfo *resourceInfo) (string, error) {
	var inventory models.Inventory
	var status string

	// if inventory is empty, return an error
	if host.Inventory == "" {
		o.log.Info("Empty Inventory for host with hostID ", *host.ID)
		nodeInfo.missingInventory = true
		status = "Missing Inventory in some of the hosts"
		return status, errors.New("Missing Inventory in some of the hosts ") // to indicate that inventory is empty and the ValidationStatus must be Pending
	} else if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return status, err
	}

	disks := getValidDiskCount(inventory.Disks)
	if disks > 1 { // OCS must use only the non-boot disks
		requiredDiskCPU := int64(disks-1) * o.config.OCSRequiredDiskCPUCount
		requiredDiskRAM := int64(disks-1) * o.config.OCSRequiredDiskRAMGiB

		if inventory.CPU.Count < requiredDiskCPU || inventory.Memory.UsableBytes < conversions.GibToBytes(requiredDiskRAM) {
			status = fmt.Sprint("Insufficient resources on host with host ID ", *host.ID, " to deploy OCS. The hosts has ", disks, " disks that require ", requiredDiskCPU, " CPUs, ", requiredDiskRAM, " GiB RAM")
			nodeInfo.insufficientHosts = append(nodeInfo.insufficientHosts, status)
			o.log.Info(status)
		} else {
			nodeInfo.cpuCount += inventory.CPU.Count - requiredDiskCPU                             // cpus excluding the cpus required for disks
			nodeInfo.ram += inventory.Memory.UsableBytes - conversions.GibToBytes(requiredDiskRAM) // ram excluding the ram required for disks
			nodeInfo.numDisks += (int64)(disks - 1)                                                // not counting the boot disk
			nodeInfo.hostsWithDisks++
		}
	} else {
		nodeInfo.cpuCount += inventory.CPU.Count
		nodeInfo.ram += inventory.Memory.UsableBytes
	}
	return status, nil
}

// used to validate resource requirements for OCS
func (o *operator) canOCSBeDeployed(hosts []*models.Host, nodeInfo *resourceInfo) (bool, string) {
	var status string
	numAvailableHosts := int64(len(hosts))

	if len(nodeInfo.insufficientHosts) > 0 {
		for _, hostStatus := range nodeInfo.insufficientHosts {
			status = status + hostStatus + ".\n"
		}
		o.log.Info("Validate Requirements status ", status)
		return false, status
	}

	if nodeInfo.numDisks%3 != 0 {
		status = fmt.Sprint("The number of disks in the cluster for OCS must be a multiple of 3 with a minimum size of ", MinDiskSize, " GB")
		o.log.Info(status)
		return false, status
	}

	if numAvailableHosts == o.config.OCSRequiredHosts { // for 3 hosts
		if !validateRequirementsForMode(o, nodeInfo, compactMode) { // check for master nodes requirements
			status = o.setStatusInsufficientResources(nodeInfo, compactMode)
			return false, status
		}
		o.config.OCSDeploymentType = compactMode
		status = "OCS Requirements for Compact Mode are satisfied"
		return true, status
	}

	if !validateRequirementsForMode(o, nodeInfo, standardMode) { // check for worker nodes requirement
		if !validateRequirementsForMode(o, nodeInfo, minimalMode) { // check for worker nodes requirements
			status = o.setStatusInsufficientResources(nodeInfo, minimalMode)
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

func validateRequirementsForMode(o *operator, nodeInfo *resourceInfo, mode ocsDeploymentMode) bool {
	var totalCPUs int64
	var totalRAM int64

	if mode == compactMode || mode == standardMode {
		totalCPUs = o.config.OCSStandardAndCompactModeCPUCount
		totalRAM = o.config.OCSStandardAndCompactModeRAMGiB
	} else {
		totalCPUs = o.config.OCSMinimumCPUCount
		totalRAM = o.config.OCSMinimumRAMGiB
	}
	return nodeInfo.cpuCount >= totalCPUs && nodeInfo.ram >= conversions.GibToBytes(totalRAM) && nodeInfo.numDisks >= o.config.OCSRequiredDisk && nodeInfo.hostsWithDisks >= o.config.OCSRequiredHosts
}

func (o *operator) setStatusInsufficientResources(nodeInfo *resourceInfo, mode ocsDeploymentMode) string {
	var totalCPUs int64
	var totalRAMGiB int64
	if mode == compactMode {
		totalCPUs = o.config.OCSStandardAndCompactModeCPUCount
		totalRAMGiB = o.config.OCSStandardAndCompactModeRAMGiB
	} else {
		totalCPUs = o.config.OCSMinimumCPUCount
		totalRAMGiB = o.config.OCSMinimumRAMGiB
	}
	status := fmt.Sprint("Insufficient Resources to deploy OCS in ", string(mode), " Mode. A minimum of ")
	if nodeInfo.cpuCount < totalCPUs {
		status = status + fmt.Sprint(totalCPUs, " CPUs, excluding disk CPU resources ")
	}
	if nodeInfo.ram < conversions.GibToBytes(totalRAMGiB) {
		status = status + fmt.Sprint(totalRAMGiB, " GiB RAM, excluding disk RAM resources ")
	}
	if nodeInfo.numDisks < o.config.OCSRequiredDisk {
		status = status + fmt.Sprint(o.config.OCSRequiredDisk, " Disks of minimum ", MinDiskSize, "GB is required ")
	}
	if nodeInfo.hostsWithDisks < o.config.OCSRequiredHosts {
		status = status + fmt.Sprint(o.config.OCSRequiredHosts, " Hosts with disks, ")
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
