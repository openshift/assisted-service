package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newAgentClassification(name, namespace string, spec v1beta1.AgentClassificationSpec, withFinalizer bool) *v1beta1.AgentClassification {
	ac := &v1beta1.AgentClassification{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
	if withFinalizer {
		ac.ObjectMeta.Finalizers = []string{AgentFinalizerName} // adding finalizer to avoid reconciling twice in the unit tests
	}
	return ac
}

func newAgentWithLabel(name, namespace string, key, value string) *v1beta1.Agent {
	return &v1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{ClassificationLabelPrefix + key: value},
		},
		Spec:   v1beta1.AgentSpec{Approved: true},
		Status: v1beta1.AgentStatus{},
	}
}

func newAgentClassificationRequest(agentClassification *v1beta1.AgentClassification) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: agentClassification.Namespace,
		Name:      agentClassification.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

var _ = Describe("AgentClassification reconcile", func() {
	var (
		c                         client.Client
		ir                        *AgentClassificationReconciler
		mockCtrl                  *gomock.Controller
		ctx                       = context.Background()
		defaultClassificationName = "medium-size"
		defaultClassificationSpec v1beta1.AgentClassificationSpec
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithStatusSubresource(&v1beta1.AgentClassification{}).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		ir = &AgentClassificationReconciler{
			Client: c,
			Log:    common.GetTestLog(),
		}
		defaultClassificationSpec = v1beta1.AgentClassificationSpec{
			LabelKey:   "size",
			LabelValue: "medium",
			Query:      ".cpu.count == 2 and .memory.physicalBytes >= 4294967296 and .memory.physicalBytes < 8589934592",
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	getTestClassification := func() *v1beta1.AgentClassification {
		classification := &v1beta1.AgentClassification{}
		Expect(c.Get(ctx,
			types.NamespacedName{
				Namespace: testNamespace,
				Name:      defaultClassificationName,
			},
			classification)).To(BeNil())
		return classification
	}

	reconcileClassification := func(classification *v1beta1.AgentClassification) {
		result, err := ir.Reconcile(ctx, newAgentClassificationRequest(classification))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	}

	It("AgentClassification add finalizer", func() {
		classification := newAgentClassification(defaultClassificationName, testNamespace, defaultClassificationSpec, false)
		Expect(c.Create(ctx, classification)).ShouldNot(HaveOccurred())

		reconcileClassification(classification)
		classification = getTestClassification()
		Expect(classification.GetFinalizers()).To(ContainElement(AgentClassificationFinalizer))
	})

	It("AgentClassification basic flow", func() {
		classification := newAgentClassification(defaultClassificationName, testNamespace, defaultClassificationSpec, true)
		Expect(c.Create(ctx, classification)).ShouldNot(HaveOccurred())

		agent1 := newAgentWithLabel("agent1", testNamespace, defaultClassificationSpec.LabelKey, defaultClassificationSpec.LabelValue)
		Expect(c.Create(ctx, agent1)).ShouldNot(HaveOccurred())
		agent2 := newAgentWithLabel("agent2", testNamespace, "differentkey", "differentvalue")
		Expect(c.Create(ctx, agent2)).ShouldNot(HaveOccurred())
		agent3 := newAgentWithLabel("agent3", testNamespace, defaultClassificationSpec.LabelKey, queryErrorValue("foo"))
		Expect(c.Create(ctx, agent3)).ShouldNot(HaveOccurred())

		reconcileClassification(classification)

		// Expect 1 match (agent1) and 1 error (agent3)
		classification = getTestClassification()
		Expect(classification.Status.MatchedCount).To(Equal(1))
		Expect(classification.Status.ErrorCount).To(Equal(1))
		Expect(conditionsv1.FindStatusCondition(classification.Status.Conditions, v1beta1.QueryErrorsCondition).Reason).To(Equal(v1beta1.QueryHasErrorsReason))

		// Deletion should not actually occur because of the finalizer
		Expect(c.Delete(ctx, classification)).ShouldNot(HaveOccurred())

		// Delete agent3 and then expect 1 match and no errors (agent1)
		Expect(c.Delete(ctx, agent3)).ShouldNot(HaveOccurred())
		reconcileClassification(classification)
		classification = getTestClassification()
		Expect(classification.Status.MatchedCount).To(Equal(1))
		Expect(classification.Status.ErrorCount).To(Equal(0))
		Expect(conditionsv1.FindStatusCondition(classification.Status.Conditions, v1beta1.QueryErrorsCondition).Reason).To(Equal(v1beta1.QueryNoErrorsReason))

		// Delete agent1 and then expect the finalizer to be removed and the classification deleted
		Expect(c.Delete(ctx, agent1)).ShouldNot(HaveOccurred())
		reconcileClassification(classification)
		classification = getTestClassification()
		Expect(classification.GetFinalizers()).ToNot(ContainElement(AgentClassificationFinalizer))
	})
})
