package ocs

import (
	"errors"
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

type Config struct {
	OCSNumMinimumDisks              int64             `envconfig:"OCS_NUM_MINIMUM_DISK" default:"3"`
	OCSDisksAvailable               int64             `envconfig:"OCS_DISKS_AVAILABLE" default:"3"`
	OCSPerDiskCPUCount              int64             `envconfig:"OCS_PER_DISK_CPU_COUNT" default:"2"` // each disk requires 2 cpus
	OCSPerDiskRAMGiB                int64             `envconfig:"OCS_PER_DISK_RAM_GIB" default:"5"`   // each disk requires 5GiB ram
	OCSNumMinimumHosts              int64             `envconfig:"OCS_NUM_MINIMUM_HOST" default:"3"`
	OCSMinimumHostsStandardMode     int64             `envconfig:"OCS_MINIMUM_HOSTS_STANDARD_MODE" default:"6"`
	OCSPerHostCPUCompactMode        int64             `envconfig:"OCS_PER_HOST_CPU_COMPACT_MODE" default:"6"`
	OCSPerHostMemoryGiBCompactMode  int64             `envconfig:"OCS_PER_HOST_MEMORY_GIB_COMPACT_MODE" default:"19"`
	OCSPerHostCPUStandardMode       int64             `envconfig:"OCS_PER_HOST_CPU_STANDARD_MODE" default:"8"`
	OCSPerHostMemoryGiBStandardMode int64             `envconfig:"OCS_PER_HOST_MEMORY_GIB_STANDARD_MODE" default:"19"`
	OCSMinDiskSizeGB                int64             `envconfig:"OCS_MIN_DISK_SIZE_GB" default:"25"`
	OCSDeploymentType               ocsDeploymentMode `envconfig:"OCS_DEPLOYMENT_TYPE" default:"None"`
}

type ocsClusterResourcesInfo struct {
	numberOfDisks    int64 //number of Valid disks in the cluster
	hostsWithDisks   int64 //number of hosts with Valid disk in the cluster
	missingInventory bool  //checks for the missing inventory
}

func (o *operator) validateRequirements(cluster *models.Cluster) (api.ValidationStatus, string) {
	var status string
	hosts := cluster.Hosts
	numAvailableHosts := int64(len(hosts))

	if numAvailableHosts < o.config.OCSNumMinimumHosts {
		status = "A minimum of 3 hosts is required to deploy OCS."
		return api.Failure, status
	}
	if numAvailableHosts > o.config.OCSNumMinimumHosts && numAvailableHosts < o.config.OCSMinimumHostsStandardMode {
		status = "A cluster with only 3 masters or with a minimum of 3 workers is required."
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
				status = "For OCS Standard Mode, all host roles must be assigned to master or worker."
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
	var status string

	// if inventory is empty, return an error
	if host.Inventory == "" {
		ocsClusterResources.missingInventory = true
		status = "Missing Inventory in some of the hosts"
		return status, errors.New("Missing Inventory in some of the hosts ") // to indicate that inventory is empty and the ValidationStatus must be Pending
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return status, err
	}

	diskCount, err := o.getValidDiskCount(inventory.Disks, host.InstallationDiskID)
	if err != nil {
		return err.Error(), err
	}

	if diskCount > 0 {
		ocsClusterResources.numberOfDisks += diskCount
		ocsClusterResources.hostsWithDisks++
	}
	return status, nil
}

// used to validate resource requirements for OCS
func (o *operator) canOCSBeDeployed(hosts []*models.Host, ocsClusterResources *ocsClusterResourcesInfo) (bool, string) {
	var status string
	numAvailableHosts := int64(len(hosts))

	if numAvailableHosts == o.config.OCSNumMinimumHosts { // for 3 hosts
		if !validateRequirements(o, ocsClusterResources) { // check for master nodes requirements
			status = o.setStatusInsufficientResources(ocsClusterResources, compactMode)
			return false, status
		}
		o.config.OCSDeploymentType = compactMode
		status = "OCS Requirements for Compact Mode are satisfied."
		return true, status
	}

	if !validateRequirements(o, ocsClusterResources) { // check for worker nodes requirement
		status = o.setStatusInsufficientResources(ocsClusterResources, standardMode)
		return false, status
	}
	status = "OCS Requirements for Standard Deployment are satisfied."
	o.config.OCSDeploymentType = standardMode
	return true, status
}

func validateRequirements(o *operator, ocsClusterResources *ocsClusterResourcesInfo) bool {
	return ocsClusterResources.numberOfDisks >= o.config.OCSNumMinimumDisks && ocsClusterResources.hostsWithDisks >= o.config.OCSNumMinimumHosts
}

func (o *operator) setStatusInsufficientResources(ocsClusterResources *ocsClusterResourcesInfo, mode ocsDeploymentMode) string {
	status := fmt.Sprint("Insufficient Resources to deploy OCS in ", string(mode), " Mode. ")

	if ocsClusterResources.numberOfDisks < o.config.OCSNumMinimumDisks || ocsClusterResources.hostsWithDisks < o.config.OCSNumMinimumHosts {
		status = status + fmt.Sprint("OCS requires a minimum of ", o.config.OCSNumMinimumHosts, " hosts with one non-bootable disk on each host of size at least ", o.config.OCSMinDiskSizeGB, " GB.")
	}

	return status
}

// count all disks of drive type ssd or hdd
func (o *operator) getValidDiskCount(disks []*models.Disk, installationDiskID string) (int64, error) {
	var countDisks int64
	var err error

	for _, disk := range disks {
		if (disk.DriveType == ssdDrive || disk.DriveType == hddDrive) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			if disk.SizeBytes < conversions.GbToBytes(o.config.OCSMinDiskSizeGB) {
				err = fmt.Errorf("OCS requires all the non-bootable disks to be more than %d GB", o.config.OCSMinDiskSizeGB)
			} else {
				countDisks++
			}
		}
	}
	return countDisks, err
}
