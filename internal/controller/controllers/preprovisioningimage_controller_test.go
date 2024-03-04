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
			WithStatusSubresource(&metal3_v1alpha1.PreprovisioningImage{}).Build()
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
			Config:           PreprovisioningImageControllerConfig{BaremetalIronicAgentImage: defaultIronicImage},
			BMOUtils:         mockBMOUtils,
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
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(defaultIronicImage))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicServiceIPs[0]))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicInspectorIPs[0]))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
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
		It("Adds the ironic IPv6 address to the ignition when the spoke cluster is IPv6 only", func() {
			backendCluster.MachineNetworks = []*models.MachineNetwork{
				{Cidr: "2001:db8::/32"},
			}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicServiceIPs[1])))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicInspectorIPs[1])))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
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

		It("adds both ironic urls to the ignition when the hub version supports it", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			ironicAgentImage := "ironic-image:4.12.0"
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return(ironicAgentImage, nil)
			// this version should be >= minimalVersionForIronicMultiURL
			mockOcRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("4.16.0", nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicServiceIPs[0])))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicInspectorIPs[0])))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicServiceIPs[1])))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(url.QueryEscape(ironicInspectorIPs[1])))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
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
			Expect(c.Update(ctx, ppi)).To(BeNil())

			newURL := "https://example.com/images/4b495e3f-6a3d-4742-aedd-7db57912c819?api_key=mykey&arch=x86_64&type=minimal-iso&version=4.13"
			infraEnv.Status.ISODownloadURL = newURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			setInfraEnvIronicConfig()

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

		It("Add the ironic Ignition to the infraEnv using the ironic agent image from the hub release", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			ironicAgentImage := "ironic-image:4.12.0"
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"x86_64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return(ironicAgentImage, nil)
			mockOcRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("4.12.0", nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic"))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicAgentImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
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
		It("uses the default ironic agent image when the infraenv arch isn't supported by the hub release image", func() {
			Expect(c.Create(ctx, clusterVersion)).To(Succeed())
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockOcRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return([]string{"arm64"}, nil)
			mockOcRelease.EXPECT().GetIronicAgentImage(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("ironic-image:4.12.0", nil)
			mockOcRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("4.12.0", nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(""))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic"))
					Expect(*internalIgnitionConfig).Should(ContainSubstring("ironic-agent-image:latest"))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
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
			mockOcRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), hubReleaseImage, "", backendInfraEnv.PullSecret).Return("4.12.0", nil).Times(1)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(*internalIgnitionConfig).Should(ContainSubstring(ironicAgentImage))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: clusterID, ID: &infraEnvID, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(2)
			mockCRDEventsHandler.EXPECT().NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace).Times(2)

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
			overrideAgentImage := "ironic-agent-override:4.13.0"
			setAnnotation(&infraEnv.ObjectMeta, ironicAgentImageOverrideAnnotation, overrideAgentImage)
			setAnnotation(&infraEnv.ObjectMeta, ironicAgentImageOverrideVersionAnnotation, "4.13.0")
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			backendInfraEnv.CPUArchitecture = "x86_64"
			backendInfraEnv.PullSecret = "mypullsecret"
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)

			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(internalIgnitionConfig).Should(HaveValue(ContainSubstring(overrideAgentImage)))
				})
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

			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
		})

		It("infraEnv not found", func() {
			res, err := pr.Reconcile(ctx, newPreprovisioningImageRequest(ppi))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
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
		mockBMOUtils.EXPECT().GetIronicIPs().AnyTimes().Return(ironicServiceIPs, ironicInspectorIPs, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Failed to update infraEnvInternal"))

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

			requests := pr.mapInfraEnvPPI()(ctx, infraEnv)

			Expect(len(requests)).To(Equal(1))
		})
		It("Multiple PreprovisioningImage for infraEnv", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			Expect(c.Create(ctx, ppi)).To(BeNil())
			ppi2 := newPreprovisioningImage("testPPI2", testNamespace, InfraEnvLabel, "testInfraEnv", bmh.Name)
			Expect(c.Create(ctx, ppi2)).To(BeNil())

			requests := pr.mapInfraEnvPPI()(ctx, infraEnv)

			Expect(len(requests)).To(Equal(2))
		})
		It("Multiple PreprovisioningImage for diffrent infraEnv label", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			Expect(c.Create(ctx, ppi)).To(BeNil())
			ppi2 := newPreprovisioningImage("testPPI2", testNamespace, InfraEnvLabel, "someOtherInfraEnv", bmh.Name)
			Expect(c.Create(ctx, ppi2)).To(BeNil())

			requests := pr.mapInfraEnvPPI()(ctx, infraEnv)

			Expect(len(requests)).To(Equal(1))
		})

		It("No PreprovisioningImage for infraEnv", func() {
			infraEnv.Status.ISODownloadURL = downloadURL
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			requests := pr.mapInfraEnvPPI()(ctx, infraEnv)

			Expect(len(requests)).To(Equal(0))
		})
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
