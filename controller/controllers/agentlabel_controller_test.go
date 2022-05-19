package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newAgentWithInventory(name, namespace string, cpu, ram int64) *v1beta1.Agent {
	return &v1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1beta1.AgentSpec{Approved: true},
		Status: v1beta1.AgentStatus{
			Inventory: v1beta1.HostInventory{
				Hostname: name,
				Cpu:      v1beta1.HostCPU{Count: cpu},
				Memory:   v1beta1.HostMemory{PhysicalBytes: ram},
			},
		},
	}
}

func newAgentRequest(agent *v1beta1.Agent) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: agent.Namespace,
		Name:      agent.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

var _ = Describe("AgentLabel reconcile", func() {
	var (
		c         client.Client
		ir        *AgentLabelReconciler
		mockCtrl  *gomock.Controller
		ctx       = context.Background()
		agentName = "agent"
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().Build()
		mockCtrl = gomock.NewController(GinkgoT())
		ir = &AgentLabelReconciler{
			Client: c,
			Log:    common.GetTestLog(),
		}

	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	getTestAgent := func() *v1beta1.Agent {
		classification := &v1beta1.Agent{}
		Expect(c.Get(ctx,
			types.NamespacedName{
				Namespace: testNamespace,
				Name:      agentName,
			},
			classification)).To(BeNil())
		return classification
	}

	reconcileAgent := func(agent *v1beta1.Agent) {
		result, err := ir.Reconcile(ctx, newAgentRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	}

	It("AgentLabel basic flow", func() {
		mediumQuery := ".cpu.count == 2 and .memory.physicalBytes >= 4294967296 and .memory.physicalBytes < 8589934592"
		xlargeQuery := ".cpu.count == 4 and .memory.physicalBytes >= 17179869184 and .memory.physicalBytes < 34359738368"
		classificationSpecMedium := v1beta1.AgentClassificationSpec{
			LabelKey:   "size",
			LabelValue: "medium",
			Query:      mediumQuery,
		}
		classificationMedium := newAgentClassification(classificationSpecMedium.LabelValue, testNamespace, classificationSpecMedium, true)
		Expect(c.Create(ctx, classificationMedium)).ShouldNot(HaveOccurred())

		classificationSpecXlarge := v1beta1.AgentClassificationSpec{
			LabelKey:   "size",
			LabelValue: "xlarge",
			Query:      xlargeQuery,
		}
		classificationXlarge := newAgentClassification(classificationSpecXlarge.LabelValue, testNamespace, classificationSpecXlarge, true)
		Expect(c.Create(ctx, classificationXlarge)).ShouldNot(HaveOccurred())

		classificationSpecError := v1beta1.AgentClassificationSpec{
			LabelKey:   "error",
			LabelValue: "error",
			Query:      ".cpu.count & .memory.physicalBytes",
		}
		classificationError := newAgentClassification(classificationSpecError.LabelValue, testNamespace, classificationSpecError, true)
		Expect(c.Create(ctx, classificationError)).ShouldNot(HaveOccurred())

		agent := newAgentWithInventory(agentName, testNamespace, 2, 4294967296)
		Expect(c.Create(ctx, agent)).ShouldNot(HaveOccurred())

		// For the first reconcile, we expect the "size=medium" label and a "size=QUERYERROR..." label
		reconcileAgent(agent)
		agent = getTestAgent()
		Expect(len(agent.GetLabels())).To(Equal(2))
		Expect(agent.GetLabels()[ClassificationLabelPrefix+"size"]).To(Equal("medium"))
		Expect(agent.GetLabels()[ClassificationLabelPrefix+"error"]).To(Equal(queryErrorValue("error")))

		// Delete the error classification and swap the queries of medium and xlarge, so now we should have
		// one label - xlarge
		Expect(c.Delete(ctx, classificationError)).ShouldNot(HaveOccurred())
		classificationMedium.Spec.Query = xlargeQuery
		Expect(c.Update(ctx, classificationMedium)).ShouldNot(HaveOccurred())
		classificationXlarge.Spec.Query = mediumQuery
		Expect(c.Update(ctx, classificationXlarge)).ShouldNot(HaveOccurred())

		reconcileAgent(agent)
		agent = getTestAgent()
		Expect(len(agent.GetLabels())).To(Equal(1))
		Expect(agent.GetLabels()[ClassificationLabelPrefix+"size"]).To(Equal("xlarge"))
	})
})
