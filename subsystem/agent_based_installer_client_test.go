package subsystem

import (
	"context"
	"os"

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
	ctx := context.Background()

	It("good flow", func() {
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"../docs/hive-integration/crds/clusterDeployment.yaml",
			"../docs/hive-integration/crds/agentClusterInstall.yaml",
			"../docs/hive-integration/crds/clusterImageSet.yaml", "", "", false)
		Expect(registerClusterErr).NotTo(HaveOccurred())
		Expect(network.GetApiVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.8"))
		Expect(network.GetIngressVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.9"))
		Expect(modelCluster.OpenshiftVersion).To(ContainSubstring("4.15.0"))
		Expect(modelCluster.CPUArchitecture).To(Equal("x86_64"))
		Expect(modelCluster.Name).To(Equal("test-cluster"))

		modelInfraEnv, registerInfraEnvErr := agentbasedinstaller.RegisterInfraEnv(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			modelCluster, "../docs/hive-integration/crds/infraEnv.yaml",
			"../docs/hive-integration/crds/nmstate.yaml", "full-iso", "")

		Expect(registerInfraEnvErr).NotTo(HaveOccurred())
		Expect(*modelInfraEnv.Name).To(Equal("myinfraenv"))
		Expect(modelInfraEnv.ClusterID).To(Equal(*modelCluster.ID))
		Expect(len(modelInfraEnv.StaticNetworkConfig)).ToNot(BeZero())
	})

	It("InstallConfig override good flow", func() {
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"../docs/hive-integration/crds/clusterDeployment.yaml",
			"../docs/hive-integration/crds/agentClusterInstall-with-installconfig-overrides.yaml",
			"../docs/hive-integration/crds/clusterImageSet.yaml", "", "", false)
		Expect(registerClusterErr).NotTo(HaveOccurred())
		Expect(network.GetApiVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.8"))
		Expect(network.GetIngressVipById(&common.Cluster{Cluster: *modelCluster}, 0)).To(Equal("1.2.3.9"))
		Expect(modelCluster.OpenshiftVersion).To(ContainSubstring("4.15.0"))
		Expect(modelCluster.CPUArchitecture).To(Equal("x86_64"))
		Expect(modelCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))
		Expect(modelCluster.Name).To(Equal("test-cluster"))

		modelInfraEnv, registerInfraEnvErr := agentbasedinstaller.RegisterInfraEnv(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			modelCluster, "../docs/hive-integration/crds/infraEnv.yaml",
			"../docs/hive-integration/crds/nmstate.yaml", "full-iso", "")

		Expect(registerInfraEnvErr).NotTo(HaveOccurred())
		Expect(*modelInfraEnv.Name).To(Equal("myinfraenv"))
		Expect(modelInfraEnv.ClusterID).To(Equal(*modelCluster.ID))
		Expect(len(modelInfraEnv.StaticNetworkConfig)).ToNot(BeZero())
	})

	It("missing one of the ZTP manifests", func() {
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"file-does-not-exist",
			"../docs/hive-integration/crds/agentClusterInstall.yaml",
			"../docs/hive-integration/crds/clusterImageSet.yaml", "", "", false)
		Expect(registerClusterErr).To(HaveOccurred())
		Expect(modelCluster).To(BeNil())
	})

	It("retry installConfig overrides on restart scenario", func() {
		// First registration with overrides
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"../docs/hive-integration/crds/clusterDeployment.yaml",
			"../docs/hive-integration/crds/agentClusterInstall-with-installconfig-overrides.yaml",
			"../docs/hive-integration/crds/clusterImageSet.yaml", "", "", false)
		Expect(registerClusterErr).NotTo(HaveOccurred())
		Expect(modelCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))

		// Simulate restart: Apply overrides again to existing cluster
		// This should be idempotent and not fail
		updatedCluster, err := agentbasedinstaller.ApplyInstallConfigOverrides(ctx, log, utils_test.TestContext.UserBMClient,
			modelCluster, "../docs/hive-integration/crds/agentClusterInstall-with-installconfig-overrides.yaml")

		Expect(err).NotTo(HaveOccurred())
		// Should return nil because overrides are already correctly applied
		Expect(updatedCluster).To(BeNil())
	})

	It("apply overrides when missing on existing cluster", func() {
		// Register cluster without overrides first
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"../docs/hive-integration/crds/clusterDeployment.yaml",
			"../docs/hive-integration/crds/agentClusterInstall.yaml",
			"../docs/hive-integration/crds/clusterImageSet.yaml", "", "", false)
		Expect(registerClusterErr).NotTo(HaveOccurred())
		Expect(modelCluster.InstallConfigOverrides).To(BeEmpty())

		// Now apply overrides (simulating a restart where overrides should be applied)
		updatedCluster, err := agentbasedinstaller.ApplyInstallConfigOverrides(ctx, log, utils_test.TestContext.UserBMClient,
			modelCluster, "../docs/hive-integration/crds/agentClusterInstall-with-installconfig-overrides.yaml")

		Expect(err).NotTo(HaveOccurred())
		Expect(updatedCluster).NotTo(BeNil())
		Expect(updatedCluster.InstallConfigOverrides).To(Equal(`{"fips": true}`))
	})

	It("retry extra manifests registration on restart", func() {
		// Register cluster first
		modelCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, utils_test.TestContext.UserBMClient, pullSecret,
			"../docs/hive-integration/crds/clusterDeployment.yaml",
			"../docs/hive-integration/crds/agentClusterInstall.yaml",
			"../docs/hive-integration/crds/clusterImageSet.yaml", "", "", false)
		Expect(registerClusterErr).NotTo(HaveOccurred())

		// Register extra manifests - this should be idempotent
		// First registration
		err := agentbasedinstaller.RegisterExtraManifests(os.DirFS("../docs/hive-integration/crds/extra-manifests"),
			ctx, log, utils_test.TestContext.UserBMClient.Manifests, modelCluster)
		Expect(err).NotTo(HaveOccurred())

		// Retry registration (simulating restart) - should not fail
		err = agentbasedinstaller.RegisterExtraManifests(os.DirFS("../docs/hive-integration/crds/extra-manifests"),
			ctx, log, utils_test.TestContext.UserBMClient.Manifests, modelCluster)
		Expect(err).NotTo(HaveOccurred())
	})
})
