package controllers

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta2"
	"github.com/openshift/assisted-service/internal/common"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newAgentClusterInstallRequest(agentClusterInstall *hiveext.AgentClusterInstall) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: agentClusterInstall.Namespace,
		Name:      agentClusterInstall.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

var _ = Describe("AgentClusterInstall reconcile", func() {
	var (
		c                              client.Client
		ir                             *AgentClusterInstallReconciler
		mockCtrl                       *gomock.Controller
		ctx                            = context.Background()
		clusterName                    = "test-cluster"
		agentClusterInstallName        = "test-cluster-aci"
		pullSecretName                 = "pull-secret"
		defaultClusterSpec             hivev1.ClusterDeploymentSpec
		defaultAgentClusterInstallSpec hiveext.AgentClusterInstallSpec
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(&hiveext.AgentClusterInstall{}).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		ir = &AgentClusterInstallReconciler{
			Client: c,
			Log:    common.GetTestLog(),
		}

		defaultClusterSpec = getDefaultClusterDeploymentSpec(clusterName, agentClusterInstallName, pullSecretName)
		defaultAgentClusterInstallSpec = getDefaultAgentClusterInstallSpec(clusterName)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	getTestClusterInstall := func() *hiveext.AgentClusterInstall {
		clusterInstall := &hiveext.AgentClusterInstall{}
		Expect(c.Get(ctx,
			types.NamespacedName{
				Namespace: testNamespace,
				Name:      agentClusterInstallName,
			},
			clusterInstall)).To(BeNil())
		return clusterInstall
	}

	It("create AgentClusterInstall - success", func() {
		clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		Expect(c.Create(ctx, clusterDeployment)).ShouldNot(HaveOccurred())
		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, clusterDeployment)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

		result, err := ir.Reconcile(ctx, newAgentClusterInstallRequest(aci))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("create AgentClusterInstall - missing ClusterDeployment", func() {
		clusterDeployment := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, clusterDeployment)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

		result, err := ir.Reconcile(ctx, newAgentClusterInstallRequest(aci))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))

		aci = getTestClusterInstall()
		Expect(FindStatusCondition(aci.Status.Conditions, hiveext.ClusterSpecSyncedCondition).Reason).To(Equal(hiveext.ClusterInputErrorReason))
		Expect(FindStatusCondition(aci.Status.Conditions, hiveext.ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
		Expect(FindStatusCondition(aci.Status.Conditions, hiveext.ClusterSpecSyncedCondition).Message).To(ContainSubstring(
			fmt.Sprintf("ClusterDeployment with name '%s' in namespace '%s' not found", clusterDeployment.Name, clusterDeployment.Namespace)))

		Expect(FindStatusCondition(aci.Status.Conditions, hiveext.ClusterCompletedCondition).Reason).To(Equal(hiveext.ClusterNotAvailableReason))
		Expect(FindStatusCondition(aci.Status.Conditions, hiveext.ClusterCompletedCondition).Status).To(Equal(corev1.ConditionUnknown))
	})
})
