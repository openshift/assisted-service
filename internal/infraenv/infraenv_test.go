package infraenv

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

var _ = Describe("DeregisterInfraEnv", func() {
	var (
		ctrl         *gomock.Controller
		ctx          = context.Background()
		db           *gorm.DB
		state        API
		infraEnv     common.InfraEnv
		dbName       string
		mockS3Client *s3wrapper.MockAPI
	)

	registerInfraEnv := func() common.InfraEnv {
		id := strfmt.UUID(uuid.New().String())
		ie := common.InfraEnv{InfraEnv: models.InfraEnv{
			ID: &id,
		}}
		Expect(db.Create(&ie).Error).ShouldNot(HaveOccurred())
		return ie
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(common.GetTestLog(), db, mockS3Client)
		infraEnv = registerInfraEnv()
	})

	It("Deregister InfraEnv - Success - no image", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
		Expect(state.DeregisterInfraEnv(ctx, *infraEnv.ID)).ShouldNot(HaveOccurred())
		_, err := common.GetInfraEnvFromDB(db, *infraEnv.ID)
		Expect(err).Should(HaveOccurred())
		Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
	})

	It("Deregister InfraEnv - Success - image to be deleted", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		Expect(state.DeregisterInfraEnv(ctx, *infraEnv.ID)).ShouldNot(HaveOccurred())
		_, err := common.GetInfraEnvFromDB(db, *infraEnv.ID)
		Expect(err).Should(HaveOccurred())
		Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
	})

	It("Deregister InfraEnv - Failure to check if image exists", func() {
		expectedError := errors.New("error")
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, expectedError).Times(1)
		Expect(state.DeregisterInfraEnv(ctx, *infraEnv.ID)).To(Equal(expectedError))
		_, err := common.GetInfraEnvFromDB(db, *infraEnv.ID)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Deregister InfraEnv - Failure to delete image", func() {
		expectedError := errors.New("error")
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(false, expectedError).Times(1)
		Expect(state.DeregisterInfraEnv(ctx, *infraEnv.ID)).To(Equal(expectedError))
		_, err := common.GetInfraEnvFromDB(db, *infraEnv.ID)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})
