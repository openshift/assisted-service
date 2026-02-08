package handler_test

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/operators"
	operatorsHandler "github.com/openshift/assisted-service/internal/operators/handler"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	restoperators "github.com/openshift/assisted-service/restapi/operations/operators"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

var _ = Describe("Operators manager", func() {
	var (
		db                     *gorm.DB
		dbName                 string
		c, c2                  *common.Cluster
		log                    = logrus.New()
		ctrl                   *gomock.Controller
		mockApi                *operators.MockAPI
		mockEvents             *eventsapi.MockHandler
		mockClusterProgressApi *cluster.MockProgressAPI
		handler                *operatorsHandler.Handler
		lastUpdatedTime        strfmt.DateTime
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockApi = operators.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockClusterProgressApi = cluster.NewMockProgressAPI(ctrl)
		handler = operatorsHandler.NewHandler(mockApi, log, db, mockEvents, mockClusterProgressApi)

		// create simple cluster #1
		clusterID := strfmt.UUID(uuid.New().String())
		c = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
				MonitoredOperators: []*models.MonitoredOperator{
					from(common.TestDefaultConfig.MonitoredOperator),
					from(lso.Operator),
					from(operators.OperatorCVO),
				},
			},
		}
		c.ImageInfo = &models.ImageInfo{}
		Expect(db.Save(&c).Error).ShouldNot(HaveOccurred())

		// create simple cluster #2
		clusterID2 := strfmt.UUID(uuid.New().String())
		c2 = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID2,
				MonitoredOperators: []*models.MonitoredOperator{
					from(lso.Operator),
				},
			},
		}
		c2.ImageInfo = &models.ImageInfo{}
		Expect(db.Save(&c2).Error).ShouldNot(HaveOccurred())
		lastUpdatedTime = c.StatusUpdatedAt
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("FindMonitoredOperator", func() {
		It("should return an operator", func() {
			operatorName := "lso"
			operator, err := handler.FindMonitoredOperator(context.TODO(), *c.ID, operatorName, db)

			Expect(err).ToNot(HaveOccurred())
			Expect(operator.Name).To(BeEquivalentTo(operatorName))
			Expect(operator.ClusterID).To(BeEquivalentTo(*c.ID))
		})

		It("should fail for empty name", func() {
			operatorName := ""
			_, err := handler.FindMonitoredOperator(context.TODO(), *c.ID, operatorName, db)

			Expect(err).To(HaveOccurred())
		})

		It("should not find a non-existing operator", func() {
			operatorName := "no-such-operator"
			_, err := handler.FindMonitoredOperator(context.TODO(), *c.ID, operatorName, db)

			Expect(err).To(HaveOccurred())
		})
	})

	Context("GetMonitoredOperators", func() {
		It("should return all monitored operators", func() {
			operators, err := handler.GetMonitoredOperators(context.TODO(), *c.ID, nil, db)
			Expect(err).ToNot(HaveOccurred())
			for _, o := range operators {
				// Ignore the status-updated-at
				o.StatusUpdatedAt = strfmt.DateTime{}
			}
			Expect(operators).To(ConsistOf(c.MonitoredOperators))
		})

		It("should return monitored operators with a name", func() {
			// Cluster #1
			operatorName := "lso"
			operators, err := handler.GetMonitoredOperators(context.TODO(), *c.ID, &operatorName, db)

			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(HaveLen(1))
			Expect(operators[0].ClusterID).To(BeEquivalentTo(*c.ID))
			Expect(operators[0].Name).To(BeEquivalentTo(operatorName))

			// Cluster #2
			operatorName2 := "lso"
			operators2, err := handler.GetMonitoredOperators(context.TODO(), *c2.ID, &operatorName2, db)

			Expect(err).ToNot(HaveOccurred())
			Expect(operators2).To(HaveLen(1))
			Expect(operators2[0].ClusterID).To(BeEquivalentTo(*c2.ID))
			Expect(operators2[0].Name).To(BeEquivalentTo(operatorName2))
		})

		It("should return no monitored operators when no match", func() {
			notExistingOperatorName := "nothing-here"
			_, err := handler.GetMonitoredOperators(context.TODO(), *c.ID, &notExistingOperatorName, db)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("UpdateMonitoredOperatorStatus", func() {
		It("should update operator status", func() {
			statusInfo := "sorry, failed"
			operatorName := common.TestDefaultConfig.MonitoredOperator.Name
			operatorVersion := "4.12"
			newStatus := models.OperatorStatusFailed

			mockEvents.EXPECT().SendClusterEvent(context.TODO(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterOperatorStatusEventName),
				eventstest.WithClusterIdMatcher(c.ID.String()))).Times(1)

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *c.ID, operatorName, operatorVersion, newStatus, statusInfo, db)

			Expect(err).ToNot(HaveOccurred())

			operators, err := handler.GetMonitoredOperators(context.TODO(), *c.ID, &operatorName, db)
			Expect(err).ToNot(HaveOccurred())
			Expect(operators).To(HaveLen(1))

			Expect(operators[0].StatusInfo).To(Equal(statusInfo))
			Expect(operators[0].Status).To(Equal(newStatus))
			Expect(time.Time(operators[0].StatusUpdatedAt).UTC()).ToNot(Equal(time.Time(lastUpdatedTime).UTC()))
			Expect(operators[0].Version).To(Equal(operatorVersion))
		})

		It("should report error when operator not found", func() {
			statusInfo := "the very new progressing info"
			newStatus := models.OperatorStatusProgressing
			operatorVersion := "4.12"
			operatorName := "unknown"

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *c.ID, operatorName, operatorVersion, newStatus, statusInfo, db)

			Expect(err).To(HaveOccurred())
			Expect(err.(*common.ApiErrorResponse).StatusCode()).To(BeEquivalentTo(http.StatusNotFound))

			operators, err := handler.GetMonitoredOperators(context.TODO(), *c.ID, swag.String(""), db)
			Expect(err).ToNot(HaveOccurred())
			for _, operator := range operators {
				Expect(time.Time(operator.StatusUpdatedAt).UTC()).To(Equal(time.Time(lastUpdatedTime).UTC()))
			}
		})

		It("should report error for empty operator name", func() {
			statusInfo := "the very new progressing info"
			newStatus := models.OperatorStatusProgressing
			operatorName := ""
			operatorVersion := "4.12"

			err := handler.UpdateMonitoredOperatorStatus(context.TODO(), *c.ID, operatorName, operatorVersion, newStatus, statusInfo, db)

			Expect(err).To(HaveOccurred())
			Expect(err.(*common.ApiErrorResponse).StatusCode()).To(BeEquivalentTo(http.StatusBadRequest))

			operators, err := handler.GetMonitoredOperators(context.TODO(), *c.ID, swag.String(""), db)
			Expect(err).ToNot(HaveOccurred())
			for _, operator := range operators {
				Expect(time.Time(operator.StatusUpdatedAt).UTC()).To(Equal(time.Time(lastUpdatedTime).UTC()))
			}
		})
	})
})

