package controllers

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	metal3_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newPreprovisioningImageRequest(image *metal3_v1alpha1.PreprovisioningImage) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: image.ObjectMeta.Namespace,
		Name:      image.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newPreprovisioningImage(name, namespace, labelKey, labelValue, bmhName string) *metal3_v1alpha1.PreprovisioningImage {
	return &metal3_v1alpha1.PreprovisioningImage{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    AddLabel(nil, labelKey, labelValue),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "metal3.io/v1alpha1",
				Kind:       "BareMetalHost",
				Name:       bmhName,
			}},
			Finalizers: []string{PreprovisioningImageFinalizerName},
		},
		Spec: metal3_v1alpha1.PreprovisioningImageSpec{
			AcceptFormats: []metal3_v1alpha1.ImageFormat{
				metal3_v1alpha1.ImageFormatISO,
				metal3_v1alpha1.ImageFormatInitRD,
			},
		},
	}
}

func newInfraEnv(name, namespace string, spec aiv1beta1.InfraEnvSpec) *aiv1beta1.InfraEnv {
	return &aiv1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

var _ = Describe("PreprovisioningImage reconcile", func() {
	var (
		c                     client.Client
		pr                    *PreprovisioningImageReconciler
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		mockCRDEventsHandler  *MockCRDEventsHandler
		mockVersionHandler    *versions.MockHandler
		mockOcRelease         *oc.MockRelease
		mockBMOUtils          *MockBMOUtils
		ctx                   = context.Background()
		infraEnvID, clusterID strfmt.UUID
		backendInfraEnv       *common.InfraEnv
		backendCluster        *common.Cluster
		downloadURL           = "https://downloadurl"
		rootfsURL             = "https://rootfs.example.com"
		kernelURL             = "https://kernel.example.com"
		initrdURL             = "https://initrd.example.com"
		infraEnvArch          = "x86_64"
		infraEnv              *aiv1beta1.InfraEnv
		ppi                   *metal3_v1alpha1.PreprovisioningImage
		bmh                   *metal3_v1alpha1.BareMetalHost
		hubReleaseImage       = "quay.io/openshift-release-dev/ocp-release@sha256:5fdcafc349e184af11f71fe78c0c87531b9df123c664ff1ac82711dc15fa1532"
		clusterVersion        *configv1.ClusterVersion
		defaultIronicImage    = "ironic-agent-image:latest"
		ironicServiceIPs      = []string{"198.51.100.1", "2001:db8::dead:beee"}
		ironicInspectorIPs    = []string{"198.51.100.2", "2001:db8::dead:beef"}
	)

	BeforeEach(func() {
		schemes := runtime.NewScheme()
		Expect(configv1.AddToScheme(schemes)).To(Succeed())
		Expect(metal3_v1alpha1.AddToScheme(schemes)).To(Succeed())
		Expect(aiv1beta1.AddToScheme(schemes)).To(Succeed())
		c = fakeclient.NewClientBuilder().WithScheme(schemes).
			WithStatusSubresource(&metal3_v1alpha1.PreprovisioningImage{}, &aiv1beta1.InfraEnv{}).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		mockCRDEventsHandler = NewMockCRDEventsHandler(mockCtrl)
		mockVersionHandler = versions.NewMockHandler(mockCtrl)
		mockOcRelease = oc.NewMockRelease(mockCtrl)
		mockBMOUtils = NewMockBMOUtils(mockCtrl)
		infraEnvID = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		backendInfraEnv = &common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID}}
		backendCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
			MachineNetworks: []*models.MachineNetwork{
				{Cidr: "198.51.100.1/24"},
			},
		}}
		mockInstallerInternal.EXPECT().GetClusterInternal(gomock.Any(), installer.V2GetClusterParams{ClusterID: clusterID}).Return(backendCluster, nil).AnyTimes()

		bmh = &metal3_v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{Name: "testBMH", Namespace: testNamespace}}
		Expect(c.Create(ctx, bmh)).To(BeNil())
		pr = &PreprovisioningImageReconciler{
			Client:           c,
			Log:              common.GetTestLog(),
			Installer:        mockInstallerInternal,
			CRDEventsHandler: mockCRDEventsHandler,
			VersionsHandler:  mockVersionHandler,
			OcRelease:        mockOcRelease,
			Config: PreprovisioningImageControllerConfig{
				BaremetalIronicAgentImage:       defaultIronicImage,
				BaremetalIronicAgentImageForArm: "ironic-agent-arm64:latest",
			},
			BMOUtils: mockBMOUtils,
		}
		clusterVersion = &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{Name: "version"},
			Spec:       configv1.ClusterVersionSpec{ClusterID: configv1.ClusterID(uuid.New().String())},
			Status: configv1.ClusterVersionStatus{
				Desired: configv1.Release{Image: hubReleaseImage, Version: "4.12.0-rc.3"},
			},
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("reconcile new PreprovisioningImage - success", func() {
		BeforeEach(func() {
			infraEnv = newInfraEnv("testInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{})
			infraEnv.Status.ISODownloadURL = downloadURL
			infraEnv.Status.BootArtifacts.RootfsURL = rootfsURL
			infraEnv.Status.BootArtifacts.KernelURL = kernelURL
			infraEnv.Status.BootArtifacts.InitrdURL = initrdURL
			createdAt := metav1.Now().Add(-InfraEnvImageCooldownPeriod)
			infraEnv.Status.CreatedTime = &metav1.Time{Time: createdAt}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
			Expect(c.Create(ctx, ppi)).To(BeNil())
			mockBMOUtils.EXPECT().GetIronicIPs().AnyTimes().Return(ironicServiceIPs, ironicInspectorIPs, nil)
		})
		AfterEach(func() {
			mockCtrl.Finish()
		})

		setInfraEnvIronicConfig := func() {
			conf, err := ignition.GenerateIronicConfig(getUrlFromIP(ironicServiceIPs[0]), getUrlFromIP(ironicInspectorIPs[0]), *backendInfraEnv, defaultIronicImage)
			Expect(err).NotTo(HaveOccurred())
			backendInfraEnv.InternalIgnitionConfigOverride = string(conf)
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		}

		It("Adds the default ironic Ignition to the infraEnv when no clusterID is set", func() {
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.ClusterID = ""
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(defaultIronicImage))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicServiceIPs[0]))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicInspectorIPs[0]))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})
		It("Adds the ironic IPv6 address to the ignition when the spoke cluster is IPv6 only", func() {
			backendCluster.ClusterNetworks = []*models.ClusterNetwork{
				{Cidr: "2001:db8::/32"},
			}
			backendCluster.MachineNetworks = nil
			backendCluster.ServiceNetworks = nil
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicServiceIPs[1])))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicInspectorIPs[1])))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})
		It("Adds the ironic IPv6 address to the ignition when the infraEnv has no cluster and is annotated for IPv6", func() {
			metav1.SetMetaDataAnnotation(&infraEnv.ObjectMeta, infraEnvIPFamilyAnnotation, ipv6Family)
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.ClusterID = ""
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicServiceIPs[1])))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicInspectorIPs[1])))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})

		It("uses ICC config URLs when ICC contains URLs and user override annotation", func() {
			overrideAgentImage := "ironic-agent-override:latest"
			iccConfig := &ICCConfig{
				IronicAgentImage: "ironic-agent-image",
				IronicBaseURL:    "ironic-base-url",
			}
			setAnnotation(&infraEnv.ObjectMeta, ironicAgentImageOverrideAnnotation, overrideAgentImage)
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "secret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(iccConfig, nil)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, _ *common.MirrorRegistryConfiguration) {
					Expect(*internalIgnitionConfig).To(ContainSubstring(iccConfig.IronicBaseURL))
					Expect(*internalIgnitionConfig).To(ContainSubstring(overrideAgentImage))
					Expect(url.QueryUnescape(*internalIgnitionConfig)).To(MatchRegexp(`(?m)^inspection_callback_url\s=\s$`)) // matches inspection_callback_url = <empty>
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)

			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("sets ironic URLs from cluster when ICC config is unavailable and ironic image annotation override is set", func() {
			overrideAgentImage := "ironic-agent-override:latest"
			setAnnotation(&infraEnv.ObjectMeta, ironicAgentImageOverrideAnnotation, overrideAgentImage)
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "secret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, _ *common.MirrorRegistryConfiguration) {
					Expect(*internalIgnitionConfig).To(ContainSubstring(url.QueryEscape(ironicServiceIPs[0])))
					Expect(*internalIgnitionConfig).To(ContainSubstring(url.QueryEscape(ironicInspectorIPs[0])))
					Expect(*internalIgnitionConfig).To(ContainSubstring(overrideAgentImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("preserves ironic urls from ICC config with user override regardless of architecture mismatch", func() {
			overrideAgentImage := "ironic-agent-override:latest"
			iccConfig := &ICCConfig{
				IronicAgentImage:       "ironic-agent-image",
				IronicBaseURL:          "ironic-base-url",
				IronicInspectorBaseUrl: "",
			}
			setAnnotation(&infraEnv.ObjectMeta, ironicAgentImageOverrideAnnotation, overrideAgentImage)
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "aarch64" // spoke is ARM64
			backendInfraEnv.PullSecret = "secret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(iccConfig, nil)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, _ *common.MirrorRegistryConfiguration) {
					Expect(*internalIgnitionConfig).To(ContainSubstring(url.QueryEscape(iccConfig.IronicBaseURL)))
					Expect(*internalIgnitionConfig).To(ContainSubstring(overrideAgentImage))
					Expect(url.QueryUnescape(*internalIgnitionConfig)).To(MatchRegexp(`(?m)^inspection_callback_url\s=\s$`)) // matches inspection_callback_url = <empty>
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)

			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("preserves ironic urls from ICC config regardless of architecture mismatch", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			iccConfig := &ICCConfig{
				IronicAgentImage:       "ironic-agent-image",
				IronicBaseURL:          "ironic-base-url",
				IronicInspectorBaseUrl: "",
			}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "aarch64"
			backendInfraEnv.PullSecret = "secret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(iccConfig, nil)
			mockOcRelease.EXPECT().GetImageArchitecture(gomock.Any(), iccConfig.IronicAgentImage, backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil)
			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"aarch64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("hub-ironic-aarch64-image", nil)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, _ *common.MirrorRegistryConfiguration) {
					Expect(*internalIgnitionConfig).To(ContainSubstring(url.QueryEscape(iccConfig.IronicBaseURL)))
					Expect(*internalIgnitionConfig).To(ContainSubstring("hub-ironic-aarch64-image"))
					Expect(url.QueryUnescape(*internalIgnitionConfig)).To(MatchRegexp(`(?m)^inspection_callback_url\s=\s$`))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)

			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("Wait for InfraEnv cool down", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			infraEnv.Status.CreatedTime = &metav1.Time{Time: time.Now()}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res.Requeue).To(Equal(true))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus("",
				&conditionsv1.Condition{
					Reason:  "WaitingForInfraEnvImageToCoolDown",
					Message: "Waiting for InfraEnv image to cool down",
					Status:  corev1.ConditionFalse},
				ppi,
			)
		})

		It("Should update the status after cool down and not reboot the host if the image didn't change", func() {
			ppi.Status.ImageUrl = downloadURL
			Expect(c.Status().Update(ctx, ppi)).To(BeNil())

			infraEnv.Status.ISODownloadURL = downloadURL
			infraEnv.Status.CreatedTime = &metav1.Time{Time: time.Now()}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(2).Return(nil, errors.Errorf("ICC configuration is not available"))

			By("initially reconciling the preprovisioningimage during cooldown")
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res.Requeue).To(Equal(true))

			By("verifying the preprovisioningimage has wait for cooldown status")
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus("",
				&conditionsv1.Condition{
					Reason:  "WaitingForInfraEnvImageToCoolDown",
					Message: "Waiting for InfraEnv image to cool down",
					Status:  corev1.ConditionFalse},
				ppi,
			)

			By("modifying the infraenv creation time to make it seem like it's after the cooldown period")
			Expect(c.Get(ctx, types.NamespacedName{Name: infraEnv.Name, Namespace: infraEnv.Namespace}, infraEnv)).To(BeNil())
			infraEnv.Status.CreatedTime = &metav1.Time{Time: metav1.Now().Add(-InfraEnvImageCooldownPeriod)}
			Expect(c.Status().Update(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()

			By("reconciling the preprovisioningimage after the cooldown period")
			res, err = pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			By("verifying the preprovisioningimage status has updated")
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus(downloadURL, conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition), ppi)

			By("verifying the BMH does not have the reboot annotation")
			bmhKey := types.NamespacedName{
				Namespace: bmh.Namespace,
				Name:      bmh.Name,
			}
			Expect(c.Get(ctx, bmhKey, bmh)).To(BeNil())
			Expect(bmh.Annotations).ToNot(HaveKey("reboot.metal3.io"))
		})

		It("sets the image on the PPI to the ISO URL and doesn't force a reboot", func() {
			createdAt := metav1.Now().Add(-InfraEnvImageCooldownPeriod)
			infraEnv.Status.CreatedTime = &metav1.Time{Time: createdAt}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus(infraEnv.Status.ISODownloadURL, conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition), ppi)

			bmhKey := types.NamespacedName{
				Namespace: bmh.Namespace,
				Name:      bmh.Name,
			}
			Expect(c.Get(ctx, bmhKey, bmh)).To(BeNil())
			Expect(bmh.Annotations).ToNot(HaveKey("reboot.metal3.io"))
		})

		It("reboots the host when the image is updated", func() {
			oldURL := "https://example.com/images/4b495e3f-6a3d-4742-aedd-7db57912c819?api_key=myotherkey&arch=x86_64&type=minimal-iso&version=4.13"
			infraEnv.Status.ISODownloadURL = oldURL
			infraEnv.Status.CreatedTime = &metav1.Time{Time: metav1.Now().Add(-InfraEnvImageCooldownPeriod)}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			SetImageUrl(ppi, *infraEnv)
			Expect(c.Status().Update(ctx, ppi)).To(BeNil())

			newURL := "https://example.com/images/4b495e3f-6a3d-4742-aedd-7db57912c819?api_key=mykey&arch=x86_64&type=minimal-iso&version=4.13"
			infraEnv.Status.ISODownloadURL = newURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus(newURL, conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition), ppi)
			bmhKey := types.NamespacedName{
				Namespace: bmh.Namespace,
				Name:      bmh.Name,
			}
			Expect(c.Get(ctx, bmhKey, bmh)).To(BeNil())
			Expect(bmh.Annotations).To(HaveKey("reboot.metal3.io"))
		})

		It("doesn't reboot and sets a condition when an image is updated for a provisioned BMH", func() {
			bmh.Status.Provisioning.State = metal3_v1alpha1.StateProvisioned
			Expect(c.Update(ctx, bmh)).To(Succeed())

			oldURL := "https://example.com/images/4b495e3f-6a3d-4742-aedd-7db57912c819?api_key=myotherkey&arch=x86_64&type=minimal-iso&version=4.13"
			infraEnv.Status.ISODownloadURL = oldURL
			infraEnv.Status.CreatedTime = &metav1.Time{Time: metav1.Now().Add(-InfraEnvImageCooldownPeriod)}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			SetImageUrl(ppi, *infraEnv)
			Expect(c.Status().Update(ctx, ppi)).To(BeNil())

			newURL := "https://example.com/images/4b495e3f-6a3d-4742-aedd-7db57912c819?api_key=mykey&arch=x86_64&type=minimal-iso&version=4.13"
			infraEnv.Status.ISODownloadURL = newURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())

			Expect(ppi.Status.ImageUrl).To(Equal(newURL))
			bmhKey := types.NamespacedName{
				Namespace: bmh.Namespace,
				Name:      bmh.Name,
			}
			Expect(c.Get(ctx, bmhKey, bmh)).To(BeNil())
			Expect(bmh.Annotations).ToNot(HaveKey("reboot.metal3.io"))
		})

		It("sets the image on the PPI to the initrd when the PPI doesn't accept ISO format", func() {
			ppi.Spec.AcceptFormats = []metal3_v1alpha1.ImageFormat{metal3_v1alpha1.ImageFormatInitRD}
			Expect(c.Update(ctx, ppi)).To(BeNil())

			createdAt := metav1.Now().Add(-InfraEnvImageCooldownPeriod)
			infraEnv.Status.CreatedTime = &metav1.Time{Time: createdAt}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus(initrdURL, conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition), ppi)
			Expect(ppi.Status.KernelUrl).To(Equal(kernelURL))
			Expect(ppi.Status.ExtraKernelParams).To(Equal(fmt.Sprintf("coreos.live.rootfs_url=%s rd.bootif=0", rootfsURL)))
		})

		It("sets the extra kernel params on the PPI based on the infraenv when the PPI doesn't accept ISO format", func() {
			ppi.Spec.AcceptFormats = []metal3_v1alpha1.ImageFormat{metal3_v1alpha1.ImageFormatInitRD}
			Expect(c.Update(ctx, ppi)).To(BeNil())

			createdAt := metav1.Now().Add(-InfraEnvImageCooldownPeriod)
			infraEnv.Status.CreatedTime = &metav1.Time{Time: createdAt}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			infraEnv.Spec.KernelArguments = []aiv1beta1.KernelArgument{
				{Operation: models.KernelArgumentOperationAppend, Value: "arg=thing"},
				{Operation: models.KernelArgumentOperationAppend, Value: "other.arg"},
			}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus(initrdURL, conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition), ppi)
			Expect(ppi.Status.KernelUrl).To(Equal(kernelURL))
			Expect(ppi.Status.ExtraKernelParams).To(Equal(fmt.Sprintf("coreos.live.rootfs_url=%s rd.bootif=0 arg=thing other.arg", rootfsURL)))
		})

		It("doesn't reboot the host when PreprovisioningImage ImageUrl is up to date", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			createdAt := metav1.Now().Add(-InfraEnvImageCooldownPeriod)
			infraEnv.Status.CreatedTime = &metav1.Time{Time: createdAt}
			infraEnv.Status.Conditions = []conditionsv1.Condition{{Type: aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  "some reason",
				Message: "Some message",
			}}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			SetImageUrl(ppi, *infraEnv)
			Expect(c.Update(ctx, ppi)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testPPI",
			}
			Expect(c.Get(ctx, key, ppi)).To(BeNil())
			validateStatus(infraEnv.Status.ISODownloadURL, conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition), ppi)
			bmhKey := types.NamespacedName{
				Namespace: bmh.Namespace,
				Name:      bmh.Name,
			}
			Expect(c.Get(ctx, bmhKey, bmh)).To(BeNil())
			Expect(bmh.Annotations).NotTo(HaveKey("reboot.metal3.io"))
		})
		It("Add the ironic Ignition to the infraEnv using the ironic agent image from the ICC configuration", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			iccConfig := ICCConfig{
				IronicAgentImage:       "ironic-image:4.12.0",
				IronicBaseURL:          "https://10.0.0.1:6534,https://[2001::1]:6534",
				IronicInspectorBaseUrl: "https://10.0.0.2:6534/v1/continue,https://[2001::2]:6534/v1/continue",
			}
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockOcRelease.EXPECT().GetImageArchitecture(gomock.Any(), iccConfig.IronicAgentImage, backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic"))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(iccConfig.IronicAgentImage))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(iccConfig.IronicBaseURL)))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(iccConfig.IronicInspectorBaseUrl)))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(&iccConfig, nil)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})
		It("uses the default ironic agent image when the infraenv arch isn't supported by the agent image in the ICC config", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			iccConfig := ICCConfig{
				IronicAgentImage:       "ironic-image:4.12.0",
				IronicBaseURL:          "https://10.0.0.1:6534,https://[2001::1]:6534",
				IronicInspectorBaseUrl: "https://10.0.0.2:6534/v1/continue,https://[2001::2]:6534/v1/continue",
			}
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockOcRelease.EXPECT().GetImageArchitecture(gomock.Any(), iccConfig.IronicAgentImage, backendInfraEnv.PullSecret).Times(1).Return([]string{"arm64"}, nil)
			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Times(1).Return([]string{"arm64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("ironic-image:4.12.0", nil)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), "4.12.0-rc.3", "x86_64", backendInfraEnv.PullSecret).Return(nil, errors.Errorf("no release found"))
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic"))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(defaultIronicImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(&iccConfig, nil)
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})
		It("Add the ironic Ignition to the infraEnv using the ironic agent image from the hub release", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			ironicAgentImage := "ironic-image:4.12.0"
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return(ironicAgentImage, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic"))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicAgentImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})
		It("uses the default ironic agent image when the infraenv arch isn't supported by the hub release image", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"arm64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("ironic-image:4.12.0", nil)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), "4.12.0-rc.3", "x86_64", backendInfraEnv.PullSecret).Return(nil, errors.Errorf("no release found"))
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic"))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic-agent-image:latest"))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			Expect(infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]).To(Equal("true"))
		})

		It("caches the hub cluster image and architecture", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			ironicAgentImage := "ironic-image:4.12.0"
			backendInfraEnv.PullSecret = "mypullsecret"
			backendInfraEnv.CPUArchitecture = "x86_64"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil).AnyTimes()

			// These should only be called once if the cache is working
			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil).Times(1)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return(ironicAgentImage, nil).Times(1)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicAgentImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(2)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(2)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(2).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "testInfraEnv",
			}
			Expect(c.Get(ctx, key, infraEnv)).To(BeNil())
			delete(infraEnv.Annotations, EnableIronicAgentAnnotation)
			Expect(c.Update(ctx, infraEnv)).To(BeNil())

			res, err = pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("uses the override ironic image when supplied", func() {
			overrideAgentImage := "ironic-agent-override:latest"
			setAnnotation(&infraEnv.ObjectMeta, ironicAgentImageOverrideAnnotation, overrideAgentImage)
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(internalIgnitionConfig).Should(HaveValue(ContainSubstring(overrideAgentImage)))
				})
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("uses ironic agent from ClusterImageSet when found for spoke architecture", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			backendInfraEnv.CPUArchitecture = "arm64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("hub-ironic-x86-image", nil)

			armReleaseURL := "quay.io/openshift-release-dev/ocp-release:4.12.0-arm64"
			armCPUArch := "arm64"
			armVersion := "4.12.0"
			armReleaseImage := &models.ReleaseImage{
				CPUArchitecture:  &armCPUArch,
				OpenshiftVersion: &armVersion,
				URL:              &armReleaseURL,
				Version:          &armVersion,
			}
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), "4.12.0-rc.3", "arm64", backendInfraEnv.PullSecret).Return(armReleaseImage, nil)

			armIronicImage := "ironic-agent-arm64:4.12.0"
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), armReleaseURL, "", backendInfraEnv.PullSecret).Return(armIronicImage, nil)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(armIronicImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("handles multi-arch hub release image containing spoke architecture", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			backendInfraEnv.CPUArchitecture = "arm64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"x86_64", "arm64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("multi-arch-ironic-image:4.12.0", nil)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, mirrorRegistryConfiguration *common.MirrorRegistryConfiguration) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("multi-arch-ironic-image:4.12.0"))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(1)

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("sets a failure condition when the infraEnv arch doesn't match the preprovisioningimage", func() {
			infraEnv.Spec.CpuArchitecture = "aarch64"
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			checkImageConditionFailed(c, ppi, archMismatchReason, "does not match InfraEnv CPU architecture")
		})

		It("doesn't fail when the normalized preprovisioning architecture matches the infraenv architecture", func() {
			infraEnv.Spec.CpuArchitecture = "arm64"
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			ppi.Spec.Architecture = "aarch64"
			Expect(c.Update(ctx, ppi)).To(Succeed())

			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			ppiKey := types.NamespacedName{Namespace: ppi.Namespace, Name: ppi.Name}
			Expect(c.Get(ctx, ppiKey, ppi)).To(Succeed())
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			errorCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
			Expect(errorCondition.Status).To(Equal(metav1.ConditionFalse))
		})

		It("doesn't fail when the normalized infraenv architecture matches the preprovisioning architecture", func() {
			infraEnv.Spec.CpuArchitecture = "aarch64"
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			ppi.Spec.Architecture = "arm64"
			Expect(c.Update(ctx, ppi)).To(Succeed())

			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			ppiKey := types.NamespacedName{Namespace: ppi.Namespace, Name: ppi.Name}
			Expect(c.Get(ctx, ppiKey, ppi)).To(Succeed())
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			errorCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
			Expect(errorCondition.Status).To(Equal(metav1.ConditionFalse))
		})

		It("doesn't fail when the infraEnv image has not been created yet", func() {
			infraEnv.Status = aiv1beta1.InfraEnvStatus{}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			setInfraEnvIronicConfig()
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("infraEnv not found", func() {
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			ppiKey := types.NamespacedName{Namespace: ppi.Namespace, Name: ppi.Name}
			Expect(c.Get(ctx, ppiKey, ppi)).To(Succeed())
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Message).To(ContainSubstring(fmt.Sprintf("InfraEnv %s/%s is not found or is being deleted", ppi.Labels[InfraEnvLabel], ppi.Namespace)))
			errorCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
			Expect(errorCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(errorCondition.Message).To(ContainSubstring(fmt.Sprintf("InfraEnv %s/%s is not found or is being deleted", ppi.Labels[InfraEnvLabel], ppi.Namespace)))
			Expect(ppi.Status.ImageUrl).To(BeEmpty())
			Expect(ppi.Status.KernelUrl).To(BeEmpty())
			Expect(ppi.Status.ExtraKernelParams).To(BeEmpty())
			Expect(ppi.Status.Format).To(BeEmpty())
			Expect(ppi.Status.Architecture).To(BeEmpty())
		})

		It("sets the not found condition when an existing infraEnv gets removed", func() {
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
			setInfraEnvIronicConfig()

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			// Check that the conditions are set to reflect an existing InfraEnv
			ppiKey := types.NamespacedName{Namespace: ppi.Namespace, Name: ppi.Name}
			Expect(c.Get(ctx, ppiKey, ppi)).To(Succeed())
			readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			errorCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
			Expect(errorCondition.Status).To(Equal(metav1.ConditionFalse))

			// Set PreprovisioningImage to a non-existing InfraEnv
			ppi.Labels[InfraEnvLabel] = "non-existing-infraenv"
			Expect(c.Update(ctx, ppi)).To(Succeed())

			res, err = pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			// Check that the conditions are updated to reflect a non-existing InfraEnv
			ppiKey = types.NamespacedName{Namespace: ppi.Namespace, Name: ppi.Name}
			Expect(c.Get(ctx, ppiKey, ppi)).To(Succeed())
			readyCondition = meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Message).To(ContainSubstring(fmt.Sprintf("InfraEnv %s/%s is not found or is being deleted", ppi.Labels[InfraEnvLabel], ppi.Namespace)))
			errorCondition = meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
			Expect(errorCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(errorCondition.Message).To(ContainSubstring(fmt.Sprintf("InfraEnv %s/%s is not found or is being deleted", ppi.Labels[InfraEnvLabel], ppi.Namespace)))
			Expect(ppi.Status.ImageUrl).To(BeEmpty())
			Expect(ppi.Status.KernelUrl).To(BeEmpty())
			Expect(ppi.Status.ExtraKernelParams).To(BeEmpty())
			Expect(ppi.Status.Format).To(BeEmpty())
			Expect(ppi.Status.Architecture).To(BeEmpty())
		})
	})
	It("PreprovisioningImage not found", func() {
		res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))
	})
	It("PreprovisioningImage doesn't accept ISO format", func() {
		ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
		ppi.Spec.AcceptFormats = []metal3_v1alpha1.ImageFormat{"some random format"}
		Expect(c.Create(ctx, ppi)).To(BeNil())

		res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))
		checkImageConditionFailed(c, ppi, "UnsupportedImageFormat", "Unsupported image format")
	})
	It("internalInfraEnv not found", func() {
		ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
		Expect(c.Create(ctx, ppi)).To(BeNil())
		infraEnv = newInfraEnv("testInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{})
		Expect(c.Create(ctx, infraEnv)).To(BeNil())
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, errors.New("Failed to get internal infra env"))

		res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("Failed to get internal infra env"))
		Expect(res).To(Equal(ctrl.Result{}))
		checkImageConditionFailed(c, ppi, "IronicAgentIgnitionUpdateFailure", "Could not add ironic agent to image:")
	})

	It("returns an error when the ironic urls can't be found", func() {
		ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
		Expect(c.Create(ctx, ppi)).To(BeNil())

		infraEnv = newInfraEnv("testInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{})
		infraEnv.ObjectMeta.Annotations = make(map[string]string)
		infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation] = "invalid value"
		Expect(c.Create(ctx, infraEnv)).To(BeNil())

		backendInfraEnv.ClusterID = ""
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

		mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
		mockBMOUtils.EXPECT().GetIronicIPs().Return(nil, nil, fmt.Errorf("failed to get urls"))
		res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("failed to get urls"))
		Expect(res).To(Equal(ctrl.Result{}))
		checkImageConditionFailed(c, ppi, "IronicAgentIgnitionUpdateFailure", "Could not add ironic agent to image:")
	})
	It("Failed to UpdateInfraEnvInternal", func() {
		ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
		Expect(c.Create(ctx, ppi)).To(BeNil())
		infraEnv = newInfraEnv("testInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{})
		backendInfraEnv.ClusterID = ""
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		Expect(c.Create(ctx, infraEnv)).To(BeNil())
		mockBMOUtils.EXPECT().getICCConfig(gomock.Any()).Times(1).Return(nil, errors.Errorf("ICC configuration is not available"))
		mockBMOUtils.EXPECT().GetIronicIPs().AnyTimes().Return(ironicServiceIPs, ironicInspectorIPs, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Failed to update infraEnvInternal"))

		res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("Failed to update infraEnvInternal"))
		Expect(res).To(Equal(ctrl.Result{}))
		checkImageConditionFailed(c, ppi, "IronicAgentIgnitionUpdateFailure", "Could not add ironic agent to image:")
	})
	Context("map InfraEnv to PPI", func() {
		BeforeEach(func() {
			infraEnv = newInfraEnv("testInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{})
			ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
		})
		AfterEach(func() {
			mockCtrl.Finish()
		})
		It("Single PreprovisioningImage for infraEnv", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			Expect(c.Create(ctx, ppi)).To(BeNil())

			requests := pr.mapInfraEnvPPI(ctx, infraEnv)

			Expect(len(requests)).To(Equal(1))
		})
		It("Multiple PreprovisioningImage for infraEnv", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			Expect(c.Create(ctx, ppi)).To(BeNil())
			ppi2 := newPreprovisioningImage("testPPI2", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
			Expect(c.Create(ctx, ppi2)).To(BeNil())

			requests := pr.mapInfraEnvPPI(ctx, infraEnv)

			Expect(len(requests)).To(Equal(2))
		})
		It("Multiple PreprovisioningImage for diffrent infraEnv label", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			Expect(c.Create(ctx, ppi)).To(BeNil())
			ppi2 := newPreprovisioningImage("testPPI2", testNamespace, InfraEnvLabel, "someOtherInfraEnv", bmh.Name)
			Expect(c.Create(ctx, ppi2)).To(BeNil())

			requests := pr.mapInfraEnvPPI(ctx, infraEnv)

			Expect(len(requests)).To(Equal(1))
		})

		It("No PreprovisioningImage for infraEnv", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			requests := pr.mapInfraEnvPPI(ctx, infraEnv)

			Expect(len(requests)).To(Equal(0))
		})
	})
})

