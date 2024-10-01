package staticnetworkconfig

import (
	"github.com/openshift/assisted-service/internal/common"
	log "github.com/sirupsen/logrus"
)

const MinimalVersionForNmstatectl = "4.14"

func NMStatectlServiceSupported(version, arch string) (bool, error) {
	// When a cluster is imported, the OpenshiftVersion isn't stored in the database.
	// Consequently, a bound InfraEnv with static networking uses the Cluster's OpenshiftVersion, which is empty.
	if version == "" {
		log.Info("ocp version is empty")
		return false, nil
	}
	versionOK, err := common.VersionGreaterOrEqual(version, MinimalVersionForNmstatectl)
	if err != nil {
		return false, err
	}
	// TODO: Remove the architecture condition after fetching the nmstatectl binary from rootfs.
	return versionOK && arch == common.X86CPUArchitecture, nil
}
