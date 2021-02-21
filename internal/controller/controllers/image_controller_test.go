package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newImageRequest(image *v1alpha1.Image) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: image.ObjectMeta.Namespace,
		Name:      image.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newImage(name, namespace string, spec v1alpha1.ImageSpec) *v1alpha1.Image {
	return &v1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

var _ = Describe("image reconcile", func() {
	var (
		c                     client.Client
		ir                    *ImageReconciler
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		ctx                   = context.Background()
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		ir = &ImageReconciler{
			Client:    c,
			Scheme:    scheme.Scheme,
			Log:       common.GetTestLog(),
			Installer: mockInstallerInternal,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("none exiting image", func() {
		image := newImage("image", "namespace", v1alpha1.ImageSpec{})
		Expect(c.Create(ctx, image)).To(BeNil())

		noneExistingImage := newImage("image2", "namespace", v1alpha1.ImageSpec{})

		result, err := ir.Reconcile(newImageRequest(noneExistingImage))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("image not found", func() {
		image := newImage("image", testNamespace, v1alpha1.ImageSpec{})
		result, err := ir.Reconcile(newImageRequest(image))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("create new image - success", func() {
		imageInfo := models.ImageInfo{
			SizeBytes:   swag.Int64(1000),
			DownloadURL: "downloadurl",
			ExpiresAt:   strfmt.DateTime(time.Now().Add(time.Hour)),
		}
		cluster := newCluster("cluster", testNamespace, getDefaultClusterSpec("cluster-test", "pull-secret"))
		Expect(c.Create(ctx, cluster)).To(BeNil())

		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).Return(
			&common.Cluster{Cluster: models.Cluster{ImageInfo: &imageInfo}}, nil).Times(1)
		image := newImage("image", testNamespace, v1alpha1.ImageSpec{
			ClusterRef: &v1alpha1.ClusterReference{Name: "cluster", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, image)).To(BeNil())

		res, err := ir.Reconcile(newImageRequest(image))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "image",
		}
		Expect(c.Get(ctx, key, image)).To(BeNil())
		Expect(image.Status.State).To(Equal(v1alpha1.ImageStateCreated))
		Expect(image.Status.SizeBytes).To(Equal(int(*imageInfo.SizeBytes)))
		Expect(image.Status.DownloadUrl).To(Equal(imageInfo.DownloadURL))
		Expect(image.Status.ExpirationTime.Time.Round(time.Minute)).To(Equal(time.Time(imageInfo.ExpiresAt).Round(time.Minute)))
	})

	It("create new image - backend failure", func() {
		cluster := newCluster("cluster", testNamespace, getDefaultClusterSpec("cluster-test", "pull-secret"))
		Expect(c.Create(ctx, cluster)).To(BeNil())

		expectedError := common.NewApiError(http.StatusInternalServerError, errors.New("server error"))
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).Return(nil, expectedError).Times(1)

		image := newImage("image", testNamespace, v1alpha1.ImageSpec{
			ClusterRef: &v1alpha1.ClusterReference{Name: "cluster", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, image)).To(BeNil())

		res, err := ir.Reconcile(newImageRequest(image))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "image",
		}
		Expect(c.Get(ctx, key, image)).To(BeNil())
		expectedState := fmt.Sprintf("%s: internal error", v1alpha1.ImageStateFailedToCreate)
		Expect(image.Status.State).To(Equal(expectedState))
	})

	It("create new image - client failure", func() {
		cluster := newCluster("cluster", testNamespace, getDefaultClusterSpec("cluster-test", "pull-secret"))
		Expect(c.Create(ctx, cluster)).To(BeNil())

		expectedError := common.NewApiError(http.StatusBadRequest, errors.New("client error"))
		mockInstallerInternal.EXPECT().GenerateClusterISOInternal(gomock.Any(), gomock.Any()).Return(nil, expectedError).Times(1)

		image := newImage("image", testNamespace, v1alpha1.ImageSpec{
			ClusterRef: &v1alpha1.ClusterReference{Name: "cluster", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, image)).To(BeNil())

		res, err := ir.Reconcile(newImageRequest(image))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "image",
		}
		Expect(c.Get(ctx, key, image)).To(BeNil())

		expectedState := fmt.Sprintf("%s: %s", v1alpha1.ImageStateFailedToCreate, expectedError.Error())
		Expect(image.Status.State).To(Equal(expectedState))
	})

	It("create new image - cluster not exists", func() {
		image := newImage("image", testNamespace, v1alpha1.ImageSpec{
			ClusterRef: &v1alpha1.ClusterReference{Name: "cluster", Namespace: testNamespace},
		})
		Expect(c.Create(ctx, image)).To(BeNil())

		res, err := ir.Reconcile(newImageRequest(image))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "image",
		}
		Expect(c.Get(ctx, key, image)).To(BeNil())

		expectedState := fmt.Sprintf(
			"%s: failed to find cluster with name cluster in namespace %s: "+
				"clusters.adi.io.my.domain \"cluster\" not found",
			v1alpha1.ImageStateFailedToCreate, testNamespace)
		Expect(image.Status.State).To(Equal(expectedState))
	})
})