var _ = Describe("mapBMHtoPPI", func() {
	It("returns a request for the matching object", func() {
		bmh := &metal3_v1alpha1.BareMetalHost{ObjectMeta: metav1.ObjectMeta{Name: "testBMH", Namespace: testNamespace}}
		requests := mapBMHtoPPI(context.Background(), bmh)
		Expect(len(requests)).To(Equal(1))
		Expect(requests[0].Namespace).To(Equal(bmh.Namespace))
		Expect(requests[0].Name).To(Equal(bmh.Name))
	})
})

func checkImageConditionFailed(c client.Client, ppi *metal3_v1alpha1.PreprovisioningImage, reason string, messageSubstring string) {
	ppiKey := types.NamespacedName{Namespace: ppi.Namespace, Name: ppi.Name}
	Expect(c.Get(context.TODO(), ppiKey, ppi)).To(Succeed())
	fmt.Printf("%+v\n", ppi.Status.Conditions)
	readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
	Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
	errorCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError))
	Expect(errorCondition.Status).To(Equal(metav1.ConditionTrue))
	for _, condition := range []metav1.Condition{*readyCondition, *errorCondition} {
		Expect(condition.Message).To(ContainSubstring(messageSubstring))
		Expect(condition.Reason).To(Equal(reason))
	}
}

