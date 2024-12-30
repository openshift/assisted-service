package odf

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

type odfClusterResourcesInfo struct {
	numberOfDisks    int64 //number of Valid disks in the cluster
	hostsWithDisks   int64 //number of hosts with Valid disk in the cluster
	missingInventory bool  //checks for the missing inventory
}

func (o *operator) validateRequirements(cluster *models.Cluster) (api.ValidationStatus, string) {
	var status string
	log := o.log

	if cluster.ID != nil {
		log = log.WithField("cluster", cluster.ID.String())
	}

	mode := getODFDeploymentMode(cluster, o.config.ODFNumMinimumHosts)
	log.Debugf("ODF validate cluster - mode: %s", string(mode))

	if mode == unknown {
		status = "The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."
		return api.Failure, status
	}

	odfClusterResources := &odfClusterResourcesInfo{}
	status, err := o.computeResourcesAllNodes(cluster, odfClusterResources, mode)
	if err != nil {
		if odfClusterResources.missingInventory {
			return api.Pending, status
		}
		return api.Failure, status
	}

	canDeployODF, status := o.canODFBeDeployed(odfClusterResources, mode)
	if canDeployODF {
		return api.Success, status
	}

	return api.Failure, status
}

func (o *operator) computeResourcesAllNodes(
	cluster *models.Cluster,
	odfClusterResources *odfClusterResourcesInfo,
	mode odfDeploymentMode,
) (string, error) {
	for _, host := range cluster.Hosts {
		hostEffectiveRole := common.GetEffectiveRole(host)

		// We want to consider only hosts that should run ODF workloads.
		shouldHostRunODF, failureReason := shouldHostRunODF(cluster, mode, hostEffectiveRole)
		if failureReason != nil || shouldHostRunODF == nil || !*shouldHostRunODF {
			continue
		}

		status, err := o.computeNodeResourceUtil(host, odfClusterResources, mode)
		if err != nil {
			return status, err
		}
	}

	return "", nil
}

func (o *operator) computeNodeResourceUtil(
	host *models.Host,
	odfClusterResources *odfClusterResourcesInfo,
	mode odfDeploymentMode,
) (string, error) {
	var status string

	// if inventory is empty, return an error
	if host.Inventory == "" {
		odfClusterResources.missingInventory = true
		status = "Missing Inventory in some of the hosts"
		return status, errors.New("Missing Inventory in some of the hosts ") // to indicate that inventory is empty and the ValidationStatus must be Pending
	}

	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		status = "Failed to parse the inventory of some of the hosts"
		return status, err
	}

	diskCount, err := o.getValidDiskCount(inventory.Disks, host.InstallationDiskID, nil, mode)
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
func (o *operator) canODFBeDeployed(
	odfClusterResources *odfClusterResourcesInfo,
	mode odfDeploymentMode,
) (bool, string) {
	if !validateRequirements(o, odfClusterResources) { // check for master nodes requirements
		return false, o.setStatusInsufficientResources(odfClusterResources, mode)
	}

	return true, fmt.Sprintf("ODF Requirements for %s Deployment are satisfied.", mode)
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
