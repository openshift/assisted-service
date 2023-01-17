package cluster

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var _ = Describe("Cluster Refresh Status Preprocessor", func() {

	var (
		preprocessor        *refreshPreprocessor
		ctrl                *gomock.Controller
		mockHostApi         *host.MockAPI
		mockOperatorManager *operators.MockAPI
		db                  *gorm.DB
		dbName              string
		cluster             *common.Cluster
		clusterID           strfmt.UUID
		ctx                 context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockHostApi = host.NewMockAPI(ctrl)
		mockOperatorManager = operators.NewMockAPI(ctrl)
		mockOperatorManager.EXPECT().ValidateCluster(ctx, gomock.Any())
		db, dbName = common.PrepareTestDB()
		preprocessor = newRefreshPreprocessor(
			logrus.New(),
			mockHostApi,
			mockOperatorManager,
		)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	createCluster := func() {
		clusterID = strfmt.UUID(uuid.New().String())
		testCluster := hostutil.GenerateTestCluster(clusterID)
		cluster = &testCluster
		clusterStatus := "insufficient"
		cluster.Status = &clusterStatus
		cluster.InstallationPreparationCompletionStatus = common.InstallationPreparationFailed
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	}

	deleteCluster := func() {
		Expect(db.Delete(&common.Cluster{}, clusterID).Error).NotTo(HaveOccurred())
	}

	mockFailingValidator := func(context *clusterPreprocessContext) (ValidationStatus, string) {
		return ValidationFailure, "Mock of a failed validation"
	}

	mockFailingCondition := func(context *clusterPreprocessContext) bool {
		return false
	}

	mockFailAllValidations := func() {
		for i, validation := range preprocessor.validations {
			validation.condition = mockFailingValidator
			preprocessor.validations[i] = validation
		}

		for i, condition := range preprocessor.conditions {
			condition.fn = mockFailingCondition
			preprocessor.conditions[i] = condition
		}
	}

	Context("Skipping Validations", func() {

		cantBeIgnored := common.NonIgnorableClusterValidations

		var (
			validationContext *clusterPreprocessContext
		)

		var conditionIsValidation = func(r *refreshPreprocessor, condition string) bool {
			for _, validation := range r.validations {
				if condition == validation.id.String() {
					return true
				}
			}
			return false
		}

		BeforeEach(func() {
			createCluster()
			mockFailAllValidations()
			validationContext = newClusterValidationContext(cluster, db)
		})

		AfterEach(func() {
			deleteCluster()
		})

		It("Should not ignore any validations if IgnoredClusterValidations is empty or invalid", func() {
			validationContext.cluster.IgnoredClusterValidations = "bad JSON"
			_, _, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unable to deserialize ignored cluster validations"))
		})

		It("Should allow permitted ignorable validations to be ignored", func() {
			validationContext.cluster.IgnoredClusterValidations = "[\"network-type-valid\", \"ingress-vips-valid\", \"ingress-vips-defined\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			Expect(conditions).ToNot(BeEmpty())
			for _, conditionID := range []string{"network-type-valid", "ingress-vips-valid"} {
				conditionState, conditionPresent := conditions[conditionID]
				Expect(conditionPresent).To(BeTrue(), conditionID+" was not present as expected")
				Expect(conditionState).To(BeTrue(), conditionID+" was not ignored as expected")
			}
			conditionState, conditionPresent := conditions["ingress-vips-defined"]
			Expect(conditionPresent).To(BeTrue(), "ingress-vips-defined was not present as expected")
			Expect(conditionState).To(BeFalse(), "ingress-vips-defined was ignored when this should not be permitted")
		})

		It("Should allow all permitted ignorable validations to be ignored", func() {
			validationContext.cluster.IgnoredClusterValidations = "[\"all\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			Expect(conditions).ToNot(BeEmpty())
			for condition, wasIgnored := range conditions {
				if funk.ContainsString(cantBeIgnored, condition) || !conditionIsValidation(preprocessor, condition) {
					continue
				}
				Expect(wasIgnored).To(BeTrue(), condition+" was not ignored as expected")
			}
		})

		It("Should never allow a specific mandatory validation to be ignored", func() {
			validationContext.cluster.IgnoredClusterValidations = "[\"all\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			for _, unskippableHostValidation := range cantBeIgnored {
				unskippableHostValidationSkipped, unskippableHostValidationPresent := conditions[unskippableHostValidation]
				Expect(unskippableHostValidationPresent).To(BeTrue(), unskippableHostValidation+" was not present as expected")
				Expect(unskippableHostValidationSkipped).To(BeFalse(), unskippableHostValidation+" was ignored when this should not be possible")
			}
		})
	})
})