func SetImageUrl(ppi *metal3_v1alpha1.PreprovisioningImage, infraEnv aiv1beta1.InfraEnv) {
	ppi.Status.ImageUrl = infraEnv.Status.ISODownloadURL
	ppi.Status.Conditions = []metav1.Condition{
		{Type: string(metal3_v1alpha1.ConditionImageReady),
			Reason:  infraEnv.Status.Conditions[0].Reason,
			Message: infraEnv.Status.Conditions[0].Message,
			Status:  metav1.ConditionStatus(infraEnv.Status.Conditions[0].Status)},
		{Type: string(metal3_v1alpha1.ConditionImageError),
			Reason:  infraEnv.Status.Conditions[0].Reason,
			Message: infraEnv.Status.Conditions[0].Message,
			Status:  metav1.ConditionFalse},
	}
}

func validateStatus(imageURL string, ExpectedImageReadyCondition *conditionsv1.Condition, ppi *metal3_v1alpha1.PreprovisioningImage) {
	Expect(imageURL).To(Equal(ppi.Status.ImageUrl))
	readyCondition := meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageReady))
	Expect(metav1.ConditionStatus(ExpectedImageReadyCondition.Status)).To(Equal(readyCondition.Status))
	Expect(ExpectedImageReadyCondition.Message).To(Equal(readyCondition.Message))
	Expect(ExpectedImageReadyCondition.Reason).To(Equal(readyCondition.Reason))
	Expect(corev1.ConditionFalse).To(Not(Equal(
		meta.FindStatusCondition(ppi.Status.Conditions, string(metal3_v1alpha1.ConditionImageError)).Status)))

}

