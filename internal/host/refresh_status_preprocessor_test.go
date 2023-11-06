package host

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var _ = Describe("Cluster Refresh Status Preprocessor", func() {

	var (
		preprocessor          *refreshPreprocessor
		ctrl                  *gomock.Controller
		mockHardwareValidator *hardware.MockValidator
		mockOperatorManager   *operators.MockAPI
		mockProviderRegistry  *registry.MockProviderRegistry
		mockS3WrapperAPI      *s3wrapper.MockAPI
		mockVersions          *versions.MockHandler
		db                    *gorm.DB
		dbName                string
		cluster               *common.Cluster
		clusterID             strfmt.UUID
		host                  *models.Host
		hostID                strfmt.UUID
		infraEnv              *common.InfraEnv
		infraEnvID            strfmt.UUID
		inventoryCache        InventoryCache
		ctx                   context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		inventoryCache = make(InventoryCache)
		mockHardwareValidator = hardware.NewMockValidator(ctrl)
		mockHardwareValidator.EXPECT().GetClusterHostRequirements(ctx, gomock.Any(), gomock.Any())
		mockHardwareValidator.EXPECT().GetPreflightHardwareRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(&models.PreflightHardwareRequirements{
			Ocp: &models.HostTypeHardwareRequirementsWrapper{
				Worker: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
				Master: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
			},
		}, nil)
		mockOperatorManager = operators.NewMockAPI(ctrl)
		mockOperatorManager.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any())
		mockProviderRegistry = registry.NewMockProviderRegistry(ctrl)
		mockS3WrapperAPI = s3wrapper.NewMockAPI(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockOperatorManager.EXPECT().ValidateCluster(ctx, gomock.Any())
		db, dbName = common.PrepareTestDB()
		validatorConfig := &hardware.ValidatorCfg{}
		disabledHostValidations := make(map[string]struct{})
		preprocessor = newRefreshPreprocessor(
			logrus.New(),
			validatorConfig,
			mockHardwareValidator,
			mockOperatorManager,
			disabledHostValidations,
			mockProviderRegistry,
			mockVersions,
		)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	createHost := func() models.Host {
		infraEnvID = strfmt.UUID(uuid.New().String())
		infraEnv = hostutil.GenerateTestInfraEnv(infraEnvID)
		Expect(db.Save(infraEnv).Error).ToNot(HaveOccurred())
		hostID = strfmt.UUID(uuid.New().String())
		newHost := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvID,
			ClusterID:  &clusterID,
			Kind:       swag.String(models.HostKindHost),
			Status:     swag.String(models.HostStatusInsufficient),
			Role:       models.HostRoleAutoAssign,
		}
		Expect(db.Create(&newHost).Error).ShouldNot(HaveOccurred())
		host = &newHost
		hostProgressInfo := models.HostProgressInfo{}
		host.Progress = &hostProgressInfo
		return newHost
	}

	createCluster := func() {
		clusterID = strfmt.UUID(uuid.New().String())
		testCluster := hostutil.GenerateTestCluster(clusterID)
		cluster = &testCluster
		clusterStatus := "insufficient"
		cluster.Status = &clusterStatus
		hostToAdd := createHost()
		cluster.Hosts = []*models.Host{&hostToAdd}
		cluster.LastInstallationPreparation = models.LastInstallationPreparation{
			Status: models.LastInstallationPreparationStatusFailed,
			Reason: "",
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	}

	deleteCluster := func() {
		Expect(db.Delete(&common.Cluster{}, clusterID).Error).NotTo(HaveOccurred())
	}

	mockFailingValidator := func(context *validationContext) (ValidationStatus, string) {
		return ValidationFailure, "Mock of a failed validation"
	}

	mockFailingCondition := func(context *validationContext) bool {
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

		cantBeIgnored := common.NonIgnorableHostValidations

		var (
			validationContext *validationContext
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
			var err error
			validationContext, err = newValidationContext(ctx, host, cluster, infraEnv, db, inventoryCache, mockHardwareValidator, false, mockS3WrapperAPI)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			deleteCluster()
		})

		It("Should raise an error if IgnoredHostValidations is empty or invalid", func() {
			validationContext.cluster.IgnoredHostValidations = "bad JSON"
			_, _, err := preprocessor.preprocess(ctx, validationContext)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unable to deserialize ignored host validations"))
		})

		It("Should allow specific ignorable validations to be ignored", func() {
			validationContext.cluster.IgnoredHostValidations = "[\"has-memory-for-role\", \"has-cpu-cores-for-role\", \"has-inventory\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			Expect(conditions).ToNot(BeEmpty())
			for _, conditionID := range []string{"has-memory-for-role", "has-cpu-cores-for-role"} {
				conditionState, conditionPresent := conditions[conditionID]
				Expect(conditionPresent).To(BeTrue(), conditionID+" was not present as expected")
				Expect(conditionState).To(BeTrue(), conditionID+" was not ignored as expected")
			}
			conditionState, conditionPresent := conditions["has-inventory"]
			Expect(conditionPresent).To(BeTrue(), "has-inventory was not present as expected")
			Expect(conditionState).To(BeFalse(), "has-inventory was ignored when this should not be permitted")
		})

		It("Should allow all non-skippable validations to be ignored", func() {
			validationContext.cluster.IgnoredHostValidations = "[\"all\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			Expect(conditions).ToNot(BeEmpty())
			for condition, wasIgnored := range conditions {
				// We need to make sure that this condition represents a validation
				if funk.ContainsString(cantBeIgnored, condition) || !conditionIsValidation(preprocessor, condition) {
					continue
				}
				Expect(wasIgnored).To(BeTrue(), condition+" was not ignored as expected")
			}
		})

		It("Should never allow a specific mandatory validation to be ignored", func() {
			validationContext.cluster.IgnoredHostValidations = "[\"all\"]"
			conditions, _, _ := preprocessor.preprocess(ctx, validationContext)
			for _, unskippableHostValidation := range cantBeIgnored {
				unskippableHostValidationSkipped, unskippableHostValidationPresent := conditions[unskippableHostValidation]
				Expect(unskippableHostValidationPresent).To(BeTrue(), unskippableHostValidation+" was not present as expected")
				Expect(unskippableHostValidationSkipped).To(BeFalse(), unskippableHostValidation+" was ignored when this should not be possible")
			}
		})
	})
})
