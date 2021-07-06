package handler_test

import (
	"context"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/operators"
	operatorsHandler "github.com/openshift/assisted-service/internal/operators/handler"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Operators manager", func() {
	var (
		db                *gorm.DB
		dbName            string
		cluster, cluster2 *common.Cluster
		log               = logrus.New()
		ctrl              *gomock.Controller
		mockApi           *operators.MockAPI
		mockEvents        *events.MockHandler
		handler           *operatorsHandler.Handler
		lastUpdatedTime   strfmt.DateTime
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockApi = operators.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		handler = operatorsHandler.NewHandler(mockApi, log, db, mockEvents)

		// create simple cluster #1
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
				MonitoredOperators: []*models.MonitoredOperator{
					from(common.TestDefaultConfig.MonitoredOperator),
					from(lso.Operator),
					from(operators.OperatorCVO),
				},
			},
		}
		cluster.ImageInfo = &models.ImageInfo{}
		Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())

		// create simple cluster #2
		clusterID2 := strfmt.UUID(uuid.New().String())
		cluster2 = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID2,
				MonitoredOperators: []*models.MonitoredOperator{
					from(lso.Operator),
				},
			},
		}
		cluster2.ImageInfo = &models.ImageInfo{}
		Expect(db.Save(&cluster2).Error).ShouldNot(HaveOccurred())
		lastUpdatedTime = cluster.StatusUpdatedAt
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("FindMonitoredOperator", func() {
		It("should return an operator", func() {
			operatorName := "lso"
			operator, err := handler.FindMonitoredOperator(context.TODO(), *cluster.ID, operatorName, db)

			Expect(err).ToNot(HaveOccurred())
			Expect(operator.Name).To(BeEquivalentTo(operatorName))
			Expect(operator.ClusterID).To(BeEquivalentTo(*cluster.ID))
		})

		It("should fail for empty name", func() {
			operatorName := ""
			_, err := handler.FindMonitoredOperator(context.TODO(), *cluster.ID, operatorName, db)

			Expect(err).To(HaveOccurred())
		})

		It("should not find a non-existing operator", func() {
			operatorName := "no-such-operator"
			_, err := handler.FindMonitoredOperator(context.TODO(), *cluster.ID, operatorName, db)

			Expect(err).To(HaveOccurred())
		})
	})

	Context("GetMonitoredOperators", func() {
		It("should return all monitored operators", func() {
			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, nil, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(ConsistOf(cluster.MonitoredOperators))
		})

		It("should return monitored operators with a name", func() {
			// Cluster #1
			operatorName := "lso"
			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, &operatorName, db)

			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(HaveLen(1))
			Expect(operators[0].ClusterID).To(BeEquivalentTo(*cluster.ID))
			Expect(operators[0].Name).To(BeEquivalentTo(operatorName))

			// Cluster #2
			operatorName2 := "lso"
			operators2, err := handler.GetMonitoredOperators(context.TODO(), *cluster2.ID, &operatorName2, db)

			Expect(err).ToNot(HaveOccurred())
			Expect(operators2).To(HaveLen(1))
			Expect(operators2[0].ClusterID).To(BeEquivalentTo(*cluster2.ID))
			Expect(operators2[0].Name).To(BeEquivalentTo(operatorName2))
		})

		It("should return no monitored operators when no match", func() {
			notExistingOperatorName := "nothing-here"
			_, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, &notExistingOperatorName, db)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("UpdateMonitoredOperatorStatus", func() {
		It("should update operator status", func() {
			statusInfo := "sorry, failed"
			operatorName := common.TestDefaultConfig.MonitoredOperator.Name
			newStatus := models.OperatorStatusFailed

			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityInfo, gomock.Any(), gomock.Any()).Times(1)

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *cluster.ID, operatorName, newStatus, statusInfo)

			Expect(err).ToNot(HaveOccurred())

			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, &operatorName, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(HaveLen(1))

			Expect(operators[0].StatusInfo).To(Equal(statusInfo))
			Expect(operators[0].Status).To(Equal(newStatus))
			Expect(operators[0].StatusUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
		})

		It("should report error when operator not found", func() {
			statusInfo := "the very new progressing info"
			newStatus := models.OperatorStatusProgressing
			operatorName := "unknown"

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *cluster.ID, operatorName, newStatus, statusInfo)

			Expect(err).To(HaveOccurred())
			Expect(err.(*common.ApiErrorResponse).StatusCode()).To(BeEquivalentTo(http.StatusNotFound))

			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, swag.String(""), db)
			Expect(err).ToNot(HaveOccurred())
			for _, operator := range operators {
				Expect(operator.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
			}
		})

		It("should report error for empty operator name", func() {
			statusInfo := "the very new progressing info"
			newStatus := models.OperatorStatusProgressing
			operatorName := ""

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *cluster.ID, operatorName, newStatus, statusInfo)

			Expect(err).To(HaveOccurred())
			Expect(err.(*common.ApiErrorResponse).StatusCode()).To(BeEquivalentTo(http.StatusBadRequest))

			operators, err := handler.GetMonitoredOperators(context.TODO(), *cluster.ID, swag.String(""), db)
			Expect(err).ToNot(HaveOccurred())
			for _, operator := range operators {
				Expect(operator.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
			}
		})
	})
})

func from(prototype models.MonitoredOperator) *models.MonitoredOperator {
	return &models.MonitoredOperator{
		Name:           prototype.Name,
		OperatorType:   prototype.OperatorType,
		TimeoutSeconds: prototype.TimeoutSeconds,
	}
}