var _ = Describe("PreprovisioningImage deletion protection", func() {
	var (
		c                     client.Client
		pr                    *PreprovisioningImageReconciler
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		mockCRDEventsHandler  *MockCRDEventsHandler
		mockVersionHandler    *versions.MockHandler
		mockOcRelease         *oc.MockRelease
		mockBMOUtils          *MockBMOUtils
		ctx                   = context.Background()
		ppi                   *metal3_v1alpha1.PreprovisioningImage
		bmh                   *metal3_v1alpha1.BareMetalHost
	)

	BeforeEach(func() {
		schemes := runtime.NewScheme()
		Expect(configv1.AddToScheme(schemes)).To(Succeed())
		Expect(metal3_v1alpha1.AddToScheme(schemes)).To(Succeed())
		Expect(aiv1beta1.AddToScheme(schemes)).To(Succeed())
		c = fakeclient.NewClientBuilder().WithScheme(schemes).
			WithStatusSubresource(&metal3_v1alpha1.PreprovisioningImage{}, &metal3_v1alpha1.BareMetalHost{}).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		mockCRDEventsHandler = NewMockCRDEventsHandler(mockCtrl)
		mockVersionHandler = versions.NewMockHandler(mockCtrl)
		mockOcRelease = oc.NewMockRelease(mockCtrl)
		mockBMOUtils = NewMockBMOUtils(mockCtrl)

		pr = &PreprovisioningImageReconciler{
			Client:           c,
			Log:              common.GetTestLog(),
			Installer:        mockInstallerInternal,
			CRDEventsHandler: mockCRDEventsHandler,
			VersionsHandler:  mockVersionHandler,
			OcRelease:        mockOcRelease,
			BMOUtils:         mockBMOUtils,
		}

		bmh = &metal3_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testBMH",
				Namespace: testNamespace,
			},
			Spec: metal3_v1alpha1.BareMetalHostSpec{
				AutomatedCleaningMode: metal3_v1alpha1.CleaningModeMetadata,
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		ppi = newPreprovisioningImage("testPPI", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("ensurePreprovisioningImageFinalizer", func() {
		It("adds finalizer when not present on PreprovisioningImage", func() {
			ppi.Finalizers = []string{}
			Expect(c.Create(ctx, ppi)).To(Succeed())

			// Reconcile and verify finalizer was added
			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{Requeue: true}))

			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(c.Get(ctx, key, ppi)).To(Succeed())
			Expect(ppi.GetFinalizers()).To(ContainElement(PreprovisioningImageFinalizerName))
		})

		It("does nothing when finalizer is already present on PreprovisioningImage", func() {
			Expect(c.Create(ctx, ppi)).To(Succeed())

			// Reconcile and verify finalizer was not added again
			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(c.Get(ctx, key, ppi)).To(Succeed())
			Expect(ppi.GetFinalizers()).To(HaveLen(1))
			Expect(ppi.GetFinalizers()).To(ContainElement(PreprovisioningImageFinalizerName))
		})
	})

	Context("handlePreprovisioningImageDeletion", func() {
		It("allows deletion when finalizer is not present", func() {
			ppi.OwnerReferences = []metav1.OwnerReference{}
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())

			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify ppi was deleted
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(k8serrors.IsNotFound(c.Get(ctx, key, ppi))).To(BeTrue())
		})

		It("removes finalizer and allows deletion when BMH not found", func() {
			// Set owner reference to a non-existent BMH
			ppi.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "metal3.io/v1alpha1",
				Kind:       "BareMetalHost",
				Name:       "nonExistentBMH",
			}}
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())
			// Reconcile and verify ppi was deleted
			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify ppi was deleted
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(k8serrors.IsNotFound(c.Get(ctx, key, ppi))).To(BeTrue())
		})

		It("blocks deletion when BMH has metadata cleaning enabled and is being deleted", func() {
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())

			// UpdateBMH to set metadata cleaning enabled
			bmh.Spec.AutomatedCleaningMode = metal3_v1alpha1.CleaningModeMetadata
			bmh.Status.Provisioning.State = metal3_v1alpha1.StateDeprovisioning
			bmh.Finalizers = []string{"arbitraryfinalizer"}
			Expect(c.Update(ctx, bmh)).To(Succeed())
			// Set BMH to be deleting
			Expect(c.Delete(ctx, bmh)).To(Succeed())

			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))

			Expect(err).To(BeNil())
			Expect(result.Requeue).To(BeTrue())

			// Verify finalizer was NOT removed
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(c.Get(ctx, key, ppi)).To(Succeed())
			Expect(ppi.GetFinalizers()).To(ContainElement(PreprovisioningImageFinalizerName))
		})

		It("allows deletion when BMH has cleaning disabled", func() {
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())
			// Set BMH cleaning to disabled
			bmh.Spec.AutomatedCleaningMode = metal3_v1alpha1.CleaningModeDisabled
			Expect(c.Update(ctx, bmh)).To(Succeed())
			Expect(c.Delete(ctx, bmh)).To(Succeed())

			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))

			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify ppi was deleted
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(k8serrors.IsNotFound(c.Get(ctx, key, ppi))).To(BeTrue())
		})

		It("allows deletion when BMH finished deprovisioning with state deleting", func() {
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())
			// Set BMH to be deleting
			bmh.Spec.AutomatedCleaningMode = metal3_v1alpha1.CleaningModeMetadata
			bmh.Status.Provisioning.State = metal3_v1alpha1.StateDeleting
			Expect(c.Update(ctx, bmh)).To(Succeed())
			Expect(c.Delete(ctx, bmh)).To(Succeed())

			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))

			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify ppi was deleted
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(k8serrors.IsNotFound(c.Get(ctx, key, ppi))).To(BeTrue())
		})

		It("allows deletion when BMH finished deprovisioning with state powering off before delete", func() {
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())
			// Set BMH to be deleting
			bmh.Spec.AutomatedCleaningMode = metal3_v1alpha1.CleaningModeMetadata
			bmh.Status.Provisioning.State = metal3_v1alpha1.StatePoweringOffBeforeDelete
			Expect(c.Update(ctx, bmh)).To(Succeed())
			Expect(c.Delete(ctx, bmh)).To(Succeed())

			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))

			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify ppi was deleted
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(k8serrors.IsNotFound(c.Get(ctx, key, ppi))).To(BeTrue())
		})

		It("blocks deletion when BMH has metadata cleaning enabled but is NOT being deleted", func() {
			Expect(c.Create(ctx, ppi)).To(Succeed())
			// Delete ppi
			Expect(c.Delete(ctx, ppi)).To(Succeed())
			// BMH is not being deleted (no DeletionTimestamp)
			result, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))

			Expect(err).To(BeNil())
			Expect(result.Requeue).To(BeTrue())

			// Verify ppi finalizer was not removed
			key := types.NamespacedName{Name: ppi.Name, Namespace: ppi.Namespace}
			Expect(c.Get(ctx, key, ppi)).To(Succeed())
			Expect(ppi.GetFinalizers()).To(ContainElement(PreprovisioningImageFinalizerName))
		})
	})
})
