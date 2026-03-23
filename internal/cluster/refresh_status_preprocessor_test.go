package cluster

import (
	"context"
	"os"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
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
		mockUsageApi        *usage.MockAPI
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
		mockUsageApi = usage.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		preprocessor = newRefreshPreprocessor(
			logrus.New(),
			mockHostApi,
			mockOperatorManager,
			mockUsageApi,
			nil,
			DisabledClusterValidations{},
		)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	createCluster := func() {
		clusterID = strfmt.UUID(uuid.New().String())
		testCluster := hostutil.GenerateTestCluster(clusterID)
		cluster = &testCluster
		clusterStatus := "insufficient"
		cluster.Status = &clusterStatus
		cluster.LastInstallationPreparation = models.LastInstallationPreparation{
			Status: models.LastInstallationPreparationStatusFailed,
			Reason: "Test Preparation Failure",
		}
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

	mockNoChangeInOperatorDependencies := func() {
		mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ *common.Cluster, previousOperators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
				return previousOperators, nil
			},
		).AnyTimes()
	}

	mockAddedOperatorDependencies := func(addedOperators ...*models.MonitoredOperator) {
		mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ *common.Cluster, previousOperators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
				currentOperators := append(previousOperators, addedOperators...)
				return currentOperators, nil
			},
		).Times(1)
		for _, addedOperator := range addedOperators {
			mockUsageApi.EXPECT().Add(gomock.Any(), strings.ToUpper(addedOperator.Name), gomock.Any()).Times(1)
		}
		mockUsageApi.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
	}

	mockOperatorValidationsSuccess := func() {
		mockOperatorManager.EXPECT().ValidateCluster(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	}

	mockOperatorEnsureOperatorPrerequisiteSuccess := func() {
		mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
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
			Expect(err.Error()).To(ContainSubstring("unable to deserialize ignored cluster validations"))
		})

		It("Should allow permitted ignorable validations to be ignored", func() {
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()
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
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()
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
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()
			validationContext.cluster.IgnoredClusterValidations = "[\"all\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			for _, unskippableHostValidation := range cantBeIgnored {
				unskippableHostValidationSkipped, unskippableHostValidationPresent := conditions[unskippableHostValidation]
				Expect(unskippableHostValidationPresent).To(BeTrue(), unskippableHostValidation+" was not present as expected")
				Expect(unskippableHostValidationSkipped).To(BeFalse(), unskippableHostValidation+" was ignored when this should not be possible")
			}
		})
	})

	Context("Recalculate operator dependencies", func() {
		var validationContext *clusterPreprocessContext

		BeforeEach(func() {
			createCluster()
			validationContext = newClusterValidationContext(cluster, db)
		})

		AfterEach(func() {
			deleteCluster()
		})

		It("Adds new dependency", func() {
			// Prepare the operators API so that it will add a new operator dependency:
			mockAddedOperatorDependencies(
				&models.MonitoredOperator{
					Name: "myoperator",
				},
			)
			mockOperatorValidationsSuccess()
			mockOperatorEnsureOperatorPrerequisiteSuccess()

			_, _, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).ToNot(HaveOccurred())

			// Check that the new dependency has been added to the cluster in memory:
			var operator *models.MonitoredOperator
			for _, current := range cluster.MonitoredOperators {
				if current.Name == "myoperator" {
					operator = current
				}
			}
			Expect(operator).ToNot(BeNil())

			// Check that the new dependency has been saved to the database:
			err = db.Where(&models.MonitoredOperator{
				ClusterID: clusterID,
				Name:      "myoperator",
			}).First(&models.MonitoredOperator{}).Error
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Disabled Cluster Validations", func() {
		var validationContext *clusterPreprocessContext

		BeforeEach(func() {
			createCluster()
			validationContext = newClusterValidationContext(cluster, db)
		})

		AfterEach(func() {
			deleteCluster()
		})

		It("Should mark disabled validations with disabled status and pass them in the state machine", func() {
			disabledValidations := DisabledClusterValidations{
				string(IsDNSDomainDefined):    {},
				string(IsNtpServerConfigured): {},
			}
			preprocessor = newRefreshPreprocessor(
				logrus.New(),
				mockHostApi,
				mockOperatorManager,
				mockUsageApi,
				nil,
				disabledValidations,
			)
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()

			conditions, validationsOutput, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).ToNot(HaveOccurred())

			for _, disabledID := range []string{string(IsDNSDomainDefined), string(IsNtpServerConfigured)} {
				conditionState, conditionPresent := conditions[disabledID]
				Expect(conditionPresent).To(BeTrue(), disabledID+" was not present in conditions")
				Expect(conditionState).To(BeTrue(), disabledID+" was not forced to true")
			}

			for _, results := range validationsOutput {
				for _, v := range results {
					if v.ID == IsDNSDomainDefined || v.ID == IsNtpServerConfigured {
						Expect(v.Status).To(Equal(ValidationDisabled), string(v.ID)+" was not disabled")
						Expect(v.Message).To(Equal(validationDisabledByConfiguration))
					}
				}
			}
		})

		It("Should run non-disabled validations normally", func() {
			disabledValidations := DisabledClusterValidations{
				string(IsDNSDomainDefined): {},
			}
			preprocessor = newRefreshPreprocessor(
				logrus.New(),
				mockHostApi,
				mockOperatorManager,
				mockUsageApi,
				nil,
				disabledValidations,
			)
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()

			_, validationsOutput, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).ToNot(HaveOccurred())

			for _, results := range validationsOutput {
				for _, v := range results {
					if v.ID == IsNtpServerConfigured {
						Expect(v.Status).ToNot(Equal(ValidationDisabled), "non-disabled validation should not be disabled")
					}
				}
			}
		})

		It("Should work with empty disabled set", func() {
			preprocessor = newRefreshPreprocessor(
				logrus.New(),
				mockHostApi,
				mockOperatorManager,
				mockUsageApi,
				nil,
				DisabledClusterValidations{},
			)
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()

			_, validationsOutput, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).ToNot(HaveOccurred())

			for _, results := range validationsOutput {
				for _, v := range results {
					Expect(v.Status).ToNot(Equal(ValidationDisabled))
				}
			}
		})

		It("Per-cluster ignored validations override service-level disabled validations", func() {
			disabledValidations := DisabledClusterValidations{
				string(IsDNSDomainDefined):    {},
				string(IsNtpServerConfigured): {},
				string(IsMachineCidrDefined):  {},
			}
			preprocessor = newRefreshPreprocessor(
				logrus.New(),
				mockHostApi,
				mockOperatorManager,
				mockUsageApi,
				nil,
				disabledValidations,
			)
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()

			cluster.IgnoredClusterValidations = `["ntp-server-configured"]`
			validationContext = newClusterValidationContext(cluster, db)

			conditions, validationsOutput, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).ToNot(HaveOccurred())

			for _, results := range validationsOutput {
				for _, v := range results {
					Expect(v.Status).ToNot(Equal(ValidationDisabled),
						string(v.ID)+" should not be disabled when per-cluster override is set")
				}
			}

			ntpCondition, ntpPresent := conditions[string(IsNtpServerConfigured)]
			Expect(ntpPresent).To(BeTrue())
			Expect(ntpCondition).To(BeTrue(), "ignored validation should be forced to true in state machine")
		})

		It("Per-cluster empty ignored list overrides service-level disabled validations", func() {
			disabledValidations := DisabledClusterValidations{
				string(IsDNSDomainDefined):    {},
				string(IsNtpServerConfigured): {},
			}
			preprocessor = newRefreshPreprocessor(
				logrus.New(),
				mockHostApi,
				mockOperatorManager,
				mockUsageApi,
				nil,
				disabledValidations,
			)
			mockNoChangeInOperatorDependencies()
			mockOperatorValidationsSuccess()

			cluster.IgnoredClusterValidations = `[]`
			validationContext = newClusterValidationContext(cluster, db)

			_, validationsOutput, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).ToNot(HaveOccurred())

			for _, results := range validationsOutput {
				for _, v := range results {
					Expect(v.Status).ToNot(Equal(ValidationDisabled),
						string(v.ID)+" should not be disabled when per-cluster override is explicitly empty")
				}
			}
		})
	})
})

