package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	amgmtv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

var _ = Describe("ValidateAccessToMultiarch", func() {
	var (
		ctrl         *gomock.Controller
		db           *gorm.DB
		dbName       string
		mockOcmAuthz *ocm.MockOCMAuthorization
		ctx          context.Context
		orgID        = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
		userName     = "test_user_1"
		authzHandler Authorizer
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()

		cfg := GetConfigRHSSO()
		cfg.EnableOrgBasedFeatureGates = true

		mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
		mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
		mockAccountsMgmt := ocm.NewMockOCMAccountsMgmt(ctrl)

		payload := &ocm.AuthPayload{
			Username:     userName,
			Organization: orgID,
			Role:         ocm.UserRole,
		}
		ctx = context.WithValue(context.Background(), restapi.AuthKey, payload)

		mockAccountsMgmt.EXPECT().CreateSubscription(ctx, gomock.Any(), gomock.Any()).Return(&amgmtv1.Subscription{}, nil)
		authzHandler = NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("succeeds with multiarch capability", func() {
		mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(true, nil).Times(1)

		err := ValidateAccessToMultiarch(ctx, authzHandler)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("fails without multiarch capability", func() {
		mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

		err := ValidateAccessToMultiarch(ctx, authzHandler)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(ContainSubstring("multiarch clusters are not available"))
	})
	It("fails with internal error", func() {
		mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(false, errors.New("some internal error")).Times(1)

		err := ValidateAccessToMultiarch(ctx, authzHandler)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(ContainSubstring(fmt.Sprintf("error getting user %s capability", ocm.MultiarchCapabilityName)))
	})
})
