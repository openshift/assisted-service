package odf

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

type Config struct {
	ODFNumMinimumDisks              int64             `envconfig:"ODF_NUM_MINIMUM_DISK" default:"3"`
	ODFDisksAvailable               int64             `envconfig:"ODF_DISKS_AVAILABLE" default:"3"`
	ODFPerDiskCPUCount              int64             `envconfig:"ODF_PER_DISK_CPU_COUNT" default:"2"` // each disk requires 2 cpus
	ODFPerDiskRAMGiB                int64             `envconfig:"ODF_PER_DISK_RAM_GIB" default:"5"`   // each disk requires 5GiB ram
	ODFNumMinimumHosts              int64             `envconfig:"ODF_NUM_MINIMUM_HOST" default:"3"`
	ODFMinimumHostsStandardMode     int64             `envconfig:"ODF_MINIMUM_HOSTS_STANDARD_MODE" default:"6"`
	ODFPerHostCPUCompactMode        int64             `envconfig:"ODF_PER_HOST_CPU_COMPACT_MODE" default:"6"`
	ODFPerHostMemoryGiBCompactMode  int64             `envconfig:"ODF_PER_HOST_MEMORY_GIB_COMPACT_MODE" default:"19"`
	ODFPerHostCPUStandardMode       int64             `envconfig:"ODF_PER_HOST_CPU_STANDARD_MODE" default:"8"`
	ODFPerHostMemoryGiBStandardMode int64             `envconfig:"ODF_PER_HOST_MEMORY_GIB_STANDARD_MODE" default:"19"`
	ODFMinDiskSizeGB                int64             `envconfig:"ODF_MIN_DISK_SIZE_GB" default:"25"`
	ODFDeploymentType               odfDeploymentMode `envconfig:"ODF_DEPLOYMENT_TYPE" default:"None"`
}

type odfClusterResourcesInfo struct {
	numberOfDisks    int64 //number of Valid disks in the cluster
	hostsWithDisks   int64 //number of hosts with Valid disk in the cluster
	missingInventory bool  //checks for the missing inventory
}

func (o *operator) validateRequirements(cluster *models.Cluster) (api.ValidationStatus, string) {
	var status string
	hosts := cluster.Hosts
	numAvailableHosts := int64(len(hosts))

	if numAvailableHosts < o.config.ODFNumMinimumHosts {
		status = "A minimum of 3 hosts is required to deploy ODF."
		return api.Failure, status
	}
	if numAvailableHosts > o.config.ODFNumMinimumHosts && numAvailableHosts < o.config.ODFMinimumHostsStandardMode {
		status = "A cluster with only 3 masters or with a minimum of 3 workers is required."
		return api.Failure, status
	}

	odfClusterResources := &odfClusterResourcesInfo{}
	status, err := o.computeResourcesAllNodes(cluster, odfClusterResources)
	if err != nil {
		if odfClusterResources.missingInventory {
			return api.Pending, status
		}
		return api.Failure, status
	}
	canDeployODF, status := o.canODFBeDeployed(hosts, odfClusterResources)

	if canDeployODF {
		// this will be used to set count of StorageDevices in StorageCluster manifest
		o.config.ODFDisksAvailable = odfClusterResources.numberOfDisks
		return api.Success, status
	}
	return api.Failure, status
}

func (o *operator) computeResourcesAllNodes(cluster *models.Cluster, odfClusterResources *odfClusterResourcesInfo) (string, error) {
	var status string
	var err error

	hosts := cluster.Hosts
	isCompactMode := int64(len(hosts)) == 3
	for _, host := range hosts { // if the worker nodes >=3 , install ODF on all the worker nodes if they satisfy ODF requirements

		/* If the Role is set to Auto-assign for a host, it is not possible to determine whether the node will end up as a master or worker node.
		As ODF will use only worker nodes for non-compact deployments, the ODF validations cannot be performed as it cannot know which nodes will be worker nodes.
		We ignore the role check for a cluster of 3 nodes as they will all be master nodes. ODF validations will proceed as for a compact deployment.
		*/

		role := common.GetEffectiveRole(host)
		if !isCompactMode {
			if role == models.HostRoleAutoAssign {
				status = "For ODF Standard Mode, all host roles must be assigned to master or worker."
				err = errors.New("Role is set to auto-assign for host ")
				return status, err
			}
			if role == models.HostRoleWorker {
				status, err = o.computeNodeResourceUtil(host, odfClusterResources, standardMode)
				if err != nil {
					return status, err
				}
			}
		} else {
			status, err = o.computeNodeResourceUtil(host, odfClusterResources, compactMode)
			if err != nil {
				return status, err
			}
		}
	}

	return status, nil
}