var _ = Describe("V2ListBundles validation", func() {
	var (
		db      *gorm.DB
		dbName  string
		log     = logrus.New()
		ctrl    *gomock.Controller
		mockApi *operators.MockAPI
		handler *operatorsHandler.Handler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockApi = operators.NewMockAPI(ctrl)
		handler = operatorsHandler.NewHandler(mockApi, log, db, nil, nil)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	// Helper function to verify API error responses
	verifyApiError := func(responder middleware.Responder, expectedHttpStatus int32) {
		ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
		concreteError := responder.(*common.ApiErrorResponse)
		ExpectWithOffset(1, concreteError.StatusCode()).To(Equal(expectedHttpStatus))
	}

	verifyApiErrorString := func(responder middleware.Responder, expectedHttpStatus int32, expectedSubstring string) {
		ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
		concreteError := responder.(*common.ApiErrorResponse)
		ExpectWithOffset(1, concreteError.StatusCode()).To(Equal(expectedHttpStatus))
		ExpectWithOffset(1, concreteError.Error()).To(ContainSubstring(expectedSubstring))
	}

	Context("Parameter validation errors", func() {
		It("should return error for invalid OpenShift version", func() {
			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("invalid-version"),
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := handler.V2ListBundles(context.Background(), params)
			verifyApiErrorString(response, http.StatusBadRequest, "invalid openshift version")
		})

		It("should return error for missing CPU architecture", func() {
			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.13.0"),
				CPUArchitecture:  nil,
			}

			response := handler.V2ListBundles(context.Background(), params)
			verifyApiErrorString(response, http.StatusBadRequest, "cpu architecture is required")
		})

		It("should return error for empty CPU architecture", func() {
			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.13.0"),
				CPUArchitecture:  swag.String(""),
			}

			response := handler.V2ListBundles(context.Background(), params)
			verifyApiErrorString(response, http.StatusBadRequest, "cpu architecture is required")
		})

		It("should return error for unsupported CPU architecture", func() {
			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.9.0"), // ARM64 not supported before 4.10
				CPUArchitecture:  swag.String("arm64"),
			}

			response := handler.V2ListBundles(context.Background(), params)
			verifyApiErrorString(response, http.StatusBadRequest, "cpu architecture arm64 is not supported for openshift version 4.9.0")
		})

		It("should return error for unsupported platform", func() {
			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.10.0"), // Nutanix not supported before 4.11
				CPUArchitecture:  swag.String("x86_64"),
				PlatformType:     swag.String("nutanix"),
			}

			response := handler.V2ListBundles(context.Background(), params)
			verifyApiErrorString(response, http.StatusBadRequest, "platform nutanix is not supported for openshift version 4.10.0")
		})
	})

	Context("Valid parameters", func() {
		It("should succeed with valid x86_64 parameters", func() {
			expectedBundles := []*models.Bundle{
				{
					ID:        "openshift-ai",
					Operators: []string{"openshift-ai", "nvidia-gpu"},
				},
			}

			mockApi.EXPECT().ListBundles(
				gomock.Any(), // SupportLevelFilters
				gomock.Any(), // featureIDs
			).Return(expectedBundles)

			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.13.0"),
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := handler.V2ListBundles(context.Background(), params)
			Expect(response).To(BeAssignableToTypeOf(restoperators.NewV2ListBundlesOK()))
		})

		It("should succeed with valid arm64 parameters", func() {
			expectedBundles := []*models.Bundle{
				{
					ID:        "openshift-ai",
					Operators: []string{"openshift-ai", "nvidia-gpu"},
				},
			}

			mockApi.EXPECT().ListBundles(
				gomock.Any(), // SupportLevelFilters
				gomock.Any(), // featureIDs
			).Return(expectedBundles)

			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.13.0"),
				CPUArchitecture:  swag.String("arm64"),
			}

			response := handler.V2ListBundles(context.Background(), params)
			Expect(response).To(BeAssignableToTypeOf(restoperators.NewV2ListBundlesOK()))
		})

		It("should succeed with valid platform parameters", func() {
			expectedBundles := []*models.Bundle{
				{
					ID:        "openshift-ai",
					Operators: []string{"openshift-ai", "nvidia-gpu"},
				},
			}

			mockApi.EXPECT().ListBundles(
				gomock.Any(), // SupportLevelFilters
				gomock.Any(), // featureIDs
			).Return(expectedBundles)

			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.13.0"),
				CPUArchitecture:  swag.String("x86_64"),
				PlatformType:     swag.String("baremetal"),
			}

			response := handler.V2ListBundles(context.Background(), params)
			Expect(response).To(BeAssignableToTypeOf(restoperators.NewV2ListBundlesOK()))
		})

		It("should handle feature_ids parameter correctly", func() {
			expectedBundles := []*models.Bundle{
				{
					ID:        "openshift-ai",
					Operators: []string{"openshift-ai", "nvidia-gpu"},
				},
			}

			params := restoperators.V2ListBundlesParams{
				OpenshiftVersion: swag.String("4.13.0"),
				CPUArchitecture:  swag.String("x86_64"),
				FeatureIds:       []string{"SNO"},
			}

			// Verify that the correct featureIDs are passed to the API
			mockApi.EXPECT().ListBundles(gomock.Any(), gomock.Any()).Return(expectedBundles)

			response := handler.V2ListBundles(context.Background(), params)
			Expect(response).To(BeAssignableToTypeOf(restoperators.NewV2ListBundlesOK()))
		})
	})

	Context("Architecture-specific validation", func() {
		DescribeTable("should validate CPU architecture support for different OpenShift versions",
			func(architecture string, openshiftVersion string, shouldSucceed bool) {
				params := restoperators.V2ListBundlesParams{
					OpenshiftVersion: &openshiftVersion,
					CPUArchitecture:  swag.String(architecture),
				}

				if shouldSucceed {
					mockApi.EXPECT().ListBundles(gomock.Any(), gomock.Any()).Return([]*models.Bundle{})
					response := handler.V2ListBundles(context.Background(), params)
					Expect(response).To(BeAssignableToTypeOf(restoperators.NewV2ListBundlesOK()))
				} else {
					response := handler.V2ListBundles(context.Background(), params)
					verifyApiError(response, http.StatusBadRequest)
				}
			},
			// x86_64 should be supported on all versions
			Entry("x86_64 on 4.9", "x86_64", "4.9.0", true),
			Entry("x86_64 on 4.10", "x86_64", "4.10.0", true),
			Entry("x86_64 on 4.11", "x86_64", "4.11.0", true),
			Entry("x86_64 on 4.12", "x86_64", "4.12.0", true),
			Entry("x86_64 on 4.13", "x86_64", "4.13.0", true),

			// ARM64 should be supported from 4.10 onwards
			Entry("arm64 on 4.9", "arm64", "4.9.0", false),
			Entry("arm64 on 4.10", "arm64", "4.10.0", true),
			Entry("arm64 on 4.11", "arm64", "4.11.0", true),
			Entry("arm64 on 4.12", "arm64", "4.12.0", true),
			Entry("arm64 on 4.13", "arm64", "4.13.0", true),

			// S390x should be supported from 4.12 onwards
			Entry("s390x on 4.11", "s390x", "4.11.0", false),
			Entry("s390x on 4.12", "s390x", "4.12.0", true),
			Entry("s390x on 4.13", "s390x", "4.13.0", true),

			// PPC64LE should be supported from 4.12 onwards
			Entry("ppc64le on 4.11", "ppc64le", "4.11.0", false),
			Entry("ppc64le on 4.12", "ppc64le", "4.12.0", true),
			Entry("ppc64le on 4.13", "ppc64le", "4.13.0", true),
		)
	})

	Context("Platform-specific validation", func() {
		DescribeTable("should validate platform support for different combinations",
			func(platformType string, openshiftVersion string, cpuArchitecture string, shouldSucceed bool) {
				params := restoperators.V2ListBundlesParams{
					OpenshiftVersion: &openshiftVersion,
					CPUArchitecture:  swag.String(cpuArchitecture),
					PlatformType:     swag.String(platformType),
				}

				if shouldSucceed {
					mockApi.EXPECT().ListBundles(gomock.Any(), gomock.Any()).Return([]*models.Bundle{})
					response := handler.V2ListBundles(context.Background(), params)
					Expect(response).To(BeAssignableToTypeOf(restoperators.NewV2ListBundlesOK()))
				} else {
					response := handler.V2ListBundles(context.Background(), params)
					verifyApiError(response, http.StatusBadRequest)
				}
			},
			// Baremetal should be supported on all combinations
			Entry("baremetal x86_64 4.13", "baremetal", "4.13.0", "x86_64", true),
			Entry("baremetal arm64 4.13", "baremetal", "4.13.0", "arm64", true),
			Entry("baremetal s390x 4.13", "baremetal", "4.13.0", "s390x", true),

			// None should be supported on all combinations
			Entry("none x86_64 4.13", "none", "4.13.0", "x86_64", true),
			Entry("none arm64 4.13", "none", "4.13.0", "arm64", true),

			// Nutanix should be supported from 4.11 onwards
			Entry("nutanix x86_64 4.10", "nutanix", "4.10.0", "x86_64", false),
			Entry("nutanix x86_64 4.11", "nutanix", "4.11.0", "x86_64", true),
			Entry("nutanix x86_64 4.13", "nutanix", "4.13.0", "x86_64", true),

			// VSphere should be supported on most combinations
			Entry("vsphere x86_64 4.13", "vsphere", "4.13.0", "x86_64", true),
			Entry("vsphere arm64 4.13", "vsphere", "4.13.0", "arm64", true),
		)
	})
})

func from(prototype models.MonitoredOperator) *models.MonitoredOperator {
	return &models.MonitoredOperator{
		Name:           prototype.Name,
		OperatorType:   prototype.OperatorType,
		TimeoutSeconds: prototype.TimeoutSeconds,
	}
}