var _ = Describe("Disabled Cluster Validation Config", func() {
	const (
		disabledClusterValidationEnvironmentName = "DISABLED_CLUSTER_VALIDATIONS"
		twoValidationIDs                         = "validation-1,validation-2"
		malformedValue                           = "validation-1,,"
	)

	AfterEach(func() {
		os.Unsetenv(disabledClusterValidationEnvironmentName)
	})

	It("should have values when environment is defined", func() {
		Expect(os.Setenv(disabledClusterValidationEnvironmentName, twoValidationIDs)).NotTo(HaveOccurred())
		cfg := Config{}
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ToNot(HaveOccurred())
		Expect(cfg.DisabledClusterValidations.IsDisabled("validation-1")).To(BeTrue())
		Expect(cfg.DisabledClusterValidations.IsDisabled("validation-2")).To(BeTrue())
	})

	It("should trim whitespace around environment values", func() {
		Expect(os.Setenv(disabledClusterValidationEnvironmentName, "validation-1, validation-2")).NotTo(HaveOccurred())
		cfg := Config{}
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ToNot(HaveOccurred())
		Expect(cfg.DisabledClusterValidations.IsDisabled("validation-1")).To(BeTrue())
		Expect(cfg.DisabledClusterValidations.IsDisabled("validation-2")).To(BeTrue())
	})

	It("should have no values when environment is not defined", func() {
		cfg := Config{}
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ToNot(HaveOccurred())
		Expect(cfg.DisabledClusterValidations.IsDisabled("validation-1")).To(BeFalse())
	})

	It("should error when environment value is malformed", func() {
		Expect(os.Setenv(disabledClusterValidationEnvironmentName, malformedValue)).NotTo(HaveOccurred())
		cfg := Config{}
		err := envconfig.Process(common.EnvConfigPrefix, &cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("empty cluster validation ID found in"))
	})
})
