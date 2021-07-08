package controllers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
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
		ctx                   = context.Background()
		sId                   strfmt.UUID
		backEndCluster        = &common.Cluster{Cluster: models.Cluster{ID: &sId}}
		ocpDisplayName        = "4.8.0"
		openshiftVersion      = &models.OpenshiftVersion{
			DisplayName:  &ocpDisplayName,
			ReleaseImage: new(string),
			RhcosImage:   new(string),
			RhcosVersion: new(string),
			SupportLevel: new(string),
		}
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		ir = &InfraEnvReconciler{
			Client:    c,
			Config:    InfraEnvConfig{ImageType: models.ImageTypeMinimalIso},
			Log:       common.GetTestLog(),
			Installer: mockInstallerInternal,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("none exiting infraEnv Image", func() {
		infraEnvImage := newInfraEnvImage("infraEnvImage", "namespace", aiv1beta1.InfraEnvSpec{})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		noneExistingImage := newInfraEnvImage("image2", "namespace", aiv1beta1.InfraEnvSpec{})

		result, err := ir.Reconcile(ctx, newInfraEnvRequest(noneExistingImage))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("create new infraEnv minimal-iso image - success", func() {
		imageInfo := models.ImageInfo{
			DownloadURL: "downloadurl",
			CreatedAt:   strfmt.DateTime(time.Now()),
		}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
				Expect(params.ImageCreateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
			}).Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
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
	})

	It("create new infraEnv full-iso image - success", func() {
		imageInfo := models.ImageInfo{
			DownloadURL: "downloadurl",
			CreatedAt:   strfmt.DateTime(time.Now()),
		}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
				Expect(params.ImageCreateParams.ImageType).To(Equal(models.ImageTypeFullIso))
			}).Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
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
		Expect(infraEnvImage.Status.ISODownloadURL).To(Equal(imageInfo.DownloadURL))
		Expect(infraEnvImage.Status.CreatedTime).ToNot(BeNil())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(aiv1beta1.ImageStateCreated))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreatedReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("create new infraEnv image - backend failure", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusInternalServerError, errors.New("server error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedError).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
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
		expectedState := fmt.Sprintf("%s: internal error", aiv1beta1.ImageStateFailedToCreate)
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
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
	})

	It("create new infraEnv image - while image is being created", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedError := common.NewApiError(http.StatusConflict, errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedError).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
			ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
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
	})

	It("create new image - client failure and retry immediately that results HTTP 409 StatusConflict", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		expectedClientError := common.NewApiError(http.StatusBadRequest, errors.New("client error"))
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(2)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedClientError).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil).Times(2)
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
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))

		// retry immediately

		expectedConflictError := common.NewApiError(http.StatusConflict, errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
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
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
				Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
			}).Return(nil, expectedError).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
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

		expectedState := fmt.Sprintf("%s: %s", aiv1beta1.ImageStateFailedToCreate, expectedError.Error())
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("create new image - clusterDeployment not exists", func() {
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

		expectedState := fmt.Sprintf(
			"%s: failed to get clusterDeployment with name clusterDeployment in namespace %s: "+
				"clusterdeployments.hive.openshift.io \"clusterDeployment\" not found",
			aiv1beta1.ImageStateFailedToCreate, testNamespace)
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Reason).To(Equal(aiv1beta1.ImageCreationErrorReason))
		Expect(conditionsv1.FindStatusCondition(infraEnvImage.Status.Conditions, aiv1beta1.ImageCreatedCondition).Status).To(Equal(corev1.ConditionUnknown))
	})

	It("create image with proxy configuration and ntp sources", func() {
		imageInfo := models.ImageInfo{DownloadURL: "downloadurl"}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				Proxy:                &aiv1beta1.Proxy{HTTPProxy: "http://192.168.1.2"},
				AdditionalNTPSources: []string{"foo.com", "bar.com"},
				ClusterRef:           &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateClusterNonInteractive(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateClusterParams) {
				Expect(swag.StringValue(param.ClusterUpdateParams.HTTPProxy)).To(Equal("http://192.168.1.2"))
				Expect(swag.StringValue(param.ClusterUpdateParams.AdditionalNtpSource)).To(Equal("foo.com,bar.com"))
			}).Return(nil, nil).Times(1)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: false}))
	})

	It("create image with ignition config override", func() {
		imageInfo := models.ImageInfo{DownloadURL: "downloadurl"}
		ignitionConfigOverride := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				ClusterRef:             &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
				IgnitionConfigOverride: ignitionConfigOverride,
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateDiscoveryIgnitionInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateDiscoveryIgnitionParams) {
				Expect(swag.StringValue(&param.DiscoveryIgnitionParams.Config)).To(Equal(ignitionConfigOverride))
			}).Return(nil).Times(1)
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
			Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)
		mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

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
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateDiscoveryIgnitionInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateDiscoveryIgnitionParams) {
				Expect(swag.StringValue(&param.DiscoveryIgnitionParams.Config)).To(Equal(ignitionConfigOverride))
			}).Return(errors.Errorf("error")).Times(1)
		res, err := ir.Reconcile(ctx, newInfraEnvRequest(infraEnvImage))
		Expect(err).ToNot(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: defaultRequeueAfterPerRecoverableError}))
	})

	It("failed to update cluster with proxy", func() {
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace,
			aiv1beta1.InfraEnvSpec{
				Proxy:      &aiv1beta1.Proxy{HTTPProxy: "http://192.168.1.2"},
				ClusterRef: &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
			})
		Expect(c.Create(ctx, infraEnvImage)).To(BeNil())

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().UpdateClusterNonInteractive(gomock.Any(), gomock.Any()).
			Return(nil, errors.Errorf("failure")).Times(1)

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

	Context("nmstate config", func() {

		var (
			NMStateLabelName  = "someName"
			NMStateLabelValue = "someValue"
			nicPrimary        = "eth0"
			nicSecondary      = "eth1"
			macPrimary        = "09:23:0f:d8:92:AA"
			macSecondary      = "09:23:0f:d8:92:AB"
			ip4Primary        = "192.168.126.30"
			ip4Secondary      = "192.168.140.30"
			dnsGW             = "192.168.126.1"
		)
		hostStaticNetworkConfig := common.FormatStaticConfigHostYAML(
			nicPrimary, nicSecondary, ip4Primary, ip4Secondary, dnsGW,
			models.MacInterfaceMap{
				&models.MacInterfaceMapItems0{MacAddress: macPrimary, LogicalNicName: nicPrimary},
				&models.MacInterfaceMapItems0{MacAddress: macSecondary, LogicalNicName: nicSecondary},
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
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
			mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
					Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
					Expect(params.ImageCreateParams.ImageType).To(Equal(models.ImageTypeMinimalIso))
					Expect(params.ImageCreateParams.StaticNetworkConfig).To(Equal([]*models.HostStaticNetworkConfig{hostStaticNetworkConfig}))

				}).Return(&common.Cluster{Cluster: models.Cluster{ImageInfo: &models.ImageInfo{DownloadURL: "downloadurl"}}}, nil).Times(1)
			infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
				NMStateConfigLabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}},
				ClusterRef:                 &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
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
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
			expectedError := common.NewApiError(http.StatusBadRequest, errors.New("internal error"))
			mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, params installer.GenerateClusterISOParams) {
					Expect(params.ClusterID).To(Equal(*backEndCluster.ID))
				}).Return(nil, expectedError).Times(1)
			infraEnvImage := newInfraEnvImage("infraEnvImage", testNamespace, aiv1beta1.InfraEnvSpec{
				NMStateConfigLabelSelector: metav1.LabelSelector{MatchLabels: map[string]string{NMStateLabelName: NMStateLabelValue}},
				ClusterRef:                 &aiv1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace},
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
