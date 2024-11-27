package odf

import (
	"errors"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

func getODFDeploymentMode(cluster *models.Cluster, odfMinimumHosts int64) odfDeploymentMode {
	masterHosts, workerHosts, autoAssignHosts := common.GetHostsByEachRole(cluster, true)

	masterCount := len(masterHosts)
	workerCount := len(workerHosts)
	autoAssignCount := len(autoAssignHosts)
	hostCount := masterCount + workerCount + autoAssignCount

	// To keep compatability with the behaviour until now.
	if hostCount == common.MinMasterHostsNeededForInstallationInHaMode && workerCount == 0 {
		return compactMode
	}

	if masterCount == hostCount && masterCount >= int(odfMinimumHosts) {
		return compactMode
	}

	if masterCount >= common.MinMasterHostsNeededForInstallationInHaMode && workerCount >= int(odfMinimumHosts) {
		return standardMode
	}

	// To be determined, the cluster is not yet in a valid form.
	return unknown
}

// To keep compatability with the behaviour until now.
// TODO - remove this once two control plane nodes OpenShift is implemented
func isAutoAssignmentAllowed(cluster *models.Cluster) bool {
	return len(cluster.Hosts) == common.MinMasterHostsNeededForInstallationInHaMode
}

// shouldHostRunODF returns:
//   - nil, nil - If the deployment mode is not known yet.
//   - nil, error - If there the host's role is auto-assign but it is not allowed.
//   - *bool, nil - If the cluster and host are suitable for checking whether the host will run ODF workloads.
func shouldHostRunODF(cluster *models.Cluster, mode odfDeploymentMode, hostEffectiveRole models.HostRole) (*bool, error) {
	// No answer yet, we return nil and let the caller handle it in its context
	if mode == unknown {
		return nil, nil
	}

	// This is not allowed as there are deployment configurations
	// of assisted-service where we can't tell what role will the host get eventually.
	if !isAutoAssignmentAllowed(cluster) && hostEffectiveRole == models.HostRoleAutoAssign {
		return nil, errors.New("for ODF with more than 3 hosts, role must be assigned to master or worker")
	}

	return swag.Bool(mode == compactMode && (hostEffectiveRole == models.HostRoleMaster || hostEffectiveRole == models.HostRoleAutoAssign) ||
		mode == standardMode && (hostEffectiveRole == models.HostRoleWorker)), nil
}