func (o *operator) computeNodeResourceUtil(host *models.Host, odfClusterResources *odfClusterResourcesInfo, mode odfDeploymentMode) (string, error) {
	var status string

	// if inventory is empty, return an error
	if host.Inventory == "" {
		odfClusterResources.missingInventory = true
		status = "Missing Inventory in some of the hosts"
		return status, errors.New("Missing Inventory in some of the hosts ") // to indicate that inventory is empty and the ValidationStatus must be Pending
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return status, err
	}

	diskCount, err := o.getValidDiskCount(inventory.Disks, host.InstallationDiskID, mode)
	if err != nil {
		return err.Error(), err
	}

	if diskCount > 0 {
		odfClusterResources.numberOfDisks += diskCount
		odfClusterResources.hostsWithDisks++
	}
	return status, nil
}

// used to validate resource requirements for ODF
func (o *operator) canODFBeDeployed(hosts []*models.Host, odfClusterResources *odfClusterResourcesInfo) (bool, string) {
	var status string
	numAvailableHosts := int64(len(hosts))

	if numAvailableHosts == o.config.ODFNumMinimumHosts { // for 3 hosts
		if !validateRequirements(o, odfClusterResources) { // check for master nodes requirements
			status = o.setStatusInsufficientResources(odfClusterResources, compactMode)
			return false, status
		}
		o.config.ODFDeploymentType = compactMode
		status = "ODF Requirements for Compact Mode are satisfied."
		return true, status
	}

	if !validateRequirements(o, odfClusterResources) { // check for worker nodes requirement
		status = o.setStatusInsufficientResources(odfClusterResources, standardMode)
		return false, status
	}
	status = "ODF Requirements for Standard Deployment are satisfied."
	o.config.ODFDeploymentType = standardMode
	return true, status
}

func validateRequirements(o *operator, odfClusterResources *odfClusterResourcesInfo) bool {
	return odfClusterResources.numberOfDisks >= o.config.ODFNumMinimumDisks && odfClusterResources.hostsWithDisks >= o.config.ODFNumMinimumHosts
}

func (o *operator) setStatusInsufficientResources(odfClusterResources *odfClusterResourcesInfo, mode odfDeploymentMode) string {
	status := fmt.Sprint("Insufficient resources to deploy ODF in ", strings.ToLower(string(mode)), " mode. ")

	if odfClusterResources.numberOfDisks < o.config.ODFNumMinimumDisks || odfClusterResources.hostsWithDisks < o.config.ODFNumMinimumHosts {
		status = status + fmt.Sprint("ODF requires a minimum of ", o.config.ODFNumMinimumHosts, " hosts. Each host must have at least 1 additional disk of ", o.config.ODFMinDiskSizeGB, " GB minimum and an installation disk.")
	}

	return status
}

// count all disks of drive type ssd or hdd
func (o *operator) getValidDiskCount(disks []*models.Disk, installationDiskID string, mode odfDeploymentMode) (int64, error) {
	var countDisks int64
	var smallDisks int64

	for _, disk := range disks {
		if (disk.DriveType == models.DriveTypeSSD || disk.DriveType == models.DriveTypeHDD) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			if disk.SizeBytes >= conversions.GbToBytes(o.config.ODFMinDiskSizeGB) {
				countDisks++
			} else {
				smallDisks++
			}
		}
	}
	if countDisks == 0 && smallDisks != 0 {
		return countDisks, fmt.Errorf("Insufficient resources to deploy ODF in %s mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of %d GB minimum and an installation disk.", strings.ToLower(string(mode)), o.config.ODFMinDiskSizeGB)
	}
	return countDisks, nil
}
