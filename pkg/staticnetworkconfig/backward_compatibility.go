package staticnetworkconfig

import "github.com/openshift/assisted-service/internal/common"

const MinimalVersionForNmstatectl = "4.14"

func NMStatectlServiceSupported(version, arch string) (bool, error) {
	versionOK, err := common.VersionGreaterOrEqual(version, MinimalVersionForNmstatectl)
	if err != nil {
		return false, err
	}
	// TODO: Remove the architecture condition after fetching the nmstatectl binary from rootfs.
	return versionOK && arch == common.X86CPUArchitecture, nil
}
