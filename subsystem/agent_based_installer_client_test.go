package subsystem

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/cmd/agentbasedinstaller"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

// Note: utils_test.TestContext.UserBMClient is used because subsystems defaults to use "rhsso" as AUTH_TYPE.
// The ephermeral installer environment will use the "none" AUTH_TYPE at the start, and
// a pre-generated infra-env-id will be used when creating the infra-env.
// A new authentication scheme suited to the agent-based installer will be implemented
// in the future and utils_test.TestContext.UserBMClient should be replaced at that time.
var _ = Describe("RegisterClusterAndInfraEnv", func() {
	var (
		ctx         = context.Background()
		tmpDir      string
		tmpCrdsDir  string
		originalDir = "../docs/hive-integration/crds"
		origWD      string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "testenv")
		Expect(err).NotTo(HaveOccurred())

		tmpCrdsDir = filepath.Join(tmpDir, "crds")
		err = utils_test.CopyDir(originalDir, tmpCrdsDir)
		Expect(err).NotTo(HaveOccurred())

		origWD, err = os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		err = os.Chdir(tmpDir)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		_ = os.Chdir(origWD)
		_ = os.RemoveAll(tmpDir)
	})

	It("good flow", func() {
		_ = utils_test.UpdateYAMLField(filepath.Join(tmpCrdsDir, "clusterImageSet.yaml"), "spec.releaseImage", "4.15.0", openshiftVersionLong)
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			filepath.Join("crds", "clusterDeployment.yaml"),
			filepath.Join("crds", "agentClusterInstall.yaml"),
			filepath.Join("crds", "clusterImageSet.yaml"), "", "")
		Expect(registerClusterErr).NotTo(HaveOccurred())
		Expect(network.GetApiVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.8"))
		Expect(network.GetIngressVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.9"))
		Expect(modelCluster.OpenshiftVersion).To(ContainSubstring(openshiftVersion))
		Expect(modelCluster.CPUArchitecture).To(Equal("x86_64"))
		Expect(modelCluster.Name).To(Equal("test-cluster"))

		modelInfraEnv, registerInfraEnvErr := agentbasedinstaller.RegisterInfraEnv(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			modelCluster, filepath.Join("crds", "infraEnv.yaml"),
			filepath.Join("crds", "nmstate.yaml"), "full-iso", "")

		Expect(registerInfraEnvErr).NotTo(HaveOccurred())
		Expect(*modelInfraEnv.Name).To(Equal("myinfraenv"))
		Expect(modelInfraEnv.ClusterID).To(Equal(*modelCluster.ID))
		Expect(len(modelInfraEnv.StaticNetworkConfig)).ToNot(BeZero())
	})

	It("InstallConfig override good flow", func() {
		_ = utils_test.UpdateYAMLField(filepath.Join(tmpCrdsDir, "clusterImageSet.yaml"), "spec.releaseImage", "4.15.0", openshiftVersionLong)
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			filepath.Join("crds", "clusterDeployment.yaml"),
			filepath.Join("crds", "agentClusterInstall-with-installconfig-overrides.yaml"),
			filepath.Join("crds", "clusterImageSet.yaml"), "", "")
		Expect(registerClusterErr).NotTo(HaveOccurred())
		Expect(network.GetApiVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.8"))
		Expect(network.GetIngressVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.9"))
		Expect(modelCluster.OpenshiftVersion).To(ContainSubstring(openshiftVersion))
		Expect(modelCluster.CPUArchitecture).To(Equal("x86_64"))
		Expect(modelCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))
		Expect(modelCluster.Name).To(Equal("test-cluster"))

		modelInfraEnv, registerInfraEnvErr := agentbasedinstaller.RegisterInfraEnv(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			modelCluster,
			filepath.Join("crds", "infraEnv.yaml"),
			filepath.Join("crds", "nmstate.yaml"),
			"full-iso", "")

		Expect(registerInfraEnvErr).NotTo(HaveOccurred())
		Expect(*modelInfraEnv.Name).To(Equal("myinfraenv"))
		Expect(modelInfraEnv.ClusterID).To(Equal(*modelCluster.ID))
		Expect(len(modelInfraEnv.StaticNetworkConfig)).ToNot(BeZero())
	})

	It("missing one of the ZTP manifests", func() {
		_ = utils_test.UpdateYAMLField(filepath.Join(tmpCrdsDir, "clusterImageSet.yaml"), "spec.releaseImage", "4.15.0", openshiftVersionLong)
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"file-does-not-exist",
			filepath.Join("crds", "agentClusterInstall.yaml"),
			filepath.Join("crds", "clusterImageSet.yaml"),
			"", "")
		Expect(registerClusterErr).To(HaveOccurred())
		Expect(modelCluster).To(BeNil())
	})
})
