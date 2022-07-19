package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newInfraEnvRequest(image *aiv1beta1.InfraEnv) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: image.ObjectMeta.Namespace,
		Name:      image.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newInfraEnvImage(name, namespace string, spec aiv1beta1.InfraEnvSpec) *aiv1beta1.InfraEnv {
	return &aiv1beta1.InfraEnv{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InfraEnv",
			APIVersion: fmt.Sprintf("%s/%s", aiv1beta1.GroupVersion.Group, aiv1beta1.GroupVersion.Version),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

func newNMStateConfig(name, namespace, NMStateLabelName, NMStateLabelValue string, spec aiv1beta1.NMStateConfigSpec) *aiv1beta1.NMStateConfig {
	return &aiv1beta1.NMStateConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NMStateConfig",
			APIVersion: fmt.Sprintf("%s/%s", aiv1beta1.GroupVersion.Group, aiv1beta1.GroupVersion.Version),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{NMStateLabelName: NMStateLabelValue},
		},
		Spec: spec,
	}
}

var _ = Describe("infraEnv reconcile", func() {
	var (
		c                     client.Client
		ir                    *InfraEnvReconciler
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		mockVersionsHandler   *versions.MockHandler
		ctx                   = context.Background()
		sId                   strfmt.UUID
		backEndCluster        = &common.Cluster{Cluster: models.Cluster{ID: &sId}}
		backendInfraEnv       = &common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId}}
		downloadURL           = "https://downloadurl"
		eventURL              string
		infraEnvArch          = "x86_64"
		ocpVersion            = "4.10"
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		mockVersionsHandler = versions.NewMockHandler(mockCtrl)
		sId = strfmt.UUID(uuid.New().String())
		ir = &InfraEnvReconciler{
			Client:              c,
			Config:              InfraEnvConfig{ImageType: models.ImageTypeMinimalIso},
			Log:                 common.GetTestLog(),
			Installer:           mockInstallerInternal,
			APIReader:           c,
			ServiceBaseURL:      "https://www.acme.com",
			ImageServiceBaseURL: "https://images.example.com",
			VersionsHandler:     mockVersionsHandler,
			AuthType:            auth.TypeNone,
		}
		pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
		eventURL = fmt.Sprintf("%s/api/assisted-install/v2/events?infra_env_id=%s", ir.ServiceBaseURL, sId)
		Expect(c.Create(ctx, pullSecret)).To(BeNil())
		mockVersionsHandler.EXPECT().GetLatestOsImage(infraEnvArch).Return(&models.OsImage{CPUArchitecture: swag.String(infraEnvArch), OpenshiftVersion: swag.String(ocpVersion)}, nil).AnyTimes()
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("none exiting infraEnv - delete", func() {
		infraEnvImage := newInfraEnvImage("infraEnvImage", "namespace", aiv1beta1.InfraEnvSpec{})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		noneExistingImage := newInfraEnvImage("image2", "namespace", aiv1beta1.InfraEnvSpec{})
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().DeregisterInfraEnvInternal(gomock.Any(), gomock.Any()).Return(nil)

		result, err := ir.Reconcile(ctx, newInfraEnvRequest(noneExistingImage))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("create new infraEnv minimal-iso image - success", func() {
		imageInfo := models.ImageInfo{
			DownloadURL: "https://downloadurl",
			CreatedAt:   time.Now(),
		}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
			}).Return(
			&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.Status.ISODownloadURL).To(Equal(imageInfo.DownloadURL))
		Expect(infraEnvImage.Status.CreatedTime).ToNot(BeNil())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(aiv1beta1.ImageStateCreated))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreatedReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))
		Expect(infraEnvImage.Status.AgentLabelSelector).To(Equal(metav1.LabelSelector{MatchLabels: map[string]string{aiv1beta1.InfraEnvNameLabel: "infraEnvImage"}}))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).NotTo(BeEmpty())
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(HavePrefix(eventURL))
	})

	It("create new infraEnv full-iso image - success", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeFullIso))
			}).Return(
			&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)

		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		ir.Config.ImageType = models.ImageTypeFullIso
		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.Status.ISODownloadURL).To(Equal(downloadURL))
		Expect(infraEnvImage.Status.CreatedTime).ToNot(BeNil())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(aiv1beta1.ImageStateCreated))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreatedReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).NotTo(BeEmpty())
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(HavePrefix(eventURL))
	})

	It("IPXE with HostRedirect script type", func() {
		dbInfraEnv := &common.InfraEnv{
			GeneratedAt: strfmt.DateTime(time.Now()),
			InfraEnv: models.InfraEnv{
				ID:              &sId,
				CPUArchitecture: infraEnvArch,
				DownloadURL:     "https://images.example.com/images/best-image",
			},
		}
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).Return(dbInfraEnv, nil).Times(1)
		kubeInfraEnv := newInfraEnvImage("myInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{
			PullSecretRef:  &corev1.LocalObjectReference{Name: "pull-secret"},
			IPXEScriptType: aiv1beta1.HostRedirect,
		})
		Expect(c.Create(ctx, kubeInfraEnv)).To(Succeed())

		_, err := ir.Reconcile(ctx, newInfraEnvRequest(kubeInfraEnv))
		Expect(err).ToNot(HaveOccurred())

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "myInfraEnv",
		}
		Expect(c.Get(ctx, key, kubeInfraEnv)).To(BeNil())

		kernelURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.KernelURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(kernelURL.Scheme).To(Equal("https"))
		Expect(kernelURL.Host).To(Equal("images.example.com"))
		Expect(kernelURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(kernelURL.Query().Get("version")).To(Equal(ocpVersion))

		rootfsURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.RootfsURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(rootfsURL.Scheme).To(Equal("https"))
		Expect(rootfsURL.Host).To(Equal("images.example.com"))
		Expect(rootfsURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(rootfsURL.Query().Get("version")).To(Equal(ocpVersion))

		initrdURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.InitrdURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(initrdURL.Scheme).To(Equal("https"))
		Expect(initrdURL.Host).To(Equal("images.example.com"))
		Expect(initrdURL.Path).To(ContainSubstring(sId.String()))
		Expect(initrdURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(initrdURL.Query().Get("version")).To(Equal(ocpVersion))

		scriptURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.IpxeScriptURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(scriptURL.Scheme).To(Equal("https"))
		Expect(scriptURL.Host).To(Equal("www.acme.com"))
		Expect(scriptURL.Path).To(ContainSubstring(sId.String()))
		Expect(scriptURL.Query().Get("file_name")).To(Equal("ipxe-script"))
		Expect(scriptURL.Query().Get("boot_control")).To(Equal("true"))
	})

	It("IPXE with Boot script type", func() {
		dbInfraEnv := &common.InfraEnv{
			GeneratedAt: strfmt.DateTime(time.Now()),
			InfraEnv: models.InfraEnv{
				ID:              &sId,
				CPUArchitecture: infraEnvArch,
				DownloadURL:     "https://images.example.com/images/best-image",
			},
		}
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).Return(dbInfraEnv, nil).Times(1)
		kubeInfraEnv := newInfraEnvImage("myInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{
			PullSecretRef:  &corev1.LocalObjectReference{Name: "pull-secret"},
			IPXEScriptType: aiv1beta1.Boot,
		})
		Expect(c.Create(ctx, kubeInfraEnv)).To(Succeed())

		_, err := ir.Reconcile(ctx, newInfraEnvRequest(kubeInfraEnv))
		Expect(err).ToNot(HaveOccurred())

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "myInfraEnv",
		}
		Expect(c.Get(ctx, key, kubeInfraEnv)).To(BeNil())

		kernelURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.KernelURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(kernelURL.Scheme).To(Equal("https"))
		Expect(kernelURL.Host).To(Equal("images.example.com"))
		Expect(kernelURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(kernelURL.Query().Get("version")).To(Equal(ocpVersion))

		rootfsURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.RootfsURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(rootfsURL.Scheme).To(Equal("https"))
		Expect(rootfsURL.Host).To(Equal("images.example.com"))
		Expect(rootfsURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(rootfsURL.Query().Get("version")).To(Equal(ocpVersion))

		initrdURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.InitrdURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(initrdURL.Scheme).To(Equal("https"))
		Expect(initrdURL.Host).To(Equal("images.example.com"))
		Expect(initrdURL.Path).To(ContainSubstring(sId.String()))
		Expect(initrdURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(initrdURL.Query().Get("version")).To(Equal(ocpVersion))

		scriptURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.IpxeScriptURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(scriptURL.Scheme).To(Equal("https"))
		Expect(scriptURL.Host).To(Equal("www.acme.com"))
		Expect(scriptURL.Path).To(ContainSubstring(sId.String()))
		Expect(scriptURL.Query().Get("file_name")).To(Equal("ipxe-script"))
		Expect(scriptURL.Query().Has("boot_control")).To(BeFalse())
	})

	It("sets boot artifact URLs correctly", func() {
		dbInfraEnv := &common.InfraEnv{
			GeneratedAt: strfmt.DateTime(time.Now()),
			InfraEnv: models.InfraEnv{
				ID:              &sId,
				CPUArchitecture: infraEnvArch,
				DownloadURL:     "https://images.example.com/images/best-image",
			},
		}
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).Return(dbInfraEnv, nil).Times(1)
		kubeInfraEnv := newInfraEnvImage("myInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, kubeInfraEnv)).To(Succeed())

		_, err := ir.Reconcile(ctx, newInfraEnvRequest(kubeInfraEnv))
		Expect(err).ToNot(HaveOccurred())

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "myInfraEnv",
		}
		Expect(c.Get(ctx, key, kubeInfraEnv)).To(BeNil())

		kernelURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.KernelURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(kernelURL.Scheme).To(Equal("https"))
		Expect(kernelURL.Host).To(Equal("images.example.com"))
		Expect(kernelURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(kernelURL.Query().Get("version")).To(Equal(ocpVersion))

		rootfsURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.RootfsURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(rootfsURL.Scheme).To(Equal("https"))
		Expect(rootfsURL.Host).To(Equal("images.example.com"))
		Expect(rootfsURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(rootfsURL.Query().Get("version")).To(Equal(ocpVersion))

		initrdURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.InitrdURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(initrdURL.Scheme).To(Equal("https"))
		Expect(initrdURL.Host).To(Equal("images.example.com"))
		Expect(initrdURL.Path).To(ContainSubstring(sId.String()))
		Expect(initrdURL.Query().Get("arch")).To(Equal(infraEnvArch))
		Expect(initrdURL.Query().Get("version")).To(Equal(ocpVersion))

		scriptURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.IpxeScriptURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(scriptURL.Scheme).To(Equal("https"))
		Expect(scriptURL.Host).To(Equal("www.acme.com"))
		Expect(scriptURL.Path).To(ContainSubstring(sId.String()))
		Expect(scriptURL.Query().Get("file_name")).To(Equal("ipxe-script"))
		Expect(scriptURL.Query().Has("boot_control")).To(BeFalse())
	})

	Context("with local auth", func() {
		BeforeEach(func() {
			_, priv, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).NotTo(HaveOccurred())
			os.Setenv("EC_PRIVATE_KEY_PEM", priv)
			ir.AuthType = auth.TypeLocal
		})

		AfterEach(func() {
			os.Unsetenv("EC_PRIVATE_KEY_PEM")
		})

		It("signs the initrd and script download URLs", func() {
			dbInfraEnv := &common.InfraEnv{
				GeneratedAt: strfmt.DateTime(time.Now()),
				InfraEnv: models.InfraEnv{
					ID:              &sId,
					CPUArchitecture: infraEnvArch,
					DownloadURL:     "https://images.example.com/images/best-image",
				},
			}
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).Return(dbInfraEnv, nil).Times(1)
			kubeInfraEnv := newInfraEnvImage("myInfraEnv", testNamespace, aiv1beta1.InfraEnvSpec{
				PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
			})
			Expect(c.Create(ctx, kubeInfraEnv)).To(Succeed())

			_, err := ir.Reconcile(ctx, newInfraEnvRequest(kubeInfraEnv))
			Expect(err).ToNot(HaveOccurred())

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "myInfraEnv",
			}
			Expect(c.Get(ctx, key, kubeInfraEnv)).To(BeNil())
			initrdURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.InitrdURL)
			Expect(err).ToNot(HaveOccurred())
			Expect(initrdURL.Query().Get("api_key")).ToNot(BeEmpty())

			scriptURL, err := url.Parse(kubeInfraEnv.Status.BootArtifacts.IpxeScriptURL)
			Expect(err).ToNot(HaveOccurred())
			Expect(scriptURL.Query().Get("api_key")).ToNot(BeEmpty())
		})
	})

	It("create new infraEnv image - backend failure", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusInternalServerError, errors.New("server error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
			}).Return(nil, expectedError).Times(1)

		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterPerRecoverableError}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		expectedState := fmt.Sprintf("%s due to an internal error: server error", aiv1beta1.ImageStateFailedToCreate)
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).NotTo(BeEmpty())
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(HavePrefix(eventURL))
	})

	It("create new infraEnv image - cluster not retrieved from database", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusInternalServerError, errors.New("server error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, expectedError)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		expectedState := fmt.Sprintf("%s: server error", aiv1beta1.ImageStateFailedToCreate)
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(BeEmpty())
	})

	It("create new infraEnv image - cluster not found in database", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		expectedState := fmt.Sprintf("%s: cluster does not exist: clusterDeployment, check AgentClusterInstall conditions: name %s in namespace %s",
			aiv1beta1.ImageStateFailedToCreate, clusterDeployment.Spec.ClusterInstallRef.Name, clusterDeployment.Namespace)
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(BeEmpty())
	})

	It("create new infraEnv image - while image is being created", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusConflict, errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
			}).Return(nil, expectedError).Times(1)

		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterPerRecoverableError}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(aiv1beta1.ImageStateCreated))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreatedReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).NotTo(BeEmpty())
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(HavePrefix(eventURL))
	})

	It("create new image - client failure and retry immediately that results HTTP 409 StatusConflict", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedClientError := common.NewApiError(http.StatusBadRequest, errors.New("client error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(2)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil).Times(2)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
			}).Return(nil, expectedClientError).Times(1)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}

		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))

		// retry immediately

		expectedConflictError := common.NewApiError(http.StatusConflict, errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
			}).Return(nil, expectedConflictError).Times(1)
		res, err = ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))

		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterPerRecoverableError}))
	})

	It("create new image - client failure", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusBadRequest, errors.New("client error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
			}).Return(nil, expectedError).Times(1)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())

		expectedState := fmt.Sprintf("%s: %s", aiv1beta1.ImageStateFailedToCreate, expectedError.Error())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).NotTo(BeEmpty())
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(HavePrefix(eventURL))
	})

	It("create new image - clusterDeployment not exists", func() {
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())

		expectedState := fmt.Sprintf(
			"%s: failed to get clusterDeployment with name clusterDeployment in namespace %s: "+
				"clusterdeployments.hive.openshift.io \"clusterDeployment\" not found",
			aiv1beta1.ImageStateFailedToCreate, testNamespace)
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))

		By("validate events URL")
		Expect(infraEnvImage.Status.InfraEnvDebugInfo.EventsURL).To(BeEmpty())
	})

	It("create image with proxy configuration and ntp sources", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				Proxy:                &aiv1beta1.Proxy{HTTPProxy: "http://192.168.1.2"},
				AdditionalNTPSources: []string{"foo.com", "bar.com"},
				ClusterRef:           &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "pull-secret",
				},
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(swag.StringValue(params.InfraEnvUpdateParams.Proxy.HTTPProxy)).To(Equal("http://192.168.1.2"))
				Expect(swag.StringValue(params.InfraEnvUpdateParams.AdditionalNtpSources)).To(Equal("foo.com,bar.com"))
			}).Return(&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}}, nil).Times(1)

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))
	})

	It("create image with ignition config override", func() {
		ignitionConfigOverride := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				ClusterRef:             &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				IgnitionConfigOverride: ignitionConfigOverride,
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "pull-secret",
				},
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(ignitionConfigOverride))
			}).Return(&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}}, nil).Times(1)

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))
	})

	It("create image with an invalid ignition config override", func() {
		ignitionConfigOverride := `bad ignition config`
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				ClusterRef:             &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				IgnitionConfigOverride: ignitionConfigOverride,
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "pull-secret",
				},
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.IgnitionConfigOverride).To(Equal(ignitionConfigOverride))
			}).Return(nil, errors.Errorf("error")).Times(1)

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterPerRecoverableError}))
	})

	It("failed to update infraenv with proxy", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				Proxy:      &aiv1beta1.Proxy{HTTPProxy: "http://192.168.1.2"},
				ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "pull-secret",
				},
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).Return(nil, errors.Errorf("failure")).Times(1)

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterPerRecoverableError}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("Delete infraEnv with no hosts verify finalizer removed", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
			}).Return(
			&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		// Verify finalizer was added
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.Finalizers).ToNot(BeNil())
		Expect(infraEnvImage.Finalizers[0]).To(Equal(InfraEnvFinalizerName))

		//Delete InfraEnv, finalizer still exists
		Expect(c.Delete(ctx, infraEnvImage)).To(BeNil())
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())
		Expect(infraEnvImage.Finalizers).ToNot(BeNil())

		// Reconcile and verify CR is deleted
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvHostsInternal(gomock.Any(), gomock.Any()).Return([]*common.Host{}, nil)
		mockInstallerInternal.EXPECT().DeregisterInfraEnvInternal(gomock.Any(), gomock.Any()).Return(nil)
		res, err = ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(apierrors.IsNotFound(c.Get(ctx, key, infraEnvImage))).To(BeTrue())
	})

	It("Delete infraEnv with Unbound hosts verify hosts are deleted", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
			}).Return(
			&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		//Delete InfraEnv, finalizer still exists
		Expect(c.Delete(ctx, infraEnvImage)).To(BeNil())
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())
		Expect(infraEnvImage.Finalizers).ToNot(BeNil())

		// Reconcile and verify only Bound Host is deleted
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		hostId := strfmt.UUID(uuid.New().String())
		host := &common.Host{Host: models.Host{ID: &hostId, Status: swag.String(models.HostStatusKnownUnbound)}}
		mockInstallerInternal.EXPECT().GetInfraEnvHostsInternal(gomock.Any(), gomock.Any()).Return([]*common.Host{host}, nil)
		mockInstallerInternal.EXPECT().V2DeregisterHostInternal(gomock.Any(), gomock.Any()).Return(nil)
		mockInstallerInternal.EXPECT().DeregisterInfraEnvInternal(gomock.Any(), gomock.Any()).Return(nil)
		res, err = ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		// Verify that InfraEnv CR is deleted
		Expect(apierrors.IsNotFound(c.Get(ctx, key, infraEnvImage))).To(BeTrue())
	})

	It("Delete infraEnv with Bound and Unbound hosts verify only Unbound hosts are deleted", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
			Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
				Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
				Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
			}).Return(
			&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(1)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
		})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "infraEnvImage",
		}
		//Delete InfraEnv, finalizer still exists
		Expect(c.Delete(ctx, infraEnvImage)).To(BeNil())
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())
		Expect(infraEnvImage.Finalizers).ToNot(BeNil())

		// Reconcile and verify Host are deleted
		mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
		hostUnboundId := strfmt.UUID(uuid.New().String())
		hostBoundId := strfmt.UUID(uuid.New().String())
		hostUnbound := &common.Host{Host: models.Host{ID: &hostUnboundId, InfraEnvID: *backendInfraEnv.ID, Status: swag.String(models.HostStatusKnownUnbound)}}
		hostBound := &common.Host{Host: models.Host{ID: &hostBoundId, InfraEnvID: *backendInfraEnv.ID, Status: swag.String(models.HostStatusKnown)}}
		mockInstallerInternal.EXPECT().GetInfraEnvHostsInternal(gomock.Any(), gomock.Any()).Return([]*common.Host{hostUnbound, hostBound}, nil)
		mockInstallerInternal.EXPECT().V2DeregisterHostInternal(gomock.Any(), installer.V2DeregisterHostParams{
			InfraEnvID: *backendInfraEnv.ID,
			HostID:     hostUnboundId,
		}).Return(nil)
		res, err = ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(Not(BeNil()))
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: longerRequeueAfterOnError}))

		//Verify that InfraEnv CR still exists with finalizer
		Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
		Expect(infraEnvImage.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())
		Expect(infraEnvImage.Finalizers).ToNot(BeNil())
	})

	Context("with ipxe http route", func() {
		BeforeEach(func() {
			ir.InsecureIPXEURLs = false
		})

		AfterEach(func() {
			ir.InsecureIPXEURLs = false
		})

		It("Update infraenv status on IPXEHTTPRoute change", func() {
			clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
			Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(2)
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil).Times(2)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}, GeneratedAt: strfmt.DateTime(time.Now())}, nil).Times(2)
			infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
				ClusterRef:    &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				PullSecretRef: &corev1.LocalObjectReference{Name: "pull-secret"},
			})
			Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

			res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "infraEnvImage",
			}
			Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
			Expect(infraEnvImage.Status.BootArtifacts.KernelURL).To(ContainSubstring("https://"))
			Expect(infraEnvImage.Status.ISODownloadURL).To(ContainSubstring("https://"))

			ir.InsecureIPXEURLs = true
			res, err = ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))
			Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
			Expect(infraEnvImage.Status.BootArtifacts.KernelURL).To(ContainSubstring("http://"))
			Expect(infraEnvImage.Status.BootArtifacts.InitrdURL).To(ContainSubstring("http://"))
			Expect(infraEnvImage.Status.BootArtifacts.RootfsURL).To(ContainSubstring("http://"))
			Expect(infraEnvImage.Status.BootArtifacts.IpxeScriptURL).To(ContainSubstring("http://"))
			Expect(infraEnvImage.Status.ISODownloadURL).To(ContainSubstring("https://"))
		})
	})

	Context("CreateInfraEnvParams", func() {
		var (
			clusterName      = "test-cluster"
			pullSecretName   = "pull-secret"
			pullSecretString = "pull-secret-string"
			cpuArch          = "x86_64"
			openshiftVersion = "4.10.0-rc1"
			imageType        = "full-iso"
		)
		It("create new param - success", func() {
			cluster := &common.Cluster{Cluster: models.Cluster{ID: &sId, CPUArchitecture: cpuArch, OpenshiftVersion: openshiftVersion}}

			infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
				ClusterRef:    &aiv1beta1.ClusterReference{Name: clusterName, Namespace: testNamespace},
				PullSecretRef: &corev1.LocalObjectReference{Name: pullSecretName},
			})
			params := CreateInfraEnvParams(infraEnvImage, models.ImageType(imageType), pullSecretString, cluster.ID, cluster.OpenshiftVersion)

			Expect(params).ToNot(BeNil())
			Expect(params.InfraenvCreateParams.ClusterID).To(Equal(cluster.ID))
			Expect(params.InfraenvCreateParams.PullSecret).To(Equal(&pullSecretString))
			Expect(params.InfraenvCreateParams.OpenshiftVersion).To(Equal(cluster.OpenshiftVersion))
			Expect(params.InfraenvCreateParams.CPUArchitecture).To(Equal(infraEnvImage.Spec.CpuArchitecture))
			Expect(params.InfraenvCreateParams.IgnitionConfigOverride).To(Equal(infraEnvImage.Spec.IgnitionConfigOverride))
			Expect(params.InfraenvCreateParams.SSHAuthorizedKey).To(Equal(&infraEnvImage.Spec.SSHAuthorizedKey))
		})
	})

	Context("nmstate config", func() {

		var (
			NMStateLabelName        = "someName"
			NMStateLabelValue       = "someValue"
			nicPrimary              = "eth0"
			nicSecondary            = "eth1"
			macPrimary              = "09:23:0f:d8:92:AA"
			macSecondary            = "09:23:0f:d8:92:AB"
			ip4Primary              = "192.168.126.30"
			ip4Secondary            = "192.168.140.30"
			dnsGW                   = "192.168.126.1"
			hostStaticNetworkConfig *models.HostStaticNetworkConfig
		)
		BeforeEach(func() {
			hostStaticNetworkConfig = common.FormatStaticConfigHostYAML(
				nicPrimary, nicSecondary, ip4Primary, ip4Secondary, dnsGW,
				models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: macPrimary, LogicalNicName: nicPrimary},
					&models.MacInterfaceMapItems0{MacAddress: macSecondary, LogicalNicName: nicSecondary},
				})
		})

		It("create new infraEnv image with nmstate config - success", func() {
			nmstateConfig := newNMStateConfig("NMStateConfig", testNamespace, NMStateLabelName, NMStateLabelValue,
				aiv1beta1.NMStateConfigSpec{
					Interfaces: []*aiv1beta1.Interface{
						{Name: nicPrimary, MacAddress: macPrimary},
						{Name: nicSecondary, MacAddress: macSecondary},
					},
					NetConfig: aiv1beta1.NetConfig{Raw: []byte(hostStaticNetworkConfig.NetworkYaml)},
				})
			Expect(c.Create(ctx, nmstateConfig)).To(BeNil())
			clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
			Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
					Expect(params.InfraEnvUpdateParams.StaticNetworkConfig).To(Equal([]*models.HostStaticNetworkConfig{hostStaticNetworkConfig}))
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}}, nil).Times(1)

			infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
				NMStateConfigLabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}},
				ClusterRef:                 &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				PullSecretRef:              &corev1.LocalObjectReference{Name: "pull-secret"},
			})
			Expect(c.Create(ctx, infraEnvImage)).To(BeNil())
			res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "infraEnvImage",
			}
			Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(aiv1beta1.ImageStateCreated))
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreatedReason))
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))

			// Remove nmstate selector from infraenv and reconcile again, this
			// time we expect the StaticNetworkConfig in the
			// InfraEnvUpdateParams to be empty. This extra assertion was added
			// to make sure that the infra env doesn't use all NMStateConfigs
			// in the namespace when the selector is omitted.
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
					Expect(params.InfraEnvUpdateParams.StaticNetworkConfig).To(BeEmpty())
				}).Return(
				&common.InfraEnv{InfraEnv: models.InfraEnv{ClusterID: sId, ID: &sId, DownloadURL: downloadURL, CPUArchitecture: infraEnvArch}}, nil).Times(1)

			infraEnvImage.Spec.NMStateConfigLabelSelector = metav1.LabelSelector{}
			Expect(c.Update(ctx, infraEnvImage)).To(BeNil())
			res, err = ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{}))

			Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(aiv1beta1.ImageStateCreated))
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreatedReason))
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("create new infraEnv image with an invalid nmstate config - fail", func() {
			hostStaticNetworkConfig.NetworkYaml = "interfaces:\n    - foo: badConfig"
			nmstateConfig := newNMStateConfig("NMStateConfig", testNamespace, NMStateLabelName, NMStateLabelValue,
				aiv1beta1.NMStateConfigSpec{
					Interfaces: []*aiv1beta1.Interface{
						{Name: nicPrimary, MacAddress: macPrimary},
						{Name: nicSecondary, MacAddress: macSecondary},
					},
					NetConfig: aiv1beta1.NetConfig{Raw: []byte(hostStaticNetworkConfig.NetworkYaml)},
				})
			Expect(c.Create(ctx, nmstateConfig)).To(BeNil())
			clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
			Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			expectedError := common.NewApiError(http.StatusBadRequest, errors.New("internal error"))
			mockInstallerInternal.EXPECT().GetInfraEnvByKubeKey(gomock.Any()).Return(backendInfraEnv, nil)
			mockInstallerInternal.EXPECT().UpdateInfraEnvInternal(gomock.Any(), gomock.Any(), nil).
				Do(func(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) {
					Expect(params.InfraEnvID).To(Equal(*backendInfraEnv.ID))
					Expect(params.InfraEnvUpdateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
				}).Return(nil, expectedError).Times(1)

			infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
				NMStateConfigLabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}},
				ClusterRef:                 &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				PullSecretRef:              &corev1.LocalObjectReference{Name: "pull-secret"},
			})
			Expect(c.Create(ctx, infraEnvImage)).To(BeNil())
			res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
			Expect(err).To(BeNil())
			Expect(res).To(Equal(ctrl.Result{Requeue: false, RequeueAfter: 0}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      "infraEnvImage",
			}
			Expect(c.Get(ctx, key, infraEnvImage)).To(BeNil())
			expectedState := fmt.Sprintf("%s: internal error", aiv1beta1.ImageStateFailedToCreate)
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
			Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
		})
	})
})
