package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-openapi/strfmt"

	"github.com/openshift/assisted-service/models"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newInstallEnvRequest(image *v1alpha1.InstallEnv) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: image.ObjectMeta.Namespace,
		Name:      image.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newInstallEnvImage(name, namespace string, spec v1alpha1.InstallEnvSpec) *v1alpha1.InstallEnv {
	return &v1alpha1.InstallEnv{
		TypeMeta: metav1.TypeMeta{
			Kind:       "InstallEnv",
			APIVersion: "adi.io.my.domain/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

func getInstallEnvConditionByType(conditionType v1alpha1.InstallEnvConditionType, installEnv *v1alpha1.InstallEnv) v1alpha1.InstallEnvCondition {
	index := findInstallEnvConditionIndexByType(conditionType, &installEnv.Status.Conditions)
	Expect(index >= 0).Should(BeTrue(), fmt.Sprintf("condition %s was not found in installEnv deployment", conditionType))
	return installEnv.Status.Conditions[index]
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
		installEnvImage := newInstallEnvImage("installEnvImage", "namespace", v1alpha1.InstallEnvSpec{})
		Expect(c.Create(ctx, installEnvImage)).To(BeNil())

		noneExistingImage := newInstallEnvImage("image2", "namespace", v1alpha1.InstallEnvSpec{})

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
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, v1alpha1.InstallEnvSpec{
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

		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, v1alpha1.InstallEnvSpec{
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
		expectedState := fmt.Sprintf("%s: internal error", v1alpha1.ImageStateFailedToCreate)
		Expect(getInstallEnvConditionByType(v1alpha1.ImageProgressCondition, installEnvImage).Message).To(Equal(expectedState))
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
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, v1alpha1.InstallEnvSpec{
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

		expectedState := fmt.Sprintf("%s: %s", v1alpha1.ImageStateFailedToCreate, expectedError.Error())
		Expect(getInstallEnvConditionByType(v1alpha1.ImageProgressCondition, installEnvImage).Message).To(Equal(expectedState))
	})

	It("create new image - cluster not exists", func() {
		installEnvImage := newInstallEnvImage("installEnvImage", testNamespace, v1alpha1.InstallEnvSpec{
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

		expectedState := fmt.Sprintf(
			"%s: failed to find clusterDeployment with name clusterDeployment in namespace %s: "+
				"clusterdeployments.hive.openshift.io \"clusterDeployment\" not found",
			v1alpha1.ImageStateFailedToCreate, testNamespace)
		Expect(getInstallEnvConditionByType(v1alpha1.ImageProgressCondition, installEnvImage).Message).To(Equal(expectedState))
	})
})
