package subsystem

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	opclient "github.com/openshift/assisted-service/client/operators"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Operators endpoint tests", func() {

	AfterEach(func() {
		clearDB()
	})

	Context("supported-operators", func() {
		It("should return all supported operators", func() {
			reply, err := userBMClient.Operators.ListSupportedOperators(context.TODO(), opclient.NewListSupportedOperatorsParams())

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.GetPayload()).To(ConsistOf("ocs", "lso", "cnv"))
		})

		It("should provide operator properties", func() {
			params := opclient.NewListOperatorPropertiesParams().WithOperatorName("ocs")
			reply, err := userBMClient.Operators.ListOperatorProperties(context.TODO(), params)

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.Payload).To(BeEquivalentTo(models.OperatorProperties{}))
		})
	})

	Context("Monitored operators", func() {
		var cluster *installer.RegisterClusterCreated
		ctx := context.Background()
		BeforeEach(func() {
			var err error
			cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(pullSecret),
					OlmOperators: []*models.OperatorCreateParams{
						{Name: "ocs"},
						{Name: "lso"},
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
			Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
			Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
		})

		It("should be all returned", func() {
			ops, err := agentBMClient.Operators.ListOfClusterOperators(ctx, opclient.NewListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))

			Expect(err).ToNot(HaveOccurred())
			Expect(len(ops.GetPayload())).To(BeNumerically(">=", 3))

			var operatorNames []string
			for _, op := range ops.GetPayload() {
				operatorNames = append(operatorNames, op.Name)
			}
			Expect(operatorNames).To(ConsistOf(
				ocs.Operator.Name,
				lso.Operator.Name,

				operators.OperatorConsole.Name,
			))
		})

		It("should selected be returned", func() {
			ops, err := agentBMClient.Operators.ListOfClusterOperators(ctx, opclient.NewListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&ocs.Operator.Name))

			Expect(err).ToNot(HaveOccurred())
			Expect(ops.GetPayload()).To(HaveLen(1))
			Expect(ops.GetPayload()[0].Name).To(BeEquivalentTo(ocs.Operator.Name))
		})

		It("should be updated", func() {
			statusInfo := "Unfortunately failed"
			_, err := agentBMClient.Operators.ReportMonitoredOperatorStatus(ctx, opclient.NewReportMonitoredOperatorStatusParams().
				WithClusterID(*cluster.Payload.ID).
				WithReportParams(&models.OperatorMonitorReport{
					Name:       ocs.Operator.Name,
					Status:     models.OperatorStatusFailed,
					StatusInfo: statusInfo,
				}))

			Expect(err).ToNot(HaveOccurred())

			ops, err := agentBMClient.Operators.ListOfClusterOperators(ctx, opclient.NewListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&ocs.Operator.Name))

			Expect(err).ToNot(HaveOccurred())
			operators := ops.GetPayload()
			Expect(operators).To(HaveLen(1))
			Expect(operators[0].StatusInfo).To(BeEquivalentTo(statusInfo))
			Expect(operators[0].Status).To(BeEquivalentTo(models.OperatorStatusFailed))

		})
	})
})
