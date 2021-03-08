package controllers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newInstallEnvRequest(image *adiiov1alpha1.InstallEnv) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: image.ObjectMeta.Namespace,
		Name:      image.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newInstallEnvImage(name, namespace string, spec adiiov1alpha1.InstallEnvSpec) *adiiov1alpha1.InstallEnv {
	return &adiiov1alpha1.InstallEnv{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InstallEnv",
			APIVersion: "adi.io.my.domain/adiiov1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

var _ = Describe("installEnv reconcile", func() {
	var (
		c                     client.Client
		ir                    *InstallEnvReconciler
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		ctx                   = context.Background()
		sId                   strfmt.UUID
		backEndCluster        = &common.Cluster{Cluster: models.Cluster{ID: &sId}}
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		ir = &InstallEnvReconciler{
			Client:    c,
			Log:       common.GetTestLog(),
			Installer: mockInstallerInternal,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("none exiting installEnv Image", func() {
		installEnvImage := newInstallEnvImage("installEnvImage", "namespace", adiiov1alpha1.InstallEnvSpec{})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		noneExistingImage := newInstallEnvImage("image2", "namespace", adiiov1alpha1.InstallEnvSpec{})

		result, err := ir.Reconcile(newInstallEnvRequest(noneExistingImage))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("create new installEnv image - success", func() {
		imageInfo := models.ImageInfo{
			DownloadURL: "downloadurl",
		}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())
		Expect(installEnvImage.Status.ISODownloadURL).To(Equal(imageInfo.DownloadURL))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(adiiov1alpha1.ImageStateCreated))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreatedReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("create new installEnv image - backend failure", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusInternalServerError, errors.New("server error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedError).Times(1)

		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())
		expectedState := fmt.Sprintf("%s: internal error", adiiov1alpha1.ImageStateFailedToCreate)
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("create new installEnv image - cluster not retrieved from database", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusInternalServerError, errors.New("server error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, expectedError)
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())
		expectedState := fmt.Sprintf("%s: server error", adiiov1alpha1.ImageStateFailedToCreate)
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))
	})

	It("create new installEnv image - cluster not found in database", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())
		expectedState := fmt.Sprintf("%s: record not found", adiiov1alpha1.ImageStateFailedToCreate)
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))
	})

	It("create new installEnv image - while image is being created", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusConflict, errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedError).Times(1)

		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(adiiov1alpha1.ImageStateCreated))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreatedReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("create new image - client failure", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusBadRequest, errors.New("client error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedError).Times(1)
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())

		expectedState := fmt.Sprintf("%s: %s", adiiov1alpha1.ImageStateFailedToCreate, expectedError.Error())
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("create new image - clusterDeployment not exists", func() {
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, adiiov1alpha1.InstallEnvSpec{
			ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())

		expectedState := fmt.Sprintf(
			"%s: failed to get clusterDeployment with name clusterDeployment in namespace %s: "+
				"clusterdeployments.hive.openshift.io \"clusterDeployment\" not found",
			adiiov1alpha1.ImageStateFailedToCreate, testNamespace)
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))
	})

	It("create image with proxy configuration and ntp sources", func() {
		imageInfo := models.ImageInfo{DownloadURL: "downloadurl"}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace,
			adiiov1alpha1.InstallEnvSpec{
				Proxy:                &adiiov1alpha1.Proxy{HTTPProxy: "http://192.168.1.2"},
				AdditionalNTPSources: []string{"foo.com", "bar.com"},
				ClusterRef:           &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateClusterParams) {
				Expect(swag.StringValue(param.ClusterUpdateParams.HTTPProxy)).To(Equal("http://192.168.1.2"))
				Expect(swag.StringValue(param.ClusterUpdateParams.AdditionalNtpSource)).To(Equal("foo.com,bar.com"))
			}).Return(nil, nil).Times(1)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))
	})

	It("create image with ignition config override", func() {
		imageInfo := models.ImageInfo{DownloadURL: "downloadurl"}
		ignitionConfigOverride := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace,
			adiiov1alpha1.InstallEnvSpec{
				ClusterRef:             &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				IgnitionConfigOverride: ignitionConfigOverride,
			})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateDiscoveryIgnitionInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateDiscoveryIgnitionParams) {
				Expect(swag.StringValue(&param.DiscoveryIgnitionParams.Config)).To(Equal(ignitionConfigOverride))
			}).Return(nil).Times(1)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))
	})

	It("create image with an invalid ignition config override", func() {
		ignitionConfigOverride := `bad ignition config`
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace,
			adiiov1alpha1.InstallEnvSpec{
				ClusterRef:             &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				IgnitionConfigOverride: ignitionConfigOverride,
			})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateDiscoveryIgnitionInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateDiscoveryIgnitionParams) {
				Expect(swag.StringValue(&param.DiscoveryIgnitionParams.Config)).To(Equal(ignitionConfigOverride))
			}).Return(errors.Errorf("error")).Times(1)
		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))
	})

	It("failed to update cluster with proxy", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace,
			adiiov1alpha1.InstallEnvSpec{
				Proxy:      &adiiov1alpha1.Proxy{HTTPProxy: "http://192.168.1.2"},
				ClusterRef: &adiiov1alpha1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).
			Return(nil, errors.Errorf("failure")).Times(1)

		res, err := ir.Reconcile(newInstallEnvRequest(installEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "installEnvImage",
		}
		Expect(c.Get(ctx, key, installEnvImage)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Reason).To(Equal(adiiov1alpha1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(installEnvImage.Status.Conditions, adiiov1alpha1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
	})
})
