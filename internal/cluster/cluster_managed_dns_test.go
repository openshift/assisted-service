package cluster

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
)

var _ = Describe("cluster-managed DNS manifests", func() {
	var (
		ctrl          *gomock.Controller
		manifests     *network.MockManifestsGeneratorAPI
		mockOperators *operators.MockAPI
		manager       *Manager
		cluster       *common.Cluster
		ctx           context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		manifests = network.NewMockManifestsGeneratorAPI(ctrl)
		mockOperators = operators.NewMockAPI(ctrl)
		manager = &Manager{
			log:                   logrus.New(),
			manifestsGeneratorAPI: manifests,
			rp:                    &refreshPreprocessor{operatorsAPI: mockOperators},
		}
		cluster = &common.Cluster{Cluster: models.Cluster{
			ControlPlaneCount: 1,
			Platform:          &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
		}}
		ctx = context.Background()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	DescribeTable("selects the dnsmasq manifest from the rendered install config",
		func(featureSet, gate string, wantDNSMasq, wantErr bool) {
			cluster.InstallConfigOverrides = installConfigOverrides(featureSet, gate)
			if !wantErr {
				manifests.EXPECT().AddChronyManifest(ctx, gomock.Any(), cluster).Return(nil)
				if wantDNSMasq {
					manifests.EXPECT().IsSNODNSMasqEnabled().Return(true)
					manifests.EXPECT().AddDnsmasqForSingleNode(ctx, gomock.Any(), cluster).Return(nil)
				}
				mockOperators.EXPECT().GenerateManifests(ctx, cluster).Return(nil)
				manifests.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), cluster).Return(nil)
				manifests.EXPECT().AddDiskEncryptionManifest(ctx, gomock.Any(), cluster).Return(nil)
				manifests.EXPECT().AddNicReapply(ctx, gomock.Any(), cluster).Return(nil)
			}

			err := manager.GenerateAdditionalManifests(ctx, cluster)
			if wantErr {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("skips dnsmasq when CustomNoUpgrade enables the gate", "CustomNoUpgrade", "true", false, false),
		Entry("skips dnsmasq for DevPreviewNoUpgrade", "DevPreviewNoUpgrade", "", false, false),
		Entry("retains dnsmasq when CustomNoUpgrade disables the gate", "CustomNoUpgrade", "false", true, false),
		Entry("stops before writes for an invalid gate", "CustomNoUpgrade", "invalid", false, true),
	)
})

func installConfigOverrides(featureSet, gate string) string {
	overrides := `{"featureSet":"` + featureSet + `"}`
	if gate == "" {
		return overrides
	}

	return `{"featureSet":"` + featureSet + `","featureGates":["UnifiedClusterManagedDNSAndLB=` + gate + `"]}`
}
