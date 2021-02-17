package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("create agent CR", func() {
	var (
		c                client.Client
		ctx              = context.Background()
		mockCtrl         *gomock.Controller
		clusterName      = "test-cluster"
		clusterNamespace = "test-namespace"
		crdUtils         *CRDUtils
		log              = common.GetTestLog().WithField("pkg", "controllers")
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		crdUtils = NewCRDUtils(c)
		mockCtrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("create agent", func() {

		It("create agent success", func() {
			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, clusterNamespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{
				Namespace: clusterNamespace,
				Name:      hostId,
			}
			Expect(c.Get(ctx, namespacedName, &v1alpha1.Agent{})).ShouldNot(HaveOccurred())
		})

		It("Empty name space", func() {
			hostId := uuid.New().String()
			err := crdUtils.CreateAgentCR(ctx, log, hostId, "", clusterName)
			namespacedName := types.NamespacedName{
				Namespace: "",
				Name:      hostId,
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Get(ctx, namespacedName, &v1alpha1.Agent{})).Should(HaveOccurred())
		})

		It("Already existing agent", func() {
			id := uuid.New().String()
			agent := newAgent(id, clusterNamespace, v1alpha1.AgentSpec{})
			Expect(c.Create(ctx, agent)).ShouldNot(HaveOccurred())
			err := crdUtils.CreateAgentCR(ctx, log, id, clusterNamespace, clusterName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

})
