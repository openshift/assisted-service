package handler_test

import (
	"context"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators"
	hndlr "github.com/openshift/assisted-service/internal/operators/handler"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const dbName = "operators_handler"

var _ = Describe("Operators manager", func() {
	var (
		db      *gorm.DB
		cluster *common.Cluster
		log     = logrus.New()
		ctrl    *gomock.Controller
		mockApi *operators.MockAPI
		handler *hndlr.Handler
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockApi = operators.NewMockAPI(ctrl)
		handler = hndlr.NewHandler(mockApi, log, db)

		// create simple cluster
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
				MonitoredOperators: []*models.MonitoredOperator{
					&common.TestDefaultConfig.MonitoredOperator,
					&lso.Operator,
				},
			},
		}
		cluster.ImageInfo = &models.ImageInfo{}
		Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("ListOfClusterOperators", func() {
		It("should return all monitored operators", func() {
			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, nil, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(ConsistOf(cluster.MonitoredOperators))
		})

		It("should return monitored operators with a name", func() {
			operatorName := "lso"
			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, &operatorName, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(ConsistOf(&lso.Operator))
		})

		It("should return no monitored operators when no match", func() {
			notExistingOperatorName := "nothing-here"
			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, &notExistingOperatorName, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(BeEmpty())
		})
	})

	Context("ReportMonitoredOperatorStatus", func() {
		It("should update operator status", func() {

			statusInfo := "sorry, failed"
			operatorName := common.TestDefaultConfig.MonitoredOperator.Name
			newStatus := models.OperatorStatusFailed

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *cluster.ID, operatorName, newStatus, statusInfo)

			Expect(err).ToNot(HaveOccurred())

			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, &operatorName, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(HaveLen(1))

			Expect(operators[0].StatusInfo).To(Equal(statusInfo))
			Expect(operators[0].Status).To(Equal(newStatus))

		})

		It("should report error when operator not found", func() {

			statusInfo := "the very new progressing info"
			newStatus := models.OperatorStatusProgressing
			operatorName := "unknown"

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *cluster.ID, operatorName, newStatus, statusInfo)

			Expect(err).To(HaveOccurred())
			Expect(err.(*common.ApiErrorResponse).StatusCode()).To(BeEquivalentTo(http.StatusNotFound))
		})

		It("should report error for empty operator name", func() {

			statusInfo := "the very new progressing info"
			newStatus := models.OperatorStatusProgressing
			operatorName := ""

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *cluster.ID, operatorName, newStatus, statusInfo)

			Expect(err).To(HaveOccurred())
			Expect(err.(*common.ApiErrorResponse).StatusCode()).To(BeEquivalentTo(http.StatusBadRequest))
		})

	})
})
